package worker

import (
	"context"
	"github.com/go-redis/redis/v8"
	"strings"
)

type IBroadcaster interface {
	BroadcastToRoom(room string, msg []byte)
}

func StartRedisSubscriber(ctx context.Context, redisClient *redis.Client, broadcaster IBroadcaster) {
	// TODO(refactor): Move channel config to config file or via configuration struct
	// Currently hard coded just for simplicity
	candleChannelPattern := "marketpulse:candles:*"
	candleChannelPrefix := "marketpulse:candles:"

	pubsub := redisClient.PSubscribe(ctx, candleChannelPattern)
	ch := pubsub.Channel()

	for msg := range ch {
		if strings.HasPrefix(msg.Channel, candleChannelPrefix) {
			room := strings.TrimPrefix(msg.Channel, candleChannelPrefix)
			broadcaster.BroadcastToRoom(room, []byte(msg.Payload))
		}
	}
}
