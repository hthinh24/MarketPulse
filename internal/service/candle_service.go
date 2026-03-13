package service

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"github.com/shopspring/decimal"
	"log"
	"sync"
)

type ICandleRepository interface {
	SaveCandle(candle *entity.CandleEntity) error
}

type CandleService struct {
	mu         sync.RWMutex
	candles    map[string]*model.CandleModel
	repository ICandleRepository
}

func NewCandleService(repository ICandleRepository) *CandleService {
	return &CandleService{
		mu:         sync.RWMutex{},
		candles:    make(map[string]*model.CandleModel),
		repository: repository,
	}
}

func (m *CandleService) ProcessTick(symbol string, tickTime int64, trade dto.Trade) {
	m.mu.Lock()
	defer m.mu.Unlock()

	candle, exists := m.candles[symbol]
	if !exists {
		alignedStartTime := (trade.EventTime / 60000) * 60000
		candle = model.NewCandleModel(symbol, alignedStartTime, 60000)
		m.candles[symbol] = candle
	}

	if tickTime > candle.EndTime {
		candleEntity := entity.NewCandleEntity(candle)
		log.Printf("CandleEntity created for %s: Start=%d, End=%d, O=%s, H=%s, L=%s, C=%s, V=%s\n",
			candleEntity.Symbol, candleEntity.StartTime.UnixMilli(), candleEntity.EndTime.UnixMilli(),
			candleEntity.Open.String(), candleEntity.High.String(), candleEntity.Low.String(), candleEntity.Close.String(), candleEntity.Volume.String(),
		)
		err := m.repository.SaveCandle(candleEntity)
		if err != nil {
			return
		}

		newStartTime := (trade.EventTime / 60000) * 60000
		candle.ResetForNextMinute(newStartTime, 60000)
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

	candle.Update(price, quantity, trade.IsMaker)
}
