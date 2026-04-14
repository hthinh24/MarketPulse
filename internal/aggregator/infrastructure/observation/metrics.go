package observation

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("market-pulse.aggregator")

	TickEvents, _ = meter.Int64Counter(
		"tick_events_total",
		metric.WithDescription("Total number of tick events"),
	)

	TickEventsLatency, _ = meter.Float64Histogram(
		"tick_events_latency_ms",
		metric.WithDescription("Time taken to process a tick"),
	)
)
