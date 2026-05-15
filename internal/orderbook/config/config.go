package config

import (
	"MarketPulse/pkg/logger"
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type AppConfig struct {
	Log     logger.LogConfig
	OTLP    OTLPConfig
	Redis   RedisPubSubConfig
	Binance ExchangeURLConfig
	Bybit   ExchangeURLConfig
	OKX     ExchangeURLConfig
}

type OTLPConfig struct {
	Endpoint string `envconfig:"OTLP_ENDPOINT" default:"localhost:4317"`
}

type RedisPubSubConfig struct {
	Addr     string `envconfig:"REDIS_PUBSUB_ADDR" required:"true"`
	Password string `envconfig:"REDIS_PUBSUB_PASSWORD" default:""`
	DB       int    `envconfig:"REDIS_PUBSUB_DB" default:"0"`
	PoolSize int    `envconfig:"REDIS_PUBSUB_POOL_SIZE" default:"8"`
}

type ExchangeURLConfig struct {
	DiscoveryURL string `envconfig:"DISCOVERY_URL" required:"true"`
	SnapshotURL  string `envconfig:"SNAPSHOT_URL" default:""`
	StreamURL    string `envconfig:"STREAM_URL" required:"true"`
}

// LoadAppConfig loads application configuration from environment variables
func LoadAppConfig() (*AppConfig, error) {
	cfg := &AppConfig{
		Binance: ExchangeURLConfig{},
		Bybit:   ExchangeURLConfig{},
		OKX:     ExchangeURLConfig{},
	}

	// Load Log config
	if err := envconfig.Process("", &cfg.Log); err != nil {
		return nil, fmt.Errorf("failed to load log config: %w", err)
	}

	// Load OTLP config
	if err := envconfig.Process("", &cfg.OTLP); err != nil {
		return nil, fmt.Errorf("failed to load otlp config: %w", err)
	}

	// Load Redis Pub/Sub config
	if err := envconfig.Process("", &cfg.Redis); err != nil {
		return nil, fmt.Errorf("failed to load redis pubsub config: %w", err)
	}

	// Load Binance URLs
	if err := envconfig.Process("BINANCE", &cfg.Binance); err != nil {
		return nil, fmt.Errorf("failed to load binance config: %w", err)
	}

	// Load Bybit URLs
	if err := envconfig.Process("BYBIT", &cfg.Bybit); err != nil {
		return nil, fmt.Errorf("failed to load bybit config: %w", err)
	}

	// Load OKX URLs
	if err := envconfig.Process("OKX", &cfg.OKX); err != nil {
		return nil, fmt.Errorf("failed to load okx config: %w", err)
	}

	return cfg, nil
}
