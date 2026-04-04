package application

import (
	"MarketPulse/internal/aggregator/domain"
	"context"
	"time"
)

type ICandleRepository interface {
	SaveCandle(ctx context.Context, candle *domain.CandleModel) error
	SaveCandles(ctx context.Context, candleModels []*domain.CandleModel) error
}

type ICandleCache interface {
	SetCandle(ctx context.Context, candleModel *domain.CandleModel, ttl time.Duration) error
	SetCandles(ctx context.Context, candleModels []*domain.CandleModel, ttl time.Duration) error
}
