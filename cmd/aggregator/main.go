package main

import (
	"MarketPulse/internal/aggregator/config"
	"MarketPulse/internal/aggregator/domain"
	worker "MarketPulse/internal/aggregator/infrastructure"
	"MarketPulse/internal/aggregator/infrastructure/common"
	"MarketPulse/internal/aggregator/infrastructure/dbsync"
	adapter2 "MarketPulse/internal/aggregator/infrastructure/dbsync/adapter"
	"MarketPulse/internal/aggregator/infrastructure/delivery"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	aggregator "MarketPulse/internal/aggregator/infrastructure/publisher"
	postgres2 "MarketPulse/internal/aggregator/infrastructure/repository/postgres"
	redis2 "MarketPulse/internal/aggregator/infrastructure/repository/redis"
	"MarketPulse/internal/telemetry"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/joho/godotenv"
	"github.com/segmentio/kafka-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
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

	go func() {
		log.Info(context.Background(), "pprof server starting", logger.String("addr", "http://localhost:6061/debug/pprof/"))
		log.Error(context.Background(), "pprof server error", fmt.Errorf("%v", http.ListenAndServe("localhost:6061", nil)))
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serviceName := "marketpulse-aggregator"

	shutdown, err := initTelemetry(ctx, serviceName, cfg.OTLP.Endpoint)
	if err != nil {
		log.Error(ctx, "failed to initialize telemetry", err)
		os.Exit(1)
	}
	defer shutdown(ctx)

	db := initDB(cfg.DB, log, ctx)
	rdb := initRedisDB(cfg.Redis)

	defer func() {
		if err := rdb.Close(); err != nil {
			return
		}
	}()

	batchSize := 400
	saveChanSize := 5000
	broadcastChanSize := 10000

	broadcastChan := make(chan common.Envelope[domain.CandleModel], broadcastChanSize)
	saveChan := make(chan common.Envelope[domain.CandleModel], saveChanSize)

	candleRepository := postgres2.NewCandleRepository(db)
	candleCache := redis2.NewCandleCache(rdb)
	dbIngestor := worker.NewDBIngestor(log, saveChan, candleCache, candleRepository, batchSize)

	consumerGroup := "aggregator-group"
	binanceExchange := "BINANCE"
	okxExchange := "OKX"
	bybitExchange := "BYBIT"

	binanceReader := kafka.NewReader(*InitKafkaReaderConfig(cfg.Kafka.Broker, cfg.Kafka.TopicPrefix+"_"+strings.ToLower(binanceExchange), consumerGroup))
	okxReader := kafka.NewReader(*InitKafkaReaderConfig(cfg.Kafka.Broker, cfg.Kafka.TopicPrefix+"_"+strings.ToLower(okxExchange), consumerGroup))
	bybitReader := kafka.NewReader(*InitKafkaReaderConfig(cfg.Kafka.Broker, cfg.Kafka.TopicPrefix+"_"+strings.ToLower(bybitExchange), consumerGroup))

	workerBuffer := 1000
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
	binanceDispatcher := delivery.NewDispatcher(log, binanceExchange, binanceReader, timeframes, workerBuffer, timeframeConfigs, saveChan, broadcastChan)
	okxDispatcher := delivery.NewDispatcher(log, okxExchange, okxReader, timeframes, workerBuffer, timeframeConfigs, saveChan, broadcastChan)
	bybitDispatcher := delivery.NewDispatcher(log, bybitExchange, bybitReader, timeframes, workerBuffer, timeframeConfigs, saveChan, broadcastChan)

	candlePublisher := aggregator.NewCandleUpdatePublisher(log, broadcastChan, rdb)

	binanceUrl := "https://api.binance.com/api/v3/exchangeInfo"
	okxUrl := "https://www.okx.com/api/v5/public/instruments?instType=SPOT"
	bybitUrl := "https://api.bybit.com/v5/market/instruments-info?category=spot"

	binanceAdapter := adapter2.NewBinanceAdapter(binanceExchange, binanceUrl)
	okxAdapter := adapter2.NewOKXAdapter(okxExchange, okxUrl)
	bybitAdapter := adapter2.NewBybitAdapter(bybitExchange, bybitUrl)

	exchangeSymbolSyncer := dbsync.NewExchangeSymbolSyncer(
		log,
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
		log.Info(ctx, "shutdown signal received, waiting for ongoing operations to finish")
	case <-timeoutContext.Done():
		log.Info(ctx, "timeout reached, forcing shutdown")
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

func initDB(dbCfg config.DBConfig, log *logger.Logger, ctx context.Context) *gorm.DB {
	db, err := gorm.Open(postgres.Open(dbCfg.DSN()), &gorm.Config{})
	if err != nil {
		log.Error(ctx, "failed to connect to database", err)
		os.Exit(1)
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
