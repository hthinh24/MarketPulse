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
