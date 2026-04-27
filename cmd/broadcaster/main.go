package main

import (
	"MarketPulse/internal/broadcaster"
	"MarketPulse/internal/broadcaster/config"
	"MarketPulse/internal/broadcaster/controller/ws"
	"MarketPulse/internal/broadcaster/infrastructure/observation"
	"MarketPulse/internal/broadcaster/service"
	"MarketPulse/internal/telemetry"
	"context"
	"github.com/go-redis/redis/v8"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-broadcaster"
	otlpEndpoint := "localhost:4317"

	shutdown := telemetry.InitProvider(serviceName, otlpEndpoint)
	defer shutdown(ctx)

	observation.BroadcastMessagesTotal.Add(ctx, 1000, metric.WithAttributes(
		attribute.String("Test", "test"),
	))

	rdb := initRedisDB()
	defer rdb.Close()

	log.Print("Connected to Redis successfully!")

	broadcasterConfig := config.NewBroadcasterConfig()
	broadcasterService := service.NewBroadcasterServiceWithConfig(broadcasterConfig)

	channels := []config.ChannelMetadata{
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
		go func(chMetadata config.ChannelMetadata) {
			defer wg.Done()
			log.Print("Starting Redis subscriber...")
			broadcaster.StartRedisSubscriber(ctx, rdb, broadcasterService, chMetadata.ChannelPattern, chMetadata.ChannelPrefix)
		}(ch)
	}

	log.Print("Starting WebSocket server on :8081...")

	wsController := ws.NewWSController(broadcasterService)

	http.HandleFunc("/ws", wsController.HandleConnection)

	server := &http.Server{
		Addr: ":8081",
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

func initRedisDB() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
}
