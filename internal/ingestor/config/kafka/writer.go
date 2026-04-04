package kafka

import (
	"github.com/segmentio/kafka-go"
)

func NewKafkaWriter(address, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(address),
		Topic:    topic,
		Balancer: &kafka.Hash{}, // Key Partition
		Async:    true,

		// TODO(refactor): Remove this in production, only for development
		// Currently allowing auto topic creation for simplicity
		// but in production should be managed manually
		// to ensure proper configuration and avoid accidental topic creation
		AllowAutoTopicCreation: true,
	}
}
