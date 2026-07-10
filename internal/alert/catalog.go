// Package alert delivers outbound notifications for noteworthy gateway events
// to a user-run, stateless Apprise (apprise-api) container.
//
// It is a single *consumer* of the internal events bus: it never instruments
// other packages, it only subscribes, filters by the operator's per-event
// selection, debounces, and POSTs a notification to apprise-api. Alerting is
// strictly best-effort — a missing or failing apprise-api never affects request
// serving and never propagates an error up the stack.
package alert

import "strings"

// EventDef describes one operator-subscribable event. The catalog is the
// single source of truth for what can be alerted on; the dashboard renders its
// event picker from this list (served via the admin API), so adding a row here
// surfaces a new checkbox in the UI with no frontend change.
//
// Severity here is the *display* severity for the picker (the coloured dot).
// The actual Apprise notification type is derived at dispatch time from the
// live event's own Severity field, not from this value.
type EventDef struct {
	Type      string `json:"type"`      // matches events.Event.Type
	Category  string `json:"category"`  // UI grouping, e.g. "Failover", "Discovery"
	Severity  string `json:"severity"`  // display severity: success|info|warning|error
	DefaultOn bool   `json:"defaultOn"` // seeds alert_events on first run
}

// catalog is the v1 event registry. Every entry is grounded in an event that is
// actually published today (verified against the codebase) — selecting an event
// that nothing emits would be misleading. Add a row here when a new
// alert-worthy events.Publish call is introduced.
var catalog = []EventDef{
	{Type: "circuit_breaker.open", Category: "Failover", Severity: "warning", DefaultOn: true},
	{Type: "circuit_breaker.closed", Category: "Failover", Severity: "success", DefaultOn: true},
	{Type: "circuit_breaker.half_open", Category: "Failover", Severity: "info", DefaultOn: false},
	{Type: "failover.sync_error", Category: "Failover", Severity: "warning", DefaultOn: true},
	{Type: "discovery.provider_failed", Category: "Discovery", Severity: "error", DefaultOn: false},
	{Type: "fleet.conflict", Category: "High Availability", Severity: "warning", DefaultOn: true},
}

// Catalog returns a copy of the event registry, safe for the caller to mutate.
func Catalog() []EventDef {
	out := make([]EventDef, len(catalog))
	copy(out, catalog)
	return out
}

// catalogIndex builds a Type -> EventDef lookup for the main-app catalog.
func catalogIndex() map[string]EventDef { return catalogIndexOf(catalog) }

// catalogIndexOf builds a Type -> EventDef lookup for any catalog. Used by the
// Dispatcher so an embedder (Front Desk) can supply its own event set.
func catalogIndexOf(defs []EventDef) map[string]EventDef {
	m := make(map[string]EventDef, len(defs))
	for _, e := range defs {
		m[e.Type] = e
	}
	return m
}

// DefaultEnabledCSV returns the comma-joined Type list of the main catalog's
// DefaultOn events, used to seed the alert_events setting on first run.
func DefaultEnabledCSV() string { return DefaultEnabledCSVFor(catalog) }

// DefaultEnabledCSVFor returns the comma-joined Type list of DefaultOn events in
// the given catalog, used to seed an embedder's enabled-events setting.
func DefaultEnabledCSVFor(defs []EventDef) string {
	on := make([]string, 0, len(defs))
	for _, e := range defs {
		if e.DefaultOn {
			on = append(on, e.Type)
		}
	}
	return strings.Join(on, ",")
}

// ParseEnabled turns the stored alert_events CSV into a membership set.
// Blank/whitespace entries are ignored.
func ParseEnabled(csv string) map[string]bool {
	out := make(map[string]bool)
	for _, t := range strings.Split(csv, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out[t] = true
		}
	}
	return out
}
