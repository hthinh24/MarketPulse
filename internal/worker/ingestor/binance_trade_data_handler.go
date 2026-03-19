package ingestor

import (
	"MarketPulse/internal/dto"
	"context"
	"encoding/json"
	segmentio "github.com/segmentio/kafka-go"
	"sync"
	"sync/atomic"
)

type WorkerPool struct {
	numWorkers int
}

type Worker struct {
	ID          int
	kafkaWriter *segmentio.Writer
	tradeChan   <-chan dto.Trade
	counter     *uint64
}

func NewWorkerPool(numWorkers int) *WorkerPool {
	return &WorkerPool{numWorkers: numWorkers}
}

func (p *WorkerPool) Start(ctx context.Context, wg *sync.WaitGroup, tradeChan <-chan dto.Trade, kafkaWriter *segmentio.Writer, counter *uint64) {
	defer wg.Done()

	poolWg := sync.WaitGroup{}

	for i := 0; i < p.numWorkers; i++ {
		poolWg.Add(1)

		worker := &Worker{
			ID:          i,
			kafkaWriter: kafkaWriter,
			tradeChan:   tradeChan,
			counter:     counter,
		}

		go worker.Start(ctx, &poolWg)
	}

	poolWg.Wait()
}

func (p *Worker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for trade := range p.tradeChan {
		msgBytes, _ := json.Marshal(trade)
		err := p.kafkaWriter.WriteMessages(ctx, segmentio.Message{
			Key:   []byte(trade.Symbol),
			Value: msgBytes,
		})
		if err == nil {
			atomic.AddUint64(p.counter, 1)
		}
	}
}
