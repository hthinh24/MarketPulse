package binance

import (
	"MarketPulse/internal/orderbook/config"
	"MarketPulse/internal/orderbook/event"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type BinanceAdapter struct {
	name               string
	symbolDiscoveryUrl string
	snapshotUrl        string
	streamUrl          string
}

func NewBinanceAdapter(config *config.ExchangeConfig) *BinanceAdapter {
	return &BinanceAdapter{
		name:               config.Name,
		symbolDiscoveryUrl: config.SymbolDiscoveryUrl,
		snapshotUrl:        config.SnapshotUrl,
		streamUrl:          config.StreamUrl,
	}
}

func (b *BinanceAdapter) DiscoverySymbol(ctx context.Context) ([]string, error) {
	resp, err := http.Get(b.symbolDiscoveryUrl)
	if err != nil {
		log.Printf("Error fetching symbol discovery for %s: %v\n", b.name, err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Binance Snapshot API Error for %s: Status %d, Body: %s", b.name, resp.StatusCode, string(bodyBytes))
	}

	var info BinanceExchangeInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}

	var streams []string
	for _, s := range info.Symbols {
		if s.QuoteAsset == "USDT" && s.Status == "TRADING" {
			// Stream coin format: <symbol>@trade
			streams = append(streams, s.Symbol)
		}
	}
	return streams, nil
}

func (b *BinanceAdapter) FetchSnapshot(ctx context.Context, symbol string) (*event.OrderBookEvent, error) {
	resp, err := http.Get(fmt.Sprintf(b.snapshotUrl+"?symbol=%s&limit=1000", strings.ToUpper(symbol)))
	if err != nil {
		log.Printf("Error fetching snapshot for %s: %v\n", symbol, err)
		return nil, err
	}

	defer resp.Body.Close()

	var snapshot BinanceSnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, err
	}

	orderBookEvent := &event.OrderBookEvent{
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

func (b *BinanceAdapter) SubscribeOrderBooks(ctx context.Context, symbols []string, deltaChan chan<- event.OrderBookEvent) error {
	chunkSize := 300

	for i := 0; i < len(symbols); i += chunkSize {
		end := i + chunkSize
		if end > len(symbols) {
			end = len(symbols)
		}

		chunk := symbols[i:end]
		go b.connectAndListen(ctx, chunk, deltaChan)
	}

	return nil
}

func (b *BinanceAdapter) GetName() string {
	return b.name
}

func (b *BinanceAdapter) connectAndListen(ctx context.Context, chunk []string, deltaChan chan<- event.OrderBookEvent) {
	var streams []string
	for _, symbol := range chunk {
		streams = append(streams, fmt.Sprintf("%s@depth@100ms", strings.ToLower(symbol)))
	}
	url := fmt.Sprintf(b.streamUrl+"?streams=%s", strings.Join(streams, "/"))

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		fmt.Printf("Dial error chunk: %v\n", err)
		return
	}
	defer conn.Close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				fmt.Printf("Read error: %v\n", err)
				return
			}

			orderBookEvent := b.parseBinanceWSMessage(message)
			if orderBookEvent == nil {
				continue
			}

			deltaChan <- *orderBookEvent
		}
	}
}

func (b *BinanceAdapter) parseBinanceWSMessage(message []byte) *event.OrderBookEvent {
	var binanceDepthUpdateStream BinanceDepthUpdateStream
	if err := json.Unmarshal(message, &binanceDepthUpdateStream); err != nil {
		fmt.Printf("JSON unmarshal error: %v\n", err)
		return nil
	}

	binanceDepthUpdate := binanceDepthUpdateStream.Data
	orderBookEvent := &event.OrderBookEvent{
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

func (b *BinanceAdapter) convertToOrderLevels(bids [][]string) []event.OrderLevel {
	var orderLevels []event.OrderLevel
	for _, bid := range bids {
		price, err1 := strconv.ParseFloat(bid[0], 64)
		size, err2 := strconv.ParseFloat(bid[1], 64)
		if err1 != nil || err2 != nil {
			fmt.Printf("Error parsing bid: %v, %v\n", err1, err2)
			continue
		}
		orderLevels = append(orderLevels, event.OrderLevel{
			Price: price,
			Size:  size,
		})
	}

	return orderLevels
}
