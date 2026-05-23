package ingestor

import (
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/pkg/logger"
	"context"
	"errors"
	"sync"
	"time"
)

type ExchangeAdapter interface {
	Connect(ctx context.Context) error
	ReadTick(ctx context.Context) (event.TickEvent, error)
	Close() error
}

type ExchangeIngestor struct {
	log             *logger.Logger
	exchangeAdapter ExchangeAdapter
	tradeChan       chan<- event.TickEnvelop
}

func NewExchangeIngestor(log *logger.Logger, exchangeAdapter ExchangeAdapter, tradeChan chan<- event.TickEnvelop) *ExchangeIngestor {
	return &ExchangeIngestor{
		log:             log,
		exchangeAdapter: exchangeAdapter,
		tradeChan:       tradeChan,
	}
}

func (i *ExchangeIngestor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	reconnectDelay := 5 * time.Second
	maxAttempts := 5
	attempts := 0

	for {
		if attempts >= maxAttempts {
			i.log.Error(ctx, "max reconnect attempts reached", errors.New("max reconnect attempts reached"))
			return
		}

		err := i.exchangeAdapter.Connect(ctx)
		if err != nil {
			attempts++
			i.log.Error(ctx, "error connecting to exchange, retrying...", errors.New(""))

			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
				continue
			}
		}

		attempts = 0
		i.log.Info(ctx, "successfully connected to exchange")

		for {
			select {
			case <-ctx.Done():
				i.cleanup(ctx)
				return
			default:
				err := i.processTick(ctx)
				if err != nil {
					i.log.Error(ctx, "error processing tick, dropping connection...", err)

					i.cleanup(ctx)
					break
				}
			}
		}
	}
}

func (i *ExchangeIngestor) processTick(ctx context.Context) error {
	tick, err := i.exchangeAdapter.ReadTick(ctx)
	if err != nil {
		i.log.Error(ctx, "error reading tick data", err)
		return err
	}

	i.tradeChan <- event.NewTickEnvelop(tick)
	return nil
}

func (i *ExchangeIngestor) cleanup(ctx context.Context) {
	err := i.exchangeAdapter.Close()
	if err != nil {
		i.log.Error(ctx, "error closing exchange connection", err)
	}
}
