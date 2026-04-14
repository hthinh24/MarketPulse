package config

type RedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	DB       int    `json:"db"`
	PoolSize int    `json:"pool_size"`
}
