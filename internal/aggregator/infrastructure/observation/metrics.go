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

	TickEventsLatencyMs, _ = meter.Float64Histogram(
		"tick_events_process_latency_ms",
		metric.WithDescription("Latency from tick event arrival to processing completion (in milliseconds)"),
		metric.WithExplicitBucketBoundaries(
			5, 10, 15, 20, 30, 40, 50,
			75, 100, 150, 200,
			500, 1000, 2000, 5000,
		),
	)
	CandleBroadcastDropsTotal, _ = meter.Int64Counter(
		"candle_broadcast_drops_total",
		metric.WithDescription("Total number of candle updates dropped due to slow consumers"),
	)
)
