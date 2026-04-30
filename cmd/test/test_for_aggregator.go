package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
)

const (
	totalSymbols = 10000
)

// SymbolDistributor generates weighted random symbols based on volume distribution
type SymbolDistributor struct {
	rng *rand.Rand
}

func NewSymbolDistributor() *SymbolDistributor {
	return &SymbolDistributor{
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// getWeightedSymbol returns a symbol based on Pareto distribution:
// - Symbol 1: 25% of volume
// - Symbols 2-3: 20% of volume combined
// - Symbols 4-10000: 55% of volume
func (sd *SymbolDistributor) getWeightedSymbol() string {
	randVal := sd.rng.Float64()

	if randVal < 0.25 {
		// Top 1: symbol "1" - 25% volume
		return "1"
	} else if randVal < 0.45 {
		// Top 2-3: symbols "2", "3" - 20% volume split evenly
		if sd.rng.Float64() < 0.5 {
			return "2"
		}
		return "3"
	} else {
		// Rest: symbols "4" to "10000" - 55% volume distributed
		symbolID := 4 + sd.rng.Intn(totalSymbols-3)
		return strconv.Itoa(symbolID)
	}
}

func main() {
	w := &kafka.Writer{
		Addr:         kafka.TCP("localhost:9092"),
		Topic:        "market_trades_okx",
		Balancer:     &kafka.LeastBytes{},
		BatchSize:    2000,
		BatchTimeout: 10 * time.Millisecond,
	}
	defer w.Close()

	// Fixed payload template
	payloadTemplate := `{"exchange":"okx","symbol":"%s","price":"68000.50","volume":"0.05","eventTime":1712530000000,"isTakerBuy":true}`

	symbolDistrib := NewSymbolDistributor()
	batchSize := 2000

	var totalCounter atomic.Int64
	var top1Counter atomic.Int64
	var top3Counter atomic.Int64
	var restCounter atomic.Int64

	// Metrics reporter
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		var lastTotal, lastTop1, lastTop3, lastRest int64

		for range ticker.C {
			currentTotal := totalCounter.Load()
			currentTop1 := top1Counter.Load()
			currentTop3 := top3Counter.Load()
			currentRest := restCounter.Load()

			tpsTot := currentTotal - lastTotal
			tpsTop1 := currentTop1 - lastTop1
			tpsTop3 := currentTop3 - lastTop3
			tpsRest := currentRest - lastRest

			lastTotal = currentTotal
			lastTop1 = currentTop1
			lastTop3 = currentTop3
			lastRest = currentRest

			fmt.Printf("TPS (Total): %d | Top1: %d (25%%) | Top3: %d (45%%) | Rest: %d (55%%)\n",
				tpsTot, tpsTop1, tpsTop3, tpsRest)
		}
	}()

	start := time.Now()
	endTime := start.Add(3 * time.Minute)

	fmt.Printf("Starting stress test with %d symbols for 3 minutes...\n", totalSymbols)
	fmt.Println("Distribution: Symbol 1 = 25%, Symbols 2-3 = 20%, Symbols 4-10000 = 55%")

	for time.Now().Before(endTime) {
		batch := make([]kafka.Message, batchSize)

		for i := 0; i < batchSize; i++ {
			symbol := symbolDistrib.getWeightedSymbol()
			payload := fmt.Sprintf(payloadTemplate, symbol)

			batch[i] = kafka.Message{
				Key:   []byte(symbol),
				Value: []byte(payload),
			}

			totalCounter.Add(1)
			if symbol == "1" {
				top1Counter.Add(1)
			} else if symbol == "2" || symbol == "3" {
				top3Counter.Add(1)
			} else {
				restCounter.Add(1)
			}
		}

		err := w.WriteMessages(context.Background(), batch...)
		if err != nil {
			log.Printf("Error writing batch: %v", err)
		}
	}

	elapsed := time.Since(start).Seconds()
	total := totalCounter.Load()
	top1 := top1Counter.Load()
	top3 := top3Counter.Load()
	rest := restCounter.Load()

	fmt.Println("\n=== Test Results ===")
	fmt.Printf("Total %d records in %.2f seconds (AVG: %.2f msg/sec)\n", total, elapsed, float64(total)/elapsed)
	fmt.Printf("Symbol 1 (Top): %d records (%.2f%%)\n", top1, float64(top1)*100/float64(total))
	fmt.Printf("Symbols 2-3 (Top 3): %d records (%.2f%%)\n", top3, float64(top3)*100/float64(total))
	fmt.Printf("Symbols 4-10000 (Rest): %d records (%.2f%%)\n", rest, float64(rest)*100/float64(total))
}
