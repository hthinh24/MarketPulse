package bybit

import (
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"context"
	"log"
	"time"
)

// BybitSymbolWorker maintains per-symbol orderbook state and sequence validation.
// It only processes events — all external interactions (WebSocket, HTTP) are handled by the dispatcher.
type BybitSymbolWorker struct {
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
	exchange, symbol string,
	deltaQueueSize int,
	state *service.OrderBookState,
	workerChan chan event.EventEnvelope,
	resyncChan chan<- string,
) *BybitSymbolWorker {
	return &BybitSymbolWorker{
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
				w.handleDelta(ctx, envelope)
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

	log.Printf("Resync succeeded for %s", w.symbol)
}

// handleDelta applies sequence validation and state management per symbol.
func (w *BybitSymbolWorker) handleDelta(ctx context.Context, orderbookEvent event.EventEnvelope) {
	delta := orderbookEvent.Payload

	if !w.isSynced {
		// Not synced: queue deltas until snapshot received
		if len(w.deltaQueue) >= w.deltaQueueSize {
			log.Printf("Delta queue overflow for %s, triggering resync...", w.symbol)
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
	if delta.UpdateID != w.lastUpdateID+1 {
		log.Printf("Sequence gap detected for %s: expected %d, got %d", w.symbol, w.lastUpdateID+1, delta.UpdateID)
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

