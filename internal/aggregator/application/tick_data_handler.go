package application

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"time"
)

type TickDataHandler struct {
	candleService *domain.CandleService
	inbox         <-chan *TickEvent
	saveChan      chan<- *domain.CandleModel
	broadcastChan chan<- *domain.CandleModel
}

func NewTickDataHandler(candleService *domain.CandleService, inbox <-chan *TickEvent, saveChan chan<- *domain.CandleModel, broadcastChan chan<- *domain.CandleModel) *TickDataHandler {
	return &TickDataHandler{
		candleService: candleService,
		inbox:         inbox,
		saveChan:      saveChan,
		broadcastChan: broadcastChan,
	}
}

func (t *TickDataHandler) Start(ctx context.Context) {
	// Just for get sample data modulo logic
	tickCount := 0

	for {
		select {
		case <-ctx.Done():
			return
		case tickEvent, ok := <-t.inbox:
			if !ok {
				return
			}

			tickData := &tickEvent.Data
			processResult := t.candleService.ProcessTick(tickData)

			tickCount++
			if tickCount%50 == 0 {
				latency := time.Since(tickEvent.Timestamp).Milliseconds()
				observation.TickEventsLatencyMs.Record(ctx, float64(latency))
			}

			observation.TickEvents.Add(ctx, 1,
				metric.WithAttributes(attribute.String("status", "processed")),
				metric.WithAttributes(attribute.String("exchange", tickData.Exchange)),
			)

			for _, updatedCandle := range processResult.UpdatedCandles {
				select {
				case t.broadcastChan <- updatedCandle:
				default:
					observation.CandleBroadcastDropsTotal.Add(ctx, 1,
						metric.WithAttributes(attribute.String("reason", "slow_consumer")),
					)
				}
			}

			for _, closedCandle := range processResult.ClosedCandles {
				t.saveChan <- closedCandle
			}
		}
	}
}
