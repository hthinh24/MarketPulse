package binance

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type BinanceAdapter struct {
	name                   string
	symbolDiscoveryUrl     string
	snapshotUrl            string
	streamUrl              string
	streamBufferSize       int
	symbolWorkerBufferSize int
	deltaQueueSize         int
	retryMaxAttempts       int
	retryInitialDelayMs    int
	retryMaxDelayMs        int
	btreeDegree            int
	snapshotQuantity       int
}

func NewBinanceAdapter(config *config.ExchangeConfig) *BinanceAdapter {
	return &BinanceAdapter{
		name:                   config.Name,
		symbolDiscoveryUrl:     config.SymbolDiscoveryUrl,
		snapshotUrl:            config.SnapshotUrl,
		streamUrl:              config.StreamUrl,
		streamBufferSize:       config.StreamBufferSize,
		symbolWorkerBufferSize: config.StreamBufferSize,
		deltaQueueSize:         config.DeltaQueueSize,

		retryMaxAttempts:    config.RetryMaxAttempts,
		retryInitialDelayMs: config.RetryInitialDelayMs,
		retryMaxDelayMs:     config.RetryMaxDelayMs,

		btreeDegree:      config.BTreeDegree,
		snapshotQuantity: config.SnapshotQuantity,
	}
}

// Start discovers symbols, creates per-symbol workers, subscribes to WebSocket feed,
// dispatches events to workers, and handles resync requests from workers.
func (b *BinanceAdapter) Start(ctx context.Context, publishChan chan<- *domain.OrderBookSnapshot) error {
	log.Printf("Starting BinanceAdapter for exchange: %s", b.name)

	// Discover symbols
	symbols, err := b.discoverSymbols(ctx)
	if err != nil {
		log.Printf("Failed to discover symbols: %v", err)
		return err
	}
	log.Printf("Discovered %d symbols on %s", len(symbols), b.name)

	// resyncChan: workers signal dispatcher which symbol needs a fresh snapshot
	resyncChan := make(chan string, len(symbols))

	// Create one worker + one channel per symbol to maintain their own order book state
	workerChans := make(map[string]chan event.EventEnvelope, len(symbols))
	for _, symbol := range symbols {
		state, err := service.NewOrderBookState(b.btreeDegree, b.snapshotQuantity)
		if err != nil {
			log.Printf("Failed to create OrderBookState for symbol %s: %v", symbol, err)
			return err
		}

		ch := make(chan event.EventEnvelope, b.symbolWorkerBufferSize)
		workerChans[symbol] = ch

		worker := newBinanceSymbolWorker(b.name, symbol, b.deltaQueueSize, state, ch, resyncChan)
		go worker.run(ctx, publishChan)
	}

	// Initial resync — staggered to avoid HTTP burst toward Binance
	for i, symbol := range symbols {
		go func(sym string, idx int) {
			jitter := time.Duration(idx) * 20 * time.Millisecond
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return
			}
			b.resyncWithBackoff(ctx, sym, workerChans[sym])
		}(symbol, i)
	}

	// Subscribe to WebSocket feed and process updates
	mainChan := make(chan event.EventEnvelope, b.streamBufferSize)
	go b.subscribeOrderBooks(ctx, symbols, mainChan)

	// Dispatch incoming events to workers, handle resync requests from workers
	go b.dispatch(ctx, mainChan, workerChans, resyncChan)

	// Wait for context cancellation
	<-ctx.Done()
	log.Printf("BinanceAdapter shutting down gracefully...")
	close(mainChan)
	return nil
}

// dispatch routes WS events to the correct worker and handles resync requests.
func (b *BinanceAdapter) dispatch(
	ctx context.Context,
	mainChan <-chan event.EventEnvelope,
	workerChans map[string]chan event.EventEnvelope,
	resyncChan <-chan string,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case envelope, ok := <-mainChan:
			if !ok {
				return
			}
			sym := envelope.Payload.Symbol
			if ch, exists := workerChans[sym]; exists {
				select {
				case ch <- envelope:
				default:
					observation.RecordEvent(ctx, b.name, "dropped_queue_full")
					log.Printf("Warning: Dropping order book event for %s due to full channel buffer", sym)
				}
			}
		case symbol := <-resyncChan:
			if ch, exists := workerChans[symbol]; exists {
				go b.resyncWithBackoff(ctx, symbol, ch)
			}
		}
	}
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

// subscribeOrderBooks connects to Binance WebSocket and streams updates.
func (b *BinanceAdapter) subscribeOrderBooks(ctx context.Context, symbols []string, deltaChan chan<- event.EventEnvelope) {
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
func (b *BinanceAdapter) connectAndListenChunk(ctx context.Context, chunk []string, deltaChan chan<- event.EventEnvelope) {
	var streams []string
	for _, symbol := range chunk {
		streams = append(streams, fmt.Sprintf("%s@depth@100ms", strings.ToLower(symbol)))
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
func (b *BinanceAdapter) listenAndProcess(ctx context.Context, conn *websocket.Conn, deltaChan chan<- event.EventEnvelope) {
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

			payload := b.parseBinanceWSMessage(message)
			if payload == nil {
				continue
			}

			orderbookEvent := event.EventEnvelope{
				ReceivedAt: time.Now(),
				Payload:    *payload,
			}

			select {
			case deltaChan <- orderbookEvent:
			case <-ctx.Done():
				return
			}
		}
	}
}

// resyncWithBackoff fetches a snapshot and pushes it into the worker channel.
// Handles Binance rate limit (429/418) base on Retry-After header.
func (b *BinanceAdapter) resyncWithBackoff(ctx context.Context, symbol string, workerChan chan<- event.EventEnvelope) {
	delay := time.Duration(b.retryInitialDelayMs) * time.Millisecond
	maxDelay := time.Duration(b.retryMaxDelayMs) * time.Millisecond

	for attempt := 0; attempt < b.retryMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		snapshot, err := b.fetchSnapshot(ctx, symbol)

		var rateLimitErr *RateLimitError
		if errors.As(err, &rateLimitErr) {
			log.Printf("Exchange: %s Rate limit hit while fetching snapshot for %s: retrying after %v", b.name, symbol, rateLimitErr.RetryAfter)
			select {
			case <-time.After(rateLimitErr.RetryAfter):
			case <-ctx.Done():
				return
			}
			attempt = 0
			continue
		} else if err != nil {
			log.Printf("Resync attempt %d for %s failed: %v, retrying...", attempt+1, symbol, err)
			// Exponential backoff: delay *= 2, capped at maxDelay
			delay = time.Duration(math.Min(float64(delay.Milliseconds())*2, float64(maxDelay.Milliseconds()))) * time.Millisecond
			continue
		}

		envelope := event.EventEnvelope{
			ReceivedAt: time.Now(),
			Payload:    *snapshot,
		}
		select {
		case workerChan <- envelope:
			log.Printf("Resync succeeded for %s after %d attempt(s)", symbol, attempt+1)
		case <-ctx.Done():
			return
		}
		return
	}

	log.Printf("Resync failed for %s after %d attempts, will retry on next gap detection or initial resync", symbol, b.retryMaxAttempts)
}

func (b *BinanceAdapter) fetchSnapshot(ctx context.Context, symbol string) (*domain.OrderBookEvent, error) {
	url := fmt.Sprintf("%s?symbol=%s&limit=100", b.snapshotUrl, strings.ToUpper(symbol))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == 418 {
		retryAfter := resp.Header.Get("Retry-After")
		wait := 60 * time.Second // default fallback
		if retryAfter != "" {
			if secs, err := strconv.Atoi(retryAfter); err == nil {
				wait = time.Duration(secs) * time.Second
			}
		}
		return nil, &RateLimitError{RetryAfter: wait}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch snapshot for %s: status %d", symbol, resp.StatusCode)
	}

	var snapshot BinanceSnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}

	orderBookEvent := &domain.OrderBookEvent{
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

func (b *BinanceAdapter) parseBinanceWSMessage(message []byte) *domain.OrderBookEvent {
	var binanceDepthUpdateStream BinanceDepthUpdateStream
	if err := json.Unmarshal(message, &binanceDepthUpdateStream); err != nil {
		log.Printf("JSON unmarshal error: %v", err)
		return nil
	}

	binanceDepthUpdate := binanceDepthUpdateStream.Data
	orderBookEvent := &domain.OrderBookEvent{
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

func (b *BinanceAdapter) convertToOrderLevels(priceSizePairs [][]string) []domain.OrderLevel {
	var orderLevels []domain.OrderLevel
	for _, pair := range priceSizePairs {
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
