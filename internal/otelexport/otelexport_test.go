package otelexport

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

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
