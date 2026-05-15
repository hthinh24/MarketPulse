package okx

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/domain"
	"MarketPulse/internal/orderbook/infrastructure/delivery/event"
	"MarketPulse/internal/orderbook/infrastructure/observation"
	"MarketPulse/internal/orderbook/service"
	"MarketPulse/pkg/logger"
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
	log                    *logger.Logger
	name                   string
	symbolDiscoveryUrl     string
	streamUrl              string
	streamBufferSize       int
	symbolWorkerBufferSize int
	deltaQueueSize         int
	btreeDegree            int
	snapshotQuantity       int
	writeMu                sync.Mutex // protect concurrent WriteJSON
}

func NewOKXAdapter(log *logger.Logger, config *config.ExchangeConfig) *OKXAdapter {
	return &OKXAdapter{
		log:                    log,
		name:                   config.Name,
		symbolDiscoveryUrl:     config.SymbolDiscoveryUrl,
		streamUrl:              config.StreamUrl,
		streamBufferSize:       config.StreamBufferSize,
		symbolWorkerBufferSize: config.StreamBufferSize,
		deltaQueueSize:         config.DeltaQueueSize,
		btreeDegree:            config.BTreeDegree,
		snapshotQuantity:       config.SnapshotQuantity,
	}
}

// Start discovers symbols, creates per-symbol workers, subscribes to WebSocket feed,
// dispatches events to workers, and handles re-subscribe requests from workers.
func (o *OKXAdapter) Start(ctx context.Context, publishChan chan<- *domain.OrderBookSnapshot) error {
	o.log.Info(ctx, "starting okx adapter", logger.String("exchange", o.name))

	// Discover symbols
	symbols, err := o.discoverSymbols(ctx)
	if err != nil {
		o.log.Error(ctx, "failed to discover symbols", err)
		return err
	}
	o.log.Info(ctx, "discovered symbols", logger.Int("count", len(symbols)), logger.String("exchange", o.name))

	// resyncChan: workers signal dispatcher which symbol needs re-subscription
	resyncChan := make(chan string, len(symbols))

	// Create one worker + one channel per symbol to maintain their own order book state
	workerChans := make(map[string]chan event.EventEnvelope, len(symbols))
	for _, symbol := range symbols {
		state, err := service.NewOrderBookState(o.btreeDegree, o.snapshotQuantity)
		if err != nil {
			o.log.Error(ctx, "failed to create orderbook state for symbol", err, logger.String("symbol", symbol))
			return err
		}

		ch := make(chan event.EventEnvelope, o.symbolWorkerBufferSize)
		workerChans[symbol] = ch

		worker := newOKXSymbolWorker(o.log, o.name, symbol, o.deltaQueueSize, state, ch, resyncChan)
		go worker.run(ctx, publishChan)
	}

	// Subscribe to WebSocket feed and process updates
	mainChan := make(chan event.EventEnvelope, o.streamBufferSize)

	// Chunk symbols into groups (OKX allows ~10 channels per connection)
	chunkSize := 10
	for i := 0; i < len(symbols); i += chunkSize {
		end := i + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}

		chunk := symbols[i:end]

		// Wait 20ms increments between goroutines
		go func(idx int) {
			jitter := time.Duration(idx) * 20 * time.Millisecond
			select {
			case <-time.After(jitter):
			case <-ctx.Done():
				return
			}
			o.connectAndListen(ctx, chunk, mainChan, resyncChan)
		}(i / chunkSize)
	}

	// Dispatch incoming events to workers
	go o.dispatch(ctx, mainChan, workerChans)

	// Wait for context cancellation
	<-ctx.Done()
	o.log.Info(ctx, "okx adapter shutting down gracefully")
	close(mainChan)
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

// dispatch routes WS events to the correct worker.
func (o *OKXAdapter) dispatch(
	ctx context.Context,
	mainChan <-chan event.EventEnvelope,
	workerChans map[string]chan event.EventEnvelope,
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
					observation.RecordEvent(ctx, o.name, "dropped_queue_full")
					o.log.Warn(ctx, "dropping order book event due to full channel buffer", logger.String("symbol", sym))
				}
			}
		}
	}
}

// connectAndListen connects to OKX WebSocket and handles subscription with re-subscribe on gap.
// Runs heartbeat, re-subscribe, and message listening in parallel on same connection.
func (o *OKXAdapter) connectAndListen(ctx context.Context, symbols []string, mainChan chan<- event.EventEnvelope, resyncChan <-chan string) {
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
			o.log.Error(ctx, "failed to connect okx websocket", err, logger.Duration("retry_after", 5*time.Second))
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
			o.log.Error(ctx, "failed to subscribe", err)
			conn.Close()
			continue
		}

		// Heartbeat goroutine — send ping every 25s (OKX closes after 30s inactivity)
		// Runs independently, doesn't need resyncChan
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
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case instId, ok := <-resyncChan:
					if !ok {
						return
					}
					_, exists := instIdMap[instId]
					if !exists {
						// Symbol not in this chunk, ignore
						continue
					}
					o.log.Info(ctx, "re-subscribing okx", logger.String("instId", instId))

					// Unsubscribe
					if err := o.sendUnsubscribe(conn, instId); err != nil {
						o.log.Error(ctx, "failed to unsubscribe", err, logger.String("instId", instId))
						return // connection broken, trigger reconnect
					}

					// Subscribe again
					if err := o.sendSubscribe(conn, []OKXWSArg{{Channel: "books", InstId: instId}}); err != nil {
						o.log.Error(ctx, "failed to re-subscribe", err, logger.String("instId", instId))
						return
					}
				}
			}
		}()

		// Listen and process messages until connection dies
		o.listenAndProcess(ctx, conn, mainChan)

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

// listenAndProcess reads WebSocket messages and sends them to mainChan as envelopes.
func (o *OKXAdapter) listenAndProcess(ctx context.Context, conn *websocket.Conn, mainChan chan<- event.EventEnvelope) {
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

			payload := domain.OrderBookEvent{
				Exchange:     o.name,
				Symbol:       instId,
				IsSnapshot:   msg.Action == "snapshot",
				UpdateID:     msg.Data[0].SeqId,
				PrevUpdateID: msg.Data[0].PrevSeqId,
				Timestamp:    time.Now().UnixMilli(),
				Bids:         o.convertToOrderLevels(msg.Data[0].Bids),
				Asks:         o.convertToOrderLevels(msg.Data[0].Asks),
			}

			envelope := event.EventEnvelope{
				ReceivedAt: time.Now(),
				Payload:    payload,
			}

			select {
			case mainChan <- envelope:
			case <-ctx.Done():
				return
			}
		}
	}
}

// convertToOrderLevels converts OKX price/size pairs to OrderLevel structs.
// OKX returns [price, size, deprecated, numOrders] — we only use first two.
func (o *OKXAdapter) convertToOrderLevels(levels [][]string) []domain.OrderLevel {
	var orderLevels []domain.OrderLevel
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
		orderLevels = append(orderLevels, domain.OrderLevel{
			Price: price,
			Size:  size,
		})
	}
	return orderLevels
}
