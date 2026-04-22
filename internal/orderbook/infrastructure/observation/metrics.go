package observation

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("market-pulse.orderbook")

	OrderBookEvents, _ = meter.Int64Counter(
		"orderbook_events_total",
		metric.WithDescription("Total order book events categorized by status"),
	)

	OrderBookEventsLatency, _ = meter.Float64Histogram(
		"orderbook_events_latency_ms",
		metric.WithDescription("Time taken to process an order book event"),
	)
)
