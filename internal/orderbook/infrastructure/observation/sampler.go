package observation

import (
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"sync/atomic"
	"time"
)

var latencySampleCounter atomic.Int64

const latencySampleRate = 100

func SampleLatency(ctx context.Context, exchange string, latency time.Duration) {
	n := latencySampleCounter.Add(1)
	if n%latencySampleRate == 0 { // sample 1%
		OrderBookEventsLatencyMs.Record(ctx, float64(latency.Milliseconds()),
			metric.WithAttributes(attribute.String("exchange", exchange)),
		)
	}
}
