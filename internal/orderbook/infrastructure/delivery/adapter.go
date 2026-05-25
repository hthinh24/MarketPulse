package delivery

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/binance"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/bybit"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/okx"
	"MarketPulse/pkg/logger"
	"context"
)

// ExchangeAdapter defines the interface for exchange-specific adapters.
// Each adapter is responsible for the complete lifecycle: symbol discovery,
// WebSocket subscription, sequence validation, gap detection, and snapshot management.
type ExchangeAdapter interface {
	Start(ctx context.Context, publishChan chan<- event.Envelope[*domain.OrderBookSnapshot]) error
}

func NewExchangeAdapter(log *logger.Logger, exchangeConfig *config.ExchangeConfig) ExchangeAdapter {
	name := exchangeConfig.Name

	switch name {
	case "BINANCE":
		return binance.NewBinanceAdapter(log, exchangeConfig)
	case "BYBIT":
		return bybit.NewBybitAdapter(log, exchangeConfig)
	case "OKX":
		return okx.NewOKXAdapter(log, exchangeConfig)
	default:
		panic("Unsupported exchange: " + name)
	}
}
