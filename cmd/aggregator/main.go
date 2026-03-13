package main

import (
	"MarketPulse/internal/infra/kafka/consumer"
	repository "MarketPulse/internal/infra/repository/postgres"
	"MarketPulse/internal/service"
	"context"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	db := InitDB()

	candleRepository := repository.NewCandleRepository(db)

	candleService := service.NewCandleService(candleRepository)

	kafkaReader := InitKafkaReader("localhost:9092", "market_trades", "vibe-aggregator-group")
	defer kafkaReader.Close()

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

func InitKafkaReader(brokerURL string, topic string, groupID string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{brokerURL},
		Topic:       topic,
		GroupID:     groupID,
		StartOffset: kafka.LastOffset,
	})
}
