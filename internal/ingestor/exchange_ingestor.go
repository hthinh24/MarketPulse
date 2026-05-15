package ingestor

import (
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/pkg/logger"
	"context"
	"sync"
)

type ExchangeAdapter interface {
	Connect(ctx context.Context) error
	ReadTick(ctx context.Context) (event.TickEvent, error)
	Close() error
}

type ExchangeIngestor struct {
	log             *logger.Logger
	exchangeAdapter ExchangeAdapter
	tradeChan       chan<- event.TickEvent
}

func NewExchangeIngestor(log *logger.Logger, exchangeAdapter ExchangeAdapter, tradeChan chan<- event.TickEvent) *ExchangeIngestor {
	return &ExchangeIngestor{
		log:             log,
		exchangeAdapter: exchangeAdapter,
		tradeChan:       tradeChan,
	}
}

func (i *ExchangeIngestor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	err := i.exchangeAdapter.Connect(ctx)
	if err != nil {
		i.log.Error(ctx, "error connecting to exchange", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			i.cleanup(ctx)
			return
		default:
			tick, err := i.exchangeAdapter.ReadTick(ctx)
			if err != nil {
				i.log.Error(ctx, "error reading tick data", err)
				continue
			}

			i.tradeChan <- tick
		}
	}
}

func (i *ExchangeIngestor) cleanup(ctx context.Context) {
	err := i.exchangeAdapter.Close()
	if err != nil {
		i.log.Error(ctx, "error closing exchange connection", err)
	}
}
