package otelexport

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
)

// serviceNameOf extracts the service.name attribute from a resource, or "".
func serviceNameOf(res *resource.Resource) string {
	if v, ok := res.Set().Value(attribute.Key("service.name")); ok {
		return v.AsString()
	}
	return ""
}

func TestLogsEnabled(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		logsEP   string
		want     bool
	}{
		{"both unset", "", "", false},
		{"generic endpoint set", "http://collector:4318", "", true},
		{"logs endpoint set", "", "http://collector:4318/v1/logs", true},
		{"both set", "http://collector:4318", "http://collector:4318/v1/logs", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", tc.endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", tc.logsEP)
			if got := LogsEnabled(); got != tc.want {
				t.Errorf("LogsEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProtocolPrefersLogsSpecific(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/protobuf")
	if got := protocol(); got != "http/protobuf" {
		t.Errorf("protocol() = %q, want the logs-specific override 'http/protobuf'", got)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "")
	if got := protocol(); got != "grpc" {
		t.Errorf("protocol() = %q, want fallback to generic 'grpc'", got)
	}
}

// TestNewSlogHandler_HTTP verifies the pipeline builds without dialing the
// collector (exporter construction is lazy) and returns a usable handler +
// shutdown for the default http/protobuf transport.
func TestNewSlogHandler_HTTP(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_SERVICE_NAME", "test-svc")

	handler, shutdown, err := NewSlogHandler(context.Background(), "model-hotel", slog.LevelInfo)
	if err != nil {
		t.Fatalf("NewSlogHandler returned error: %v", err)
	}
	if handler == nil {
		t.Fatal("NewSlogHandler returned a nil handler")
	}
	if shutdown == nil {
		t.Fatal("NewSlogHandler returned a nil shutdown func")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown returned error: %v", err)
	}
}

// TestLevelHandler_GatesBelowLevel is the regression guard for the parity bug:
// the OTel bridge reports every level as enabled, so the levelHandler wrapper is
// what stops DEBUG records reaching OTLP when the app level is Info.
func TestLevelHandler_GatesBelowLevel(t *testing.T) {
	h := &levelHandler{level: slog.LevelInfo, inner: slog.NewJSONHandler(io.Discard, nil)}

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("Debug should be gated out at Info level")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Info should be enabled at Info level")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Error should be enabled at Info level")
	}

	// A nil leveler must not panic and defaults to Info.
	nilLvl := &levelHandler{inner: slog.NewJSONHandler(io.Discard, nil)}
	if nilLvl.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("nil level should default to Info and gate Debug")
	}
}

// TestNewSlogHandler_GRPC covers the grpc transport branch of newExporter. Like
// the http exporter, otlploggrpc.New does not dial at construction, so this does
// not require a live collector.
func TestNewSlogHandler_GRPC(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
	t.Setenv("OTEL_SERVICE_NAME", "test-svc")

	handler, shutdown, err := NewSlogHandler(context.Background(), "model-hotel", slog.LevelInfo)
	if err != nil {
		t.Fatalf("NewSlogHandler(grpc) returned error: %v", err)
	}
	if handler == nil || shutdown == nil {
		t.Fatal("NewSlogHandler(grpc) returned nil handler or shutdown")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("grpc shutdown returned error: %v", err)
	}
}

func TestServiceResource(t *testing.T) {
	t.Run("defaults service.name when env unset", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "")
		t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")
		// Merge(default, ours) lets our attribute win, so the assertion holds
		// regardless of resource.Default()'s process-level caching.
		if got := serviceNameOf(serviceResource("model-hotel")); got != "model-hotel" {
			t.Errorf("service.name = %q, want model-hotel", got)
		}
	})

	t.Run("honours operator-provided OTEL_SERVICE_NAME (no merge)", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "operator-set")
		// Env set → the default branch returns the base resource without forcing
		// our default; we only assert it doesn't override to "model-hotel".
		if got := serviceNameOf(serviceResource("model-hotel")); got == "model-hotel" {
			t.Error("operator-provided service name must not be overridden by the default")
		}
	})

	t.Run("empty service name returns a usable resource", func(t *testing.T) {
		if serviceResource("") == nil {
			t.Error("serviceResource(\"\") returned nil")
		}
	})

	// Regression: a substring check would treat the standard service.namespace
	// attribute as "service.name is set" and skip our default. Merge wins, so a
	// correct parse still yields model-hotel here.
	t.Run("service.namespace does not suppress the default service name", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "")
		t.Setenv(
			"OTEL_RESOURCE_ATTRIBUTES",
			"service.namespace=prod,deployment.environment=staging",
		)
		if got := serviceNameOf(serviceResource("model-hotel")); got != "model-hotel" {
			t.Errorf(
				"service.name = %q, want model-hotel (service.namespace must not be read as service.name)",
				got,
			)
		}
	})

	t.Run("explicit service.name in OTEL_RESOURCE_ATTRIBUTES is not overridden", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "")
		t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=from-attrs")
		if got := serviceNameOf(serviceResource("model-hotel")); got == "model-hotel" {
			t.Error("an explicit service.name in OTEL_RESOURCE_ATTRIBUTES must not be overridden by the default")
		}
	})
}

func TestResourceAttrsHaveServiceName(t *testing.T) {
	cases := []struct {
		name  string
		attrs string
		want  bool
	}{
		{"empty", "", false},
		{"exact key", "service.name=foo", true},
		{"exact key among others", "deployment.environment=prod,service.name=foo", true},
		{"namespace only (substring trap)", "service.namespace=prod", false},
		{"spaced key", " service.name = foo ", true},
		{"unrelated", "deployment.environment=prod", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OTEL_RESOURCE_ATTRIBUTES", tc.attrs)
			if got := resourceAttrsHaveServiceName(); got != tc.want {
				t.Errorf("resourceAttrsHaveServiceName(%q) = %v, want %v", tc.attrs, got, tc.want)
			}
		})
	}
}
