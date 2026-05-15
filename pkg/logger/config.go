package logger

type LogConfig struct {
	Env       string `envconfig:"ENV"             default:"development"`
	Level     string `envconfig:"LOG_LEVEL"       default:"debug"`
	Format    string `envconfig:"LOG_FORMAT"      default:"console"`
	AddSource bool   `envconfig:"LOG_ADD_SOURCE"  default:"true"`
}
