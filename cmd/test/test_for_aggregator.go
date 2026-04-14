package main

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
)

func main() {
	w := &kafka.Writer{
		Addr:         kafka.TCP("localhost:9092"),
		Topic:        "market_trades_okx",
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    2000,
		BatchTimeout: 10 * time.Millisecond,
	}
	defer w.Close()

	payload := []byte(`{"exchange":"binance","symbol":"BTCUSDT","price":"68000.50","volume":"0.05","eventTime":1712530000000,"isTakerBuy":true}`)

	batchSize := 2000
	batch := make([]kafka.Message, batchSize)
	for i := 0; i < batchSize; i++ {
		batch[i] = kafka.Message{Value: payload}
	}

	var counter atomic.Int64

	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		var lastCount int64

		for range ticker.C {
			current := counter.Load()
			tps := current - lastCount
			lastCount = current
			fmt.Printf("TPS: %d msg/sec\n", tps)
		}
	}()

	start := time.Now()
	endTime := start.Add(2 * time.Minute)
	fmt.Println("Starting test...")

	for time.Now().Before(endTime) {
		err := w.WriteMessages(context.Background(), batch...)
		if err != nil {
			log.Printf("Error writing batch: %v", err)
		}

		counter.Add(int64(batchSize))
	}

	elapsed := time.Since(start).Seconds()
	total := counter.Load()
	fmt.Printf("Total %d records in %.2f second (AVG: %.2f msg/sec)\n", total, elapsed, float64(total)/elapsed)
}
