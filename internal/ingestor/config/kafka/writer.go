package kafka

import (
	"github.com/segmentio/kafka-go"
	"time"
)

func NewKafkaWriter(address, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(address),
		Topic:    topic,
		Balancer: &kafka.Hash{}, // Key Partition
		Async:    false,

		BatchSize:    100,
		BatchTimeout: 5 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,

		// TODO(refactor): Remove this in production, only for development
		// Currently allowing auto topic creation for simplicity
		// but in production should be managed manually
		// to ensure proper configuration and avoid accidental topic creation
		AllowAutoTopicCreation: true,
	}
}
