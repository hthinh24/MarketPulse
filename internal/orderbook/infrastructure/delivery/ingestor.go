package delivery

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"log"
)

type ExchangeIngestor struct {
	adapterConfig *config.ExchangeConfig
	adapter       ExchangeAdapter
}

func NewExchangeIngestor(adapterConfig *config.ExchangeConfig) *ExchangeIngestor {
	adapter := NewExchangeAdapter(adapterConfig)

	return &ExchangeIngestor{
		adapterConfig: adapterConfig,
		adapter:       adapter,
	}
}

func (e *ExchangeIngestor) Start(ctx context.Context, publishChan chan<- *event.OrderBookSnapshot) {
	log.Print("Starting ingestor for exchange: ", e.adapterConfig.Name)

	symbols, err := e.adapter.DiscoverySymbol(ctx)
	if err != nil {
		log.Printf("Error discovering symbols for %s: %v\n", e.adapterConfig.Name, err)
		return
	}

	mainChan := make(chan event.OrderBookEvent, e.adapterConfig.StreamBufferSize)
	if err := e.adapter.SubscribeOrderBooks(ctx, symbols, mainChan); err != nil {
		return
	}

	engineChans := make(map[string]chan event.OrderBookEvent)
	reSyncChan := make(chan string, 1000)

	for _, symbol := range symbols {
		ch := make(chan event.OrderBookEvent, 5000)
		engineChans[symbol] = ch

		engine := service.NewOrderBookEngine(e.adapterConfig.Name, symbol, e.adapterConfig.DeltaQueueSize)
		go engine.Start(ctx, ch, publishChan, reSyncChan)

	}

	go func() {
		defer func() {
			for _, ch := range engineChans {
				close(ch)
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case deltaEvent := <-mainChan:
				if ch, exists := engineChans[deltaEvent.Symbol]; exists {
					select {
					case ch <- deltaEvent:
					default:
						observation.OrderBookEvents.Add(ctx, 1,
							metric.WithAttributes(attribute.String("status", "dropped_queue_full")),
						)
						log.Printf("Warning: Dropping order book event for %s due to full channel buffer", deltaEvent.Symbol)
					}
				}
			}
		}
	}()

	// Re-sync order book snapshots when gap detected from engine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case symbol := <-reSyncChan:
				log.Printf("Re-syncing order book for symbol: %s", symbol)
				snapshot, err := e.adapter.FetchSnapshot(ctx, symbol)
				if err != nil {
					continue
				}

				if ch, exists := engineChans[symbol]; exists {
					ch <- *snapshot
				}
			}
		}
	}()
}
