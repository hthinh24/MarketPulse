package service

import (
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"context"
	"fmt"
	"github.com/google/btree"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"sync"
	"time"
)

type OrderTree = btree.BTreeG[event.OrderLevel]

type OrderBookState struct {
	mu               sync.RWMutex
	bids             *OrderTree
	asks             *OrderTree
	snapshotQuantity int
}

func NewOrderBookState(btreeDegree, snapshotQuantity int) (*OrderBookState, error) {
	if btreeDegree <= 0 {
		return nil, fmt.Errorf("btreeDegree must be positive, got %d", btreeDegree)
	}
	if snapshotQuantity <= 0 {
		return nil, fmt.Errorf("snapshotQuantity must be positive, got %d", snapshotQuantity)
	}
	if snapshotQuantity > btreeDegree {
		return nil, fmt.Errorf("snapshotQuantity (%d) cannot exceed btreeDegree (%d)", snapshotQuantity, btreeDegree)
	}

	return &OrderBookState{
		bids:             btree.NewG(btreeDegree, func(a, b event.OrderLevel) bool { return a.Price < b.Price }),
		asks:             btree.NewG(btreeDegree, func(a, b event.OrderLevel) bool { return a.Price < b.Price }),
		snapshotQuantity: snapshotQuantity,
	}, nil
}

func (s *OrderBookState) ApplyUpdate(delta event.OrderBookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range delta.Bids {
		if b.Size == 0 {
			s.bids.Delete(event.OrderLevel{Price: b.Price})
		} else {
			s.bids.ReplaceOrInsert(event.OrderLevel{Price: b.Price, Size: b.Size})
		}
	}

	for _, a := range delta.Asks {
		if a.Size == 0 {
			s.asks.Delete(event.OrderLevel{Price: a.Price})
		} else {
			s.asks.ReplaceOrInsert(event.OrderLevel{Price: a.Price, Size: a.Size})
		}
	}
}

func (s *OrderBookState) ApplySnapshot(snapshot event.OrderBookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bids.Clear(false)
	s.asks.Clear(false)

	for _, b := range snapshot.Bids {
		s.bids.ReplaceOrInsert(event.OrderLevel{Price: b.Price, Size: b.Size})
	}
	for _, a := range snapshot.Asks {
		s.asks.ReplaceOrInsert(event.OrderLevel{Price: a.Price, Size: a.Size})
	}
}

func (s *OrderBookState) EmitSnapshot(exchange, symbol string, publishChan chan<- *event.OrderBookSnapshot) {
	snapshot := event.SnapshotPool.Get().(*event.OrderBookSnapshot)

	snapshot.EventType = event.EventSnapshot
	snapshot.Exchange = exchange
	snapshot.Symbol = symbol
	snapshot.Timestamp = time.Now().UnixMilli()

	s.mu.RLock()
	count := 0
	s.bids.Descend(func(item event.OrderLevel) bool {
		snapshot.Bids = append(snapshot.Bids, item)
		count++
		return count < s.snapshotQuantity
	})

	count = 0
	s.asks.Ascend(func(item event.OrderLevel) bool {
		snapshot.Asks = append(snapshot.Asks, item)
		count++
		return count < s.snapshotQuantity
	})
	s.mu.RUnlock()

	select {
	case publishChan <- snapshot:
	default:
		snapshot.Bids = snapshot.Bids[:0]
		snapshot.Asks = snapshot.Asks[:0]
		event.SnapshotPool.Put(snapshot)
	}
}

func (s *OrderBookState) RunEmitter(ctx context.Context, exchange, symbol string, publishChan chan<- *event.OrderBookSnapshot) {
	emitTicker := time.NewTicker(100 * time.Millisecond)
	defer emitTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-emitTicker.C:
			s.EmitSnapshot(exchange, symbol, publishChan)
		}
	}
}

func (s *OrderBookState) EmitClear(exchange, symbol string, publishChan chan<- *event.OrderBookSnapshot) {
	snapshot := event.SnapshotPool.Get().(*event.OrderBookSnapshot)

	snapshot.EventType = event.EventClear
	snapshot.Exchange = exchange
	snapshot.Symbol = symbol
	snapshot.Timestamp = time.Now().UnixMilli()

	select {
	case publishChan <- snapshot:
	default:
		snapshot.Bids = snapshot.Bids[:0]
		snapshot.Asks = snapshot.Asks[:0]
		event.SnapshotPool.Put(snapshot)
	}
}

func UpdateMetric(ctx context.Context, status string) {
	observation.OrderBookEvents.Add(ctx, 1,
		metric.WithAttributes(attribute.String("status", status)),
	)
}
