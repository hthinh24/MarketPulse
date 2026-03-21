package service

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"github.com/shopspring/decimal"
	"log"
	"sync"
	"time"
)

type CandleAggregateService struct {
	mu          sync.RWMutex
	candles     map[string]*model.CandleModel
	saveChan    chan entity.CandleEntity
	publishChan chan dto.CandleUpdatedEvent
}

func NewCandleAggregateService(saveChan chan entity.CandleEntity, publishChan chan dto.CandleUpdatedEvent) *CandleAggregateService {
	return &CandleAggregateService{
		mu:          sync.RWMutex{},
		candles:     make(map[string]*model.CandleModel),
		saveChan:    saveChan,
		publishChan: publishChan,
	}
}

func (m *CandleAggregateService) ProcessTick(symbol string, tickTime int64, trade dto.Trade) {
	m.mu.RLock()
	candle, exists := m.candles[symbol]
	m.mu.RUnlock()

	if !exists {
		m.mu.Lock()

		candle, exists = m.candles[symbol]
		if !exists {
			alignedStartTime := (trade.EventTime / 60000) * 60000
			candle = model.NewCandleModel(symbol, alignedStartTime, 60000)
			m.candles[symbol] = candle
		}

		m.mu.Unlock()
	}

	var price decimal.Decimal
	var quantity decimal.Decimal
	var err error

	if price, err = decimal.NewFromString(trade.Price); err != nil {
		log.Printf("Error parsing price: %v\n", err)
		return
	}
	if quantity, err = decimal.NewFromString(trade.Quantity); err != nil {
		log.Printf("Error parsing quantity: %v\n", err)
		return
	}

	candle.Mu.Lock()
	if tickTime > candle.EndTime {
		candleEntity := entity.NewCandleEntity(candle)

		m.saveChan <- *candleEntity

		newStartTime := (trade.EventTime / 60000) * 60000
		candle.ResetForNextMinute(newStartTime, 60000)
	}

	candle.Update(price, quantity, trade.IsMaker)
	candleUpdatedEvent := createCandleUpdatedEvent(candle)
	candle.Mu.Unlock()

	select {
	case m.publishChan <- *candleUpdatedEvent:
	default:
	}
}

func createCandleUpdatedEvent(candle *model.CandleModel) *dto.CandleUpdatedEvent {
	startTime := time.UnixMilli(candle.StartTime).UTC()
	open, _ := candle.Open.Float64()
	high, _ := candle.High.Float64()
	low, _ := candle.Low.Float64()
	closePrice, _ := candle.Close.Float64()
	volume, _ := candle.Volume.Float64()

	return &dto.CandleUpdatedEvent{
		Symbol:   candle.Symbol,
		OpenTime: startTime.UnixMilli(),
		Open:     open,
		High:     high,
		Low:      low,
		Close:    closePrice,
		Volume:   volume,
	}
}
