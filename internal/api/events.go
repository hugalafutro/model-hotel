package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// sseHeartbeatInterval is how often an idle SSE stream emits a keepalive
// comment, and therefore also how often the caller's credentials are re-checked.
const sseHeartbeatInterval = 30 * time.Second

// sseReauthFailuresBeforeClose is how many consecutive failed credential
// re-checks close a stream, bounding authorization staleness at
// sseHeartbeatInterval * this.
//
// Tolerance rather than fail-fast because resolveCredentials cannot distinguish
// "this session was revoked" from "the store could not be asked": both surface
// as a plain false. A revoked session fails every check and is dropped on the
// second, while a transient store failure (Postgres restart, pool exhaustion)
// recovers on the next tick. Failing on the first miss would turn a brief
// outage into a fleet-wide forced logout, since the dashboard's SSE reconnect
// treats a 401 as "session gone" and reloads to the login screen.
const sseReauthFailuresBeforeClose = 2

// eventVisible decides whether one bus event may be forwarded to the caller.
// The bus itself is identity-blind, so the SSE handler filters per subscriber:
// admins see everything; request lifecycle events (the routing metadata the
// logs page tails live) need the logs grant; every other type is operational
// admin activity (backups, discovery, failover, fleet) and stays admin-only.
// "request.discovery.*" carries the request. prefix only to suppress frontend
// toasts; it is discovery progress, so it stays on the admin side. Default
// deny: an event type added later is invisible to limited users until someone
// deliberately maps it to a grant here.
func eventVisible(id *user.Identity, ev events.Event) bool {
	if id.IsAdmin() {
		return true
	}
	if strings.HasPrefix(ev.Type, "request.") && !strings.HasPrefix(ev.Type, "request.discovery.") {
		// The logs grant is necessary but not sufficient: a non-admin may only see
		// request events for keys they own, matching the owner-scoping the logs
		// REST API applies (ownerScopeFromIdentity). Without this, any logs-granted
		// user could tail every other user's live request metadata over SSE.
		return id.Can(user.GrantLogs) && eventOwnedBy(id, ev)
	}
	return false
}

// eventOwnedBy reports whether a request lifecycle event belongs to the caller's
// own virtual keys. It compares the event's owner_user_id metadata (the owning
// dashboard user's UUID, "" for unowned keys) against the caller's user id. A
// caller with no user id, or an event carrying no owner, is denied — unowned-key
// activity stays admin-only, exactly as the logs REST API scopes it.
func eventOwnedBy(id *user.Identity, ev events.Event) bool {
	if id.UserID == nil {
		return false
	}
	owner, _ := ev.Metadata["owner_user_id"].(string)
	return owner != "" && owner == id.UserID.String()
}

// StreamEvents handles server-sent events for real-time dashboard updates.
//
// The caller's identity is re-checked on every heartbeat rather than pinned at
// connect. AuthMiddleware only runs once, so a stream opened before a session
// was revoked, or before a user was disabled or had a grant taken away, would
// otherwise keep delivering events under its connect-time permissions for as
// long as the client held the socket open - unbounded, since the heartbeat
// keeps it alive. Re-resolving costs one session lookup per stream per
// heartbeat interval; once the credential stops resolving the stream closes and
// the client's normal SSE reconnect then fails at the middleware.
func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, sseHeartbeatInterval)
}

// reauthLoop re-resolves the caller's credentials on every tick and reports the
// result on out: the refreshed identity, or nil when the credentials did not
// resolve. Exits when the request context is cancelled.
//
// Runs off the read loop deliberately. resolveCredentials makes a session-store
// round-trip (plus a users lookup for a UUID handle), and the event bus drops
// events for any subscriber that is not draining its channel
// (internal/events/bus.go), so doing this inline would trade a slow credential
// lookup for lost live log rows on a busy gateway.
func (h *Handler) reauthLoop(r *http.Request, every time.Duration, out chan<- *user.Identity) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			id, _, ok, _ := h.resolveCredentials(r, false)
			if !ok {
				id = nil
			}
			select {
			case out <- id:
			case <-r.Context().Done():
				return
			}
		}
	}
}

// streamEvents is StreamEvents with the heartbeat cadence supplied by the
// caller, so the keepalive/re-auth tick can be driven without waiting out the
// production interval.
func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request, heartbeatEvery time.Duration) {
	identity := user.IdentityFrom(r.Context())
	flusher, ok := w.(http.Flusher)
	if !ok {
		util.WriteOpenAIError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	bus := h.eventBus
	if bus == nil {
		bus = events.DefaultBus
	}
	// Subscribe BEFORE announcing the stream. Announcing first opens a window
	// where the client believes it is attached and any event published in it is
	// dropped on the floor — a dashboard that connects during a burst silently
	// misses the start of it. It also makes the connected frame a sound
	// handshake: a reader that has seen those bytes knows the subscription
	// exists, which is what the tests rely on instead of sleeping.
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Write initial comment to establish the stream
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	// Buffered so a re-auth result never parks its goroutine while this loop is
	// busy writing an event; the loop drains it on the next pass.
	reauth := make(chan *user.Identity, 1)
	go h.reauthLoop(r, heartbeatEvery, reauth)

	failures := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				// The bus was closed (server shutting down): end the stream so
				// http.Server.Shutdown does not wait out its deadline on us.
				return
			}
			if !eventVisible(identity, event) {
				continue
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case id := <-reauth:
			// A re-auth result doubles as the keepalive tick. nil means the
			// credentials did not resolve; a live one refreshes the identity, so
			// a user who lost a grant stops seeing what it no longer covers.
			if id == nil {
				failures++
				if failures >= sseReauthFailuresBeforeClose {
					debuglog.Info("events: stream closed, credentials no longer valid",
						"remote_addr", clientip.From(r), "consecutive_failures", failures)
					return
				}
			} else {
				failures = 0
				identity = id
			}
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
