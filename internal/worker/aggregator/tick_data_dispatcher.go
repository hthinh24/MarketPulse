package aggregator

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

type TimeframeConfig struct {
	Timeframe   string
	IntervalMs  int64
	PublishRate time.Duration
}

type Dispatcher struct {
	exchangeName     string
	kafkaReader      *kafka.Reader
	workers          map[string]chan model.TickModel
	workerBuffer     int
	timeframeConfigs []TimeframeConfig
	mu               sync.RWMutex
	dbSaveChan       chan<- entity.CandleEntity
	publishChan      chan dto.CandleUpdatedEvent
}

func NewDispatcher(exchange string, reader *kafka.Reader, workerBuffer int, timeframeConfigs []TimeframeConfig, dbChan chan<- entity.CandleEntity, publishChan chan dto.CandleUpdatedEvent) *Dispatcher {
	return &Dispatcher{
		exchangeName:     exchange,
		kafkaReader:      reader,
		workers:          make(map[string]chan model.TickModel),
		workerBuffer:     workerBuffer,
		timeframeConfigs: timeframeConfigs,
		dbSaveChan:       dbChan,
		publishChan:      publishChan,
	}
}

func (d *Dispatcher) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer d.kafkaReader.Close()

	log.Printf("Start Dispatcher for %s exchange", d.exchangeName)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := d.kafkaReader.ReadMessage(ctx)
			if err != nil {
				log.Println("Error reading Kafka message:", err)
				continue
			}

			var tick model.TickModel
			if err := json.Unmarshal(msg.Value, &tick); err != nil {
				log.Println("Error unmarshalling tick data:", err)
				continue
			}

			d.mu.RLock()
			workerChan, exists := d.workers[tick.Symbol]
			d.mu.RUnlock()

			if !exists {
				d.mu.Lock()
				workerChan, exists = d.workers[tick.Symbol]
				if !exists {
					workerChan = make(chan model.TickModel, d.workerBuffer)
					d.workers[tick.Symbol] = workerChan

					tickDataHandler := NewTickDataHandler(d.exchangeName, tick.Symbol, d.timeframeConfigs, workerChan, d.dbSaveChan, d.publishChan)
					go tickDataHandler.Start()
				}
				d.mu.Unlock()
			}

			workerChan <- tick
		}
	}
}
