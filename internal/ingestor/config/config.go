package config

import (
	"MarketPulse/pkg/logger"
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type AppConfig struct {
	Log     logger.LogConfig
	Kafka   KafkaConfig
	Binance ExchangeStreamConfig
	Bybit   ExchangeStreamConfig
	OKX     ExchangeStreamConfig
}

type KafkaConfig struct {
	Broker      string `envconfig:"KAFKA_BROKER" required:"true"`
	TopicPrefix string `envconfig:"KAFKA_TOPIC_PREFIX" default:"market_trades"`
}

type ExchangeStreamConfig struct {
	StreamURL string `envconfig:"STREAM_URL"`
}

// LoadAppConfig loads application configuration from environment variables
func LoadAppConfig() (*AppConfig, error) {
	cfg := &AppConfig{
		Binance: ExchangeStreamConfig{},
		Bybit:   ExchangeStreamConfig{},
		OKX:     ExchangeStreamConfig{},
	}
	
	// Load Log config
	if err := envconfig.Process("", &cfg.Log); err != nil {
		return nil, fmt.Errorf("failed to load log config: %w", err)
	}

	// Load Kafka config
	if err := envconfig.Process("KAFKA", &cfg.Kafka); err != nil {
		return nil, fmt.Errorf("failed to load kafka config: %w", err)
	}

	// Load Binance config
	if err := envconfig.Process("BINANCE", &cfg.Binance); err != nil {
		return nil, fmt.Errorf("failed to load binance config: %w", err)
	}

	// Load Bybit config
	if err := envconfig.Process("BYBIT", &cfg.Bybit); err != nil {
		return nil, fmt.Errorf("failed to load bybit config: %w", err)
	}

	// Load OKX config
	if err := envconfig.Process("OKX", &cfg.OKX); err != nil {
		return nil, fmt.Errorf("failed to load okx config: %w", err)
	}

	return cfg, nil
}
