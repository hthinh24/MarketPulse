package postgres

import (
	entity2 "MarketPulse/internal/server/entity"
	"MarketPulse/internal/server/model"
	"gorm.io/gorm"
	"time"
)

type CandleRepository struct {
	db *gorm.DB
}

func NewCandleRepository(db *gorm.DB) *CandleRepository {
	return &CandleRepository{db: db}
}

func (c *CandleRepository) IsSymbolExisted(exchange string, symbol string, timeframe string) (bool, error) {
	var candles []*entity2.CandleEntity

	err := c.db.Table(entity2.CandleEntity{}.TableNameWithTimeframe(timeframe)).
		Where("exchange = ? AND symbol = ?", exchange, symbol).
		Limit(1).
		Find(&candles).
		Error
	if err != nil {
		return false, err
	}

	return len(candles) > 0, nil
}

func (c *CandleRepository) GetNewestCandles(exchange string, symbol string, timeframe string, limit int) ([]*entity2.CandleEntity, error) {
	var candles []*entity2.CandleEntity

	err := c.db.Table(entity2.CandleEntity{}.TableNameWithTimeframe(timeframe)).
		Where("exchange = ? AND symbol = ?", exchange, symbol).
		Order("start_time desc").
		Limit(limit).
		Find(&candles).
		Error
	if err != nil {
		return nil, err
	}

	return candles, nil
}

func (c *CandleRepository) GetHistoricalCandles(exchange string, symbol string, timeframe string, startTime int64, limit int) ([]*entity2.CandleEntity, error) {
	var candles []*entity2.CandleEntity
	if startTime != 0 {
		err := c.db.Table(entity2.CandleEntity{}.TableNameWithTimeframe(timeframe)).
			Where("exchange = ? AND symbol = ? AND start_time < ?", exchange, symbol, time.UnixMilli(startTime).UTC()).
			Order("start_time desc").
			Limit(limit).
			Find(&candles).
			Error
		if err != nil {
			return nil, err
		}
	} else {
		return c.GetNewestCandles(exchange, symbol, timeframe, limit)
	}

	return candles, nil
}

func (c *CandleRepository) GetActiveExchanges() ([]entity2.Exchange, error) {
	var exchanges []entity2.Exchange

	err := c.db.Table(entity2.Exchange{}.TableName()).
		Where("status = ?", "ACTIVE").
		Find(&exchanges).Error
	if err != nil {
		return nil, err
	}

	return exchanges, nil
}

// GetExchangeQuoteVolumeScores
// TODO(refactor): This function should calculate base on other table not on candles_1m
func (c *CandleRepository) GetExchangeQuoteVolumeScores() ([]model.ExchangeScore, error) {
	var scores []model.ExchangeScore

	err := c.db.Table(entity2.CandleEntity{}.TableName()).
		Select("exchange, SUM(quote_volume) as total_quote_volume").
		Where("start_time >= ?", time.Now().Add(-24*time.Hour)).
		Group("exchange").
		Scan(&scores).Error

	// TODO(refactor): Cuz server not always running, so we need fallback logic to calculate score
	// Currently will calculate base on near 7 days data
	if len(scores) == 0 {
		err = c.db.Table(entity2.CandleEntity{}.TableName()).
			Select("exchange, SUM(quote_volume) as total_quote_volume").
			Where("start_time >= ?", time.Now().Add(-7*24*time.Hour)).
			Group("exchange").
			Scan(&scores).Error
	}

	return scores, err
}

// GetSymbolDayVolumeScores
// TODO(refactor): Analytic should calculate base on other table not on candles_1m
func (c *CandleRepository) GetSymbolDayVolumeScores(exchange string) ([]model.ExchangeSymbolScore, error) {
	var scores []model.ExchangeSymbolScore

	err := c.db.Table(entity2.CandleEntity{}.TableName()).
		Select("symbol, SUM(volume * close) as score").
		Where("exchange = ? AND start_time >= ?", exchange, time.Now().Add(-24*time.Hour)).
		Group("symbol").
		Scan(&scores).Error

	// TODO(refactor): Cuz server not always running, so we need fallback logic to calculate score
	// Currently will calculate base on near 7 days data
	if len(scores) == 0 {
		err = c.db.Table(entity2.CandleEntity{}.TableName()).
			Select("symbol, SUM(volume * close) as score").
			Where("exchange = ? AND start_time >= ?", exchange, time.Now().Add(-7*24*time.Hour)).
			Group("symbol").
			Scan(&scores).Error
	}

	return scores, err
}
