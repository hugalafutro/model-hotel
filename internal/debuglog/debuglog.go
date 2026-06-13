// Package debuglog provides a thin wrapper around log/slog that reads the
// DEBUG_LOG configuration to set the default log level.
//
// When DEBUG_LOG is enabled, the level is set to Debug, which means
// slog.Debug calls produce output. Otherwise the level is Info and Debug
// calls are discarded with zero allocation.
//
// Usage:
//
//	debuglog.Info("proxy: request start", "model", model, "stream", stream)
//	debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode)
//	debuglog.Error("proxy: all providers exhausted", "model", model)
//	debuglog.Debug("proxy: TouchLastUsed failed", "provider", pid, "error", err)
package debuglog

import (
	"log/slog"
	"os"
	"strings"
)

// currentLevel stores the active slog level so it can be preserved when
// reconfiguring the handler via SetHandler.
var currentLevel slog.Level

// Init configures the default slog logger based on the debug flag.
// If debug is true, log level is set to Debug; otherwise Info.
// This also reads the DEBUG_LOG env var as a fallback if debug is false
// but the env var is explicitly set to a truthy value.
//
// The output format honors LOG_FORMAT (see JSONFormat): "json" emits one JSON
// object per line for external log collectors, anything else (default) keeps
// the human-readable text format. The choice lives here, not in the caller, so
// every binary that calls Init (server today, Front Desk later) inherits it.
func Init(debug bool) {
	if debug || isDebugLogEnv() {
		currentLevel = slog.LevelDebug
	} else {
		currentLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: currentLevel}
	var handler slog.Handler
	if JSONFormat() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// JSONFormat reports whether structured JSON log output is requested via
// LOG_FORMAT=json (case-insensitive). When false, callers should emit the
// human-readable text format. This is the single source of truth for the log
// output format across all binaries and handlers.
func JSONFormat() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("LOG_FORMAT")), "json")
}

// Level returns the current slog log level (set by Init).
func Level() slog.Level {
	return currentLevel
}

// SetHandler replaces the default slog handler. Use this to route slog
// output through a custom handler (e.g. one that writes to the app log
// ring buffer and database). Call after api.InitAppLogBuffer.
func SetHandler(h slog.Handler) {
	slog.SetDefault(slog.New(h))
}

// isDebugLogEnv returns true if the DEBUG_LOG env var is set to a truthy value.
func isDebugLogEnv() bool {
	v := strings.ToLower(os.Getenv("DEBUG_LOG"))
	return v == "true" || v == "1" || v == "yes"
}

// Debug logs at Debug level. These are discarded when DEBUG_LOG is not enabled.
func Debug(msg string, args ...any) {
	slog.Debug(msg, args...)
}

// Info logs at Info level.
func Info(msg string, args ...any) {
	slog.Info(msg, args...)
}

// Warn logs at Warn level.
func Warn(msg string, args ...any) {
	slog.Warn(msg, args...)
}

// Error logs at Error level.
func Error(msg string, args ...any) {
	slog.Error(msg, args...)
}
