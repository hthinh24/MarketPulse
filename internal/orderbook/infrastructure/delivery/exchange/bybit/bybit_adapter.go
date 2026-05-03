package bybit

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type BybitAdapter struct {
	name               string
	symbolDiscoveryUrl string
	snapshotUrl        string
	streamUrl          string
	btreeDegree        int
	snapshotQuantity   int

	// Per-symbol sequence state tracking
	mu           sync.RWMutex
	lastUpdateID map[string]int64
	isSynced     map[string]bool
	deltaQueues  map[string][]event.EventEnvelope

	resyncChan chan string
	writeMu    sync.Mutex
}

func NewBybitAdapter(config *config.ExchangeConfig) *BybitAdapter {
	return &BybitAdapter{
		name:               config.Name,
		symbolDiscoveryUrl: config.SymbolDiscoveryUrl,
		snapshotUrl:        config.SnapshotUrl,
		streamUrl:          config.StreamUrl,
		btreeDegree:        config.BTreeDegree,
		snapshotQuantity:   config.SnapshotQuantity,

		lastUpdateID: make(map[string]int64),
		isSynced:     make(map[string]bool),
		deltaQueues:  make(map[string][]event.EventEnvelope),

		resyncChan: make(chan string, 100),
	}
}

// Start discovers symbols, subscribes to orderbook updates, manages per-symbol state,
// validates sequences, and handles snapshot/delta flow specific to Bybit protocol.
// Includes re-subscribe mechanism on gap detection.
func (b *BybitAdapter) Start(ctx context.Context, publishChan chan<- *domain.OrderBookSnapshot) error {
	log.Printf("Starting BybitAdapter for exchange: %s", b.name)

	// Discover symbols
	symbols, err := b.discoverSymbols(ctx)
	if err != nil {
		log.Printf("Failed to discover symbols: %v", err)
		return err
	}
	log.Printf("Discovered %d symbols on %s", len(symbols), b.name)

	// Initialize per-symbol state
	b.mu.Lock()
	statePerSymbol := make(map[string]*service.OrderBookState)
	for _, symbol := range symbols {
		b.lastUpdateID[symbol] = 0
		b.isSynced[symbol] = false
		b.deltaQueues[symbol] = make([]event.EventEnvelope, 0, 100)

		state, err := service.NewOrderBookState(b.btreeDegree, b.snapshotQuantity)
		if err != nil {
			b.mu.Unlock()
			log.Printf("Failed to create OrderBookState for symbol %s: %v", symbol, err)
			return err
		}
		statePerSymbol[symbol] = state
	}
	b.mu.Unlock()

	// Start emitters for each symbol
	for _, symbol := range symbols {
		go func(sym string) {
			state := statePerSymbol[sym]
			state.RunEmitter(ctx, b.name, sym, publishChan)
		}(symbol)
	}

	// Chunk symbols into groups (Bybit allows max ~10 topics per connection)
	chunkSize := 10
	for i := 0; i < len(symbols); i += chunkSize {
		end := i + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}

		chunk := symbols[i:end]
		go b.connectAndListen(ctx, chunk, statePerSymbol)
	}

	// Wait for context cancellation
	<-ctx.Done()
	log.Printf("BybitAdapter shutting down gracefully...")
	return nil
}

// discoverSymbols fetches active USDT trading pairs from Bybit.
func (b *BybitAdapter) discoverSymbols(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.symbolDiscoveryUrl, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Bybit Instruments API Error: Status %d", resp.StatusCode)
	}

	var respData BybitInstrumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, err
	}

	if respData.RetCode != 0 {
		return nil, fmt.Errorf("Bybit API returned error code: %d", respData.RetCode)
	}

	var symbols []string
	for _, instrument := range respData.Result.List {
		if instrument.QuoteCoin == "USDT" && instrument.Status == "Trading" {
			symbols = append(symbols, instrument.Symbol)
		}
	}
	return symbols, nil
}

// connectAndListen connects to Bybit WebSocket and handles subscription with re-subscribe on gap.
// Runs re-subscribe and message listening in parallel goroutines on same connection.
func (b *BybitAdapter) connectAndListen(ctx context.Context, symbols []string, statePerSymbol map[string]*service.OrderBookState) {
	topicMap := make(map[string]string)
	for _, symbol := range symbols {
		topicMap[symbol] = fmt.Sprintf("orderbook.50.%s", symbol)
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, b.streamUrl, nil)
		if err != nil {
			log.Printf("Failed to connect Bybit WebSocket: %v, retrying in 5s...", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Subscribe all topics for this chunk
		topics := make([]string, 0, len(topicMap))
		for _, topic := range topicMap {
			topics = append(topics, topic)
		}
		if err := b.sendSubscribe(conn, topics); err != nil {
			log.Printf("Failed to subscribe: %v", err)
			conn.Close()
			continue
		}

		// Goroutine handles re-subscribe requests from resyncChan
		stopResync := make(chan struct{})
		go func() {
			defer close(stopResync)
			for {
				select {
				case <-ctx.Done():
					return
				case symbol, ok := <-b.resyncChan:
					if !ok {
						return
					}
					topic, exists := topicMap[symbol]
					if !exists {
						// Symbol not in this chunk, ignore
						continue
					}
					log.Printf("Re-subscribing topic: %s", topic)

					// Unsubscribe
					b.writeMu.Lock()
					unsubMsg := BybitWsCommandMessage{
						Op:   "subscribe",
						Args: []string{topic},
					}
					err := conn.WriteJSON(unsubMsg)
					b.writeMu.Unlock()

					if err != nil {
						log.Printf("Failed to unsubscribe %s: %v", topic, err)
						return
					}

					// Subscribe again
					if err := b.sendSubscribe(conn, []string{topic}); err != nil {
						log.Printf("Failed to re-subscribe %s: %v", topic, err)
						return
					}
				}
			}
		}()

		b.listenAndProcess(ctx, conn, statePerSymbol)
		conn.Close()

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			continue
		}
	}
}

// sendSubscribe sends a subscribe message with mutex protection.
func (b *BybitAdapter) sendSubscribe(conn *websocket.Conn, topics []string) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()

	msg := BybitWsCommandMessage{
		Op:   "subscribe",
		Args: topics,
	}

	return conn.WriteJSON(msg)
}

// listenAndProcess reads WebSocket messages and dispatches to handlers.
func (b *BybitAdapter) listenAndProcess(ctx context.Context, conn *websocket.Conn, statePerSymbol map[string]*service.OrderBookState) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg BybitWSOrderBookMessage
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("WebSocket read error: %v", err)
				return
			}

			state, exists := statePerSymbol[msg.Data.S]
			if !exists {
				continue
			}

			payload := domain.OrderBookEvent{
				Exchange:     b.name,
				Symbol:       msg.Data.S,
				IsSnapshot:   msg.Type == "snapshot",
				UpdateID:     msg.Data.U,
				PrevUpdateID: msg.Data.U - 1,
				Timestamp:    time.Now().UnixMilli(),
				Bids:         b.convertToOrderLevels(msg.Data.B),
				Asks:         b.convertToOrderLevels(msg.Data.A),
			}

			envelope := event.EventEnvelope{
				ReceivedAt: time.Now(),
				Payload:    payload,
			}

			switch msg.Type {
			case "snapshot":
				b.handleSnapshot(ctx, envelope, state)
			case "delta":
				b.handleDelta(ctx, envelope, state)
			}
		}
	}
}

// handleSnapshot processes a snapshot message from Bybit.
func (b *BybitAdapter) handleSnapshot(ctx context.Context, envelope event.EventEnvelope, state *service.OrderBookState) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delta := envelope.Payload
	symbol := delta.Symbol

	state.ApplySnapshot(delta)

	b.lastUpdateID[symbol] = delta.UpdateID
	b.isSynced[symbol] = true

	if queued, exists := b.deltaQueues[symbol]; exists && len(queued) > 0 {
		for _, queuedEnvelope := range queued {
			queuedDelta := queuedEnvelope.Payload
			if queuedDelta.UpdateID > delta.UpdateID {
				state.ApplyUpdate(queuedDelta)
				b.lastUpdateID[symbol] = queuedDelta.UpdateID
			}
		}
		b.deltaQueues[symbol] = b.deltaQueues[symbol][:0]
	}

	observation.SymbolSynced(ctx, b.name)
	log.Printf("Snapshot applied for %s (UpdateID: %d)", symbol, delta.UpdateID)
}

// handleDelta processes delta messages with gap detection and re-subscribe signaling.
func (b *BybitAdapter) handleDelta(ctx context.Context, envelope event.EventEnvelope, state *service.OrderBookState) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delta := envelope.Payload
	symbol := delta.Symbol
	isSynced := b.isSynced[symbol]
	lastUpdateID := b.lastUpdateID[symbol]

	if !isSynced {
		// Not synced: queue delta until snapshot received
		b.deltaQueues[symbol] = append(b.deltaQueues[symbol], envelope)
		observation.RecordEvent(ctx, b.name, "queued")
		return
	}

	// Service restart signal
	if delta.UpdateID == 1 {
		log.Printf("Service restart signal received for %s (u=1)", symbol)
		b.isSynced[symbol] = false
		b.deltaQueues[symbol] = b.deltaQueues[symbol][:0]
		observation.RecordEvent(ctx, b.name, "dropped_service_restart")

		select {
		case b.resyncChan <- symbol:
		default:
		}
		return
	}

	// Check for sequence gap
	if delta.PrevUpdateID > lastUpdateID+1 {
		log.Printf("Sequence gap detected for %s: expected %d, got %d", symbol, lastUpdateID+1, delta.PrevUpdateID)
		b.isSynced[symbol] = false
		b.deltaQueues[symbol] = b.deltaQueues[symbol][:0]
		observation.RecordEvent(ctx, b.name, "dropped_gap")
		observation.SymbolGapped(ctx, b.name)

		select {
		case b.resyncChan <- symbol:
		default:
		}
		return
	}

	state.ApplyUpdate(delta)
	b.lastUpdateID[symbol] = delta.UpdateID
	observation.RecordEvent(ctx, b.name, "applied")
	observation.SampleLatency(ctx, b.name, time.Since(envelope.ReceivedAt))
}

// convertToOrderLevels converts string price/size pairs to OrderLevel structs.
func (b *BybitAdapter) convertToOrderLevels(priceSizePairs [][]string) []domain.OrderLevel {
	var orderLevels []domain.OrderLevel
	for _, pair := range priceSizePairs {
		if len(pair) < 2 {
			continue
		}
		price, err1 := strconv.ParseFloat(pair[0], 64)
		size, err2 := strconv.ParseFloat(pair[1], 64)
		if err1 != nil || err2 != nil {
			log.Printf("Error parsing price/size: %v, %v", err1, err2)
			continue
		}
		orderLevels = append(orderLevels, domain.OrderLevel{
			Price: price,
			Size:  size,
		})
	}
	return orderLevels
}
