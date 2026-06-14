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
// reconfiguring the handler via SetHandler. It is the level the underlying
// slog handler accepts — Debug whenever any Debug output is possible (global
// debug OR per-scope debug), so scoped Debug records reach the handler instead
// of being dropped by its level gate before Debug()'s per-scope check runs.
var currentLevel slog.Level

// globalDebug is true when Debug output is enabled for every scope (DEBUG_LOG).
var globalDebug bool

// enabledScopes holds the scope prefixes — the text before the first ':' in a
// message, e.g. "failover" in "failover: …" — for which Debug output is enabled
// even when globalDebug is false. Populated from DEBUG_LOG_SCOPES so an operator
// can turn on Debug for one noisy area without flooding everything at high RPS.
var enabledScopes map[string]bool

// Init configures the default slog logger based on the debug flag.
// If debug is true, log level is set to Debug; otherwise Info.
// This also reads the DEBUG_LOG env var as a fallback if debug is false
// but the env var is explicitly set to a truthy value.
//
// DEBUG_LOG_SCOPES (comma-separated scope prefixes) enables Debug output for
// just those scopes when global Debug is off.
//
// The output format honors LOG_FORMAT (see JSONFormat): "json" emits one JSON
// object per line for external log collectors, anything else (default) keeps
// the human-readable text format. The choice lives here, not in the caller, so
// every binary that calls Init (server today, Front Desk later) inherits it.
func Init(debug bool) {
	globalDebug = debug || isDebugLogEnv()
	enabledScopes = parseScopes(os.Getenv("DEBUG_LOG_SCOPES"))

	// The handler must accept Debug records whenever any Debug output is
	// possible; the per-scope gate in Debug() then decides which actually emit.
	if globalDebug || len(enabledScopes) > 0 {
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

	// Confirm scoped debug at startup so the operator sees it took effect.
	if !globalDebug && len(enabledScopes) > 0 {
		slog.Info("debuglog: per-scope debug enabled", "scopes", os.Getenv("DEBUG_LOG_SCOPES"))
	}
}

// parseScopes splits a comma-separated DEBUG_LOG_SCOPES value into a set of
// trimmed, lower-cased, non-empty scope prefixes. Returns nil when empty.
func parseScopes(raw string) map[string]bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	scopes := make(map[string]bool)
	for _, s := range strings.Split(raw, ",") {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			scopes[s] = true
		}
	}
	if len(scopes) == 0 {
		return nil
	}
	return scopes
}

// scopeOf returns a message's scope prefix — the text before the first ':'
// (e.g. "failover" in "failover: skipping row"). Empty when there is none.
func scopeOf(msg string) string {
	if i := strings.IndexByte(msg, ':'); i > 0 {
		return msg[:i]
	}
	return ""
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

// Debug logs at Debug level. Discarded unless global Debug (DEBUG_LOG) is on or
// the message's scope is enabled via DEBUG_LOG_SCOPES. When neither is set the
// check short-circuits without inspecting the message, preserving the cheap
// discard on the hot path.
func Debug(msg string, args ...any) {
	if globalDebug || (len(enabledScopes) > 0 && enabledScopes[scopeOf(msg)]) {
		slog.Debug(msg, args...)
	}
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

// Fatal logs at Error level and then exits the process with status 1. Unlike
// the stdlib log.Fatal, the message flows through the configured slog handler,
// so once the app-log ring buffer is installed it is captured there (and shown
// in the live log viewer) before the process exits.
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
