package ingestor

import (
	"MarketPulse/internal/model"
	"context"
	"encoding/json"
	segmentio "github.com/segmentio/kafka-go"
	"sync"
	"sync/atomic"
)

type TickDataProducerManager struct {
	numWorkers int
}

type TickDataProducer struct {
	ID          int
	kafkaWriter *segmentio.Writer
	tradeChan   <-chan model.TickModel
	counter     *uint64
}

func NewTickDataProducerManager(numWorkers int) *TickDataProducerManager {
	return &TickDataProducerManager{numWorkers: numWorkers}
}

func (p *TickDataProducerManager) Start(ctx context.Context, wg *sync.WaitGroup, tradeChan <-chan model.TickModel, kafkaWriter *segmentio.Writer, counter *uint64) {
	defer wg.Done()

	poolWg := sync.WaitGroup{}

	for i := 0; i < p.numWorkers; i++ {
		poolWg.Add(1)

		worker := &TickDataProducer{
			ID:          i,
			kafkaWriter: kafkaWriter,
			tradeChan:   tradeChan,
			counter:     counter,
		}

		go worker.Start(ctx, &poolWg)
	}

	poolWg.Wait()
}

func (p *TickDataProducer) Start(ctx context.Context, wg *sync.WaitGroup) {
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
