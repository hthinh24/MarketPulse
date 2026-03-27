package aggregator

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"fmt"
	"github.com/shopspring/decimal"
)

var candleType = "1m"
var channelPrefix = "marketpulse:"
var channelFormat = channelPrefix + "candles:%s:%s:%s"

func StartTickDataHandler(exchange string, symbol string, inbox <-chan model.TickModel, saveChan chan<- entity.CandleEntity, broadcastChan chan<- dto.CandleUpdatedEvent) {
	var candle *model.CandleModel

	for tick := range inbox {
		price, errP := decimal.NewFromString(tick.Price)
		quantity, errQ := decimal.NewFromString(tick.Volume)
		if errP != nil || errQ != nil {
			continue
		}

		if candle == nil {
			alignedStartTime := (tick.EventTime / 60000) * 60000
			candle = model.NewCandleModel(exchange, symbol, alignedStartTime, 60000)
		}

		if tick.EventTime >= candle.EndTime {
			candleEntity := entity.NewCandleEntity(candle)
			saveChan <- *candleEntity

			newStartTime := (tick.EventTime / 60000) * 60000
			candle.ResetForNextMinute(newStartTime, 60000)
		} else if tick.EventTime < candle.StartTime {
			continue
		}
		candle.Update(price, quantity, tick.IsTakerBuy)

		select {
		case broadcastChan <- createCandleUpdatedEvent(dto.CandleUpdated, exchange, symbol, candleType, *candle):
		default:
			// Ignore if channel is full to avoid blocking
		}
	}
}

func createCandleUpdatedEvent(eventType dto.CandleEvent, exchange string, symbol string, interval string, candle model.CandleModel) dto.CandleUpdatedEvent {
	roomName := fmt.Sprintf(channelFormat, exchange, symbol, interval)

	return dto.CandleUpdatedEvent{
		Event: eventType,
		Room:  roomName,
		Data:  candle,
	}
}
