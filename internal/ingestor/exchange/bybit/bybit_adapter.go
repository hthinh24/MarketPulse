package bybit

import (
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/pkg/logger"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type BybitAdapter struct {
	log      *logger.Logger
	url      string
	args     []string // this contain list of topics, for ex: "publicTrade.BTCUSDT"
	conn     *websocket.Conn
	mu       sync.Mutex
	stopPing chan struct{}
}

func NewBybitAdapter(log *logger.Logger, url string, args []string) *BybitAdapter {
	return &BybitAdapter{
		log:      log,
		url:      url,
		args:     args,
		stopPing: make(chan struct{}),
	}
}

func (b *BybitAdapter) Connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.Dial(b.url, nil)
	if err != nil {
		return fmt.Errorf("bybit dial error: %w", err)
	}
	b.conn = conn

	intervalTime := 20 * time.Second
	go b.startPinger(intervalTime)

	maxArgsPerRequest := 10
	for i := 0; i < len(b.args); i += maxArgsPerRequest {
		end := i + maxArgsPerRequest
		if end > len(b.args) {
			end = len(b.args)
		}

		subMsg := BybitSubscribePayload{
			Op:   "subscribe",
			Args: b.args[i:end],
		}

		b.mu.Lock()
		err = b.conn.WriteJSON(subMsg)
		b.mu.Unlock()

		if err != nil {
			return fmt.Errorf("bybit subscribe error: %w", err)
		}

		time.Sleep(50 * time.Millisecond)
	}

	return nil
}

func (b *BybitAdapter) startPinger(intervalTime time.Duration) {
	ticker := time.NewTicker(intervalTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			pingMsg := map[string]string{"op": "ping"}
			b.mu.Lock()
			err := b.conn.WriteJSON(pingMsg)
			b.mu.Unlock()
			if err != nil {
				return
			}
		case <-b.stopPing:
			return
		}
	}
}

func (b *BybitAdapter) ReadTick(ctx context.Context) (event.TickEvent, error) {
	for {
		b.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, message, err := b.conn.ReadMessage()
		if err != nil {
			return event.TickEvent{}, err
		}

		var payload BybitWsPayload
		if err := json.Unmarshal(message, &payload); err != nil {
			b.log.Warn(ctx, "failed to unmarshal bybit websocket message",
				logger.Error(err),
			)
			continue
		}

		if payload.Op == "pong" || payload.Op == "subscribe" {
			continue
		}

		if len(payload.Data) == 0 {
			continue
		}

		trade := payload.Data[0]

		tick := event.TickEvent{
			// TODO(refactor): Add exchange field to the payload
			Exchange:   "BYBIT",
			Symbol:     trade.S,
			Price:      trade.P,
			Volume:     trade.V,
			IsTakerBuy: trade.Side == "Buy", // Bybit return "Buy" / "Sell"
			EventTime:  trade.T,
		}

		return tick, nil
	}
}

func (b *BybitAdapter) Close() error {
	close(b.stopPing)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn != nil {
		return b.conn.Close()
	}
	return nil
}
