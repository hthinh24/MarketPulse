package main

import (
	ingestor2 "MarketPulse/internal/ingestor"
	"MarketPulse/internal/ingestor/config"
	"MarketPulse/internal/ingestor/config/kafka"
	binance2 "MarketPulse/internal/ingestor/exchange/binance"
	bybit2 "MarketPulse/internal/ingestor/exchange/bybit"
	okx2 "MarketPulse/internal/ingestor/exchange/okx"
	"MarketPulse/internal/ingestor/producer"
	"MarketPulse/internal/ingestor/producer/event"
	"context"
	"fmt"
	"github.com/joho/godotenv"
	"log"
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var counter uint64
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		StartTicker(ctx, ticker, &counter)
	}()

	writerWg := sync.WaitGroup{}
	pollerWg := sync.WaitGroup{}

	// ------------------- Binance Exchange Ingestor -------------------
	binanceExchange := "Binance"
	binanceKafkaTopic := cfg.Kafka.TopicPrefix + "_" + strings.ToLower(binanceExchange)
	kafkaWriter := kafka.NewKafkaWriter(cfg.Kafka.Broker, binanceKafkaTopic)
	defer kafkaWriter.Close()

	binanceProducerManager := producer.NewTickDataProducerManager(8)
	binanceTradeChan := make(chan event.TickEvent, 5000)

	writerWg.Add(1)
	go binanceProducerManager.Start(ctx, &writerWg, binanceTradeChan, kafkaWriter, &counter)

	allStreams, err := binance2.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from %s! Err:  %v", binanceExchange, err)
	}
	log.Printf("Founded %d USDT trade pair on %s !", len(allStreams), binanceExchange)

	chunks := binance2.ChunkSlice(allStreams, 300)
	for i, chunk := range chunks {

		streamPath := strings.Join(chunk, "/")
		url := fmt.Sprintf("%s?streams=%s", cfg.Binance.StreamURL, streamPath)

		binanceExchange := binance2.NewBinanceAdapter(url)
		exchangeIngestor := ingestor2.NewExchangeIngestor(
			binanceExchange,
			binanceTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Printf("Started Binance poller for chunk %d with %d coins\n", i+1, len(chunk))
	}

	// ------------------- OKX Exchange Ingestor -------------------
	okxExchange := "OKX"
	okxKafkaTopic := cfg.Kafka.TopicPrefix + "_" + strings.ToLower(okxExchange)
	okxKafkaWriter := kafka.NewKafkaWriter(cfg.Kafka.Broker, okxKafkaTopic)
	defer okxKafkaWriter.Close()

	okxProducerManager := producer.NewTickDataProducerManager(8)
	okxTradeChan := make(chan event.TickEvent, 5000)

	writerWg.Add(1)
	go okxProducerManager.Start(ctx, &writerWg, okxTradeChan, okxKafkaWriter, &counter)

	okxStreams, err := okx2.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from OKX! Err:  %v", err)
	}
	log.Printf("Founded %d USDT trade pair on %s !", len(okxStreams), okxExchange)

	okxChunks := okx2.ChunkSlice(okxStreams, 100)
	for i, chunk := range okxChunks {
		var okxAdapter *okx2.OKXAdapter
		okxAdapter = okx2.NewOKXAdapter(
			cfg.OKX.StreamURL,
			chunk,
		)

		exchangeIngestor := ingestor2.NewExchangeIngestor(
			okxAdapter,
			okxTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Printf("Started OKX poller for chunk %d with %d coins\n", i+1, len(chunk))
	}

	// ------------------- Bybit Exchange Ingestor -------------------
	bybitExchange := "bybit"
	bybitKafkaTopic := cfg.Kafka.TopicPrefix + "_" + strings.ToLower(bybitExchange)
	bybitKafkaWriter := kafka.NewKafkaWriter(cfg.Kafka.Broker, bybitKafkaTopic)
	defer bybitKafkaWriter.Close()

	bybitProducerManager := producer.NewTickDataProducerManager(8)
	bybitTradeChan := make(chan event.TickEvent, 5000)

	writerWg.Add(1)
	go bybitProducerManager.Start(ctx, &writerWg, bybitTradeChan, bybitKafkaWriter, &counter)

	bybitStreams, err := bybit2.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from %s! Err:  %v", bybitExchange, err)
	}
	log.Printf("Founded %d USDT trade pair on %s !", len(bybitStreams), bybitExchange)

	bybitChunks := bybit2.ChunkSlice(bybitStreams, 100)
	for i, chunk := range bybitChunks {
		var bybitAdapter *bybit2.BybitAdapter
		bybitAdapter = bybit2.NewBybitAdapter(
			cfg.Bybit.StreamURL,
			chunk,
		)

		exchangeIngestor := ingestor2.NewExchangeIngestor(
			bybitAdapter,
			bybitTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Printf("Started Bybit poller for chunk %d with %d coins\n", i+1, len(chunk))
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
		log.Println("Shutdown signal received, waiting for ongoing operations to finish...")
	case <-timeoutContext.Done():
		log.Println("Timeout reached, forcing shutdown...")
	}
}

func StartTicker(ctx context.Context, ticker *time.Ticker, counter *uint64) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentTPS := atomic.SwapUint64(counter, 0)
			log.Printf("[Metrics] %d trades/sec\n", currentTPS)
		}
	}
}
