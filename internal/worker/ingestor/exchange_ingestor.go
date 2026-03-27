package ingestor

import (
	"MarketPulse/internal/model"
	"context"
	"log"
	"sync"
)

type ExchangeAdapter interface {
	Connect() error
	ReadTick() (model.TickModel, error)
	Close() error
}

type ExchangeIngestor struct {
	exchangeAdapter ExchangeAdapter
	tradeChan       chan<- model.TickModel
}

func NewExchangeIngestor(exchangeAdapter ExchangeAdapter, tradeChan chan<- model.TickModel) *ExchangeIngestor {
	return &ExchangeIngestor{
		exchangeAdapter: exchangeAdapter,
		tradeChan:       tradeChan,
	}
}

func (i *ExchangeIngestor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	err := i.exchangeAdapter.Connect()
	if err != nil {
		log.Println("Error connecting to exchange:", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			i.cleanup()
			return
		default:
			tick, err := i.exchangeAdapter.ReadTick()
			if err != nil {
				log.Println("Error reading tick data:", err)
				continue
			}

			i.tradeChan <- tick
		}
	}
}

func (i *ExchangeIngestor) cleanup() {
	err := i.exchangeAdapter.Close()
	if err != nil {
		log.Println("Error closing exchange connection:", err)
	}
}
