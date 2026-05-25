package observation

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var Tracer trace.Tracer = otel.Tracer("noop")

func InitTracer(serviceName string) {
	Tracer = otel.Tracer(serviceName)
}
