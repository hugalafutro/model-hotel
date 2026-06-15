// Package otelexport wires the application's slog pipeline to an OpenTelemetry
// OTLP log exporter. It is logs-only: structured log records already emitted via
// debuglog are also pushed to an OTel collector when the standard OTLP endpoint
// environment variables are set. It does NOT add request tracing (spans) or
// OTLP metrics — Prometheus remains the metrics path.
//
// Enabling is purely environment-driven, mirroring debuglog.JSONFormat()
// (LOG_FORMAT) and METRICS_TOKEN, and uses the standard OTEL_EXPORTER_OTLP_*
// variables so existing collectors and tooling work without app-specific config.
//
// No-content rule: the bridge only forwards the structured records the app
// already emits (routing/metering metadata). Request/response bodies are never
// logged, so they are never exported.
package otelexport

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

// instrumentationName identifies this application as the source of the bridged
// log records (becomes the OTel scope name).
const instrumentationName = "github.com/hugalafutro/model-hotel"

// LogsEnabled reports whether OTLP log export should be activated, based on the
// standard OTLP endpoint environment variables. Reading env directly mirrors
// debuglog.JSONFormat(), so no config plumbing is required.
func LogsEnabled() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != ""
}

// NewSlogHandler builds an OTLP log pipeline and returns an slog.Handler that
// bridges records into it, plus a shutdown function that flushes and closes the
// exporter (call it during graceful shutdown so batched records aren't lost).
//
// The exporter's protocol, endpoint, and headers are configured entirely from
// the standard OTEL_EXPORTER_OTLP_* environment variables. serviceName seeds
// service.name when the operator hasn't set it via those env vars.
//
// level gates the bridge so OTLP receives exactly the records the rest of the
// app logs: the OTel log SDK reports every level as enabled, so without this
// gate the fan-out would export DEBUG records even when DEBUG_LOG is off.
//
// Export failures (e.g. an unreachable collector) are reported by the OTel SDK's
// default error handler to stderr; records are dropped from the bounded batch
// queue rather than blocking the caller.
func NewSlogHandler(ctx context.Context, serviceName string, level slog.Leveler) (slog.Handler, func(context.Context) error, error) {
	exporter, err := newExporter(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("otelexport: create OTLP log exporter: %w", err)
	}

	provider := sdklog.NewLoggerProvider(
		sdklog.WithResource(serviceResource(serviceName)),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	handler := &levelHandler{
		level: level,
		inner: otelslog.NewHandler(instrumentationName, otelslog.WithLoggerProvider(provider)),
	}
	return handler, provider.Shutdown, nil
}

// serviceResource builds the OTel resource. It honours an operator-provided
// service name (OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES) and otherwise
// defaults service.name to serviceName — without mutating the process
// environment. Merging with the default resource's schema URL avoids any
// schema-conflict error.
func serviceResource(serviceName string) *resource.Resource {
	res := resource.Default()
	if serviceName == "" ||
		os.Getenv("OTEL_SERVICE_NAME") != "" ||
		resourceAttrsHaveServiceName() {
		return res
	}
	merged, err := resource.Merge(res,
		resource.NewWithAttributes(res.SchemaURL(), attribute.String("service.name", serviceName)))
	if err != nil {
		return res
	}
	return merged
}

// resourceAttrsHaveServiceName reports whether OTEL_RESOURCE_ATTRIBUTES sets the
// service.name key. It parses the standard "key=value,key=value" form and matches
// the key exactly, so a different key that merely contains the text — notably the
// standard service.namespace — does not false-positive and suppress our default.
func resourceAttrsHaveServiceName() bool {
	for _, kv := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		if key, _, ok := strings.Cut(kv, "="); ok && strings.TrimSpace(key) == "service.name" {
			return true
		}
	}
	return false
}

// newExporter builds the OTLP log exporter for the configured transport. Both
// exporters read endpoint, headers, TLS, and timeout from the standard
// OTEL_EXPORTER_OTLP_* (and _LOGS_*) environment variables; this only selects
// the wire protocol. Defaults to http/protobuf — the most common, most
// firewall-friendly choice for self-hosted collectors — and honours the
// standard OTEL_EXPORTER_OTLP[_LOGS]_PROTOCOL="grpc" override.
func newExporter(ctx context.Context) (sdklog.Exporter, error) {
	if protocol() == "grpc" {
		return otlploggrpc.New(ctx)
	}
	return otlploghttp.New(ctx)
}

// protocol returns the configured OTLP protocol, lower-cased, preferring the
// logs-specific override. Empty (the default) is treated as http/protobuf by
// the caller.
func protocol() string {
	p := os.Getenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL")
	if p == "" {
		p = os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL")
	}
	return strings.ToLower(strings.TrimSpace(p))
}

// levelHandler gates an slog.Handler to a minimum level. The OTel log SDK
// reports all levels as enabled, so this keeps OTLP export in lockstep with the
// configured log level instead of exporting suppressed DEBUG records. A nil
// level defaults to Info.
type levelHandler struct {
	level slog.Leveler
	inner slog.Handler
}

func (h *levelHandler) Enabled(_ context.Context, l slog.Level) bool {
	threshold := slog.LevelInfo
	if h.level != nil {
		threshold = h.level.Level()
	}
	return l >= threshold
}

func (h *levelHandler) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelHandler{level: h.level, inner: h.inner.WithAttrs(attrs)}
}

func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{level: h.level, inner: h.inner.WithGroup(name)}
}
