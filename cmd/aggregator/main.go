package main

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	repository "MarketPulse/internal/infra/repository/postgres"
	cache "MarketPulse/internal/infra/repository/redis"
	"MarketPulse/internal/service"
	"MarketPulse/internal/worker"
	"MarketPulse/internal/worker/aggregator"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db := InitDB()
	rdb := initRedisDB()

	defer func() {
		if err := rdb.Close(); err != nil {
			return
		}
	}()

	batchSize := 400
	saveChanSize := 5000
	publishChanSize := 10000

	saveChan := make(chan entity.CandleEntity, saveChanSize)
	publishChan := make(chan dto.CandleUpdatedEvent, publishChanSize)

	candleService := service.NewCandleAggregateService(saveChan, publishChan)
	candleUpdatePublisher := aggregator.NewCandleUpdatePublisher(publishChan, rdb)

	readerConfig := InitKafkaReaderConfig("localhost:9092", "market_trades", "vibe-aggregator-group")

	candleRepository := repository.NewCandleRepository(db)
	candleCache := cache.NewCandleCache(rdb)
	dbIngestor := worker.NewDBIngestor(saveChan, candleCache, candleRepository, batchSize)

	tickDataReaderManager := aggregator.NewTickDataReaderManager(4, *readerConfig, candleService)

	wg := sync.WaitGroup{}
	wg.Add(1)
	go dbIngestor.Start(ctx, &wg)
	wg.Add(1)
	go candleUpdatePublisher.Start(ctx, &wg)
	wg.Add(1)
	go tickDataReaderManager.Start(ctx, &wg)

	<-ctx.Done()

	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	doneChan := make(chan struct{})
	go func() {
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
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	return rdb
}

func InitKafkaReaderConfig(brokerURL string, topic string, groupID string) *kafka.ReaderConfig {
	return &kafka.ReaderConfig{
		Brokers:     []string{brokerURL},
		Topic:       topic,
		GroupID:     groupID,
		StartOffset: kafka.LastOffset,
	}
}
