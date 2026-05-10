package config

import (
	"fmt"
	"github.com/kelseyhightower/envconfig"
)

type AppConfig struct {
	OTLP  OTLPConfig
	Redis RedisCacheConfig
	Kafka KafkaConfig
	DB    DBConfig
}

type OTLPConfig struct {
	Endpoint string `envconfig:"OTLP_ENDPOINT" default:"localhost:4317"`
}

type RedisCacheConfig struct {
	Addr     string `envconfig:"REDIS_CACHE_ADDR" required:"true"`
	Password string `envconfig:"REDIS_CACHE_PASSWORD" default:""`
	DB       int    `envconfig:"REDIS_CACHE_DB" default:"1"`
	PoolSize int    `envconfig:"REDIS_CACHE_POOL_SIZE" default:"4"`
}

type KafkaConfig struct {
	Broker      string `envconfig:"KAFKA_BROKER" required:"true"`
	TopicPrefix string `envconfig:"KAFKA_TOPIC_PREFIX" default:"market_trades"`
}

type DBConfig struct {
	Host     string `envconfig:"DB_HOST" required:"true"`
	Port     string `envconfig:"DB_PORT" default:"5432"`
	User     string `envconfig:"DB_USER" required:"true"`
	Password string `envconfig:"DB_PASSWORD" required:"true"`
	Name     string `envconfig:"DB_NAME" required:"true"`
	SSLMode  string `envconfig:"DB_SSL_MODE" default:"disable"`
	Timezone string `envconfig:"DB_TIMEZONE" default:"UTC"`
}

// DSN generates PostgreSQL connection string
func (d *DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		d.Host, d.User, d.Password, d.Name, d.Port, d.SSLMode, d.Timezone,
	)
}

// LoadAppConfig loads application configuration from environment variables
func LoadAppConfig() (*AppConfig, error) {
	cfg := &AppConfig{}

	// Load OTLP config
	if err := envconfig.Process("", &cfg.OTLP); err != nil {
		return nil, fmt.Errorf("failed to load otlp config: %w", err)
	}

	// Load Redis Cache config
	if err := envconfig.Process("", &cfg.Redis); err != nil {
		return nil, fmt.Errorf("failed to load redis cache config: %w", err)
	}

	// Load Kafka config
	if err := envconfig.Process("", &cfg.Kafka); err != nil {
		return nil, fmt.Errorf("failed to load kafka config: %w", err)
	}

	// Load DB config
	if err := envconfig.Process("", &cfg.DB); err != nil {
		return nil, fmt.Errorf("failed to load db config: %w", err)
	}

	return cfg, nil
}

