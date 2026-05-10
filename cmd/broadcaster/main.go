package main

import (
	"MarketPulse/internal/broadcaster"
	broadcasterConfig "MarketPulse/internal/broadcaster/config"
	"MarketPulse/internal/broadcaster/controller/ws"
	"MarketPulse/internal/broadcaster/infrastructure/observation"
	"MarketPulse/internal/broadcaster/service"
	"MarketPulse/internal/telemetry"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
		log.Println("pprof: http://localhost:6063/debug/pprof/")
		log.Println(http.ListenAndServe("localhost:6063", nil))
	}()

	_ = godotenv.Load()

	cfg, err := broadcasterConfig.LoadAppConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-broadcaster"

	shutdown := telemetry.InitProvider(serviceName, cfg.OTLP.Endpoint)
	defer shutdown(ctx)

	observation.BroadcastMessagesTotal.Add(ctx, 1000, metric.WithAttributes(
		attribute.String("Test", "test"),
	))

	rdb := initRedisDB(cfg.Redis)
	defer rdb.Close()

	log.Print("Connected to Redis successfully!")

	broadcasterServiceConfig := broadcasterConfig.NewBroadcasterConfig()
	broadcasterService := service.NewBroadcasterServiceWithConfig(broadcasterServiceConfig)

	channels := []broadcasterConfig.ChannelMetadata{
		{
			ChannelPattern: "marketpulse:candles:*",
			ChannelPrefix:  "marketpulse:",
		},
		{
			ChannelPattern: "marketpulse:orderbook:*",
			ChannelPrefix:  "marketpulse:",
		},
	}

	wg := sync.WaitGroup{}

	// Start Dispatcher
	wg.Add(1)
	go func() {
		defer wg.Done()
		broadcasterService.Start(ctx)
	}()

	// Start Redis subscribers
	for _, ch := range channels {
		wg.Add(1)
		go func(chMetadata broadcasterConfig.ChannelMetadata) {
			defer wg.Done()
			log.Print("Starting Redis subscriber...")
			broadcaster.StartRedisSubscriber(ctx, rdb, broadcasterService, chMetadata.ChannelPattern, chMetadata.ChannelPrefix)
		}(ch)
	}

	log.Printf("Starting WebSocket server on :%s...", cfg.Port)

	wsController := ws.NewWSController(broadcasterService)

	http.HandleFunc("/ws", wsController.HandleConnection)

	server := &http.Server{
		Addr: ":" + cfg.Port,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start WebSocket server: %v", err)
		}
	}()

	<-ctx.Done()

	log.Println("Shutdown signal received, waiting for ongoing operations to finish...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		log.Println("All services shutdown gracefully")
	case <-shutdownCtx.Done():
		log.Println("Timeout reached during graceful shutdown")
	}
}

func initRedisDB(redisCfg broadcasterConfig.RedisPubSubConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     redisCfg.Addr,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
		PoolSize: redisCfg.PoolSize,
	})
}
