package main

import (
	"MarketPulse/internal/aggregator/domain"
	worker "MarketPulse/internal/aggregator/infrastructure"
	"MarketPulse/internal/aggregator/infrastructure/dbsync"
	adapter2 "MarketPulse/internal/aggregator/infrastructure/dbsync/adapter"
	"MarketPulse/internal/aggregator/infrastructure/delivery"
	aggregator "MarketPulse/internal/aggregator/infrastructure/publisher"
	postgres2 "MarketPulse/internal/aggregator/infrastructure/repository/postgres"
	redis2 "MarketPulse/internal/aggregator/infrastructure/repository/redis"
	"MarketPulse/internal/telemetry"
	"context"
	"github.com/go-redis/redis/v8"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	go func() {
		log.Println("pprof: http://localhost:6061/debug/pprof/")
		log.Println(http.ListenAndServe("localhost:6061", nil))
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-aggregator"
	otlpEndpoint := "localhost:4317"

	shutdown := telemetry.InitProvider(serviceName, otlpEndpoint)
	defer shutdown(ctx)

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
	broadcastChan := make(chan *domain.CandleModel, broadcastChanSize)

	saveChan := make(chan *domain.CandleModel, saveChanSize)

	candleRepository := postgres2.NewCandleRepository(db)
	candleCache := redis2.NewCandleCache(rdb)
	dbIngestor := worker.NewDBIngestor(saveChan, candleCache, candleRepository, batchSize)

	consumerGroup := "aggregator-group"
	kafkaTopicPrefix := "market_trades"
	binanceExchange := "BINANCE"
	okxExchange := "OKX"
	bybitExchange := "BYBIT"

	binanceReader := kafka.NewReader(*InitKafkaReaderConfig("localhost:9092", kafkaTopicPrefix+"_"+strings.ToLower(binanceExchange), consumerGroup))
	okxReader := kafka.NewReader(*InitKafkaReaderConfig("localhost:9092", kafkaTopicPrefix+"_"+strings.ToLower(okxExchange), consumerGroup))
	bybitReader := kafka.NewReader(*InitKafkaReaderConfig("localhost:9092", kafkaTopicPrefix+"_"+strings.ToLower(bybitExchange), consumerGroup))

	workerBuffer := 300
	timeframeConfigs := []delivery.TimeframeConfig{
		{Timeframe: "1m", IntervalMs: 60 * 1000, PublishRate: 250 * time.Millisecond},
		{Timeframe: "5m", IntervalMs: 300 * 1000, PublishRate: 500 * time.Millisecond},
		{Timeframe: "15m", IntervalMs: 900 * 1000, PublishRate: 1 * time.Second},
		{Timeframe: "1h", IntervalMs: 3600 * 1000, PublishRate: 2 * time.Second},
		{Timeframe: "1d", IntervalMs: 86400 * 1000, PublishRate: 5 * time.Second},
		{Timeframe: "1w", IntervalMs: 604800 * 1000, PublishRate: 10 * time.Second},
		{Timeframe: "1M", IntervalMs: 2592000 * 1000, PublishRate: 30 * time.Second},
	}

	timeframes := []string{"1m", "5m", "15m", "1h", "1d", "1w", "1M"}
	binanceDispatcher := delivery.NewDispatcher(binanceExchange, binanceReader, timeframes, workerBuffer, timeframeConfigs, saveChan, broadcastChan)
	okxDispatcher := delivery.NewDispatcher(okxExchange, okxReader, timeframes, workerBuffer, timeframeConfigs, saveChan, broadcastChan)
	bybitDispatcher := delivery.NewDispatcher(bybitExchange, bybitReader, timeframes, workerBuffer, timeframeConfigs, saveChan, broadcastChan)

	candlePublisher := aggregator.NewCandleUpdatePublisher(broadcastChan, rdb)

	binanceUrl := "https://api.binance.com/api/v3/exchangeInfo"
	okxUrl := "https://www.okx.com/api/v5/public/instruments?instType=SPOT"
	bybitUrl := "https://api.bybit.com/v5/market/instruments-info?category=spot"

	binanceAdapter := adapter2.NewBinanceAdapter(binanceExchange, binanceUrl)
	okxAdapter := adapter2.NewOKXAdapter(okxExchange, okxUrl)
	bybitAdapter := adapter2.NewBybitAdapter(bybitExchange, bybitUrl)

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

		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
		MaxWait:  50 * time.Millisecond,

		CommitInterval: 1 * time.Second,
	}
}
