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
	// One model has opened its circuit three times inside a day. Default-off
	// like the Discovery backlog events and for the same reason: an instance
	// upgrading with a provider that has been flaky for weeks would be notified
	// on the third open about a condition its operator already lives with. The
	// flag gates outbound notification only; the dashboard's event feed is not
	// catalogue-gated and shows it either way.
	{Type: "circuit_breaker.unstable", Category: "Failover", Severity: "warning", DefaultOn: false},
	{Type: "failover.sync_error", Category: "Failover", Severity: "warning", DefaultOn: true},
	{Type: "discovery.provider_failed", Category: "Discovery", Severity: "error", DefaultOn: false},
	// The Models badge has been asking for attention for longer than the
	// operator's threshold. Default-off like its Discovery sibling: an instance
	// upgrading with a long-standing backlog would otherwise be alerted about
	// history it already knows about, on the very first scan after the upgrade.
	{Type: "discovery.claims_outstanding", Category: "Discovery", Severity: "warning", DefaultOn: false},
	{Type: "fleet.conflict", Category: "High Availability", Severity: "warning", DefaultOn: true},
	{Type: "auth.sso_identity_bound", Category: "Security", Severity: "warning", DefaultOn: false},
	// A provider reshaped its quota response. Default-on because the failure it
	// guards against is silent: a normalizer written against the old shape keeps
	// answering, wrongly, and nothing else in the system would ever say so.
	{Type: "quota.schema_drift", Category: "Quota", Severity: "warning", DefaultOn: true},
	// The gateway disabled a model because the provider kept refusing it as
	// retired. Default-on: this is the only warning an operator gets that a
	// model they route to has died, and it is not recoverable by waiting.
	// Discovery cannot raise it, because providers keep retired models in their
	// listings (Google served gemini-2.0-flash from /models for two months after
	// shutting it down), so nothing else in the system will ever say so.
	{Type: "model.auto_disabled_gone", Category: "Discovery", Severity: "warning", DefaultOn: true},
	// A provider reached the disable date the operator set for it and the
	// background sweep switched it off. Grouped with the circuit breaker because
	// the operator-visible consequence is the same — that provider stops serving
	// — and default-on because the date is typically set weeks ahead of the day
	// it fires, so the firing itself is the only reminder routing just changed.
	{Type: "provider.scheduled_disable", Category: "Failover", Severity: "warning", DefaultOn: true},
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
	for t := range strings.SplitSeq(csv, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out[t] = true
		}
	}
	return out
}
