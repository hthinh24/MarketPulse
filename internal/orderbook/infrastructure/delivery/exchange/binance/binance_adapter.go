package binance

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"MarketPulse/internal/orderbook/service"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type BinanceAdapter struct {
	name                string
	symbolDiscoveryUrl  string
	snapshotUrl         string
	streamUrl           string
	streamBufferSize    int
	deltaQueueSize      int
	retryMaxAttempts    int
	retryInitialDelayMs int
	retryMaxDelayMs     int
	btreeDegree         int
	snapshotQuantity    int

	// Per-symbol state tracking
	mu             sync.RWMutex
	lastUpdateID   map[string]int64
	isSynced       map[string]bool
	deltaQueues    map[string][]event.OrderBookEvent
	statePerSymbol map[string]*service.OrderBookState
}

func NewBinanceAdapter(config *config.ExchangeConfig) *BinanceAdapter {
	return &BinanceAdapter{
		name:                config.Name,
		symbolDiscoveryUrl:  config.SymbolDiscoveryUrl,
		snapshotUrl:         config.SnapshotUrl,
		streamUrl:           config.StreamUrl,
		streamBufferSize:    config.StreamBufferSize,
		deltaQueueSize:      config.DeltaQueueSize,
		retryMaxAttempts:    config.RetryMaxAttempts,
		retryInitialDelayMs: config.RetryInitialDelayMs,
		retryMaxDelayMs:     config.RetryMaxDelayMs,
		btreeDegree:         config.BTreeDegree,
		snapshotQuantity:    config.SnapshotQuantity,

		lastUpdateID:   make(map[string]int64),
		isSynced:       make(map[string]bool),
		deltaQueues:    make(map[string][]event.OrderBookEvent),
		statePerSymbol: make(map[string]*service.OrderBookState),
	}
}

// Start discovers symbols, subscribes to orderbook updates, manages per-symbol state,
// validates sequences, handles resync with exponential backoff, and emits snapshots.
func (b *BinanceAdapter) Start(ctx context.Context, publishChan chan<- *event.OrderBookSnapshot) error {
	log.Printf("Starting BinanceAdapter for exchange: %s", b.name)

	// Discover symbols
	symbols, err := b.discoverSymbols(ctx)
	if err != nil {
		log.Printf("Failed to discover symbols: %v", err)
		return err
	}
	log.Printf("Discovered %d symbols", len(symbols))

	// Initialize per-symbol state
	b.mu.Lock()
	for _, symbol := range symbols {
		b.lastUpdateID[symbol] = 0
		b.isSynced[symbol] = false
		b.deltaQueues[symbol] = make([]event.OrderBookEvent, 0, b.deltaQueueSize)

		state, err := service.NewOrderBookState(b.btreeDegree, b.snapshotQuantity)
		if err != nil {
			b.mu.Unlock()
			log.Printf("Failed to create OrderBookState for symbol %s: %v", symbol, err)
			return err
		}
		b.statePerSymbol[symbol] = state
	}
	b.mu.Unlock()

	// Start emitters for each symbol
	for _, symbol := range symbols {
		go func(sym string) {
			state := b.statePerSymbol[sym]
			state.RunEmitter(ctx, b.name, sym, publishChan)
		}(symbol)
	}

	// Initial resync for all symbols
	for _, symbol := range symbols {
		go b.resyncWithBackoff(ctx, symbol)
	}

	// Subscribe to WebSocket feed and process updates
	mainChan := make(chan event.OrderBookEvent, b.streamBufferSize)
	go b.subscribeOrderBooks(ctx, symbols, mainChan)

	// Process incoming updates
	go b.processUpdates(ctx, mainChan)

	// Wait for context cancellation
	<-ctx.Done()
	log.Printf("BinanceAdapter shutting down gracefully...")
	close(mainChan)
	return nil
}

// discoverSymbols fetches active USDT trading pairs from Binance.
func (b *BinanceAdapter) discoverSymbols(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", b.symbolDiscoveryUrl, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Error fetching exchange info: %v", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Binance Exchange Info API Error: Status %d, Body: %s", resp.StatusCode, string(bodyBytes))
	}

	var info BinanceExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	var symbols []string
	for _, s := range info.Symbols {
		if s.QuoteAsset == "USDT" && s.Status == "TRADING" {
			symbols = append(symbols, s.Symbol)
		}
	}
	return symbols, nil
}

func (b *BinanceAdapter) fetchSnapshot(ctx context.Context, symbol string) (*event.OrderBookEvent, error) {
	url := fmt.Sprintf("%s?symbol=%s&limit=1000", b.snapshotUrl, strings.ToUpper(symbol))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch snapshot for %s: status %d", symbol, resp.StatusCode)
	}

	var snapshot BinanceSnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}

	orderBookEvent := &event.OrderBookEvent{
		Exchange:     b.name,
		Symbol:       symbol,
		IsSnapshot:   true,
		UpdateID:     snapshot.LastUpdateId,
		PrevUpdateID: 0,
		Timestamp:    time.Now().UnixMilli(),
		Bids:         b.convertToOrderLevels(snapshot.Bids),
		Asks:         b.convertToOrderLevels(snapshot.Asks),
	}

	return orderBookEvent, nil
}

// subscribeOrderBooks connects to Binance WebSocket and streams updates.
func (b *BinanceAdapter) subscribeOrderBooks(ctx context.Context, symbols []string, deltaChan chan<- event.OrderBookEvent) {
	chunkSize := 300

	for i := 0; i < len(symbols); i += chunkSize {
		end := i + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}

		chunk := symbols[i:end]
		go b.connectAndListenChunk(ctx, chunk, deltaChan)
	}
}

// connectAndListenChunk handles a chunk of symbols via WebSocket with reconnection.
func (b *BinanceAdapter) connectAndListenChunk(ctx context.Context, chunk []string, deltaChan chan<- event.OrderBookEvent) {
	var streams []string
	for _, symbol := range chunk {
		streams = append(streams, fmt.Sprintf("%s@depth", strings.ToLower(symbol)))
	}
	url := fmt.Sprintf("%s?streams=%s", b.streamUrl, strings.Join(streams, "/"))

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
			if err != nil {
				log.Printf("Failed to connect WebSocket for chunk: %v, retrying in 5s...", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					continue
				}
			}

			b.listenAndProcess(ctx, conn, deltaChan)
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
}

// listenAndProcess reads messages from WebSocket and sends them to deltaChan.
func (b *BinanceAdapter) listenAndProcess(ctx context.Context, conn *websocket.Conn, deltaChan chan<- event.OrderBookEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				return
			}

			orderBookEvent := b.parseBinanceWSMessage(message)
			if orderBookEvent == nil {
				continue
			}

			select {
			case deltaChan <- *orderBookEvent:
			case <-ctx.Done():
				return
			}
		}
	}
}

// processUpdates handles incoming orderbook events: sequences validation, queueing, syncing.
func (b *BinanceAdapter) processUpdates(ctx context.Context, deltaChan <-chan event.OrderBookEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case delta, ok := <-deltaChan:
			if !ok {
				return
			}

			b.handleUpdate(ctx, delta)
		}
	}
}

// handleUpdate applies sequence validation and state management per symbol.
func (b *BinanceAdapter) handleUpdate(ctx context.Context, delta event.OrderBookEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()

	symbol := delta.Symbol
	state := b.statePerSymbol[symbol]
	if state == nil {
		return
	}

	isSynced := b.isSynced[symbol]
	lastUpdateID := b.lastUpdateID[symbol]

	if !isSynced {
		// Not synced: queue deltas until snapshot received
		if len(b.deltaQueues[symbol]) >= b.deltaQueueSize {
			log.Printf("Delta queue overflow for %s, triggering resync...", symbol)
			b.deltaQueues[symbol] = b.deltaQueues[symbol][:0]
			b.isSynced[symbol] = false

			go b.resyncWithBackoff(ctx, symbol)
			return
		}

		b.deltaQueues[symbol] = append(b.deltaQueues[symbol], delta)
		service.UpdateMetric(ctx, "queued")
		return
	}

	// Check for sequence gap
	if delta.PrevUpdateID > lastUpdateID+1 {
		log.Printf("Sequence gap detected for %s: expected %d, got %d", symbol, lastUpdateID+1, delta.PrevUpdateID)
		b.isSynced[symbol] = false
		b.deltaQueues[symbol] = b.deltaQueues[symbol][:0]
		service.UpdateMetric(ctx, "dropped_gap")

		go b.resyncWithBackoff(ctx, symbol)
		return
	}

	// Update is valid, apply it
	state.ApplyUpdate(delta)
	b.lastUpdateID[symbol] = delta.UpdateID
	service.UpdateMetric(ctx, "applied")
}

func (b *BinanceAdapter) resyncWithBackoff(ctx context.Context, symbol string) {
	delay := time.Duration(b.retryInitialDelayMs) * time.Millisecond
	maxDelay := time.Duration(b.retryMaxDelayMs) * time.Millisecond

	for attempt := 0; attempt < b.retryMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		snapshot, err := b.fetchSnapshot(ctx, symbol)
		if err != nil {
			log.Printf("Resync attempt %d for %s failed: %v, retrying...", attempt+1, symbol, err)
			// Exponential backoff: delay *= 2, capped at maxDelay
			delay = time.Duration(math.Min(float64(delay.Milliseconds())*2, float64(maxDelay.Milliseconds()))) * time.Millisecond
			continue
		}

		// Success: apply snapshot and process queued deltas
		b.mu.Lock()
		state := b.statePerSymbol[symbol]
		if state != nil {
			state.ApplySnapshot(*snapshot)
			b.lastUpdateID[symbol] = snapshot.UpdateID
			b.isSynced[symbol] = true

			// Apply queued deltas that are newer than snapshot
			for _, delta := range b.deltaQueues[symbol] {
				if delta.UpdateID > snapshot.UpdateID {
					state.ApplyUpdate(delta)
					b.lastUpdateID[symbol] = delta.UpdateID
				}
			}
			b.deltaQueues[symbol] = b.deltaQueues[symbol][:0]

			log.Printf("Resync succeeded for %s after %d attempt(s)", symbol, attempt+1)
		}
		b.mu.Unlock()
		return
	}

	log.Printf("Resync failed for %s after %d attempts, will retry on next gap detection or initial resync", symbol, b.retryMaxAttempts)
}

func (b *BinanceAdapter) parseBinanceWSMessage(message []byte) *event.OrderBookEvent {
	var binanceDepthUpdateStream BinanceDepthUpdateStream
	if err := json.Unmarshal(message, &binanceDepthUpdateStream); err != nil {
		log.Printf("JSON unmarshal error: %v", err)
		return nil
	}

	binanceDepthUpdate := binanceDepthUpdateStream.Data
	orderBookEvent := &event.OrderBookEvent{
		Exchange:     b.name,
		Symbol:       binanceDepthUpdate.Symbol,
		IsSnapshot:   false,
		UpdateID:     binanceDepthUpdate.FinalUpdateId,
		PrevUpdateID: binanceDepthUpdate.FirstUpdateId,
		Timestamp:    binanceDepthUpdate.EventTime,
		Bids:         b.convertToOrderLevels(binanceDepthUpdate.Bids),
		Asks:         b.convertToOrderLevels(binanceDepthUpdate.Asks),
	}

	return orderBookEvent
}

func (b *BinanceAdapter) convertToOrderLevels(priceSizePairs [][]string) []event.OrderLevel {
	var orderLevels []event.OrderLevel
	for _, pair := range priceSizePairs {
		price, err1 := strconv.ParseFloat(pair[0], 64)
		size, err2 := strconv.ParseFloat(pair[1], 64)
		if err1 != nil || err2 != nil {
			log.Printf("Error parsing price/size: %v, %v", err1, err2)
			continue
		}
		orderLevels = append(orderLevels, event.OrderLevel{
			Price: price,
			Size:  size,
		})
	}
	return orderLevels
}
