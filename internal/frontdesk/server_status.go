package frontdesk

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/otelexport"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := EventFilter{
		MemberID: q.Get("member_id"),
		Type:     q.Get("type"),
		Severity: q.Get("severity"),
		Since:    parseRFC3339(q.Get("since")),
		Until:    parseRFC3339(q.Get("until")),
		Limit:    clampEventsLimit(atoiDefault(q.Get("limit"), defaultEventsLimit)),
		Offset:   max(atoiDefault(q.Get("offset"), 0), 0),
	}
	evs, total, err := s.store.ListEvents(r.Context(), f)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs, "total": total})
}

// ---------------------------------------------------------------------------
// Status + SSE
// ---------------------------------------------------------------------------

func (s *Server) traefikStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.poller.Snapshot())
}

// buildCommit is the source commit SHA this Front Desk binary was built from,
// stamped at build time via -ldflags -X (see the Makefile / Dockerfile.frontdesk)
// and surfaced read-only as app_commit so the UI footer can show which commit a
// `dev` build corresponds to. Defaults to "unknown" for un-stamped builds.
var buildCommit = "unknown"

// getVersion returns the running build's version and source commit so the UI
// footer can show which Front Desk build is deployed (and link a `dev` build to
// its commit on GitHub). app_version is "dev" for un-stamped builds; app_commit
// is normalized to a short prefix so it reads the same across build paths.
func (s *Server) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"app_version": s.version,
		"app_commit":  util.ShortCommit(buildCommit),
	})
}

// getObservability reports which log-export integrations are active, derived
// read-only from the process environment (LOG_FORMAT, OTEL_EXPORTER_OTLP_*).
// It mirrors the main server's log_export_* status keys so the Front Desk
// Observability panel can reflect the same state. Nothing here is runtime-
// changeable; each integration is enabled by its own environment variable.
// log_export_metrics reports whether a dedicated scrape token is configured
// (the /metrics endpoint itself always exists, gated by admin auth otherwise),
// matching the main server's semantics.
func (s *Server) getObservability(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{
		"log_export_json":    debuglog.JSONFormat(),
		"log_export_otel":    otelexport.LogsEnabled(),
		"log_export_metrics": s.metricsToken != "",
	})
}

// sseHeartbeat keeps idle SSE connections alive through proxies.
const sseHeartbeat = 25 * time.Second

func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.bus.Subscribe()
	defer s.bus.Unsubscribe(ch)

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// TOTP-enabled cache (mirrors the main server's cache so the gate stays DB-free
// on the hot path)
// ---------------------------------------------------------------------------

// totpStatusReader is the one method of *totp.Repository the cache depends on.
// It is an interface so the fail-closed behaviour on a read error is testable
// without a live database.
type totpStatusReader interface {
	IsEnabled(ctx context.Context) (bool, error)
}

type totpEnabledCache struct {
	repo totpStatusReader
	val  atomic.Bool
}

func newTotpEnabledCache(repo totpStatusReader) *totpEnabledCache {
	c := &totpEnabledCache{repo: repo}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	enabled, err := repo.IsEnabled(ctx)
	if err != nil {
		// Fail closed: treat as enabled so a startup DB blip cannot silently
		// weaken the gate.
		debuglog.Error("frontdesk: seeding TOTP-enabled cache failed, failing closed", "error", err)
		enabled = true
	}
	c.val.Store(enabled)
	return c
}

func (c *totpEnabledCache) Enabled() bool { return c.val.Load() }

func (c *totpEnabledCache) Refresh(ctx context.Context) {
	enabled, err := c.repo.IsEnabled(ctx)
	if err != nil {
		// Fail closed, matching the main server's RefreshTotpEnabled: a failed
		// re-read must never leave a stale "disabled" cached, which would let a
		// raw FRONTDESK_TOKEN through as a full session after TOTP was enabled.
		debuglog.Error("frontdesk: refreshing TOTP-enabled cache failed, failing closed", "error", err)
		c.val.Store(true)
		return
	}
	c.val.Store(enabled)
}

// emit persists a control-plane event and publishes it on the SSE bus.
func (s *Server) emit(ctx context.Context, e Event) {
	stored, err := s.store.InsertEvent(ctx, e)
	if err != nil {
		debuglog.Warn("frontdesk: persist event", "type", e.Type, "error", err)
		stored = e
	}
	s.bus.Publish(busEvent(stored))
}

// busEvent maps a stored Front Desk Event to a bus event. When the event concerns
// a member, its MemberID is copied into the metadata as "member_id" (on a copy, so
// the persisted metadata is untouched) so the alert dispatcher debounces per
// member. Shared by Server.emit and Poller.recordEvent.
func busEvent(e Event) events.Event {
	meta := e.Metadata
	if e.MemberID != "" {
		meta = make(map[string]any, len(e.Metadata)+1)
		maps.Copy(meta, e.Metadata)
		meta["member_id"] = e.MemberID
	}
	return events.Event{
		ID: e.ID, Type: e.Type, Severity: e.Severity, Source: e.Source,
		Message: e.Message, Metadata: meta, Timestamp: e.CreatedAt,
	}
}
