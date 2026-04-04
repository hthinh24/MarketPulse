package dto

import "MarketPulse/internal/aggregator/domain"

type CandleEvent string

const (
	CandleUpdated CandleEvent = "candle_update"
)

type CandleUpdatedEvent struct {
	Event CandleEvent        `json:"event"`
	Room  string             `json:"room"`
	Data  domain.CandleModel `json:"data"`
}
