package okx

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
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

type OKXAdapter struct {
	name               string
	symbolDiscoveryUrl string
	streamUrl          string
	btreeDegree        int
	snapshotQuantity   int

	// Per-symbol sequence state tracking
	mu          sync.RWMutex
	lastSeqId   map[string]int64
	isSynced    map[string]bool
	deltaQueues map[string][]OKXOrderBookData

	// Re-subscribe mechanism
	resyncChan chan string // signal symbol needs re-subscribe
	writeMu    sync.Mutex  // protect concurrent WriteJSON
}

func NewOKXAdapter(config *config.ExchangeConfig) *OKXAdapter {
	return &OKXAdapter{
		name:               config.Name,
		symbolDiscoveryUrl: config.SymbolDiscoveryUrl,
		streamUrl:          config.StreamUrl,
		btreeDegree:        config.BTreeDegree,
		snapshotQuantity:   config.SnapshotQuantity,

		lastSeqId:   make(map[string]int64),
		isSynced:    make(map[string]bool),
		deltaQueues: make(map[string][]OKXOrderBookData),

		resyncChan: make(chan string, 100),
	}
}

// Start discovers symbols, subscribes to orderbook updates, manages per-symbol state,
// validates sequences, and handles snapshot/delta flow specific to OKX protocol.
// Includes re-subscribe mechanism on gap detection and heartbeat for connection keep-alive.
func (o *OKXAdapter) Start(ctx context.Context, publishChan chan<- *event.OrderBookSnapshot) error {
	log.Printf("Starting OKXAdapter for exchange: %s", o.name)

	// Discover symbols
	symbols, err := o.discoverSymbols(ctx)
	if err != nil {
		log.Printf("Failed to discover symbols: %v", err)
		return err
	}
	log.Printf("Discovered %d symbols on %s", len(symbols), o.name)

	// Initialize per-symbol state
	o.mu.Lock()
	statePerSymbol := make(map[string]*service.OrderBookState)
	for _, symbol := range symbols {
		o.lastSeqId[symbol] = 0
		o.isSynced[symbol] = false
		o.deltaQueues[symbol] = make([]OKXOrderBookData, 0, 100)

		state, err := service.NewOrderBookState(o.btreeDegree, o.snapshotQuantity)
		if err != nil {
			o.mu.Unlock()
			log.Printf("Failed to create OrderBookState for symbol %s: %v", symbol, err)
			return err
		}
		statePerSymbol[symbol] = state
	}
	o.mu.Unlock()

	// Start emitters for each symbol
	for _, symbol := range symbols {
		go func(sym string) {
			state := statePerSymbol[sym]
			state.RunEmitter(ctx, o.name, sym, publishChan)
		}(symbol)
	}

	// Chunk symbols into groups (OKX allows ~10 channels per connection)
	chunkSize := 10
	for i := 0; i < len(symbols); i += chunkSize {
		end := i + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}

		chunk := symbols[i:end]
		go o.connectAndListen(ctx, chunk, statePerSymbol)
	}

	// Wait for context cancellation
	<-ctx.Done()
	log.Printf("OKXAdapter shutting down gracefully...")
	return nil
}

// discoverSymbols fetches active USDT spot trading pairs from OKX.
func (o *OKXAdapter) discoverSymbols(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", o.symbolDiscoveryUrl, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OKX Instruments API Error: Status %d", resp.StatusCode)
	}

	var respData OKXInstrumentResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return nil, err
	}

	// OKX returns code as string "0" for success
	if respData.Code != "0" {
		return nil, fmt.Errorf("OKX API returned error code: %s, msg: %s", respData.Code, respData.Msg)
	}

	var symbols []string
	for _, instrument := range respData.Data {
		if instrument.QuoteCcy == "USDT" && instrument.State == "live" {
			symbols = append(symbols, instrument.InstId)
		}
	}
	return symbols, nil
}

// connectAndListen connects to OKX WebSocket and handles subscription with re-subscribe on gap.
// Runs heartbeat, re-subscribe, and message listening in parallel on same connection.
func (o *OKXAdapter) connectAndListen(ctx context.Context, symbols []string, statePerSymbol map[string]*service.OrderBookState) {
	// Build instId map: instId → OKXWSArg object
	instIdMap := make(map[string]OKXWSArg)
	for _, symbol := range symbols {
		instIdMap[symbol] = OKXWSArg{Channel: "books", InstId: symbol}
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.DialContext(ctx, o.streamUrl, nil)
		if err != nil {
			log.Printf("Failed to connect OKX WebSocket: %v, retrying in 5s...", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		// Subscribe all symbols for this chunk
		args := make([]OKXWSArg, 0, len(instIdMap))
		for _, arg := range instIdMap {
			args = append(args, arg)
		}
		if err := o.sendSubscribe(conn, args); err != nil {
			log.Printf("Failed to subscribe: %v", err)
			conn.Close()
			continue
		}

		// Heartbeat goroutine — send ping every 25s (OKX closes after 30s inactivity)
		stopHeartbeat := make(chan struct{})
		go func() {
			ticker := time.NewTicker(25 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stopHeartbeat:
					return
				case <-ticker.C:
					o.writeMu.Lock()
					conn.WriteMessage(websocket.TextMessage, []byte("ping"))
					o.writeMu.Unlock()
				}
			}
		}()

		// Re-subscribe goroutine — handles gap recovery
		stopResync := make(chan struct{})
		go func() {
			defer close(stopResync)
			for {
				select {
				case <-ctx.Done():
					return
				case instId, ok := <-o.resyncChan:
					if !ok {
						return
					}
					_, exists := instIdMap[instId]
					if !exists {
						// Symbol not in this chunk, ignore
						continue
					}
					log.Printf("Re-subscribing OKX: %s", instId)

					// Unsubscribe
					if err := o.sendUnsubscribe(conn, instId); err != nil {
						log.Printf("Failed to unsubscribe %s: %v", instId, err)
						return // connection broken, trigger reconnect
					}

					// Subscribe again
					if err := o.sendSubscribe(conn, []OKXWSArg{{Channel: "books", InstId: instId}}); err != nil {
						log.Printf("Failed to re-subscribe %s: %v", instId, err)
						return
					}
				}
			}
		}()

		// Listen and process messages until connection dies
		o.listenAndProcess(ctx, conn, statePerSymbol)

		close(stopHeartbeat)
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
func (o *OKXAdapter) sendSubscribe(conn *websocket.Conn, args []OKXWSArg) error {
	msg := OKXWSCommandMessage{
		Op:   "subscribe",
		Args: args,
	}
	o.writeMu.Lock()
	defer o.writeMu.Unlock()
	return conn.WriteJSON(msg)
}

// sendUnsubscribe sends an unsubscribe message with mutex protection.
func (o *OKXAdapter) sendUnsubscribe(conn *websocket.Conn, instId string) error {
	msg := OKXWSCommandMessage{
		Op: "unsubscribe",
		Args: []OKXWSArg{
			{Channel: "books", InstId: instId},
		},
	}
	o.writeMu.Lock()
	defer o.writeMu.Unlock()
	return conn.WriteJSON(msg)
}

// listenAndProcess reads WebSocket messages and dispatches to handlers.
func (o *OKXAdapter) listenAndProcess(ctx context.Context, conn *websocket.Conn, statePerSymbol map[string]*service.OrderBookState) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Read raw message to handle both text (pong) and JSON (data)
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				return
			}

			// Handle heartbeat response
			if msgType == websocket.TextMessage && string(data) == "pong" {
				continue
			}

			// Parse JSON message
			var msg OKXWSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("JSON unmarshal error: %v", err)
				continue
			}

			// Handle subscription/unsubscription acks
			if msg.Event == "subscribe" || msg.Event == "unsubscribe" {
				continue
			}

			// Handle error events
			if msg.Event == "error" || msg.Code != "" && msg.Code != "0" {
				log.Printf("OKX error: code=%s msg=%s", msg.Code, msg.Msg)
				continue
			}

			// Skip if no data
			if len(msg.Data) == 0 {
				continue
			}

			instId := msg.Arg.InstId
			state, exists := statePerSymbol[instId]
			if !exists {
				continue
			}

			// Dispatch to handlers (OKX sends data as array with single element for books)
			switch msg.Action {
			case "snapshot":
				o.handleSnapshot(instId, msg.Data[0], state)
			case "update":
				o.handleDelta(instId, msg.Data[0], state)
			}
		}
	}
}

// handleSnapshot processes a snapshot message from OKX.
func (o *OKXAdapter) handleSnapshot(instId string, data OKXOrderBookData, state *service.OrderBookState) {
	o.mu.Lock()
	defer o.mu.Unlock()

	ev := event.OrderBookEvent{
		Exchange:     o.name,
		Symbol:       instId,
		IsSnapshot:   true,
		UpdateID:     data.SeqId,
		PrevUpdateID: 0,
		Timestamp:    time.Now().UnixMilli(),
		Bids:         o.convertToOrderLevels(data.Bids),
		Asks:         o.convertToOrderLevels(data.Asks),
	}

	state.ApplySnapshot(ev)

	o.lastSeqId[instId] = data.SeqId
	o.isSynced[instId] = true

	// Apply any queued deltas that are newer than this snapshot
	if queued, exists := o.deltaQueues[instId]; exists && len(queued) > 0 {
		for _, delta := range queued {
			if delta.SeqId > data.SeqId {
				deltaEv := event.OrderBookEvent{
					Exchange:     o.name,
					Symbol:       instId,
					IsSnapshot:   false,
					UpdateID:     delta.SeqId,
					PrevUpdateID: delta.SeqId - 1,
					Timestamp:    time.Now().UnixMilli(),
					Bids:         o.convertToOrderLevels(delta.Bids),
					Asks:         o.convertToOrderLevels(delta.Asks),
				}
				state.ApplyUpdate(deltaEv)
				o.lastSeqId[instId] = delta.SeqId
			}
		}
		o.deltaQueues[instId] = o.deltaQueues[instId][:0]
	}

	log.Printf("Snapshot applied for %s (SeqId: %d)", instId, data.SeqId)
}

// handleDelta processes delta (update) messages with gap detection via prevSeqId.
func (o *OKXAdapter) handleDelta(instId string, data OKXOrderBookData, state *service.OrderBookState) {
	o.mu.Lock()
	defer o.mu.Unlock()

	isSynced := o.isSynced[instId]
	lastSeqId := o.lastSeqId[instId]

	if !isSynced {
		// Not synced: queue delta until snapshot received
		o.deltaQueues[instId] = append(o.deltaQueues[instId], data)
		service.UpdateMetric(context.Background(), "queued")
		return
	}

	// OKX gap detection: prevSeqId should match lastSeqId
	if data.PrevSeqId != lastSeqId {
		log.Printf("Sequence gap detected for %s: expected prevSeqId=%d, got %d", instId, lastSeqId, data.PrevSeqId)
		o.isSynced[instId] = false
		o.deltaQueues[instId] = o.deltaQueues[instId][:0]
		service.UpdateMetric(context.Background(), "dropped_gap")

		// Signal re-subscribe to trigger snapshot
		select {
		case o.resyncChan <- instId:
		default:
		}
		return
	}

	ev := event.OrderBookEvent{
		Exchange:     o.name,
		Symbol:       instId,
		IsSnapshot:   false,
		UpdateID:     data.SeqId,
		PrevUpdateID: data.PrevSeqId,
		Timestamp:    time.Now().UnixMilli(),
		Bids:         o.convertToOrderLevels(data.Bids),
		Asks:         o.convertToOrderLevels(data.Asks),
	}

	state.ApplyUpdate(ev)
	o.lastSeqId[instId] = data.SeqId
	service.UpdateMetric(context.Background(), "applied")
}

// convertToOrderLevels converts OKX price/size pairs to OrderLevel structs.
// OKX returns [price, size, deprecated, numOrders] — we only use first two.
func (o *OKXAdapter) convertToOrderLevels(levels [][]string) []event.OrderLevel {
	var orderLevels []event.OrderLevel
	for _, level := range levels {
		if len(level) < 2 {
			continue
		}
		price, err1 := strconv.ParseFloat(level[0], 64)
		size, err2 := strconv.ParseFloat(level[1], 64)
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
