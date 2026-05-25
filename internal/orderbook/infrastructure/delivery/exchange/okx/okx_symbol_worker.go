package okx

import (
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"MarketPulse/pkg/logger"
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"time"
)

// OKXSymbolWorker maintains per-symbol orderbook state and sequence validation.
// It only processes events — all external interactions (WebSocket, HTTP) are handled by the dispatcher.
type OKXSymbolWorker struct {
	log            *logger.Logger
	exchange       string
	symbol         string // format: "BTC-USDT"
	lastSeqId      int64
	isSynced       bool
	deltaQueue     []event.Envelope[domain.OrderBookEvent]
	deltaQueueSize int
	state          *service.OrderBookState

	workerChan chan event.Envelope[domain.OrderBookEvent] // receives both deltas and snapshots from dispatcher
	resyncChan chan<- string                              // signals dispatcher to re-subscribe
}

func newOKXSymbolWorker(
	log *logger.Logger,
	exchange, symbol string,
	deltaQueueSize int,
	state *service.OrderBookState,
	workerChan chan event.Envelope[domain.OrderBookEvent],
	resyncChan chan<- string,
) *OKXSymbolWorker {
	return &OKXSymbolWorker{
		log:            log,
		exchange:       exchange,
		symbol:         symbol,
		lastSeqId:      0,
		isSynced:       false,
		deltaQueue:     make([]event.Envelope[domain.OrderBookEvent], 0, deltaQueueSize),
		deltaQueueSize: deltaQueueSize,
		state:          state,
		workerChan:     workerChan,
		resyncChan:     resyncChan,
	}
}

func (w *OKXSymbolWorker) run(ctx context.Context, publishChan chan<- event.Envelope[*domain.OrderBookSnapshot]) {
	go w.state.RunEmitter(ctx, w.exchange, w.symbol, publishChan)

	for {
		select {
		case <-ctx.Done():
			return
		case envelope, ok := <-w.workerChan:
			if !ok {
				return
			}
			if envelope.Payload.IsSnapshot {
				w.handleSnapshot(ctx, envelope)
			} else {
				w.handleDelta(ctx, envelope)
			}
		}
	}
}

// handleSnapshot applies snapshot to orderbook state and drains queued deltas.
func (w *OKXSymbolWorker) handleSnapshot(ctx context.Context, orderbookEvent event.Envelope[domain.OrderBookEvent]) {
	_, span := observation.Tracer.Start(ctx, "orderbook_snapshot",
		trace.WithAttributes(
			attribute.String("exchange", w.exchange),
			attribute.String("symbol", w.symbol),
		),
	)
	defer span.End()

	snapshot := orderbookEvent.Payload

	w.state.ApplySnapshot(snapshot)
	w.lastSeqId = snapshot.UpdateID
	w.isSynced = true
	observation.SymbolSynced(ctx, w.exchange)

	// Apply queued deltas that are newer than snapshot
	for _, queued := range w.deltaQueue {
		delta := queued.Payload
		if delta.UpdateID > snapshot.UpdateID {
			w.state.ApplyUpdate(delta)
			w.lastSeqId = delta.UpdateID
		}
	}
	w.deltaQueue = w.deltaQueue[:0]

	w.log.Info(ctx, "resync succeeded for symbol", logger.String("symbol", w.symbol))
}

// handleDelta applies sequence validation and state management per symbol (OKX uses prevSeqId).
func (w *OKXSymbolWorker) handleDelta(ctx context.Context, orderbookEvent event.Envelope[domain.OrderBookEvent]) {
	_, span := observation.Tracer.Start(ctx, "orderbook_update",
		trace.WithAttributes(
			attribute.String("exchange", w.exchange),
			attribute.String("symbol", w.symbol),
		),
	)
	defer span.End()

	delta := orderbookEvent.Payload

	if !w.isSynced {
		// Not synced: queue deltas until snapshot received
		if len(w.deltaQueue) >= w.deltaQueueSize {
			w.log.Info(ctx, "delta queue is full", logger.String("symbol", w.symbol))
			w.deltaQueue = w.deltaQueue[:0]
			w.isSynced = false

			select {
			case w.resyncChan <- w.symbol:
			default:
			}
			observation.RecordEvent(ctx, w.exchange, "queued")
			return
		}

		w.deltaQueue = append(w.deltaQueue, orderbookEvent)
		observation.RecordEvent(ctx, w.exchange, "queued")
		return
	}

	// Check for sequence gap (OKX uses prevSeqId field)
	// PrevUpdateID contains the prevSeqId from the message
	if delta.PrevUpdateID != w.lastSeqId {
		w.log.Warn(ctx, "sequence gap detected",
			logger.String("symbol", w.symbol),
			logger.Int64("expected_prev_seq_id", w.lastSeqId),
			logger.Int64("actual_prev_seq_id", delta.PrevUpdateID),
		)
		w.isSynced = false
		w.deltaQueue = w.deltaQueue[:0]
		observation.RecordEvent(ctx, w.exchange, "dropped_gap")
		observation.SymbolGapped(ctx, w.exchange)

		select {
		case w.resyncChan <- w.symbol:
		default:
		}
		return
	}

	// Update is valid, apply it
	w.state.ApplyUpdate(delta)
	w.lastSeqId = delta.UpdateID
	observation.RecordEvent(ctx, w.exchange, "applied")
	observation.SampleLatency(ctx, w.exchange, time.Since(orderbookEvent.Timestamp))
}
