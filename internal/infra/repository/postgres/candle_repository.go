package postgres

import (
	"MarketPulse/internal/entity"
	"gorm.io/gorm"
)

type CandleRepository struct {
	db *gorm.DB
}

func NewCandleRepository(db *gorm.DB) *CandleRepository {
	return &CandleRepository{db: db}
}

func (c *CandleRepository) SaveCandle(candle *entity.CandleEntity) error {
	return c.db.Create(candle).Error
}

func (c *CandleRepository) SaveCandles(candles []entity.CandleEntity) error {
	return c.db.Create(&candles).Error
}

func (c *CandleRepository) GetHistoricalCandles(symbol string, limit int) ([]*entity.CandleEntity, error) {
	var candles []*entity.CandleEntity
	err := c.db.Where("symbol = ?", symbol).
		Order("start_time desc").
		Limit(limit).
		Find(&candles).
		Error
	if err != nil {
		return nil, err
	}

	return candles, nil
}

func (c *CandleRepository) GetAvailableSymbols() ([]string, error) {
	var symbols []string
	err := c.db.Model(&entity.CandleEntity{}).
		Distinct("symbol").
		Pluck("symbol", &symbols).
		Error
	if err != nil {
		return nil, err
	}

	return symbols, nil
}
