package main

import (
	"MarketPulse/internal/infra/kafka/consumer"
	repository "MarketPulse/internal/infra/repository/postgres"
	cache "MarketPulse/internal/infra/repository/redis"
	"MarketPulse/internal/service"
	"MarketPulse/internal/worker"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	db := InitDB()
	rdb := initRedisDB()

	defer func() {
		if err := rdb.Close(); err != nil {
			return
		}
	}()

	bufferSize := 5000
	candleService := service.NewCandleAggregateService(rdb, bufferSize)

	kafkaReader := InitKafkaReader("localhost:9092", "market_trades", "vibe-aggregator-group")
	defer kafkaReader.Close()

	candleRepository := repository.NewCandleRepository(db)
	candleCache := cache.NewCandleCache(rdb)
	dbIngester := worker.NewDBIngestor(bufferSize, candleCache, candleRepository)

	go dbIngester.Start()
	defer dbIngester.Stop()

	consumer := consumer.NewConsumer(kafkaReader, candleService)
	consumer.StartConsuming(context.Background())
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

func InitKafkaReader(brokerURL string, topic string, groupID string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{brokerURL},
		Topic:       topic,
		GroupID:     groupID,
		StartOffset: kafka.LastOffset,
	})
}
