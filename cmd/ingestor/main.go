package main

import (
	"MarketPulse/internal/config/kafka"
	"MarketPulse/internal/dto"
	"MarketPulse/internal/worker/ingestor"
	"context"
	"log"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var counter uint64

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				currentTPS := atomic.SwapUint64(&counter, 0)
				log.Printf("[Metrics] %d trades/sec\n", currentTPS)
			}
		}
	}()

	wg := sync.WaitGroup{}

	kafkaWriter := kafka.NewKafkaWriter("localhost:9092", "market_trades")
	defer kafkaWriter.Close()

	writerPool := ingestor.NewWorkerPool(5)
	tradeChan := make(chan dto.Trade, 5000)

	wg.Add(1)
	go writerPool.Start(ctx, &wg, tradeChan, kafkaWriter, &counter)

	url := "wss://stream.binance.com:9443/ws/btcusdt@trade"
	binanceIngester := ingestor.NewBinanceIngestor(url, tradeChan)
	wg.Add(1)
	go binanceIngester.Start(ctx, &wg)

	// Graceful shutdown
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
