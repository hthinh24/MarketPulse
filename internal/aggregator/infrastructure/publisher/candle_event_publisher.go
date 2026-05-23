package aggregator

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/common"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"MarketPulse/internal/aggregator/infrastructure/publisher/dto"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/go-redis/redis/v8"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"sync"
	"time"
)

var channelPrefix = "marketpulse:"
var channelFormat = channelPrefix + "candles:%s:%s:%s"
var tickDuration = 250 * time.Millisecond

type CandleEventPublisher struct {
	log         *logger.Logger
	publishChan <-chan common.Envelope[domain.CandleModel]
	redisClient *redis.Client
}

func NewCandleUpdatePublisher(log *logger.Logger, publishChan <-chan common.Envelope[domain.CandleModel], redisClient *redis.Client) *CandleEventPublisher {
	return &CandleEventPublisher{
		log:         log,
		publishChan: publishChan,
		redisClient: redisClient,
	}
}

func (b *CandleEventPublisher) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	b.log.Info(ctx, "candle event publisher worker started")

	buffer := make(map[string]common.Envelope[dto.CandleUpdatedEvent])

	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			b.log.Info(ctx, "candle event publisher stopping")
			return

		case candle, ok := <-b.publishChan:
			if !ok {
				return
			}

			ctx := candle.ExtractContext(ctx)
			candleEvent := b.createCandleUpdatedEvent(dto.CandleUpdated, candle.Payload)
			buffer[candleEvent.Room] = common.NewEnvelope(ctx, candleEvent)

		case <-ticker.C:

			if len(buffer) == 0 {
				continue
			}

			b.flushBuffer(ctx, buffer)
			clear(buffer)
		}
	}
}

func (b *CandleEventPublisher) createCandleUpdatedEvent(eventType dto.CandleEvent, candle domain.CandleModel) dto.CandleUpdatedEvent {
	roomName := fmt.Sprintf(channelFormat, candle.Exchange, candle.Symbol, candle.Timeframe)

	return dto.CandleUpdatedEvent{
		Event: eventType,
		Room:  roomName,
		Data:  candle,
	}
}

func (b *CandleEventPublisher) flushBuffer(workerCtx context.Context, buffer map[string]common.Envelope[dto.CandleUpdatedEvent]) {
	pipe := b.redisClient.Pipeline()

	for room, candleData := range buffer {
		itemCtx := candleData.ExtractContext(workerCtx)
		itemCtx, span := observation.Tracer.Start(itemCtx, "publish_candle",
			trace.WithAttributes(
				attribute.String("room", room),
				attribute.Int64("buffer_wait_ms", time.Since(candleData.Timestamp).Milliseconds()),
			),
		)

		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(itemCtx, carrier)

		wsEvent := dto.WSEvent[dto.CandleUpdatedEvent]{
			TraceParent: carrier["traceparent"],
			Timestamp:   time.Now().UnixMilli(),
			EventType:   string(candleData.Payload.Event),
			Data:        candleData.Payload,
		}

		redisMessage, _ := sonic.MarshalString(wsEvent)
		pipe.Publish(itemCtx, room, redisMessage)

		span.End()
	}

	_, err := pipe.Exec(workerCtx)
	if err != nil {
		b.log.Error(workerCtx, "failed to execute redis pipeline", err)
	}
}
