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
		wg.Add(1)
		//go p.worker(ctx, &poolWg, i)

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

//func (p *WorkerPool) Start(ctx context.Context, wg *sync.WaitGroup) {
//	defer wg.Done()
//	var poolWg sync.WaitGroup
//
//	for i := 0; i < p.numWorkers; i++ {
//		poolWg.Add(1)
//		go p.worker(ctx, &poolWg, i)
//	}
//	poolWg.Wait() // Chờ đám công nhân dọn dẹp xong mới báo lên main
//}

func (p *Worker) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case trade, ok := <-p.tradeChan:
			if !ok {
				return
			}

			msgBytes, _ := json.Marshal(trade)
			err := p.kafkaWriter.WriteMessages(ctx, segmentio.Message{
				Key:   []byte(trade.Symbol),
				Value: msgBytes,
			})
			if err == nil {
				atomic.AddUint64(p.counter, 1) // Ghi thành công thì đếm +1
			}
		}
	}
}
