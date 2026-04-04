package aggregator

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/publisher/dto"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"log"
	"sync"
	"time"
)

var channelPrefix = "marketpulse:"
var channelFormat = channelPrefix + "candles:%s:%s:%s"
var tickDuration = 250 * time.Millisecond

type CandleEventPublisher struct {
	publishChan <-chan *domain.CandleModel
	redisClient *redis.Client
}

func NewCandleUpdatePublisher(publishChan <-chan *domain.CandleModel, redisClient *redis.Client) *CandleEventPublisher {
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

			candleEvent := b.createCandleUpdatedEvent(dto.CandleUpdated, *candle)
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

func (b *CandleEventPublisher) createCandleUpdatedEvent(eventType dto.CandleEvent, candle domain.CandleModel) dto.CandleUpdatedEvent {
	roomName := fmt.Sprintf(channelFormat, candle.Exchange, candle.Symbol, candle.Timeframe)

	return dto.CandleUpdatedEvent{
		Event: eventType,
		Room:  roomName,
		Data:  candle,
	}
}
