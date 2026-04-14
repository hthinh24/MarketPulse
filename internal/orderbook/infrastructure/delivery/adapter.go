package delivery

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/delivery/exchange/binance"
	"context"
)

type ExchangeAdapter interface {
	DiscoverySymbol(ctx context.Context) ([]string, error)
	FetchSnapshot(ctx context.Context, symbol string) (*event.OrderBookEvent, error)
	SubscribeOrderBooks(ctx context.Context, symbols []string, deltaChan chan<- event.OrderBookEvent) error
	GetName() string
}

func NewExchangeAdapter(exchangeConfig *config.ExchangeConfig) ExchangeAdapter {
	name := exchangeConfig.Name

	switch name {
	case "BINANCE":
		return binance.NewBinanceAdapter(exchangeConfig)
	//case "OKX":
	//	return okx.NewAdapter(exchangeConfig.SnapshotUrl)
	//case "BYBIT":
	default:
		panic("Unsupported exchange: " + name)
	}
}
