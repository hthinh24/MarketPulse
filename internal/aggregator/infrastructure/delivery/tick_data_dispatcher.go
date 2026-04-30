package delivery

import (
	"MarketPulse/internal/aggregator/application"
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"context"
	"github.com/bytedance/sonic"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
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
	workers          map[string]chan *application.TickEvent
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
		workers:          make(map[string]chan *application.TickEvent),
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

			var tickData domain.TickModel
			if err := sonic.Unmarshal(msg.Value, &tickData); err != nil {
				log.Println("Error unmarshalling tickEvent data:", err)
				continue
			}

			tickEvent := application.TickEvent{
				Timestamp: time.Now(),
				Data:      tickData,
			}

			d.mu.RLock()
			workerChan, exists := d.workers[tickEvent.Data.Symbol]
			d.mu.RUnlock()

			if !exists {
				d.mu.Lock()
				workerChan, exists = d.workers[tickEvent.Data.Symbol]
				if !exists {
					workerChan = make(chan *application.TickEvent, d.workerBuffer)
					d.workers[tickEvent.Data.Symbol] = workerChan

					tickDataHandler := application.NewTickDataHandler(domain.NewCandleService(d.timeframeStates), workerChan, d.dbSaveChan, d.publishChan)
					go tickDataHandler.Start(ctx)
				}
				d.mu.Unlock()
			}

			select {
			case workerChan <- &tickEvent:
			default:
				observation.TickEvents.Add(ctx, 1,
					metric.WithAttributes(attribute.String("status", "dropped")),
					metric.WithAttributes(attribute.String("exchange", tickEvent.Data.Exchange)),
					metric.WithAttributes(attribute.String("symbol", tickEvent.Data.Symbol)),
					metric.WithAttributes(attribute.String("reason", "worker_channel_full")),
				)
			}
		}
	}
}
