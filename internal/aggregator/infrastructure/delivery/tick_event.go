package delivery

import "MarketPulse/internal/aggregator/domain"

type KafkaTickEvent struct {
	ProducedAt int64
	Payload    domain.TickModel
}
