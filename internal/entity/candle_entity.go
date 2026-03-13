package entity

import (
	"MarketPulse/internal/model"
	"github.com/shopspring/decimal"
	"time"
)

type CandleEntity struct {
	Symbol    string    `gorm:"column:symbol;type:varchar(20);uniqueIndex:idx_symbol_time,priority:1;not null"`
	StartTime time.Time `gorm:"column:start_time;uniqueIndex:idx_symbol_time,priority:2;not null"`

	EndTime time.Time `gorm:"column:end_time;not null"`

	Open  decimal.Decimal `gorm:"column:open;type:numeric(32,8);not null"`
	High  decimal.Decimal `gorm:"column:high;type:numeric(32,8);not null"`
	Low   decimal.Decimal `gorm:"column:low;type:numeric(32,8);not null"`
	Close decimal.Decimal `gorm:"column:close;type:numeric(32,8);not null"`

	Volume         decimal.Decimal `gorm:"column:volume;type:numeric(32,8);not null"`
	QuoteVolume    decimal.Decimal `gorm:"column:quote_volume;type:numeric(32,8);not null"`
	TakerBuyVolume decimal.Decimal `gorm:"column:taker_buy_volume;type:numeric(32,8);not null"`

	NumberOfTrades int64 `gorm:"column:number_of_trades;not null"`
}

func (CandleEntity) TableName() string {
	return "candles"
}

func NewCandleEntity(candle *model.CandleModel) *CandleEntity {
	return &CandleEntity{
		Symbol:         candle.Symbol,
		StartTime:      time.UnixMilli(candle.StartTime).UTC(),
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
