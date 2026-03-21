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

var candleChannelPrefix = "marketpulse:candles:"
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

		case candle, ok := <-b.publishChan:
			if !ok {
				return
			}

			buffer[candle.Symbol] = candle

		case <-ticker.C:
			if len(buffer) == 0 {
				continue
			}

			for symbol, candleData := range buffer {
				wsEvent := dto.WSEvent{
					Type: dto.CandleUpdated,
					Data: candleData,
				}
				redisMessage, _ := json.Marshal(wsEvent)

				channel := candleChannelPrefix + symbol + ":1m"
				b.redisClient.Publish(context.Background(), channel, redisMessage)
			}

			log.Printf("Published %d candle updates to Redis\n", len(buffer))

			clear(buffer)
		}
	}
}
