package consumer

import (
	"MarketPulse/internal/dto"
	"context"
	"encoding/json"
	"github.com/segmentio/kafka-go"
	"log"
)

type ICandleProcessor interface {
	ProcessTick(symbol string, tickTime int64, trade dto.Trade)
}

type Consumer struct {
	reader    *kafka.Reader
	processor ICandleProcessor
}

func NewConsumer(reader *kafka.Reader, processor ICandleProcessor) *Consumer {
	return &Consumer{
		reader:    reader,
		processor: processor,
	}
}

func (c *Consumer) StartConsuming(ctx context.Context) {
	log.Println("Kafka consumer started, waiting for messages...")

	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				log.Println("Đã nhận lệnh dừng Kafka Consumer.")
				break
			}
			log.Printf("Lỗi khi hút data: %v\n", err)
			continue
		}

		var trade dto.Trade
		if err := json.Unmarshal(msg.Value, &trade); err != nil {
			log.Printf("Lỗi khi parse message: %v\n", err)
			continue
		}

		c.processor.ProcessTick(trade.Symbol, trade.EventTime, trade)

		if err := c.reader.CommitMessages(ctx, msg); err != nil {
			log.Println("Lỗi commit Kafka:", err)
			return
		}
	}
}
