package telemetry

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkMetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.9.0"
	"os"
	"runtime/metrics"
	"time"
)

func InitMetricsProvider(ctx context.Context, serviceName string, grpcEndpoint string) (func(context.Context) error, error) {
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(grpcEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP gRPC exporter: %w", err)
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
		return nil, fmt.Errorf("failed to start OpenTelemetry runtime instrumentation: %w", err)
	}

	go collectCPUMetric(ctx, provider)

	return provider.Shutdown, nil
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
