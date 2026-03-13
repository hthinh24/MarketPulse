package main

import (
	"MarketPulse/internal/dto"
	"MarketPulse/internal/infra/kafka/producer"
	"context"
	"encoding/json"
	segmentio "github.com/segmentio/kafka-go"
	"log"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	url := "wss://stream.binance.com:9443/ws/btcusdt@trade"

	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("Failed to connect to Binance stream: %v", err)
	}
	defer conn.Close()

	var counter uint64
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			currentTPS := atomic.SwapUint64(&counter, 0)
			log.Printf("[Metrics] %d trades/sec", currentTPS)
		}
	}()

	log.Println("Websocket connected to Binance stream successfully!")

	kafkaWriter := producer.NewProducer("localhost:9092", "market_trades")
	defer kafkaWriter.Close()

	var trade dto.Trade
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Can`t read message, err:", err)
			break
		}

		log.Println("Received raw message:", string(message))

		json.Unmarshal(message, &trade)

		err = kafkaWriter.WriteMessages(context.Background(),
			segmentio.Message{
				Key:   []byte(trade.Symbol),
				Value: message,
			},
		)
		if err != nil {
			log.Printf("Failed to write message to Kafka: %v", err)
		}

		atomic.AddUint64(&counter, 1)
	}
}
