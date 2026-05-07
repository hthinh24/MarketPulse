package telemetry

import (
	"context"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkMetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.9.0"
	"log"
	"os"
	"runtime/metrics"
	"time"
)

func InitProvider(serviceName string, grpcEndpoint string) func(context.Context) error {
	ctx := context.Background()

	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(grpcEndpoint),
	)
	if err != nil {
		log.Fatalf("Failed to create OTLP gRPC exporter: %v", err)
	}

	hostName, _ := os.Hostname()
	// UUIDv7 has timestamp base that auto-increased support to sorted
	instanceID := hostName + "-" + uuid.Must(uuid.NewV7()).String()

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(serviceName),
		semconv.ServiceInstanceIDKey.String(instanceID),
	)

	provider := sdkMetric.NewMeterProvider(
		sdkMetric.WithReader(
			sdkMetric.NewPeriodicReader(
				exporter,
				sdkMetric.WithInterval(15*time.Second),
				sdkMetric.WithProducer(runtime.NewProducer()),
			),
		),
		sdkMetric.WithResource(res),
	)

	otel.SetMeterProvider(provider)

	err = runtime.Start(
		runtime.WithMeterProvider(provider),
		runtime.WithMinimumReadMemStatsInterval(15*time.Second),
	)
	if err != nil {
		log.Fatalf("Failed to start OpenTelemetry runtime instrumentation: %v", err)
	}

	go collectCPUMetric(ctx, provider)

	return provider.Shutdown
}

func collectCPUMetric(ctx context.Context, provider *sdkMetric.MeterProvider) {
	meter := provider.Meter("runtime.extended")

	cpuCounter, _ := meter.Float64ObservableCounter(
		"process_cpu_seconds_total",
		metric.WithDescription("Total CPU seconds consumed by process"),
	)

	sample := []metrics.Sample{
		{Name: "/cpu/classes/total:cpu-seconds"},
	}

	meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		metrics.Read(sample)
		o.ObserveFloat64(cpuCounter, sample[0].Value.Float64())
		return nil
	}, cpuCounter)

	<-ctx.Done()
}
