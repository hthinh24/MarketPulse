package publisher

import (
	"MarketPulse/internal/aggregator/infrastructure/publisher/dto"
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/go-redis/redis/v8"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"hash/fnv"
	"time"
)

var channelPrefix = "marketpulse:"
var channelFormat = channelPrefix + "orderbook:%s:%s"
var maxBatchSize = 1000
var tickDuration = 200 * time.Millisecond

type OrderBookPublisher struct {
	log         *logger.Logger
	redisClient *redis.Client
}

func NewOrderBookPublisher(log *logger.Logger, redisClient *redis.Client) *OrderBookPublisher {
	return &OrderBookPublisher{
		log:         log,
		redisClient: redisClient,
	}
}

func (p *OrderBookPublisher) Start(ctx context.Context, publishChan <-chan event.Envelope[*domain.OrderBookSnapshot], numOfPublishChannel int) {
	dispatcherChans := make([]chan event.Envelope[*domain.OrderBookSnapshot], numOfPublishChannel)
	for i := 0; i < numOfPublishChannel; i++ {
		dispatcherChans[i] = make(chan event.Envelope[*domain.OrderBookSnapshot], maxBatchSize)
	}

	for i := 0; i < numOfPublishChannel; i++ {
		go p.worker(ctx, dispatcherChans[i])
	}

	for {
		select {
		case <-ctx.Done():
			p.log.Info(ctx, "orderbook publisher received shutdown signal, exiting")
			for _, ch := range dispatcherChans {
				close(ch)
			}
			return
		case envelop, ok := <-publishChan:
			if !ok {
				p.log.Info(ctx, "orderbook publisher publish channel closed, exiting")
				for _, ch := range dispatcherChans {
					close(ch)
				}
				return
			}

			// Hash-based routing: distribute by symbol
			hash := p.hashSymbol(envelop.Payload.Symbol)
			workerIdx := hash % uint32(numOfPublishChannel)
			select {
			case dispatcherChans[workerIdx] <- envelop:
			default:
				p.log.Warn(ctx, "worker channel full, dropping snapshot",
					logger.Uint32("worker_idx", workerIdx),
					logger.String("exchange", envelop.Payload.Exchange),
					logger.String("symbol", envelop.Payload.Symbol),
				)
			}
		}
	}
}

func (p *OrderBookPublisher) hashSymbol(symbol string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(symbol))
	return h.Sum32()
}

func (p *OrderBookPublisher) worker(ctx context.Context, workerChan <-chan event.Envelope[*domain.OrderBookSnapshot]) {
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	batch := make([]event.Envelope[*domain.OrderBookSnapshot], 0, maxBatchSize)

	for {
		select {
		case <-ctx.Done():
			if len(batch) > 0 {
				p.flush(ctx, batch)
			}
			return
		case <-ticker.C:
			if len(batch) > 0 {
				batch = p.flush(ctx, batch)
				ticker.Reset(tickDuration)
			}
		case envelop, ok := <-workerChan:
			if !ok {
				if len(batch) > 0 {
					p.flush(ctx, batch)
				}
				return
			}

			batch = append(batch, envelop)

			if len(batch) >= maxBatchSize {
				batch = p.flush(ctx, batch)
			}
		}
	}
}

func (p *OrderBookPublisher) flush(ctx context.Context, batch []event.Envelope[*domain.OrderBookSnapshot]) []event.Envelope[*domain.OrderBookSnapshot] {
	pipe := p.redisClient.Pipeline()

	for _, envelop := range batch {
		snapshot := envelop.Payload
		room := fmt.Sprintf(channelFormat, snapshot.Exchange, snapshot.Symbol)

		itemCtx := envelop.ExtractContext(ctx)
		itemCtx, span := observation.Tracer.Start(itemCtx, "publish_orderbook",
			trace.WithAttributes(
				attribute.String("room", room),
			),
		)

		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(itemCtx, carrier)

		wsEvent := dto.WSEvent[*domain.OrderBookSnapshot]{
			TraceParent: carrier["traceparent"],
			Timestamp:   time.Now().UnixMilli(),
			EventType:   snapshot.EventType,
			Data:        snapshot,
		}

		redisMessage, err := sonic.MarshalString(wsEvent)
		if err != nil {
			p.log.Error(ctx, "error marshaling snapshot for batch publish", err, logger.String("room", room))
			continue
		}

		intCmd := pipe.Publish(ctx, room, redisMessage)
		if intCmd.Err() != nil {
			p.log.Error(ctx, "error publishing to redis", intCmd.Err(), logger.String("room", room))
		}

		span.End()
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		p.log.Error(ctx, "error executing redis pipeline for batch publish", err)
	}

	for _, snapshot := range batch {
		p.cleanupPool(snapshot.Payload)
	}

	batch = batch[:0]
	return batch
}

func (p *OrderBookPublisher) cleanupPool(snapshot *domain.OrderBookSnapshot) {
	snapshot.Bids = snapshot.Bids[:0]
	snapshot.Asks = snapshot.Asks[:0]
	domain.SnapshotPool.Put(snapshot)
}
