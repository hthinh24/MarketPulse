package main

import (
	controller "MarketPulse/internal/controller/http"
	repository "MarketPulse/internal/infra/repository/postgres"
	cache "MarketPulse/internal/infra/repository/redis"
	"MarketPulse/internal/service"
	"context"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"time"
)

func main() {
	// TODO(refactor): Move Init function to separate package and use dependency injection
	db := InitDB()
	rdb := initRedisDB()

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

	r := gin.Default()
	r.Use(cors.Default())

	v1 := r.Group("/api/v1")
	candleController.RegisterRoutes(v1)

	log.Print("Server is running on port 8080")
	r.Run(":8000")
}

func InitDB() *gorm.DB {
	dsn := "host=localhost user=postgres password=root dbname=marketpulse port=5432 sslmode=disable TimeZone=UTC"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	return db
}

func initRedisDB() *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	return rdb
}

func InitCacheWarmup(ctx context.Context, repo service.ICandleRepository, cache service.ICandleCache) {
	log.Println("Warm up cache, fetching available symbols from repository")

	symbols, err := repo.GetAvailableSymbols()
	if err != nil {
		log.Printf("Error fetching available symbols from repository: %v\n", err)
		return
	}

	if len(symbols) == 0 {
		log.Println("No available symbols found in repository, skipping cache warm up")
		return
	}

	expiredTime := 24 * time.Hour
	err = cache.SetAvailableSymbols(ctx, symbols, expiredTime)
	if err != nil {
		log.Printf("Error setting available symbols into cache: %v\n", err)
	} else {
		log.Printf("Cache warm up completed, cached %d available symbols for %.0f hours\n", len(symbols), expiredTime.Hours())
	}
}
