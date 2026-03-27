package main

import (
	"MarketPulse/internal/config/kafka"
	"MarketPulse/internal/exchange/binance"
	"MarketPulse/internal/exchange/bybit"
	"MarketPulse/internal/exchange/okx"
	"MarketPulse/internal/model"
	"MarketPulse/internal/worker/ingestor"
	"context"
	"fmt"
	"log"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	// TODO(refactor): Move config values to config file or via configuration struct
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var counter uint64
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	go func() {
		StartTicker(ctx, ticker, &counter)
	}()

	kafkaTopicPrefix := "market_trades"

	writerWg := sync.WaitGroup{}
	pollerWg := sync.WaitGroup{}

	// ------------------- Binance Exchange Ingestor -------------------
	binanceExchange := "Binance"
	binanceKafkaTopic := kafkaTopicPrefix + "_" + strings.ToLower(binanceExchange)
	kafkaWriter := kafka.NewKafkaWriter("localhost:9092", binanceKafkaTopic)
	defer kafkaWriter.Close()

	binanceProducerManager := ingestor.NewTickDataProducerManager(8)
	binanceTradeChan := make(chan model.TickModel, 5000)

	writerWg.Add(1)
	go binanceProducerManager.Start(ctx, &writerWg, binanceTradeChan, kafkaWriter, &counter)

	allStreams, err := binance.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from %s! Err:  %v", binanceExchange, err)
	}
	log.Printf("Founded %d USDT trade pair on %s !", len(allStreams), binanceExchange)

	chunks := binance.ChunkSlice(allStreams, 300)
	for i, chunk := range chunks {

		streamPath := strings.Join(chunk, "/")
		url := fmt.Sprintf("wss://stream.binance.com:9443/stream?streams=%s", streamPath)

		binanceExchange := binance.NewBinanceAdapter(url)
		exchangeIngestor := ingestor.NewExchangeIngestor(
			binanceExchange,
			binanceTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Printf("Started Binance poller for chunk %d with %d coins\n", i+1, len(chunk))
	}

	// ------------------- OKX Exchange Ingestor -------------------
	okxExchange := "OKX"
	okxKafkaTopic := kafkaTopicPrefix + "_" + strings.ToLower(okxExchange)
	okxKafkaWriter := kafka.NewKafkaWriter("localhost:9092", okxKafkaTopic)
	defer okxKafkaWriter.Close()

	okxProducerManager := ingestor.NewTickDataProducerManager(8)
	okxTradeChan := make(chan model.TickModel, 5000)

	writerWg.Add(1)
	go okxProducerManager.Start(ctx, &writerWg, okxTradeChan, okxKafkaWriter, &counter)

	okxStreams, err := okx.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from OKX! Err:  %v", err)
	}
	log.Printf("Founded %d USDT trade pair on %s !", len(okxStreams), okxExchange)

	okxChunks := okx.ChunkSlice(okxStreams, 100)
	for i, chunk := range okxChunks {
		var okxAdapter *okx.OKXAdapter
		okxAdapter = okx.NewOKXAdapter(
			"wss://ws.okx.com:8443/ws/v5/public",
			chunk,
		)

		exchangeIngestor := ingestor.NewExchangeIngestor(
			okxAdapter,
			okxTradeChan,
		)

		pollerWg.Add(1)
		go exchangeIngestor.Start(ctx, &pollerWg)
		log.Printf("Started OKX poller for chunk %d with %d coins\n", i+1, len(chunk))
	}

	// ------------------- Bybit Exchange Ingestor -------------------
	bybitExchange := "bybit"
	bybitKafkaTopic := kafkaTopicPrefix + "_" + strings.ToLower(bybitExchange)
	bybitKafkaWriter := kafka.NewKafkaWriter("localhost:9092", bybitKafkaTopic)
	defer bybitKafkaWriter.Close()

	bybitProducerManager := ingestor.NewTickDataProducerManager(8)
	bybitTradeChan := make(chan model.TickModel, 5000)

	writerWg.Add(1)
	go bybitProducerManager.Start(ctx, &writerWg, bybitTradeChan, bybitKafkaWriter, &counter)

	bybitStreams, err := bybit.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from %s! Err:  %v", bybitExchange, err)
	}
	log.Printf("Founded %d USDT trade pair on %s !", len(bybitStreams), bybitExchange)

	bybitChunks := bybit.ChunkSlice(bybitStreams, 100)
	for i, chunk := range bybitChunks {
		var bybitAdapter *bybit.BybitAdapter
		bybitAdapter = bybit.NewBybitAdapter(
			"wss://stream.bytick.com/v5/public/spot",
			chunk,
		)

		exchangeIngestor := ingestor.NewExchangeIngestor(
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

		//close(binanceTradeChan)
		//close(okxTradeChan)
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
