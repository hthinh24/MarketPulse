package delivery

import (
	"MarketPulse/internal/aggregator/application"
	"MarketPulse/internal/aggregator/domain"
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
	timeframeStates  []string
	workers          map[string]chan *domain.TickModel
	workerBuffer     int
	timeframeConfigs []TimeframeConfig
	mu               sync.RWMutex
	dbSaveChan       chan<- *domain.CandleModel
	publishChan      chan *domain.CandleModel
}

func NewDispatcher(exchange string, reader *kafka.Reader, timeframeStates []string, workerBuffer int, timeframeConfigs []TimeframeConfig, dbChan chan<- *domain.CandleModel, publishChan chan *domain.CandleModel) *Dispatcher {
	return &Dispatcher{
		exchangeName:     exchange,
		kafkaReader:      reader,
		timeframeStates:  timeframeStates,
		workers:          make(map[string]chan *domain.TickModel),
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

			var tick domain.TickModel
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
					workerChan = make(chan *domain.TickModel, d.workerBuffer)
					d.workers[tick.Symbol] = workerChan

					tickDataHandler := application.NewTickDataHandler(domain.NewCandleService(d.timeframeStates), workerChan, d.dbSaveChan, d.publishChan)
					go tickDataHandler.Start()
				}
				d.mu.Unlock()
			}

			workerChan <- &tick
		}
	}
}
