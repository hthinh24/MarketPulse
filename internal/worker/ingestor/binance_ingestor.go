package ingestor

import (
	"MarketPulse/internal/dto"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"log"
	"sync"
	"time"
)

type BinanceIngestor struct {
	url       string
	tradeChan chan<- dto.Trade
}

func NewBinanceIngestor(url string, tradeChan chan<- dto.Trade) *BinanceIngestor {
	return &BinanceIngestor{
		url:       url,
		tradeChan: tradeChan,
	}
}

func (i *BinanceIngestor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	// TODO: Adding reconnection logic with debounce to avoid banned by Binance
	conn, _, err := websocket.DefaultDialer.Dial(i.url, nil)
	if err != nil {
		log.Fatal(err)
	}

	defer func() {
		conn.Close()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var binanceWsPayload dto.BinanceWsPayload
			if err := json.Unmarshal(message, &binanceWsPayload); err == nil {
				i.tradeChan <- binanceWsPayload.Data
			}
		}
	}
}
