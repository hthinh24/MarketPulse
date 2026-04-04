package postgres

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/repository/postgres/entity"
	"context"
	"gorm.io/gorm"
	"time"
)

type CandleRepository struct {
	db *gorm.DB
}

func NewCandleRepository(db *gorm.DB) *CandleRepository {
	return &CandleRepository{db: db}
}

func (c *CandleRepository) SaveCandle(ctx context.Context, candle *domain.CandleModel) error {
	candleEntity := c.createCandleEntity(candle)
	return c.db.WithContext(ctx).Create(candleEntity).Error
}

func (c *CandleRepository) SaveCandles(ctx context.Context, candles []*domain.CandleModel) error {
	candleEntities := make([]entity.CandleEntity, len(candles))
	for i, candle := range candles {
		candleEntities[i] = *c.createCandleEntity(candle)
	}

	return c.db.WithContext(ctx).Create(&candleEntities).Error
}

func (c *CandleRepository) createCandleEntity(candle *domain.CandleModel) *entity.CandleEntity {
	startTime := time.UnixMilli(candle.StartTime).UTC()

	return &entity.CandleEntity{
		Exchange:       candle.Exchange,
		Symbol:         candle.Symbol,
		StartTime:      startTime,
		EndTime:        time.UnixMilli(candle.EndTime).UTC(),
		Open:           candle.Open,
		High:           candle.High,
		Low:            candle.Low,
		Close:          candle.Close,
		Volume:         candle.Volume,
		QuoteVolume:    candle.QuoteVolume,
		TakerBuyVolume: candle.TakerBuyVolume,
		NumberOfTrades: candle.NumberOfTrades,
	}
}
