package bybit

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
	"net/http"
	"strconv"
	"sync"
	"time"
)

type BybitAdapter struct {
	log                    *logger.Logger
	name                   string
	symbolDiscoveryUrl     string
	snapshotUrl            string
	streamUrl              string
	streamBufferSize       int
	symbolWorkerBufferSize int
	deltaQueueSize         int
	btreeDegree            int
	snapshotQuantity       int
	writeMu                sync.Mutex
}

func NewBybitAdapter(log *logger.Logger, config *config.ExchangeConfig) *BybitAdapter {
	return &BybitAdapter{
		log:                    log,
		name:                   config.Name,
		symbolDiscoveryUrl:     config.SymbolDiscoveryUrl,
		snapshotUrl:            config.SnapshotUrl,
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
func (b *BybitAdapter) Start(ctx context.Context, publishChan chan<- *domain.OrderBookSnapshot) error {
	b.log.Info(ctx, "starting bybit adapter", logger.String("exchange", b.name))

	// Discover symbols
	symbols, err := b.discoverSymbols(ctx)
	if err != nil {
		b.log.Error(ctx, "failed to discover symbols", err)
		return err
	}
	b.log.Info(ctx, "discovered symbols", logger.Int("count", len(symbols)), logger.String("exchange", b.name))

	// resyncChan: workers signal dispatcher which symbol needs re-subscription
	resyncChan := make(chan string, len(symbols))

	// Create one worker + one channel per symbol to maintain their own order book state
	workerChans := make(map[string]chan event.EventEnvelope, len(symbols))
	for _, symbol := range symbols {
		state, err := service.NewOrderBookState(b.btreeDegree, b.snapshotQuantity)
		if err != nil {
			b.log.Error(ctx, "failed to create orderbook state for symbol", err, logger.String("symbol", symbol))
			return err
		}

		ch := make(chan event.EventEnvelope, b.symbolWorkerBufferSize)
		workerChans[symbol] = ch

		worker := newBybitSymbolWorker(b.log, b.name, symbol, b.deltaQueueSize, state, ch, resyncChan)
		go worker.run(ctx, publishChan)
	}

	// Subscribe to WebSocket feed and process updates
	mainChan := make(chan event.EventEnvelope, b.streamBufferSize)

	// Chunk symbols into groups (Bybit allows max ~10 topics per connection)
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
			b.connectAndListen(ctx, chunk, mainChan, resyncChan)
		}(i / chunkSize)
	}

	// Dispatch incoming events to workers
	go b.dispatch(ctx, mainChan, workerChans)

	// Wait for context cancellation
	<-ctx.Done()
	b.log.Info(ctx, "bybit adapter shutting down gracefully")
	close(mainChan)
	return nil
}

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

// dispatch routes WS events to the correct worker.
func (b *BybitAdapter) dispatch(
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
					observation.RecordEvent(ctx, b.name, "dropped_queue_full")
					b.log.Warn(ctx, "dropping order book event due to full channel buffer", logger.String("symbol", sym))
				}
			}
		}
	}
}

// connectAndListen connects to Bybit WebSocket and handles subscription with re-subscribe on gap.
// Runs re-subscribe and message listening in parallel goroutines on same connection.
func (b *BybitAdapter) connectAndListen(ctx context.Context, symbols []string, mainChan chan<- event.EventEnvelope, resyncChan <-chan string) {
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
			b.log.Error(ctx, "failed to connect bybit websocket", err, logger.Duration("retry_after", 5*time.Second))
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
			b.log.Error(ctx, "failed to subscribe", err)
			conn.Close()
			continue
		}

		// Goroutine handles re-subscribe requests from resyncChan
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case symbol, ok := <-resyncChan:
					if !ok {
						return
					}
					topic, exists := topicMap[symbol]
					if !exists {
						// Symbol not in this chunk, ignore
						continue
					}
					b.log.Info(ctx, "re-subscribing topic", logger.String("topic", topic))

					// Unsubscribe
					b.writeMu.Lock()
					unsubMsg := BybitWsCommandMessage{
						Op:   "unsubscribe",
						Args: []string{topic},
					}
					err := conn.WriteJSON(unsubMsg)
					b.writeMu.Unlock()

					if err != nil {
						b.log.Error(ctx, "failed to unsubscribe", err, logger.String("topic", topic))
						return
					}

					// Subscribe again
					if err := b.sendSubscribe(conn, []string{topic}); err != nil {
						b.log.Error(ctx, "failed to re-subscribe", err, logger.String("topic", topic))
						return
					}
				}
			}
		}()

		b.listenAndProcess(ctx, conn, mainChan)
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

// listenAndProcess reads WebSocket messages and sends them to mainChan as envelopes.
func (b *BybitAdapter) listenAndProcess(ctx context.Context, conn *websocket.Conn, mainChan chan<- event.EventEnvelope) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			var msg BybitWSOrderBookMessage
			if err := conn.ReadJSON(&msg); err != nil {
				b.log.Error(ctx, "websocket read error", err)
				return
			}

			payload := domain.OrderBookEvent{
				Exchange:     b.name,
				Symbol:       msg.Data.S,
				IsSnapshot:   msg.Type == "snapshot",
				UpdateID:     msg.Data.U,
				PrevUpdateID: msg.Data.U - 1,
				Timestamp:    time.Now().UnixMilli(),
				Bids:         b.convertToOrderLevels(ctx, msg.Data.B),
				Asks:         b.convertToOrderLevels(ctx, msg.Data.A),
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

// convertToOrderLevels converts string price/size pairs to OrderLevel structs.
func (b *BybitAdapter) convertToOrderLevels(ctx context.Context, priceSizePairs [][]string) []domain.OrderLevel {
	var orderLevels []domain.OrderLevel
	for _, pair := range priceSizePairs {
		if len(pair) < 2 {
			continue
		}
		price, err1 := strconv.ParseFloat(pair[0], 64)
		size, err2 := strconv.ParseFloat(pair[1], 64)
		if err1 != nil || err2 != nil {
			b.log.Error(ctx, "failed to discover symbols", err1, logger.String("price_str", pair[0]))
			b.log.Error(ctx, "failed to discover symbols", err2, logger.String("size_str", pair[1]))
			continue
		}
		orderLevels = append(orderLevels, domain.OrderLevel{
			Price: price,
			Size:  size,
		})
	}
	return orderLevels
}
