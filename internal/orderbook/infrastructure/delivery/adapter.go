package delivery

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/binance"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/bybit"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/okx"
	"context"
)

// ExchangeAdapter defines the interface for exchange-specific adapters.
// Each adapter is responsible for the complete lifecycle: symbol discovery,
// WebSocket subscription, sequence validation, gap detection, and snapshot management.
type ExchangeAdapter interface {
	Start(ctx context.Context, publishChan chan<- *event.OrderBookSnapshot) error
}

func NewExchangeAdapter(exchangeConfig *config.ExchangeConfig) ExchangeAdapter {
	name := exchangeConfig.Name

	switch name {
	case "BINANCE":
		return binance.NewBinanceAdapter(exchangeConfig)
	case "BYBIT":
		return bybit.NewBybitAdapter(exchangeConfig)
	case "OKX":
		return okx.NewOKXAdapter(exchangeConfig)
	default:
		panic("Unsupported exchange: " + name)
	}
}
