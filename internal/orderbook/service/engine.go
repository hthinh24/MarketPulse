package service

import (
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"context"
	"github.com/google/btree"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"log"
	"sync"
	"time"
)

type OrderTree = btree.BTreeG[event.OrderLevel]

type OrderBookEngine struct {
	mu sync.RWMutex

	exchange     string
	symbol       string
	isSynced     bool
	lastUpdateID int64
	updateID     int64

	bids *OrderTree
	asks *OrderTree

	queueSize  int
	deltaQueue []event.OrderBookEvent
}

func NewOrderBookEngine(exchange, symbol string, queueSize int) *OrderBookEngine {
	degree := 32

	return &OrderBookEngine{
		exchange:     exchange,
		symbol:       symbol,
		isSynced:     false,
		lastUpdateID: 0,
		updateID:     0,
		bids:         btree.NewG(degree, func(a, b event.OrderLevel) bool { return a.Price < b.Price }),
		asks:         btree.NewG(degree, func(a, b event.OrderLevel) bool { return a.Price < b.Price }),
		queueSize:    queueSize,
		deltaQueue:   make([]event.OrderBookEvent, 0, queueSize),
	}
}

func (o *OrderBookEngine) Start(ctx context.Context, deltaChan <-chan event.OrderBookEvent, publishChan chan<- *event.OrderBookSnapshot, reSyncChan chan<- string) {
	reSyncChan <- o.symbol

	emitTicker := time.NewTicker(100 * time.Millisecond)
	defer emitTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case update := <-deltaChan:
			if update.IsSnapshot {
				o.handleSnapshot(update)
			} else {
				o.handleDelta(ctx, update, reSyncChan)
			}
		case <-emitTicker.C:
			o.emitSnapshot(publishChan)
		}
	}
}

func (o *OrderBookEngine) handleSnapshot(snapshot event.OrderBookEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.bids.Clear(false)
	o.asks.Clear(false)

	for _, b := range snapshot.Bids {
		o.bids.ReplaceOrInsert(event.OrderLevel{Price: b.Price, Size: b.Size})
	}
	for _, a := range snapshot.Asks {
		o.asks.ReplaceOrInsert(event.OrderLevel{Price: a.Price, Size: a.Size})
	}

	o.lastUpdateID = snapshot.UpdateID
	o.isSynced = true

	for _, delta := range o.deltaQueue {
		if delta.UpdateID <= o.lastUpdateID {
			continue
		}

		o.applyUpdate(delta)
	}

	o.deltaQueue = o.deltaQueue[:0]
}

func (o *OrderBookEngine) handleDelta(ctx context.Context, delta event.OrderBookEvent, resyncChan chan<- string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.isSynced {
		if len(o.deltaQueue) >= o.queueSize {
			log.Printf("Delta queue overflow for %s %s, resyncing...\n", o.exchange, o.symbol)
			o.deltaQueue = o.deltaQueue[:0]

			select {
			case resyncChan <- o.symbol:
			default:
			}
			return
		}

		o.deltaQueue = append(o.deltaQueue, delta)

		observation.OrderBookEvents.Add(ctx, 1,
			metric.WithAttributes(attribute.String("status", "queued")),
		)
		return
	}

	if delta.PrevUpdateID > o.lastUpdateID+1 {
		o.isSynced = false
		o.deltaQueue = o.deltaQueue[:0]

		select {
		case resyncChan <- o.symbol:
		default:
		}

		observation.OrderBookEvents.Add(ctx, 1,
			metric.WithAttributes(attribute.String("status", "dropped_gap")),
		)
		return
	}

	o.applyUpdate(delta)
	observation.OrderBookEvents.Add(ctx, 1,
		metric.WithAttributes(attribute.String("status", "applied")),
	)
}

func (e *OrderBookEngine) applyUpdate(delta event.OrderBookEvent) {
	for _, b := range delta.Bids {
		if b.Size == 0 {
			e.bids.Delete(event.OrderLevel{Price: b.Price})
		} else {
			e.bids.ReplaceOrInsert(event.OrderLevel{Price: b.Price, Size: b.Size})
		}
	}

	for _, a := range delta.Asks {
		if a.Size == 0 {
			e.asks.Delete(event.OrderLevel{Price: a.Price})
		} else {
			e.asks.ReplaceOrInsert(event.OrderLevel{Price: a.Price, Size: a.Size})
		}
	}

	e.lastUpdateID = delta.UpdateID
}

func (e *OrderBookEngine) emitSnapshot(publishChan chan<- *event.OrderBookSnapshot) {
	snapshot := event.SnapshotPool.Get().(*event.OrderBookSnapshot)

	snapshot.EventType = event.EventSnapshot
	snapshot.Exchange = e.exchange
	snapshot.Symbol = e.symbol
	snapshot.Timestamp = time.Now().UnixMilli()

	if !e.isSynced {
		snapshot.EventType = event.EventClear
		publishChan <- snapshot
		return
	}

	e.mu.RLock()
	count := 0
	e.bids.Descend(func(item event.OrderLevel) bool {
		snapshot.Bids = append(snapshot.Bids, item)
		count++
		return count < 20
	})

	count = 0
	e.asks.Ascend(func(item event.OrderLevel) bool {
		snapshot.Asks = append(snapshot.Asks, item)
		count++
		return count < 20
	})
	e.mu.RUnlock()

	select {
	case publishChan <- snapshot:
	default:
		snapshot.Bids = snapshot.Bids[:0]
		snapshot.Asks = snapshot.Asks[:0]
		event.SnapshotPool.Put(snapshot)
	}
}
