package observation

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("market-pulse.aggregator")

	//OrderBookEventsProcessed, _ = meter.Int64Counter(
	//	"orderbook_events_total",
	//	metric.WithDescription("Total order book events processed"),
	//)

	OrderBookEventsByStatus, _ = meter.Int64Counter(
		"orderbook_events_by_status_total",
		metric.WithDescription("Order book events by exchange and status (applied, dropped_gap, queued)"),
	)

	OrderBookEventsLatencyMs, _ = meter.Float64Histogram(
		"orderbook_events_latency_ms",
		metric.WithDescription("Time taken to process an order book event (in milliseconds)"),
		metric.WithExplicitBucketBoundaries(
			5, 10, 15, 20, 30, 40, 50,
			75, 100, 150, 200,
			500, 1000, 2000, 5000,
		),
	)

	ActiveSymbols, _ = meter.Int64UpDownCounter(
		"orderbook_active_symbols",
		metric.WithDescription("Number of symbols currently synced and receiving live updates per exchange"),
	)
)

//func RecordOrderBookEventProcessed(exchange string) {
//	OrderBookEventsProcessed.Add(context.Background(), 1,
//		metric.WithAttributes(attribute.String("exchange", exchange)),
//	)
//}

func RecordEvent(ctx context.Context, exchange, status string) {
	OrderBookEventsByStatus.Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("exchange", exchange),
			attribute.String("status", status),
		),
	)
}

func SymbolSynced(ctx context.Context, exchange string) {
	ActiveSymbols.Add(ctx, 1,
		metric.WithAttributes(attribute.String("exchange", exchange)),
	)
}

func SymbolGapped(ctx context.Context, exchange string) {
	ActiveSymbols.Add(ctx, -1,
		metric.WithAttributes(attribute.String("exchange", exchange)),
	)
}
