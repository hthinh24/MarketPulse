package main

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/domain"
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

	exchangeConfigs := loadExchangeConfigs()

	publishChan := make(chan *domain.OrderBookSnapshot, 10000)
	numOfPublishChannel := redisConfig.PoolSize
	redisPublisher := publisher.NewOrderBookPublisher(redisClient)

	wg := sync.WaitGroup{}
	for _, cfg := range exchangeConfigs {
		wg.Add(1)

		go func(config *config.ExchangeConfig) {
			defer wg.Done()

			adapter := delivery.NewExchangeAdapter(config)
			if err := adapter.Start(ctx, publishChan); err != nil {
				log.Printf("Adapter %s failed to start: %v", config.Name, err)
			}
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

func loadExchangeConfigs() []*config.ExchangeConfig {
	return []*config.ExchangeConfig{
		{
			Name:                "BINANCE",
			SymbolDiscoveryUrl:  "https://api.binance.com/api/v3/exchangeInfo",
			SnapshotUrl:         "https://api.binance.com/api/v3/depth",
			StreamUrl:           "wss://stream.binance.com:9443/stream",
			StreamBufferSize:    5000,
			DeltaQueueSize:      1000,
			RetryMaxAttempts:    10,
			RetryInitialDelayMs: 100,
			RetryMaxDelayMs:     5000,
			BTreeDegree:         32,
			SnapshotQuantity:    20,
		},
		{
			Name:                "BYBIT",
			SymbolDiscoveryUrl:  "https://api.bybit.com/v5/market/instruments-info?category=spot&status=Trading",
			SnapshotUrl:         "https://api.bybit.com/v5/market/orderbook",
			StreamUrl:           "wss://stream.bybit.com/v5/public/spot",
			StreamBufferSize:    5000,
			DeltaQueueSize:      1000,
			RetryMaxAttempts:    10,
			RetryInitialDelayMs: 100,
			RetryMaxDelayMs:     5000,
			BTreeDegree:         32,
			SnapshotQuantity:    20,
		},
		{
			Name:                "OKX",
			SymbolDiscoveryUrl:  "https://www.okx.com/api/v5/public/instruments?instType=SPOT",
			StreamUrl:           "wss://ws.okx.com:8443/ws/v5/public",
			StreamBufferSize:    5000,
			DeltaQueueSize:      1000,
			RetryMaxAttempts:    8,
			RetryInitialDelayMs: 200,
			RetryMaxDelayMs:     10000,
			BTreeDegree:         32,
			SnapshotQuantity:    20,
		},
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
