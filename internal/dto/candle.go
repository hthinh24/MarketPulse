package dto

import "MarketPulse/internal/model"

type GetCandlesRequest struct {
	Exchange    string `form:"exchange" binding:"required"`
	Symbol      string `form:"symbol" binding:"required"`
	Timeframe   string `form:"timeframe" binding:"required,oneof=1m 5m 15m 1h 1d 1w 1M"`
	Limit       int    `form:"limit" binding:"omitempty,min=1,max=1000"`
	EndTime     int64  `form:"endTime" binding:"omitempty"`
	ByPassCache bool   `form:"byPassCache" binding:"omitempty"`
}

type CandleResponse struct {
	OpenTime int64   `json:"openTime"`
	Open     float64 `json:"open"`
	High     float64 `json:"high"`
	Low      float64 `json:"low"`
	Close    float64 `json:"close"`
	Volume   float64 `json:"volume"`
}

type CandleHistoryResponse struct {
	Exchange    string            `json:"exchange"`
	Symbol      string            `json:"symbol"`
	Interval    string            `json:"interval"`
	HasMore     bool              `json:"has_more"`
	NextEndTime int64             `json:"next_end_time"`
	IsColdData  bool              `json:"is_cold_data"`
	Candles     []*CandleResponse `json:"candles"`
}

type CandleEvent string

const (
	CandleUpdated CandleEvent = "candle_update"
)

type CandleUpdatedEvent struct {
	Event CandleEvent       `json:"event"`
	Room  string            `json:"room"`
	Data  model.CandleModel `json:"data"`
}
