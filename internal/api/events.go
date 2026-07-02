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
func eventVisible(id *user.Identity, eventType string) bool {
	if id.IsAdmin() {
		return true
	}
	if strings.HasPrefix(eventType, "request.") && !strings.HasPrefix(eventType, "request.discovery.") {
		return id.Can(user.GrantLogs)
	}
	return false
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
			if !eventVisible(identity, event.Type) {
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
