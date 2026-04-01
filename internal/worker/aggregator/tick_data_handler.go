package aggregator

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"fmt"
	"github.com/shopspring/decimal"
	"time"
)

var candleEntityTimeframe = "1m"
var channelPrefix = "marketpulse:"
var channelFormat = channelPrefix + "candles:%s:%s:%s"

type TimeframeState struct {
	TimeframeConfig
	CurrentStartTime int64
	Candle           *model.CandleModel
	LastPublishTime  time.Time
}

type TickDataHandler struct {
	exchange        string
	symbol          string
	timeframeStates []*TimeframeState
	inbox           <-chan model.TickModel
	saveChan        chan<- entity.CandleEntity
	broadcastChan   chan<- dto.CandleUpdatedEvent
}

func NewTickDataHandler(exchange string, symbol string, timeframeConfigs []TimeframeConfig, inbox <-chan model.TickModel, saveChan chan<- entity.CandleEntity, broadcastChan chan<- dto.CandleUpdatedEvent) *TickDataHandler {
	var timeframeStates []*TimeframeState
	for _, config := range timeframeConfigs {
		timeframeStates = append(timeframeStates, &TimeframeState{
			TimeframeConfig:  config,
			CurrentStartTime: 0,
			Candle:           nil,
			LastPublishTime:  time.Now(),
		})
	}

	return &TickDataHandler{
		exchange:        exchange,
		symbol:          symbol,
		timeframeStates: timeframeStates,
		inbox:           inbox,
		saveChan:        saveChan,
		broadcastChan:   broadcastChan,
	}
}

func (t *TickDataHandler) Start() {
	for tick := range t.inbox {
		now := time.Now()

		for _, state := range t.timeframeStates {
			startTime := GetBucketStartTime(tick.EventTime, state.IntervalMs)

			if state.Candle == nil {
				state.CurrentStartTime = startTime
				state.Candle = model.NewCandleModel(t.exchange, t.symbol, startTime, state.IntervalMs)
				continue
			}

			if state.TimeframeConfig.Timeframe == candleEntityTimeframe && startTime > state.CurrentStartTime {
				candleEntity := entity.NewCandleEntity(state.Candle)
				t.saveChan <- *candleEntity

				state.CurrentStartTime = startTime
				state.Candle.ResetForNextInterval(startTime, state.IntervalMs)
			}

			t.updateTimeframeCandle(state, tick)

			if now.Sub(state.LastPublishTime) >= state.PublishRate {
				t.publishEvent(state.Candle, state.Timeframe, t.broadcastChan)
				state.LastPublishTime = now
			}
		}
	}
}

func (t *TickDataHandler) updateTimeframeCandle(state *TimeframeState, tick model.TickModel) {
	price, errP := decimal.NewFromString(tick.Price)
	quantity, errQ := decimal.NewFromString(tick.Volume)
	if errP != nil || errQ != nil {
		return
	}

	state.Candle.Update(price, quantity, tick.IsTakerBuy)
}

func (t *TickDataHandler) publishEvent(candle *model.CandleModel, timeframe string, broadcastChan chan<- dto.CandleUpdatedEvent) {
	select {
	case broadcastChan <- createCandleUpdatedEvent(dto.CandleUpdated, t.exchange, t.symbol, timeframe, *candle):
	default:
		// Ignore if channel is full to avoid blocking
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

func GetBucketStartTime(candleStartTime int64, intervalMs int64) int64 {
	return candleStartTime - (candleStartTime % intervalMs)
}
