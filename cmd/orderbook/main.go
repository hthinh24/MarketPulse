package main

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery"
	"MarketPulse/internal/orderbook/infrastructure/publisher"
	"MarketPulse/internal/telemetry"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
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

	_ = godotenv.Load()

	cfg, err := config.LoadAppConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-orderbook"

	shutdown := telemetry.InitProvider(serviceName, cfg.OTLP.Endpoint)
	defer shutdown(ctx)

	redisClient := initRedisDB(cfg.Redis)
	defer redisClient.Close()

	exchangeConfigs := loadExchangeConfigs(cfg)

	publishChan := make(chan *domain.OrderBookSnapshot, 10000)
	numOfPublishChannel := cfg.Redis.PoolSize
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

func loadExchangeConfigs(cfg *config.AppConfig) []*config.ExchangeConfig {
	return []*config.ExchangeConfig{
		{
			Name:                   "BINANCE",
			SymbolDiscoveryUrl:     cfg.Binance.DiscoveryURL,
			SnapshotUrl:            cfg.Binance.SnapshotURL,
			StreamUrl:              cfg.Binance.StreamURL,
			StreamBufferSize:       1000,
			SymbolStreamBufferSize: 100,
			DeltaQueueSize:         50,
			RetryMaxAttempts:       10,
			RetryInitialDelayMs:    100,
			RetryMaxDelayMs:        5000,
			BTreeDegree:            32,
			SnapshotQuantity:       20,
		},
		{
			Name:                   "BYBIT",
			SymbolDiscoveryUrl:     cfg.Bybit.DiscoveryURL,
			SnapshotUrl:            cfg.Bybit.SnapshotURL,
			StreamUrl:              cfg.Bybit.StreamURL,
			StreamBufferSize:       1000,
			SymbolStreamBufferSize: 100,
			DeltaQueueSize:         500,
			RetryMaxAttempts:       10,
			RetryInitialDelayMs:    100,
			RetryMaxDelayMs:        5000,
			BTreeDegree:            32,
			SnapshotQuantity:       20,
		},
		{
			Name:                   "OKX",
			SymbolDiscoveryUrl:     cfg.OKX.DiscoveryURL,
			SnapshotUrl:            cfg.OKX.SnapshotURL,
			StreamUrl:              cfg.OKX.StreamURL,
			StreamBufferSize:       1000,
			SymbolStreamBufferSize: 100,
			DeltaQueueSize:         100,
			RetryMaxAttempts:       8,
			RetryInitialDelayMs:    200,
			RetryMaxDelayMs:        10000,
			BTreeDegree:            32,
			SnapshotQuantity:       20,
		},
	}
}

func initRedisDB(redisCfg config.RedisPubSubConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     redisCfg.Addr,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
		PoolSize: redisCfg.PoolSize,
	})
}
