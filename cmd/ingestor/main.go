package main

import (
	"MarketPulse/internal/config/kafka"
	"MarketPulse/internal/dto"
	"MarketPulse/internal/exchange/binance"
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

	writerWg := sync.WaitGroup{}
	ingestorWg := sync.WaitGroup{}

	kafkaWriter := kafka.NewKafkaWriter("localhost:9092", "market_trades")
	defer kafkaWriter.Close()

	writerPool := ingestor.NewWorkerPool(8)
	tradeChan := make(chan dto.Trade, 5000)

	writerWg.Add(1)
	go writerPool.Start(ctx, &writerWg, tradeChan, kafkaWriter, &counter)

	allStreams, err := binance.GetActiveUSDTStreams()
	if err != nil {
		log.Fatalf("Err when fetching data from Binance! Err:  %v", err)
	}
	log.Printf("Founded %d USDT trade pair!", len(allStreams))

	chunks := binance.ChunkSlice(allStreams, 300)
	for i, chunk := range chunks {
		streamPath := strings.Join(chunk, "/")
		url := fmt.Sprintf("wss://stream.binance.com:9443/stream?streams=%s", streamPath)
		binanceIngestor := ingestor.NewBinanceIngestor(url, tradeChan)

		ingestorWg.Add(1)
		go binanceIngestor.Start(ctx, &ingestorWg)

		log.Printf("Started binanceIngestor for chunk %d with %d coins\n", i+1, len(chunk))
	}

	<-ctx.Done()

	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	doneChan := make(chan struct{})
	go func() {
		ingestorWg.Wait()
		close(tradeChan)

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
