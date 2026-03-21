package aggregator

import (
	"MarketPulse/internal/dto"
	"context"
	"encoding/json"
	"github.com/segmentio/kafka-go"
	"log"
	"sync"
)

type ITickDataProcessor interface {
	ProcessTick(symbol string, tickTime int64, trade dto.Trade)
}

type TickDataReaderManager struct {
	numReaders int
	config     kafka.ReaderConfig
	processor  ITickDataProcessor
}

func NewTickDataReaderManager(numReaders int, config kafka.ReaderConfig, processor ITickDataProcessor) *TickDataReaderManager {
	return &TickDataReaderManager{
		numReaders: numReaders,
		config:     config,
		processor:  processor,
	}
}

func (cp *TickDataReaderManager) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	var workerWg sync.WaitGroup

	for i := 0; i < cp.numReaders; i++ {
		workerWg.Add(1)

		reader := kafka.NewReader(cp.config)

		go func(r *kafka.Reader, id int) {
			defer workerWg.Done()
			defer r.Close()

			log.Printf("Aggregator Worker %d joined Consumer Group\n", id)

			for {
				select {
				case <-ctx.Done():
					log.Printf("Worker %d stopped due to context cancel\n", id)
					return
				default:
					msg, err := r.ReadMessage(ctx)
					if err != nil {
						if ctx.Err() != nil {
							return
						}
						log.Println("Error reading Kafka:", err)
						continue
					}

					var trade dto.Trade
					if err := json.Unmarshal(msg.Value, &trade); err != nil {
						log.Println("Error unmarshaling trade:", err)
						continue
					}

					cp.processor.ProcessTick(trade.Symbol, trade.EventTime, trade)
				}
			}
		}(reader, i)
	}

	workerWg.Wait()
	log.Println("TickDataReaderManager Stopped!")
}
