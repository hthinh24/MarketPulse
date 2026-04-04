package domain

import (
	"github.com/shopspring/decimal"
)

var DefaultCandleModelTimeframe = "1m"

type TimeframeState struct {
	CurrentStartTime int64
	Timeframe        string
	IntervalMs       int64
	Candle           *CandleModel
}

type ProcessResult struct {
	UpdatedCandles []*CandleModel
	ClosedCandles  []*CandleModel
}

type CandleService struct {
	timeframeStates []*TimeframeState
}

func NewCandleService(timeframes []string) *CandleService {
	timeframesState := make([]*TimeframeState, len(timeframes))
	for i, tf := range timeframes {
		intervalMs := GetIntervalMs(tf)
		timeframesState[i] = &TimeframeState{
			CurrentStartTime: 0,
			Timeframe:        tf,
			IntervalMs:       intervalMs,
			Candle:           nil,
		}
	}

	return &CandleService{
		timeframeStates: timeframesState,
	}
}

func (s *CandleService) ProcessTick(tick *TickModel) ProcessResult {
	processResult := ProcessResult{
		UpdatedCandles: []*CandleModel{},
		ClosedCandles:  []*CandleModel{},
	}

	for _, state := range s.timeframeStates {
		startTime := GetBucketStartTime(tick.EventTime, state.IntervalMs)

		if state.Candle == nil {
			state.CurrentStartTime = startTime
			state.Candle = NewCandleModel(tick.Exchange, tick.Symbol, startTime, state.Timeframe, state.IntervalMs)
			continue
		}

		if state.Timeframe == DefaultCandleModelTimeframe && startTime > state.CurrentStartTime {
			processResult.ClosedCandles = append(processResult.ClosedCandles, state.Candle)

			state.CurrentStartTime = startTime
			state.Candle = NewCandleModel(tick.Exchange, tick.Symbol, startTime, state.Timeframe, state.IntervalMs)
		}

		s.updateTimeframeCandle(state, tick)
		processResult.UpdatedCandles = append(processResult.UpdatedCandles, state.Candle)
	}

	return processResult
}

func (s *CandleService) updateTimeframeCandle(state *TimeframeState, tick *TickModel) {
	price, errP := decimal.NewFromString(tick.Price)
	quantity, errQ := decimal.NewFromString(tick.Volume)
	if errP != nil || errQ != nil {
		return
	}

	state.Candle.Update(price, quantity, tick.IsTakerBuy)
}

func GetBucketStartTime(candleStartTime int64, intervalMs int64) int64 {
	return candleStartTime - (candleStartTime % intervalMs)
}
