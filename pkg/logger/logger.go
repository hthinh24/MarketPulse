package logger

import (
	"context"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Field = zap.Field

type Logger struct {
	zap     *zap.Logger
	service string
}

func New(service string, config LogConfig) (*Logger, error) {
	cfg := zap.NewProductionConfig()
	switch config.Level {
	case "debug":
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warn":
		cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	cfg.Encoding = config.Format
	cfg.DisableCaller = !config.AddSource
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	z, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{
		zap:     z.With(zap.String("service", service)),
		service: service,
	}, nil
}

func (l *Logger) Info(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, zap.InfoLevel, msg, fields...)
}

func (l *Logger) Error(ctx context.Context, msg string, err error, fields ...Field) {
	l.log(ctx, zap.ErrorLevel, msg, append(fields, zap.Error(err))...)
}

func (l *Logger) Warn(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, zap.WarnLevel, msg, fields...)
}

func (l *Logger) With(fields ...Field) *Logger {
	return &Logger{
		zap:     l.zap.With(fields...),
		service: l.service,
	}
}

func (l *Logger) log(ctx context.Context, level zapcore.Level, msg string, fields ...Field) {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sc := span.SpanContext()
		fields = append(fields,
			zap.String("trace_id", sc.TraceID().String()),
			zap.String("span_id", sc.SpanID().String()),
		)
	}

	switch level {
	case zap.InfoLevel:
		l.zap.Info(msg, fields...)
	case zap.WarnLevel:
		l.zap.Warn(msg, fields...)
	case zap.ErrorLevel:
		l.zap.Error(msg, fields...)
	}
}
