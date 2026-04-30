package application

import (
	"MarketPulse/internal/aggregator/domain"
	"time"
)

type TickEvent struct {
	Timestamp time.Time
	Data      domain.TickModel
}
