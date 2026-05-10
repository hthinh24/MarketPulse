package config

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type AppConfig struct {
	OTLP  OTLPConfig
	Redis RedisPubSubConfig
	Port  string `envconfig:"BROADCASTER_PORT" default:"8081"`
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

// LoadAppConfig loads application configuration from environment variables
func LoadAppConfig() (*AppConfig, error) {
	cfg := &AppConfig{}

	// Load OTLP config
	if err := envconfig.Process("", &cfg.OTLP); err != nil {
		return nil, fmt.Errorf("failed to load otlp config: %w", err)
	}

	// Load Redis Pub/Sub config
	if err := envconfig.Process("", &cfg.Redis); err != nil {
		return nil, fmt.Errorf("failed to load redis pubsub config: %w", err)
	}

	// Load port
	if err := envconfig.Process("", cfg); err != nil {
		return nil, fmt.Errorf("failed to load port config: %w", err)
	}

	return cfg, nil
}

