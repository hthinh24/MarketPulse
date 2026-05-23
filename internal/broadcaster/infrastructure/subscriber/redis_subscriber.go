package subscriber

import (
	"MarketPulse/internal/broadcaster/infrastructure/observation"
	"MarketPulse/internal/broadcaster/infrastructure/subscriber/dto"
	"MarketPulse/pkg/logger"
	"context"
	"github.com/bytedance/sonic"
	"github.com/go-redis/redis/v8"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"strings"
	"time"
)

type IBroadcaster interface {
	BroadcastToRoom(ctx context.Context, room string, msg []byte)
}

func StartRedisSubscriber(ctx context.Context, log *logger.Logger, redisClient *redis.Client, broadcaster IBroadcaster, channelPattern string, channelPrefix string) {
	pubsub := redisClient.PSubscribe(ctx, channelPattern)
	ch := pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			pubsub.Close()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if strings.HasPrefix(msg.Channel, channelPrefix) {
				room := strings.TrimPrefix(msg.Channel, channelPrefix)
				handleRedisMessage(ctx, log, broadcaster, room, msg)
			}
		}
	}
}

func handleRedisMessage(ctx context.Context, log *logger.Logger, broadcaster IBroadcaster, room string, msg *redis.Message) {
	var wsEvent dto.WSEvent[interface{}]
	if err := sonic.Unmarshal([]byte(msg.Payload), &wsEvent); err != nil {
		log.Error(ctx, "failed to unmarshal ws event", err)
		return
	}

	carrier := propagation.MapCarrier{"traceparent": wsEvent.TraceParent}
	ctx = otel.GetTextMapPropagator().Extract(context.Background(), carrier)

	_, span := observation.Tracer.Start(ctx, "broadcast_to_clients",
		trace.WithAttributes(
			attribute.String("room", room),
			attribute.Int64("redis_lag_ms",
				time.Since(time.UnixMilli(wsEvent.Timestamp)).Milliseconds()),
		),
	)
	defer span.End()

	broadcaster.BroadcastToRoom(ctx, room, []byte(msg.Payload))
}
