package observation

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("market-pulse.broadcaster")

	BroadcastMessagesTotal, _ = meter.Int64Counter(
		"broadcaster_messages_total",
		metric.WithDescription("Total number of messages broadcast by stream type"),
	)

	ClientDropsTotal, _ = meter.Int64Counter(
		"broadcaster_client_drops_total",
		metric.WithDescription("Total number of clients dropped by reason (slow_consumer or disconnect)"),
	)

	ActiveRooms, _ = meter.Int64Gauge(
		"broadcaster_active_rooms",
		metric.WithDescription("Current number of active rooms"),
	)

	ActiveClients, _ = meter.Int64Gauge(
		"broadcaster_active_clients",
		metric.WithDescription("Total number of active WebSocket clients"),
	)

	BroadcastLatencyMs, _ = meter.Float64Histogram(
		"broadcaster_broadcast_latency_ms",
		metric.WithDescription("Latency from Redis message arrival to start of client broadcast (in milliseconds)"),
		metric.WithExplicitBucketBoundaries(1, 5, 10, 20, 30, 50, 75, 100, 200, 500),
	)

	CmdChanQueueLength, _ = meter.Int64Gauge(
		"broadcaster_cmdchan_queue_length",
		metric.WithDescription("Current queue length of a room worker's command channel"),
	)
)
