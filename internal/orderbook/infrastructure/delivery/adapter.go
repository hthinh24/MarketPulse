package delivery

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/binance"
	"context"
)

// ExchangeAdapter defines the interface for exchange-specific adapters.
// Each adapter is responsible for the complete lifecycle: symbol discovery,
// WebSocket subscription, sequence validation, gap detection, resync with backoff,
// and metrics reporting. Adapters own per-symbol state internally.
type ExchangeAdapter interface {
	Start(ctx context.Context, publishChan chan<- *event.OrderBookSnapshot) error
}

func NewExchangeAdapter(exchangeConfig *config.ExchangeConfig) ExchangeAdapter {
	name := exchangeConfig.Name

	switch name {
	case "BINANCE":
		return binance.NewBinanceAdapter(exchangeConfig)
	//case "OKX":
	//	return okx.NewAdapter(exchangeConfig)
	//case "BYBIT":
	//	return bybit.NewAdapter(exchangeConfig)
	default:
		panic("Unsupported exchange: " + name)
	}
}
