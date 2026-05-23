package delivery

import (
	"MarketPulse/internal/aggregator/application"
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/common"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"MarketPulse/pkg/logger"
	"context"
	"github.com/bytedance/sonic"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
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
	log              *logger.Logger
	exchangeName     string
	kafkaReader      *kafka.Reader
	timeframeStates  []string
	workers          map[string]chan common.Envelope[domain.TickModel]
	workerBuffer     int
	timeframeConfigs []TimeframeConfig
	mu               sync.RWMutex
	dbSaveChan       chan<- common.Envelope[domain.CandleModel]
	publishChan      chan common.Envelope[domain.CandleModel]
}

func NewDispatcher(log *logger.Logger, exchange string, reader *kafka.Reader, timeframeStates []string, workerBuffer int, timeframeConfigs []TimeframeConfig, dbChan chan<- common.Envelope[domain.CandleModel], publishChan chan common.Envelope[domain.CandleModel]) *Dispatcher {
	return &Dispatcher{
		log:              log,
		exchangeName:     exchange,
		kafkaReader:      reader,
		timeframeStates:  timeframeStates,
		workers:          make(map[string]chan common.Envelope[domain.TickModel]),
		workerBuffer:     workerBuffer,
		timeframeConfigs: timeframeConfigs,
		dbSaveChan:       dbChan,
		publishChan:      publishChan,
	}
}

func (d *Dispatcher) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer d.kafkaReader.Close()

	d.log.Info(ctx, "dispatcher started", logger.String("exchange", d.exchangeName))

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := d.kafkaReader.ReadMessage(ctx)
			if err != nil {
				d.log.Error(ctx, "error reading kafka message", err)
				continue
			}

			var tickEvent KafkaTickEvent
			if err := sonic.Unmarshal(msg.Value, &tickEvent); err != nil {
				d.log.Error(ctx, "error unmarshalling tick event data", err)
				continue
			}

			ctx := d.extractCtxFromMessage(msg)
			ctx, span := observation.Tracer.Start(ctx, "receive_tick",
				trace.WithAttributes(
					attribute.Int64("kafka_lag_ms",
						time.Since(time.UnixMilli(tickEvent.ProducedAt)).Milliseconds()),
					attribute.Int64("broker_lag_ms",
						time.Since(msg.Time).Milliseconds()), // msg.Time = broker received time
					attribute.Int64("producer_lag_ms",
						msg.Time.Sub(time.UnixMilli(tickEvent.ProducedAt)).Milliseconds()),
				),
			)

			d.dispatchTickEvent(ctx, tickEvent)

			span.End()
		}
	}
}

func (d *Dispatcher) extractCtxFromMessage(msg kafka.Message) context.Context {
	carrier := propagation.MapCarrier{}
	for _, h := range msg.Headers {
		carrier[h.Key] = string(h.Value)
	}
	return otel.GetTextMapPropagator().Extract(context.Background(), carrier)
}

func (d *Dispatcher) dispatchTickEvent(ctx context.Context, kafkaTickEvent KafkaTickEvent) {
	tickData := kafkaTickEvent.Payload
	tickEvent := common.NewEnvelope(ctx, tickData)

	d.mu.RLock()
	workerChan, exists := d.workers[tickEvent.Payload.Symbol]
	d.mu.RUnlock()

	if !exists {
		d.mu.Lock()
		workerChan, exists = d.workers[tickEvent.Payload.Symbol]
		if !exists {
			workerChan = make(chan common.Envelope[domain.TickModel], d.workerBuffer)
			d.workers[tickEvent.Payload.Symbol] = workerChan

			tickDataHandler := application.NewTickDataHandler(domain.NewCandleService(d.timeframeStates), workerChan, d.dbSaveChan, d.publishChan)
			go tickDataHandler.Start(ctx)
		}
		d.mu.Unlock()
	}

	select {
	case workerChan <- tickEvent:
	default:
		observation.TickEvents.Add(ctx, 1,
			metric.WithAttributes(attribute.String("status", "dropped")),
			metric.WithAttributes(attribute.String("exchange", tickEvent.Payload.Exchange)),
			metric.WithAttributes(attribute.String("symbol", tickEvent.Payload.Symbol)),
			metric.WithAttributes(attribute.String("reason", "worker_channel_full")),
		)
	}
}
