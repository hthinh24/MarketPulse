package application

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type TickDataHandler struct {
	candleService *domain.CandleService
	inbox         <-chan *domain.TickModel
	saveChan      chan<- *domain.CandleModel
	broadcastChan chan<- *domain.CandleModel
}

func NewTickDataHandler(candleService *domain.CandleService, inbox <-chan *domain.TickModel, saveChan chan<- *domain.CandleModel, broadcastChan chan<- *domain.CandleModel) *TickDataHandler {
	return &TickDataHandler{
		candleService: candleService,
		inbox:         inbox,
		saveChan:      saveChan,
		broadcastChan: broadcastChan,
	}
}

func (t *TickDataHandler) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-t.inbox:
			if !ok {
				return
			}

			processResult := t.candleService.ProcessTick(tick)

			observation.TickEvents.Add(ctx, 1,
				metric.WithAttributes(attribute.String("status", "processed")),
				metric.WithAttributes(attribute.String("exchange", tick.Exchange)),
			)

			for _, closedCandle := range processResult.ClosedCandles {
				t.saveChan <- closedCandle
			}

			for _, updatedCandle := range processResult.UpdatedCandles {
				t.broadcastChan <- updatedCandle
			}
		}
	}
}
