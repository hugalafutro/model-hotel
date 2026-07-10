package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
)

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
func (h *Handler) StreamEvents(w http.ResponseWriter, r *http.Request) {
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

	// Write initial comment to establish the stream
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(ch)

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			if !eventVisible(identity, event) {
				continue
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
