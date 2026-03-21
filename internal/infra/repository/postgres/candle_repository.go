package postgres

import (
	"MarketPulse/internal/entity"
	"MarketPulse/internal/model"
	"gorm.io/gorm"
	"time"
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

func (c *CandleRepository) GetSymbolDayVolumeScores() ([]model.SymbolScore, error) {
	var scores []model.SymbolScore

	err := c.db.Table(entity.CandleEntity{}.TableName()).
		Select("symbol, SUM(volume * close) as score").
		Where("start_time >= ?", time.Now().Add(-24*time.Hour)).
		Group("symbol").
		Scan(&scores).Error

	return scores, err
}
