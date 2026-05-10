package main

import (
	"MarketPulse/internal/server/config"
	"MarketPulse/internal/server/controller"
	"MarketPulse/internal/server/infrastructure"
	repository "MarketPulse/internal/server/infrastructure/repository/postgres"
	cache "MarketPulse/internal/server/infrastructure/repository/redis"
	"MarketPulse/internal/server/service"
	"context"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"net/http"
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

	db := initDB(cfg.DB)
	rdb := initRedisDB(cfg.Redis)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	defer func() {
		if err := rdb.Close(); err != nil {
			log.Printf("Error closing Redis client: %v", err)
		}
	}()

	candleRepository := repository.NewCandleRepository(db)
	candleCache := cache.NewCandleCache(rdb)
	candleQueryService := service.NewCandleQueryService(candleCache, candleRepository)
	candleController := controller.NewCandleController(candleQueryService)

	InitCacheWarmup(context.Background(), candleRepository, candleCache)

	intervalTime := 1 * time.Hour
	symbolRankingUpdater := infrastructure.NewSymbolRankingUpdater(candleRepository, candleCache, intervalTime)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go symbolRankingUpdater.Start(ctx, &wg)

	r := gin.Default()
	r.Use(cors.Default())

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
		log.Println("Shutdown signal received, waiting for ongoing operations to finish...")
	case <-timeoutContext.Done():
		log.Println("Timeout reached, forcing shutdown...")
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

func InitCacheWarmup(ctx context.Context, repository service.ICandleRepository, cache service.ICandleCache) {
	log.Println("Warm up cache, fetching available symbols from repository")

	exchanges, err := repository.GetExchangeQuoteVolumeScores()
	if err != nil {
		log.Printf("Error fetching active exchanges from repository: %v\n", err)
		return
	}

	if err := cache.UpdateExchangeRanking(ctx, exchanges, 24*time.Hour); err != nil {
		log.Printf("Error setting active exchanges into cache: %v\n", err)
		return
	}

	for _, exchange := range exchanges {
		exchangeCode := exchange.Exchange
		symbolScores, err := repository.GetSymbolDayVolumeScores(exchangeCode)
		if err != nil {
			log.Printf("Error fetching available symbols from repository: %v\n", err)
			return
		}

		if len(symbolScores) == 0 {
			log.Printf("No available symbols found for exchange %s\n", exchangeCode)
			continue
		}

		expiredTime := 1 * time.Hour
		err = cache.UpdateSymbolRanking(ctx, exchangeCode, symbolScores, expiredTime)
		if err != nil {
			log.Printf("Error setting available symbols into cache: %v\n", err)
		}
	}

	log.Println("Cache warm up completed")
}
