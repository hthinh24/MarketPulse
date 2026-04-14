package broadcaster

import (
	"context"
	"github.com/go-redis/redis/v8"
	"strings"
)

type IBroadcaster interface {
	BroadcastToRoom(room string, msg []byte)
}

func StartRedisSubscriber(ctx context.Context, redisClient *redis.Client, broadcaster IBroadcaster, channelPattern string, channelPrefix string) {
	pubsub := redisClient.PSubscribe(ctx, channelPattern)
	ch := pubsub.Channel()

	for msg := range ch {
		if strings.HasPrefix(msg.Channel, channelPrefix) {
			room := strings.TrimPrefix(msg.Channel, channelPrefix)
			//log.Printf("Received message for room %s", room)
			broadcaster.BroadcastToRoom(room, []byte(msg.Payload))
		}
	}
}
