package main

import (
	"MarketPulse/internal/server/config"
	"MarketPulse/internal/server/controller"
	"MarketPulse/internal/server/infrastructure"
	"MarketPulse/internal/server/infrastructure/observation"
	repository "MarketPulse/internal/server/infrastructure/repository/postgres"
	cache "MarketPulse/internal/server/infrastructure/repository/redis"
	"MarketPulse/internal/server/service"
	"MarketPulse/internal/telemetry"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"net/http"
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

	log, err := logger.New("aggregator", cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	db := initDB(cfg.DB)
	rdb := initRedisDB(cfg.Redis)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-api-server"

	shutdown, err := initTelemetry(ctx, serviceName, cfg.OTLP.Endpoint)
	if err != nil {
		log.Error(ctx, "failed to initialize telemetry", err)
		os.Exit(1)
	}
	defer shutdown(ctx)

	defer func() {
		if err := rdb.Close(); err != nil {
			log.Info(ctx, "Redis client closed")
		}
	}()

	candleRepository := repository.NewCandleRepository(db)
	candleCache := cache.NewCandleCache(log, rdb)
	candleQueryService := service.NewCandleQueryService(log, candleCache, candleRepository)
	candleController := controller.NewCandleController(candleQueryService)

	InitCacheWarmup(context.Background(), log, candleRepository, candleCache)

	intervalTime := 1 * time.Hour
	symbolRankingUpdater := infrastructure.NewSymbolRankingUpdater(log, candleRepository, candleCache, intervalTime)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go symbolRankingUpdater.Start(ctx, &wg)

	r := gin.Default()
	r.Use(cors.Default())
	r.Use(otelgin.Middleware(serviceName))

	v1 := r.Group("/api/v1")
	candleController.RegisterRoutes(v1)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		srv.ListenAndServe()
	}()

	<-ctx.Done()

	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	doneChan := make(chan struct{})
	go func() {
		srv.Shutdown(timeoutContext)
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		log.Info(ctx, "shutdown signal received, waiting for ongoing operations to finish")
	case <-timeoutContext.Done():
		log.Info(ctx, "timeout reached, forcing shutdown")
	}
}

func initDB(dbCfg config.DBConfig) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dbCfg.DSN()), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	return db
}

func initRedisDB(redisCfg config.RedisCacheConfig) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisCfg.Addr,
		Password: redisCfg.Password,
		DB:       redisCfg.DB,
		PoolSize: redisCfg.PoolSize,
	})
	return rdb
}

func InitCacheWarmup(ctx context.Context, log *logger.Logger, repository service.ICandleRepository, cache service.ICandleCache) {
	log.Info(ctx, "Warm up cache, fetching available symbols from repository")

	exchanges, err := repository.GetExchangeQuoteVolumeScores()
	if err != nil {
		log.Error(ctx, "Error fetching active exchanges from repository", err)
		return
	}

	if err := cache.UpdateExchangeRanking(ctx, exchanges, 24*time.Hour); err != nil {
		log.Error(ctx, "Error updating exchange ranking", err)
		return
	}

	for _, exchange := range exchanges {
		exchangeCode := exchange.Exchange
		symbolScores, err := repository.GetSymbolDayVolumeScores(exchangeCode)
		if err != nil {
			log.Error(ctx, "Error fetching active symbols from repository", err)
			return
		}

		if len(symbolScores) == 0 {
			log.Info(ctx, "No active symbols found", logger.String("exchange", exchangeCode))
			continue
		}

		expiredTime := 1 * time.Hour
		err = cache.UpdateSymbolRanking(ctx, exchangeCode, symbolScores, expiredTime)
		if err != nil {
			log.Error(ctx, "Error updating symbol ranking", err)
		}
	}

	log.Info(ctx, "Cache warm up completed")
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
