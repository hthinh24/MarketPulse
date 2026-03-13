package model

import "github.com/shopspring/decimal"

type CandleModel struct {
	Symbol    string `json:"symbol"`
	StartTime int64  `json:"start_time"`

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

func NewCandleModel(symbol string, startTime int64, intervalMs int64) *CandleModel {
	return &CandleModel{
		Symbol:         symbol,
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
	isMarker bool,
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
	if !isMarker {
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
