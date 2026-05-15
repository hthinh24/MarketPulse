package bybit

import (
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"MarketPulse/pkg/logger"
	"context"
	"time"
)

// BybitSymbolWorker maintains per-symbol orderbook state and sequence validation.
// It only processes events — all external interactions (WebSocket, HTTP) are handled by the dispatcher.
type BybitSymbolWorker struct {
	log            *logger.Logger
	exchange       string
	symbol         string
	lastUpdateID   int64
	isSynced       bool
	deltaQueue     []event.EventEnvelope
	deltaQueueSize int
	state          *service.OrderBookState

	workerChan chan event.EventEnvelope // receives both deltas and snapshots from dispatcher
	resyncChan chan<- string            // signals dispatcher to re-subscribe
}

func newBybitSymbolWorker(
	log *logger.Logger,
	exchange, symbol string,
	deltaQueueSize int,
	state *service.OrderBookState,
	workerChan chan event.EventEnvelope,
	resyncChan chan<- string,
) *BybitSymbolWorker {
	return &BybitSymbolWorker{
		log:            log,
		exchange:       exchange,
		symbol:         symbol,
		lastUpdateID:   0,
		isSynced:       false,
		deltaQueue:     make([]event.EventEnvelope, 0, deltaQueueSize),
		deltaQueueSize: deltaQueueSize,
		state:          state,
		workerChan:     workerChan,
		resyncChan:     resyncChan,
	}
}

func (w *BybitSymbolWorker) run(ctx context.Context, publishChan chan<- *domain.OrderBookSnapshot) {
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
				w.handleUpdate(ctx, envelope)
			}
		}
	}
}

// handleSnapshot applies snapshot to orderbook state and drains queued deltas.
func (w *BybitSymbolWorker) handleSnapshot(ctx context.Context, orderbookEvent event.EventEnvelope) {
	snapshot := orderbookEvent.Payload

	w.state.ApplySnapshot(snapshot)
	w.lastUpdateID = snapshot.UpdateID
	w.isSynced = true
	observation.SymbolSynced(ctx, w.exchange)

	// Apply queued deltas that are newer than snapshot
	for _, queued := range w.deltaQueue {
		delta := queued.Payload
		if delta.UpdateID > snapshot.UpdateID {
			w.state.ApplyUpdate(delta)
			w.lastUpdateID = delta.UpdateID
		}
	}
	w.deltaQueue = w.deltaQueue[:0]

	w.log.Info(ctx, "resync succeeded", logger.String("symbol", w.symbol))
}

// handleUpdate applies sequence validation and state management per symbol.
func (w *BybitSymbolWorker) handleUpdate(ctx context.Context, orderbookEvent event.EventEnvelope) {
	delta := orderbookEvent.Payload

	if !w.isSynced {
		// Not synced: queue deltas until snapshot received
		if len(w.deltaQueue) >= w.deltaQueueSize {
			w.log.Warn(ctx, "delta queue overflow, triggering resync", logger.String("symbol", w.symbol))
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

	// Check for sequence gap
	if delta.PrevUpdateID > w.lastUpdateID+1 {
		w.log.Warn(ctx, "sequence gap detected",
			logger.String("symbol", w.symbol),
			logger.Int64("expected", w.lastUpdateID+1),
			logger.Int64("got", delta.PrevUpdateID),
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
	w.lastUpdateID = delta.UpdateID
	observation.RecordEvent(ctx, w.exchange, "applied")
	observation.SampleLatency(ctx, w.exchange, time.Since(orderbookEvent.ReceivedAt))
}
