package application

import (
	"MarketPulse/internal/aggregator/domain"
	"MarketPulse/internal/aggregator/infrastructure/common"
	"MarketPulse/internal/aggregator/infrastructure/observation"
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"time"
)

type TickDataHandler struct {
	candleService *domain.CandleService
	inbox         <-chan common.Envelope[domain.TickModel]
	saveChan      chan<- common.Envelope[domain.CandleModel]
	broadcastChan chan<- common.Envelope[domain.CandleModel]
}

func NewTickDataHandler(candleService *domain.CandleService, inbox <-chan common.Envelope[domain.TickModel], saveChan chan<- common.Envelope[domain.CandleModel], broadcastChan chan<- common.Envelope[domain.CandleModel]) *TickDataHandler {
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
		case tickEvent, ok := <-t.inbox:
			if !ok {
				return
			}

			ctx := tickEvent.ExtractContext(ctx)
			t.processTickEvent(ctx, tickEvent)
		}
	}
}

func (t *TickDataHandler) processTickEvent(ctx context.Context, tickEvent common.Envelope[domain.TickModel]) {
	ctx, span := observation.Tracer.Start(ctx, "process_tick_event")
	defer span.End()

	tickData := &tickEvent.Payload
	processResult := t.candleService.ProcessTick(tickData)

	observation.TickEventsLatencyMs.Record(ctx, float64(time.Since(tickEvent.Timestamp).Milliseconds()))
	observation.TickEvents.Add(ctx, 1,
		metric.WithAttributes(attribute.String("status", "processed")),
		metric.WithAttributes(attribute.String("exchange", tickData.Exchange)),
	)

	for _, updatedCandle := range processResult.UpdatedCandles {
		select {
		case t.broadcastChan <- common.NewEnvelope[domain.CandleModel](ctx, *updatedCandle):
		default:
			observation.CandleBroadcastDropsTotal.Add(ctx, 1,
				metric.WithAttributes(attribute.String("reason", "slow_consumer")),
			)
		}
	}

	for _, closedCandle := range processResult.ClosedCandles {
		t.saveChan <- common.NewEnvelope[domain.CandleModel](ctx, *closedCandle)
	}
}
