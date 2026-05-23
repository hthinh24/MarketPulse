package main

import (
	ingestor2 "MarketPulse/internal/ingestor"
	"MarketPulse/internal/ingestor/config"
	"MarketPulse/internal/ingestor/config/kafka"
	binance2 "MarketPulse/internal/ingestor/exchange/binance"
	bybit2 "MarketPulse/internal/ingestor/exchange/bybit"
	okx2 "MarketPulse/internal/ingestor/exchange/okx"
	"MarketPulse/internal/ingestor/infrastructure/observation"
	"MarketPulse/internal/ingestor/producer"
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/internal/telemetry"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/joho/godotenv"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.LoadAppConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	serviceName := "ingestor"
	log, err := logger.New(serviceName, cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := initTelemetry(ctx, serviceName, cfg.OTLP.Endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init telemetry: %v\n", err)
		os.Exit(1)
	}
	defer shutdown(context.Background())

	log.Info(ctx, "ingestor service started")

	var counter uint64
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		StartTicker(ctx, log, ticker, &counter)
	}()

	writerWg := sync.WaitGroup{}
	pollerWg := sync.WaitGroup{}

	// ------------------- Binance Exchange Ingestor -------------------
	binanceExchange := "BINANCE"
	binanceKafkaTopic := cfg.Kafka.TopicPrefix + "_" + strings.ToLower(binanceExchange)
	kafkaWriter := kafka.NewKafkaWriter(cfg.Kafka.Broker, binanceKafkaTopic)
	defer kafkaWriter.Close()

	binanceProducerManager := producer.NewTickDataProducerManager(8)
	binanceTradeChan := make(chan event.TickEnvelop, 5000)

	writerWg.Add(1)
	go binanceProducerManager.Start(ctx, &writerWg, binanceTradeChan, kafkaWriter, &counter)

	allStreams, err := binance2.GetActiveUSDTStreams()
	if err != nil {
		log.Error(ctx, "failed to fetch binance exchange data", err, logger.String("exchange", binanceExchange))
		os.Exit(1)
	}
	log.Info(ctx, "found USDT trade pairs", logger.Int("count", len(allStreams)), logger.String("exchange", binanceExchange))

	chunks := binance2.ChunkSlice(allStreams, 300)
	for i, chunk := range chunks {

		streamPath := strings.Join(chunk, "/")
		url := fmt.Sprintf("%s?streams=%s", cfg.Binance.StreamURL, streamPath)

		binanceExchange := binance2.NewBinanceAdapter(log, binanceExchange, url)
		exchangeIngestor := ingestor2.NewExchangeIngestor(
			log,
			binanceExchange,
			binanceTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Info(ctx, "started binance poller", logger.Int("chunk", i+1), logger.Int("coins", len(chunk)))
	}

	// ------------------- OKX Exchange Ingestor -------------------
	okxExchange := "OKX"
	okxKafkaTopic := cfg.Kafka.TopicPrefix + "_" + strings.ToLower(okxExchange)
	okxKafkaWriter := kafka.NewKafkaWriter(cfg.Kafka.Broker, okxKafkaTopic)
	defer okxKafkaWriter.Close()

	okxProducerManager := producer.NewTickDataProducerManager(8)
	okxTradeChan := make(chan event.TickEnvelop, 5000)

	writerWg.Add(1)
	go okxProducerManager.Start(ctx, &writerWg, okxTradeChan, okxKafkaWriter, &counter)

	okxStreams, err := okx2.GetActiveUSDTStreams()
	if err != nil {
		log.Error(ctx, "failed to fetch okx exchange data", err, logger.String("exchange", okxExchange))
		os.Exit(1)
	}
	log.Info(ctx, "found USDT trade pairs", logger.Int("count", len(okxStreams)), logger.String("exchange", okxExchange))

	okxChunks := okx2.ChunkSlice(okxStreams, 100)
	for i, chunk := range okxChunks {
		var okxAdapter *okx2.OKXAdapter
		okxAdapter = okx2.NewOKXAdapter(
			log,
			okxExchange,
			cfg.OKX.StreamURL,
			chunk,
		)

		exchangeIngestor := ingestor2.NewExchangeIngestor(
			log,
			okxAdapter,
			okxTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Info(ctx, "started okx poller", logger.Int("chunk", i+1), logger.Int("coins", len(chunk)))
	}

	// ------------------- Bybit Exchange Ingestor -------------------
	bybitExchange := "bybit"
	bybitKafkaTopic := cfg.Kafka.TopicPrefix + "_" + strings.ToLower(bybitExchange)
	bybitKafkaWriter := kafka.NewKafkaWriter(cfg.Kafka.Broker, bybitKafkaTopic)
	defer bybitKafkaWriter.Close()

	bybitProducerManager := producer.NewTickDataProducerManager(8)
	bybitTradeChan := make(chan event.TickEnvelop, 5000)

	writerWg.Add(1)
	go bybitProducerManager.Start(ctx, &writerWg, bybitTradeChan, bybitKafkaWriter, &counter)

	bybitStreams, err := bybit2.GetActiveUSDTStreams()
	if err != nil {
		log.Error(ctx, "failed to fetch bybit exchange data", err, logger.String("exchange", bybitExchange))
		os.Exit(1)
	}
	log.Info(ctx, "found USDT trade pairs", logger.Int("count", len(bybitStreams)), logger.String("exchange", bybitExchange))

	bybitChunks := bybit2.ChunkSlice(bybitStreams, 100)
	for i, chunk := range bybitChunks {
		var bybitAdapter *bybit2.BybitAdapter
		bybitAdapter = bybit2.NewBybitAdapter(
			log,
			bybitExchange,
			cfg.Bybit.StreamURL,
			chunk,
		)

		exchangeIngestor := ingestor2.NewExchangeIngestor(
			log,
			bybitAdapter,
			bybitTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Info(ctx, "started bybit poller", logger.Int("chunk", i+1), logger.Int("coins", len(chunk)))
	}

	// -------------------- Graceful Shutdown Handling -------------------
	<-ctx.Done()

	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	doneChan := make(chan struct{})
	go func() {
		pollerWg.Wait()

		close(binanceTradeChan)
		close(okxTradeChan)
		close(bybitTradeChan)

		writerWg.Wait()

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

func StartTicker(ctx context.Context, log *logger.Logger, ticker *time.Ticker, counter *uint64) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentTPS := atomic.SwapUint64(counter, 0)
			log.Info(ctx, "metrics", logger.Uint64("trades_per_sec", currentTPS))
		}
	}
}
