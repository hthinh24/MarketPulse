package main

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/delivery"
	"MarketPulse/internal/orderbook/infrastructure/publisher"
	"MarketPulse/internal/telemetry"
	"context"
	"github.com/go-redis/redis/v8"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	go func() {
		log.Println("pprof: http://localhost:6060/debug/pprof/")
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-orderbook"
	otlpEndpoint := "localhost:4317"

	shutdown := telemetry.InitProvider(serviceName, otlpEndpoint)
	defer shutdown(ctx)

	redisConfig := &config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 8,
	}

	redisClient := initRedisDB(redisConfig)
	defer redisClient.Close()

	// Code here
	exchangeConfigs := []*config.ExchangeConfig{
		{
			Name:               "BINANCE",
			SymbolDiscoveryUrl: "https://api.binance.com/api/v3/exchangeInfo",
			SnapshotUrl:        "https://api.binance.com/api/v3/depth",
			StreamUrl:          "wss://stream.binance.com:9443/stream",
			StreamBufferSize:   50000,
			DeltaQueueSize:     1000,
		},
		//{Name: "OKX", SnapshotUrl: "wss://ws.okx.com:8443/ws/v5/public?brokerId=9999", BufferSize: 5000},
		//{Name: "Bybit", SnapshotUrl: "wss://stream.bybit.com/realtime_public", BufferSize: 5000},
	}

	publishChan := make(chan *event.OrderBookSnapshot, 10000)
	numOfPublishChannel := redisConfig.PoolSize
	redisPublisher := publisher.NewOrderBookPublisher(redisClient)

	wg := sync.WaitGroup{}
	for _, cfg := range exchangeConfigs {
		wg.Add(1)

		go func(config *config.ExchangeConfig) {
			defer wg.Done()

			exchangeIngestor := delivery.NewExchangeIngestor(cfg)
			exchangeIngestor.Start(ctx, publishChan)
		}(cfg)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		redisPublisher.Start(ctx, publishChan, numOfPublishChannel)
	}()

	// -------------------- Graceful Shutdown Handling -------------------
	<-ctx.Done()

	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	doneChan := make(chan struct{})
	go func() {

		close(doneChan)
	}()

	select {
	case <-doneChan:
		log.Println("Shutdown signal received, waiting for ongoing operations to finish...")
	case <-timeoutContext.Done():
		log.Println("Timeout reached, forcing shutdown...")
	}
}

func initRedisDB(config *config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,
		PoolSize: config.PoolSize,
	})
}
