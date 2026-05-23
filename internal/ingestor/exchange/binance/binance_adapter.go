package binance

import (
	"MarketPulse/internal/ingestor/producer/event"
	"MarketPulse/pkg/logger"
	"context"
	"fmt"
	"github.com/bytedance/sonic"
	"github.com/gorilla/websocket"
	"time"
)

type BinanceAdapter struct {
	log      *logger.Logger
	exchange string
	url      string
	conn     *websocket.Conn
}

func NewBinanceAdapter(log *logger.Logger, exchange string, url string) *BinanceAdapter {
	return &BinanceAdapter{
		log:      log,
		exchange: exchange,
		url:      url,
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
	err := b.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	if err != nil {
		return event.TickEvent{}, err
	}

	_, message, err := b.conn.ReadMessage()
	if err != nil {
		return event.TickEvent{}, err
	}

	var binanceWsPayload BinanceWsPayload
	if err := sonic.Unmarshal(message, &binanceWsPayload); err != nil {
		b.log.Warn(ctx, "failed to unmarshal binance websocket message", logger.Error(err))
		return event.TickEvent{}, err
	}

	return event.TickEvent{
		Exchange:   b.exchange,
		Symbol:     binanceWsPayload.Data.Symbol,
		Price:      binanceWsPayload.Data.Price,
		Volume:     binanceWsPayload.Data.Quantity,
		EventTime:  binanceWsPayload.Data.EventTime,
		IsTakerBuy: !binanceWsPayload.Data.IsMaker,
	}, nil
}

func (b *BinanceAdapter) Close() error {
	return b.conn.Close()
}
