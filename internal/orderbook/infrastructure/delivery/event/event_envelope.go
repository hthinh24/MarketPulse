package event

import (
	"MarketPulse/internal/orderbook/domain"
	"time"
)

type EventEnvelope struct {
	ReceivedAt time.Time
	Payload    domain.OrderBookEvent
}
