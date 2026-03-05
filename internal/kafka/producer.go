package kafka

import (
	"github.com/segmentio/kafka-go"
)

func NewProducer(address, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(address),
		Topic:    topic,
		Balancer: &kafka.Hash{}, // Key Partition
		Async:    true,

		// TODO: Remove this in production, only for development
		AllowAutoTopicCreation: true,
	}
}
