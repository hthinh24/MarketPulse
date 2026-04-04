package domain

import (
	"github.com/shopspring/decimal"
)

type CandleModel struct {
	Exchange  string `json:"exchange"`
	Symbol    string `json:"symbol"`
	Timeframe string `json:"timeframe"`

	StartTime int64 `json:"start_time"`

	EndTime int64 `json:"end_time"`

	Open  decimal.Decimal `json:"open"`
	High  decimal.Decimal `json:"high"`
	Low   decimal.Decimal `json:"low"`
	Close decimal.Decimal `json:"close"`

	Volume         decimal.Decimal `json:"volume"`
	QuoteVolume    decimal.Decimal `json:"quote_volume"`
	TakerBuyVolume decimal.Decimal `json:"taker_buy_volume"`

	NumberOfTrades int64 `json:"number_of_trades"`
}

func NewCandleModel(exchange string, symbol string, startTime int64, Timeframe string, intervalMs int64) *CandleModel {
	return &CandleModel{
		Exchange:       exchange,
		Symbol:         symbol,
		Timeframe:      Timeframe,
		StartTime:      startTime,
		EndTime:        startTime + intervalMs - 1,
		Open:           decimal.Zero,
		High:           decimal.Zero,
		Low:            decimal.Zero,
		Close:          decimal.Zero,
		Volume:         decimal.Zero,
		QuoteVolume:    decimal.Zero,
		TakerBuyVolume: decimal.Zero,
		NumberOfTrades: 0,
	}
}

func (c *CandleModel) Update(
	price decimal.Decimal,
	quantity decimal.Decimal,
	isTakerBuy bool,
) {
	if c.Open.IsZero() {
		c.Open = price
		c.High = price
		c.Low = price
	} else {
		if price.GreaterThan(c.High) {
			c.High = price
		}
		if price.LessThan(c.Low) {
			c.Low = price
		}
	}
	c.Close = price

	c.Volume = c.Volume.Add(quantity)
	c.QuoteVolume = c.QuoteVolume.Add(price.Mul(quantity))
	if isTakerBuy {
		c.TakerBuyVolume = c.TakerBuyVolume.Add(quantity)
	}

	c.NumberOfTrades++
}

func (c *CandleModel) ResetForNextMinute(newStartTime int64, intervalMs int64) {
	c.StartTime = newStartTime
	c.EndTime = newStartTime + intervalMs - 1
	c.Open = decimal.Zero
	c.High = decimal.Zero
	c.Low = decimal.Zero
	c.Close = decimal.Zero
	c.Volume = decimal.Zero
	c.QuoteVolume = decimal.Zero
	c.TakerBuyVolume = decimal.Zero
	c.NumberOfTrades = 0
}

func (c *CandleModel) ResetForNextInterval(time int64, intervalMs int64) {
	c.StartTime = time
	c.EndTime = time + intervalMs - 1
	c.Open = decimal.Zero
	c.High = decimal.Zero
	c.Low = decimal.Zero
	c.Close = decimal.Zero
	c.Volume = decimal.Zero
	c.QuoteVolume = decimal.Zero
	c.TakerBuyVolume = decimal.Zero
	c.NumberOfTrades = 0
}

func (c *CandleModel) GetIntervalMs() int64 {
	if (c.EndTime - c.StartTime) < 0 {
		return 0
	}

	return c.EndTime - c.StartTime + 1
}

func GetIntervalMs(timeframe string) int64 {
	switch timeframe {
	case "1m":
		return 60 * 1000
	case "5m":
		return 5 * 60 * 1000
	case "15m":
		return 15 * 60 * 1000
	case "1h":
		return 60 * 60 * 1000
	case "1d":
		return 24 * 60 * 60 * 1000
	case "1w":
		return 7 * 24 * 60 * 60 * 1000
	case "1M":
		return 30 * 24 * 60 * 60 * 1000
	default:
		return -1
	}
}
