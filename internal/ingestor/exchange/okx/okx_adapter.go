package okx

import (
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type OKXAdapter struct {
	log      *logger.Logger
	url      string
	exchange string
	args     []OKXArg
	conn     *websocket.Conn
	mu       sync.Mutex
	stopPing chan struct{}
}

func NewOKXAdapter(log *logger.Logger, exchange string, url string, args []OKXArg) *OKXAdapter {
	return &OKXAdapter{
		log:      log,
		exchange: exchange,
		url:      url,
		args:     args,
		stopPing: make(chan struct{}),
	}
}

func (o *OKXAdapter) Connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.Dial(o.url, nil)
	if err != nil {
		return fmt.Errorf("okx dial error: %w", err)
	}
	o.conn = conn

	intervalTime := 20 * time.Second
	go o.startPinger(intervalTime)

	subMsg := OKXSubscribePayload{
		Op:   "subscribe",
		Args: o.args,
	}

	o.mu.Lock()
	err = o.conn.WriteJSON(subMsg)
	o.mu.Unlock()

	if err != nil {
		return fmt.Errorf("okx subscribe error: %w", err)
	}

	return nil
}

// startPinger ping to OKX every intervalTime to keep the ws connection
func (o *OKXAdapter) startPinger(intervalTime time.Duration) {
	ticker := time.NewTicker(intervalTime)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			o.mu.Lock()
			err := o.conn.WriteMessage(websocket.TextMessage, []byte("ping"))
			o.mu.Unlock()
			if err != nil {
				return
			}
		case <-o.stopPing:
			return
		}
	}
}

func (o *OKXAdapter) ReadTick(ctx context.Context) (event.TickEvent, error) {
	for {
		err := o.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		if err != nil {
			return event.TickEvent{}, err
		}

		_, message, err := o.conn.ReadMessage()
		if err != nil {
			return event.TickEvent{}, err
		}

		if string(message) == "pong" {
			continue
		}

		var payload OKXWsPayload
		if err := sonic.Unmarshal(message, &payload); err != nil {
			continue
		}

		if payload.Event == "subscribe" || payload.Event == "error" {
			continue
		}

		if len(payload.Data) == 0 {
			continue
		}

		trade := payload.Data[0]

		eventTime, err := strconv.Atoi(trade.Ts)
		if err != nil {
			return event.TickEvent{}, err
		}

		tick := event.TickEvent{
			Exchange:   o.exchange,
			Symbol:     trade.InstId,
			Price:      trade.Px,
			Volume:     trade.Sz,
			IsTakerBuy: trade.Side == "buy", // okx return buy / sell
			EventTime:  int64(eventTime),
		}

		return tick, nil
	}
}

func (o *OKXAdapter) Close() error {
	close(o.stopPing)
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.conn != nil {
		return o.conn.Close()
	}
	return nil
}
