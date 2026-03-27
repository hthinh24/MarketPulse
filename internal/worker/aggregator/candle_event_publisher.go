package aggregator

import (
	"MarketPulse/internal/dto"
	"context"
	"encoding/json"
	"github.com/go-redis/redis/v8"
	"log"
	"sync"
	"time"
)

var tickDuration = 250 * time.Millisecond

type CandleEventPublisher struct {
	publishChan <-chan dto.CandleUpdatedEvent
	redisClient *redis.Client
}

func NewCandleUpdatePublisher(publishChan <-chan dto.CandleUpdatedEvent, redisClient *redis.Client) *CandleEventPublisher {
	return &CandleEventPublisher{
		publishChan: publishChan,
		redisClient: redisClient,
	}
}

func (b *CandleEventPublisher) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Println("CandleEventPublisher Worker started...")

	buffer := make(map[string]dto.CandleUpdatedEvent)

	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("CandleEventPublisher stopping...")
			return

		case candleEvent, ok := <-b.publishChan:
			if !ok {
				return
			}

			buffer[candleEvent.Room] = candleEvent

		case <-ticker.C:
			if len(buffer) == 0 {
				continue
			}

			for room, candleData := range buffer {
				wsEvent := dto.WSEvent{
					EventType: string(candleData.Event),
					Data:      candleData,
				}
				redisMessage, _ := json.Marshal(wsEvent)

				channel := room
				b.redisClient.Publish(context.Background(), channel, redisMessage)
			}

			clear(buffer)
		}
	}
}
