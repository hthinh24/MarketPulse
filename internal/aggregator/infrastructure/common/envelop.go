package common

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"time"
)

type MessageMeta struct {
	MessageID  string
	Version    uint8
	Source     string
	SentAt     time.Time
	RetryCount uint8
}

type TraceMeta struct {
	TraceParent string
}

type Envelope[T any] struct {
	Trace     TraceMeta
	Timestamp time.Time
	Payload   T
}

type RichEnvelope[T any] struct {
	Meta MessageMeta
	Envelope[T]
}

func NewEnvelope[T any](ctx context.Context, payload T) Envelope[T] {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return Envelope[T]{
		Trace: TraceMeta{
			TraceParent: carrier["traceparent"],
		},
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

func (e Envelope[T]) ExtractContext(ctx context.Context) context.Context {
	if e.Trace.TraceParent == "" {
		return ctx
	}

	carrier := propagation.MapCarrier{"traceparent": e.Trace.TraceParent}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}
