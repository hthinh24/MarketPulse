package binance

import (
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/pkg/logger"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"time"
)

type BinanceAdapter struct {
	log  *logger.Logger
	url  string
	conn *websocket.Conn
}

func NewBinanceAdapter(log *logger.Logger, url string) *BinanceAdapter {
	return &BinanceAdapter{
		log: log,
		url: url,
	}
}

func (b *BinanceAdapter) Connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.Dial(b.url, nil)
	if err != nil {
		return fmt.Errorf("binance dial error: %w", err)
	}

	b.conn = conn
	return nil
}

func (b *BinanceAdapter) ReadTick(ctx context.Context) (event.TickEvent, error) {
	var tick event.TickEvent

	b.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := b.conn.ReadMessage()
	if err != nil {
		return tick, err
	}

	var binanceWsPayload BinanceWsPayload
	if err := json.Unmarshal(message, &binanceWsPayload); err != nil {
		b.log.Warn(ctx, "failed to unmarshal binance websocket message",
			logger.Error(err),
		)
	}

	tick = event.TickEvent{
		// TODO(refactor): Add exchange field to the payload
		Exchange:   "BINANCE",
		Symbol:     binanceWsPayload.Data.Symbol,
		Price:      binanceWsPayload.Data.Price,
		Volume:     binanceWsPayload.Data.Quantity,
		EventTime:  binanceWsPayload.Data.EventTime,
		IsTakerBuy: !binanceWsPayload.Data.IsMaker,
	}

	return tick, err
}

func (b *BinanceAdapter) Close() error {
	return b.conn.Close()
}
