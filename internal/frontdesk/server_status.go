package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/clientip"
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

// sseHeartbeat keeps idle SSE connections alive through proxies, and is
// therefore also how often the caller's credentials are re-checked.
const sseHeartbeat = 25 * time.Second

// sseReauthFailuresBeforeClose is how many consecutive failed credential
// re-checks close a stream, bounding authorization staleness at
// sseHeartbeat * this.
//
// Tolerance rather than fail-fast because revalidate cannot distinguish "this
// device was unpaired" from "the store could not be asked": both surface as a
// plain false. A revoked credential fails every check and is dropped on the
// second, while a transient store failure (a locked SQLite file, a restart mid
// query) recovers on the next tick. Failing on the first miss would turn a brief
// blip into a forced logout, since the SPA's SSE reconnect treats a 401 as
// "session gone" and sends the operator back to the login screen.
const sseReauthFailuresBeforeClose = 2

// sse streams control-plane events to the dashboard and to paired devices.
//
// The caller's credentials are re-checked on every heartbeat rather than pinned
// at connect. requireAuth only runs once, so a stream opened before a device was
// unpaired or a session revoked would otherwise keep delivering events for as
// long as the client held the socket open - unbounded, since the heartbeat keeps
// it alive. Unlike the main server's stream this re-check is validity-only: Front
// Desk SSE has no per-subscriber filtering (every authenticated caller sees the
// whole bus), so there is no identity to refresh.
func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	s.streamEvents(w, r, sseHeartbeat)
}

// revalidate re-runs the connect-time admission of requireAuth: a device token
// that still resolves to a non-revoked device, otherwise the admin-or-session
// gate. Neither branch counts as use: a device's last_seen_at is not
// re-stamped and a session is verified without a last-seen stamp or an expiry
// slide (adminauth.ValidAdminOrSession), because this is a heartbeat the server
// drives, not a request the person made.
//
// A device-lookup failure falls through to the admin/session gate, exactly as
// requireAuth does: that gate never reads paired_devices, so a broken or
// unavailable table cannot close an admin-bearer stream. A device token then
// fails the gate and the tick counts as a failed check, which the tolerance
// absorbs when the failure was a one-off blip. The error is logged only while
// the request is live; a client hanging up races the tick and is not a fault.
func (s *Server) revalidate(r *http.Request) bool {
	if token, ok := util.ParseBearerToken(r); ok {
		_, err := s.store.DeviceByTokenHash(r.Context(), hashDeviceToken(token))
		if err == nil {
			return true
		}
		if !errors.Is(err, ErrNotFound) && r.Context().Err() == nil {
			debuglog.Error("frontdesk: sse device token re-check", "error", err)
		}
	}
	return adminauth.ValidAdminOrSession(r, s.adminMgr, s.sessionMgr, s.totpStatus.Enabled, authcookie.FrontDesk)
}

// reauthLoop re-checks the caller's credentials on every tick and reports the
// verdict on out. Exits when ctx is cancelled.
//
// ctx is the stream's own child of the request context, not r.Context() itself:
// the parked send below escapes only on a cancel, and the request context does not
// fire until after the handler has returned, which is too late for the handler to
// wait on this goroutine. Cancelling the child first is what lets streamEvents
// join it.
//
// Runs off the read loop deliberately. revalidate makes a store round-trip, and
// the event bus drops events for any subscriber that is not draining its channel
// (internal/events/bus.go), so doing this inline would trade a slow credential
// lookup for lost live events on a busy control plane.
func (s *Server) reauthLoop(ctx context.Context, r *http.Request, every time.Duration, out chan<- bool) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			valid := s.revalidate(r)
			select {
			case out <- valid:
			case <-ctx.Done():
				return
			}
		}
	}
}

// streamEvents is sse with the heartbeat cadence supplied by the caller, so the
// keep-alive/re-auth tick can be driven without waiting out the production
// interval.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request, heartbeatEvery time.Duration) {
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

	// Buffered so a re-auth verdict never parks its goroutine while this loop is
	// busy writing an event; the loop drains it on the next pass.
	reauth := make(chan bool, 1)
	// The ticker reads the session and device tables, so it is tracked (Shutdown's
	// drain covers it, even when the HTTP drain gave up on this stream) and joined
	// (the handler does not return while a store read of its own is still in
	// flight). Cancelling loopCtx before the join is what unparks a ticker sitting
	// on the send above.
	loopCtx, cancelLoop := context.WithCancel(r.Context())
	loopDone := make(chan struct{})
	if !s.StartBackground(loopCtx, func(ctx context.Context) {
		defer close(loopDone)
		s.reauthLoop(ctx, r, heartbeatEvery, reauth)
	}) {
		// Shutting down: nothing will re-check this caller or drive the keep-alive,
		// so end the stream now rather than serve one that cannot expire.
		cancelLoop()
		return
	}
	defer func() {
		cancelLoop()
		<-loopDone
	}()

	failures := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case valid := <-reauth:
			// A re-auth verdict doubles as the keep-alive tick.
			if valid {
				failures = 0
			} else {
				failures++
				if failures >= sseReauthFailuresBeforeClose {
					debuglog.Info("frontdesk: sse stream closed, credentials no longer valid",
						"remote_addr", clientip.From(r), "consecutive_failures", failures)
					return
				}
			}
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

// emit persists a control-plane event and publishes it on the SSE bus. The
// publish is best-effort on a failed insert; closeSyncHold, whose correctness
// leans on the persisted log, inserts and publishes by hand instead.
func (s *Server) emit(ctx context.Context, e Event) {
	stored, err := s.store.InsertEvent(ctx, e)
	if err != nil {
		debuglog.Warn("frontdesk: persist event", "type", e.Type, "error", err)
		stored = e
	}
	logEvent(stored)
	s.bus.Publish(busEvent(stored))
}

// logEvent mirrors a control-plane event into the process log at the level
// its severity implies, so the stdout/OTLP log exports carry the fleet's
// operational story (members going up/down, syncs, holds, alerts) and not
// only failures of Front Desk itself: without this, a healthy Front Desk
// emits almost nothing above DEBUG. Message and metadata are the same
// operator-facing values the Events tab shows; the event id lets a log line
// be matched to its row. Shared by Server.emit, Poller.recordEvent and
// Server.closeSyncHold.
func logEvent(e Event) {
	attrs := make([]any, 0, 2*(len(e.Metadata)+3))
	attrs = append(attrs, "event", e.Type)
	// Empty ids (a failed insert leaves no event id; fleet-wide events have no
	// member) are omitted rather than logged blank, so label-based collectors
	// never see an empty value.
	if e.ID != "" {
		attrs = append(attrs, "event_id", e.ID)
	}
	if e.MemberID != "" {
		attrs = append(attrs, "member_id", e.MemberID)
	}
	keys := slices.Sorted(maps.Keys(e.Metadata))
	for _, k := range keys {
		attrs = append(attrs, k, e.Metadata[k])
	}
	msg := "frontdesk: " + e.Message
	switch e.Severity {
	case "error", "critical":
		debuglog.Error(msg, attrs...)
	case "warning":
		debuglog.Warn(msg, attrs...)
	default: // info, success
		debuglog.Info(msg, attrs...)
	}
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
