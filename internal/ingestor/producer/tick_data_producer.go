package producer

import (
	"MarketPulse/internal/ingestor/infrastructure/observation"
	"MarketPulse/internal/ingestor/producer/event"
	"context"
	"github.com/bytedance/sonic"
	segmentio "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"sync"
	"sync/atomic"
	"time"
)

type TickDataProducerManager struct {
	numWorkers int
}

type TickDataProducer struct {
	ID          int
	kafkaWriter *segmentio.Writer
	tradeChan   <-chan event.TickEnvelop
	counter     *uint64
}

func NewTickDataProducerManager(numWorkers int) *TickDataProducerManager {
	return &TickDataProducerManager{numWorkers: numWorkers}
}

func (p *TickDataProducerManager) Start(ctx context.Context, wg *sync.WaitGroup, tradeChan <-chan event.TickEnvelop, kafkaWriter *segmentio.Writer, counter *uint64) {
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

	for {
		select {
		case <-ctx.Done():
			return
		case trade := <-p.tradeChan:
			p.publishEvent(ctx, trade)
		}
	}
}

func (p *TickDataProducer) publishEvent(ctx context.Context, event event.TickEnvelop) {
	ctx, span := observation.Tracer.Start(ctx, "publish_tick",
		trace.WithAttributes(
			attribute.String("exchange", event.Payload.Exchange),
		),
	)
	defer span.End()

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	headers := make([]segmentio.Header, 0, len(carrier))
	for k, v := range carrier {
		headers = append(headers, segmentio.Header{Key: k, Value: []byte(v)})
	}

	event.ProducedAt = time.Now().UnixMilli()
	msgBytes, _ := sonic.Marshal(event)
	err := p.kafkaWriter.WriteMessages(ctx, segmentio.Message{
		Key:     []byte(event.Payload.Symbol),
		Value:   msgBytes,
		Headers: headers,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to publish tick")
		return
	}

	atomic.AddUint64(p.counter, 1)
}
