package binance

import (
	"MarketPulse/internal/ingestor/producer/event"
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
	"time"
)

type BinanceAdapter struct {
	url  string
	conn *websocket.Conn
}

func NewBinanceAdapter(url string) *BinanceAdapter {
	return &BinanceAdapter{url: url}
}

func (b *BinanceAdapter) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(b.url, nil)
	if err != nil {
		log.Fatal(err)
	}

	b.conn = conn
	return nil
}

func (b *BinanceAdapter) ReadTick() (event.TickEvent, error) {
	var tick event.TickEvent

	b.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, message, err := b.conn.ReadMessage()
	if err != nil {
		return tick, err
	}

	var binanceWsPayload BinanceWsPayload
	if err := json.Unmarshal(message, &binanceWsPayload); err != nil {
		log.Printf("Failed to unmarshal Binance WebSocket message: %s, error: %v\n", string(message), err)
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
