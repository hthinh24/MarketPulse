package broadcaster

import (
	"context"
	"github.com/go-redis/redis/v8"
	"strings"
)

type IBroadcaster interface {
	BroadcastToRoom(ctx context.Context, room string, msg []byte)
}

func StartRedisSubscriber(ctx context.Context, redisClient *redis.Client, broadcaster IBroadcaster, channelPattern string, channelPrefix string) {
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
				broadcaster.BroadcastToRoom(ctx, room, []byte(msg.Payload))
			}
		}
	}
}
