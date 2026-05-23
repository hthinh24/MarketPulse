package main

import (
	"MarketPulse/internal/broadcaster/config"
	"MarketPulse/internal/broadcaster/controller/ws"
	"MarketPulse/internal/broadcaster/infrastructure/observation"
	"MarketPulse/internal/broadcaster/infrastructure/subscriber"
	"MarketPulse/internal/broadcaster/service"
	"MarketPulse/internal/telemetry"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.LoadAppConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log, err := logger.New("broadcaster", cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	go func() {
		log.Info(context.Background(), "pprof server starting", logger.String("addr", "http://localhost:6063/debug/pprof/"))
		log.Error(context.Background(), "pprof server error", fmt.Errorf("%v", http.ListenAndServe("localhost:6063", nil)))
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-broadcaster"

	shutdown, err := initTelemetry(ctx, serviceName, cfg.OTLP.Endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init telemetry: %v\n", err)
		os.Exit(1)
	}
	defer shutdown(ctx)

	observation.BroadcastMessagesTotal.Add(ctx, 1000, metric.WithAttributes(
		attribute.String("Test", "test"),
	))

	rdb := initRedisDB(cfg.Redis)
	defer rdb.Close()

	log.Info(context.Background(), "Connected to Redis successfully!")

	broadcasterServiceConfig := config.NewBroadcasterConfig()
	broadcasterService := service.NewBroadcasterService(log, broadcasterServiceConfig)

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
			log.Info(context.Background(), "Starting Redis subscriber", logger.String("pattern", chMetadata.ChannelPattern))
			subscriber.StartRedisSubscriber(ctx, log, rdb, broadcasterService, chMetadata.ChannelPattern, chMetadata.ChannelPrefix)
		}(ch)
	}

	log.Info(context.Background(), "Starting Websocket server", logger.String("port", cfg.Port))

	wsController := ws.NewWSController(log, broadcasterService)

	http.HandleFunc("/ws", wsController.HandleConnection)

	server := &http.Server{
		Addr: ":" + cfg.Port,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(context.Background(), "Failed to listen and serve", err)
		}
	}()

	<-ctx.Done()

	log.Info(context.Background(), "Shutdown signal received, waiting for ongoing operations to finish...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error(context.Background(), "Failed to shutdown server", err)
	}

	doneChan := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		//log.Println("All services shutdown gracefully")
		log.Info(context.Background(), "All services shutdown gracefully")
	case <-shutdownCtx.Done():
		log.Info(context.Background(), "Timeout reached during graceful shutdown")
	}
}

func initTelemetry(ctx context.Context, serviceName string, otlpEndpoint string) (func(context.Context) error, error) {
	shutdownMetrics, err := telemetry.InitMetricsProvider(ctx, serviceName, otlpEndpoint)
	if err != nil {
		return nil, fmt.Errorf("metrics provider: %w", err)
	}

	shutdownTracing, err := telemetry.InitTracingProvider(ctx, serviceName, otlpEndpoint)
	if err != nil {
		return nil, fmt.Errorf("tracing provider: %w", err)
	}
	observation.InitTracer(serviceName)

	return func(ctx context.Context) error {
		if err := shutdownTracing(ctx); err != nil {
			return err
		}
		return shutdownMetrics(ctx)
	}, nil
}

func initRedisDB(redisCfg config.RedisPubSubConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     redisCfg.Addr,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
		PoolSize: redisCfg.PoolSize,
	})
}
