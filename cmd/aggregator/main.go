package main

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	repository "MarketPulse/internal/infra/repository/postgres"
	cache "MarketPulse/internal/infra/repository/redis"
	"MarketPulse/internal/worker"
	"MarketPulse/internal/worker/aggregator"
	"MarketPulse/internal/worker/dbsync"
	"MarketPulse/internal/worker/dbsync/adapter"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"os/signal"
	"strings"
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
	broadcastChanSize := 10000
	broadcastChan := make(chan dto.CandleUpdatedEvent, broadcastChanSize)

	saveChan := make(chan entity.CandleEntity, saveChanSize)

	candleRepository := repository.NewCandleRepository(db)
	candleCache := cache.NewCandleCache(rdb)
	dbIngestor := worker.NewDBIngestor(saveChan, candleCache, candleRepository, batchSize)

	consumerGroup := "aggregator-group"
	kafkaTopicPrefix := "market_trades"
	binanceExchange := "BINANCE"
	okxExchange := "OKX"
	bybitExchange := "BYBIT"

	binanceReader := kafka.NewReader(*InitKafkaReaderConfig("localhost:9092", kafkaTopicPrefix+"_"+strings.ToLower(binanceExchange), consumerGroup))
	okxReader := kafka.NewReader(*InitKafkaReaderConfig("localhost:9092", kafkaTopicPrefix+"_"+strings.ToLower(okxExchange), consumerGroup))
	bybitReader := kafka.NewReader(*InitKafkaReaderConfig("localhost:9092", kafkaTopicPrefix+"_"+strings.ToLower(bybitExchange), consumerGroup))

	workerBuffer := 100
	binanceDispatcher := aggregator.NewDispatcher(binanceExchange, binanceReader, workerBuffer, saveChan, broadcastChan)
	okxDispatcher := aggregator.NewDispatcher(okxExchange, okxReader, workerBuffer, saveChan, broadcastChan)
	bybitDispatcher := aggregator.NewDispatcher(bybitExchange, bybitReader, workerBuffer, saveChan, broadcastChan)

	candlePublisher := aggregator.NewCandleUpdatePublisher(broadcastChan, rdb)

	binanceUrl := "https://api.binance.com/api/v3/exchangeInfo"
	okxUrl := "https://www.okx.com/api/v5/public/instruments?instType=SPOT"
	bybitUrl := "https://api.bybit.com/v5/market/instruments-info?category=spot"

	binanceAdapter := adapter.NewBinanceAdapter(binanceExchange, binanceUrl)
	okxAdapter := adapter.NewOKXAdapter(okxExchange, okxUrl)
	bybitAdapter := adapter.NewBybitAdapter(bybitExchange, bybitUrl)

	exchangeSymbolSyncer := dbsync.NewExchangeSymbolSyncer(
		db,
		[]dbsync.IExchangeAPIAdapter{binanceAdapter, okxAdapter, bybitAdapter},
	)

	wg := sync.WaitGroup{}

	wg.Add(1)
	go candlePublisher.Start(ctx, &wg)

	wg.Add(1)
	go dbIngestor.Start(ctx, &wg)

	wg.Add(1)
	go binanceDispatcher.Start(ctx, &wg)
	wg.Add(1)
	go okxDispatcher.Start(ctx, &wg)
	wg.Add(1)
	go bybitDispatcher.Start(ctx, &wg)

	wg.Add(1)
	go exchangeSymbolSyncer.Start(ctx, &wg)

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
