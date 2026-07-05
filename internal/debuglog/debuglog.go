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
	"context"
	"log/slog"
	"os"
	"sort"
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

	slog.SetDefault(slog.New(maybeScopeFilter(StdoutHandler())))

	// Confirm scoped debug at startup so the operator sees it took effect. Log
	// the parsed/normalized scopes (not the raw env string) so a tainted value
	// can't forge log lines.
	if !globalDebug && len(enabledScopes) > 0 {
		scopeList := make([]string, 0, len(enabledScopes))
		for s := range enabledScopes {
			scopeList = append(scopeList, s)
		}
		sort.Strings(scopeList)
		slog.Info("debuglog: per-scope debug enabled", "scopes", scopeList)
	}
}

// StdoutHandler builds the base slog handler that writes to os.Stdout, honouring
// LOG_FORMAT (JSON vs text) and the level chosen by Init. It is the single
// source of truth for the stdout log sink, so callers that install their own
// fan-out (e.g. Front Desk fanning stdout out to an OTLP exporter) start from
// the exact handler Init would have installed. Call after Init so the level is
// set; before Init it uses the zero value (Info).
func StdoutHandler() slog.Handler {
	opts := &slog.HandlerOptions{Level: currentLevel}
	if JSONFormat() {
		return slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.NewTextHandler(os.Stdout, opts)
}

// maybeScopeFilter wraps h with per-scope Debug filtering, but only when
// DEBUG_LOG_SCOPES is active and global Debug is off (the one case where some
// Debug records must be dropped). Otherwise h is returned unchanged, so the
// common paths — debug fully off, or fully on — keep slog's behaviour exactly.
func maybeScopeFilter(h slog.Handler) slog.Handler {
	if globalDebug || len(enabledScopes) == 0 {
		return h
	}
	return scopeFilterHandler{inner: h}
}

// scopeFilterHandler drops Debug records whose scope — the prefix before the
// first ':' in the message, matched case-insensitively against DEBUG_LOG_SCOPES
// — is not enabled. Non-Debug records always pass through. Filtering in the
// handler (rather than in Debug()) keeps "debuglog.Debug always reaches the
// installed handler" true, which callers that install their own slog handler
// rely on.
type scopeFilterHandler struct{ inner slog.Handler }

func (h scopeFilterHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h scopeFilterHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level == slog.LevelDebug && !enabledScopes[strings.ToLower(scopeOf(r.Message))] {
		return nil
	}
	return h.inner.Handle(ctx, r)
}

func (h scopeFilterHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return scopeFilterHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h scopeFilterHandler) WithGroup(name string) slog.Handler {
	return scopeFilterHandler{inner: h.inner.WithGroup(name)}
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
	slog.SetDefault(slog.New(maybeScopeFilter(h)))
}

// isDebugLogEnv returns true if the DEBUG_LOG env var is set to a truthy value.
func isDebugLogEnv() bool {
	v := strings.ToLower(os.Getenv("DEBUG_LOG"))
	return v == "true" || v == "1" || v == "yes"
}

// Debug logs at Debug level. Discarded with zero allocation unless Debug output
// is enabled (DEBUG_LOG, or DEBUG_LOG_SCOPES for the message's scope). The level
// gate lives in the installed handler; per-scope filtering is applied there too
// (see scopeFilterHandler), so this stays a plain pass-through.
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

// Fatal logs at Error level and then exits the process with status 1. Unlike
// the stdlib log.Fatal, the message flows through the configured slog handler,
// so once the app-log ring buffer is installed it is captured there (and shown
// in the live log viewer) before the process exits.
func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}
