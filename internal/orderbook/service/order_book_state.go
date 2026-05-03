package service

import (
	"MarketPulse/internal/orderbook/domain"
	"context"
	"fmt"
	"github.com/google/btree"
	"sync"
	"time"
)

type OrderTree = btree.BTreeG[domain.OrderLevel]

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
		bids:             btree.NewG(btreeDegree, func(a, b domain.OrderLevel) bool { return a.Price < b.Price }),
		asks:             btree.NewG(btreeDegree, func(a, b domain.OrderLevel) bool { return a.Price < b.Price }),
		snapshotQuantity: snapshotQuantity,
	}, nil
}

func (s *OrderBookState) ApplyUpdate(delta domain.OrderBookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, b := range delta.Bids {
		if b.Size == 0 {
			s.bids.Delete(domain.OrderLevel{Price: b.Price})
		} else {
			s.bids.ReplaceOrInsert(domain.OrderLevel{Price: b.Price, Size: b.Size})
		}
	}

	for _, a := range delta.Asks {
		if a.Size == 0 {
			s.asks.Delete(domain.OrderLevel{Price: a.Price})
		} else {
			s.asks.ReplaceOrInsert(domain.OrderLevel{Price: a.Price, Size: a.Size})
		}
	}
}

func (s *OrderBookState) ApplySnapshot(snapshot domain.OrderBookEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bids.Clear(false)
	s.asks.Clear(false)

	for _, b := range snapshot.Bids {
		s.bids.ReplaceOrInsert(domain.OrderLevel{Price: b.Price, Size: b.Size})
	}
	for _, a := range snapshot.Asks {
		s.asks.ReplaceOrInsert(domain.OrderLevel{Price: a.Price, Size: a.Size})
	}
}

func (s *OrderBookState) EmitSnapshot(exchange, symbol string, publishChan chan<- *domain.OrderBookSnapshot) {
	snapshot := domain.SnapshotPool.Get().(*domain.OrderBookSnapshot)

	snapshot.EventType = domain.EventSnapshot
	snapshot.Exchange = exchange
	snapshot.Symbol = symbol
	snapshot.Timestamp = time.Now().UnixMilli()

	s.mu.RLock()
	count := 0
	s.bids.Descend(func(item domain.OrderLevel) bool {
		snapshot.Bids = append(snapshot.Bids, item)
		count++
		return count < s.snapshotQuantity
	})

	count = 0
	s.asks.Ascend(func(item domain.OrderLevel) bool {
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
		domain.SnapshotPool.Put(snapshot)
	}
}

func (s *OrderBookState) RunEmitter(ctx context.Context, exchange, symbol string, publishChan chan<- *domain.OrderBookSnapshot) {
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

func (s *OrderBookState) EmitClear(exchange, symbol string, publishChan chan<- *domain.OrderBookSnapshot) {
	snapshot := domain.SnapshotPool.Get().(*domain.OrderBookSnapshot)

	snapshot.EventType = domain.EventClear
	snapshot.Exchange = exchange
	snapshot.Symbol = symbol
	snapshot.Timestamp = time.Now().UnixMilli()

	select {
	case publishChan <- snapshot:
	default:
		snapshot.Bids = snapshot.Bids[:0]
		snapshot.Asks = snapshot.Asks[:0]
		domain.SnapshotPool.Put(snapshot)
	}
}
