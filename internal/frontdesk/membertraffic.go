package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// This file proxies a member's own request/error time series for the Traffic
// tab. Front Desk calls the member's admin-authenticated /api/stats/timeseries
// with the stored admin token and reshapes it to the small payload the UI
// charts. No request or prompt content is read: only per-bucket request and
// error counts, which are routing/metering metadata.

// memberTimeSeriesPath is the member series we chart: 5-minute buckets. Despite
// the "1h" period name the member returns roughly the last 24h of buckets (the
// period only selects bucket granularity, not the span), so a caller asking for
// a shorter window is served by trimming the tail here rather than by asking the
// member for less.
const memberTimeSeriesPath = "/api/stats/timeseries?period=1h"

const (
	// defaultTrafficWindowMinutes is the window_minutes echoed when the caller
	// sends no ?window= (e.g. Front Desk's own Traffic page): the legacy
	// full-series behaviour, kept unchanged.
	defaultTrafficWindowMinutes = 60
	// A requested window is clamped to this range: the 5-minute series only spans
	// ~24h, and a single bucket is the floor.
	minTrafficWindowMinutes = 5
	maxTrafficWindowMinutes = 24 * 60
)

// trafficPoint is one time bucket: total requests and the error subset.
type trafficPoint struct {
	Bucket   string `json:"bucket"`
	Requests int    `json:"requests"`
	Errors   int    `json:"errors"`
}

// memberTrafficResponse is the Traffic-tab payload for one member. Reachable is
// false when the member has no stored token or its stats API could not be read,
// so the UI can show an explanatory empty state instead of an error.
type memberTrafficResponse struct {
	MemberID      string         `json:"member_id"`
	Reachable     bool           `json:"reachable"`
	WindowMinutes int            `json:"window_minutes"`
	TotalRequests int            `json:"total_requests"`
	TotalErrors   int            `json:"total_errors"`
	Points        []trafficPoint `json:"points"`
}

// memberTimeSeries is the subset of the member /api/stats/timeseries response we
// consume.
type memberTimeSeries struct {
	Points []struct {
		Bucket string `json:"bucket"`
		Count  int    `json:"count"`
		Errors int    `json:"errors"`
	} `json:"points"`
}

func (s *Server) memberTraffic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	windowMin := parseTrafficWindow(r)
	m, token, err := s.memberTokenOrErr(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, err)
			return
		}
		// No stored token (ErrValidation) or a decrypt failure: not chartable,
		// but a normal state, so report unreachable rather than erroring.
		writeJSON(w, http.StatusOK, memberTrafficResponse{MemberID: id, Reachable: false, WindowMinutes: reportedWindow(windowMin)})
		return
	}

	resp := s.fetchMemberTraffic(r.Context(), m, token, windowMin)
	writeJSON(w, http.StatusOK, resp)
}

// parseTrafficWindow reads the optional ?window=<minutes> filter. Absent, blank,
// non-numeric, or non-positive means "no window" (0): the legacy full-series
// behaviour, so a caller that never sends the param (Front Desk's own Traffic
// page) is unaffected. A given value is clamped to [min,max].
func parseTrafficWindow(r *http.Request) int {
	q := r.URL.Query().Get("window")
	if q == "" {
		return 0
	}
	n, err := strconv.Atoi(q)
	if err != nil || n <= 0 {
		return 0
	}
	if n < minTrafficWindowMinutes {
		return minTrafficWindowMinutes
	}
	if n > maxTrafficWindowMinutes {
		return maxTrafficWindowMinutes
	}
	return n
}

// reportedWindow is the window_minutes echoed to the caller: the requested
// window, or the legacy default when none was asked for.
func reportedWindow(windowMin int) int {
	if windowMin <= 0 {
		return defaultTrafficWindowMinutes
	}
	return windowMin
}

// fetchMemberTraffic proxies and reshapes one member's time series, returning a
// reachable=false response on any transport, status, or parse failure (the
// member may simply be down; the Traffic tab degrades gracefully per member).
// It uses the longer-deadline read client, not the fast health probe: a member
// aggregating an hour of buckets can take longer than a liveness check, and the
// probe timeout would mislabel that slow-but-successful read as unreachable.
func (s *Server) fetchMemberTraffic(ctx context.Context, m *Member, token string, windowMin int) memberTrafficResponse {
	out := memberTrafficResponse{MemberID: m.ID, WindowMinutes: reportedWindow(windowMin), Points: []trafficPoint{}}

	status, body, err := s.callMemberWith(ctx, s.readClient, http.MethodGet, m.URL, memberTimeSeriesPath, token, nil)
	if err != nil {
		debuglog.Debug("frontdesk: member traffic fetch failed", "member", m.Name, "error", err)
		return out
	}
	if status != http.StatusOK {
		debuglog.Debug("frontdesk: member traffic non-200", "member", m.Name, "status", status)
		return out
	}
	var ts memberTimeSeries
	if err := json.Unmarshal(body, &ts); err != nil {
		// Don't wrap the decoder error: it can echo a fragment of the response.
		debuglog.Debug("frontdesk: member traffic parse failed", "member", m.Name)
		return out
	}

	// A requested window keeps only the tail of the ~24h series; totals are
	// summed over the kept buckets so they match what's charted. window 0 (no
	// param) keeps everything, the legacy behaviour. A bucket whose timestamp
	// can't be parsed is kept rather than aged out, so odd data is never silently
	// dropped.
	var cutoff time.Time
	if windowMin > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(windowMin) * time.Minute)
	}
	out.Reachable = true
	out.Points = make([]trafficPoint, 0, len(ts.Points))
	for _, p := range ts.Points {
		if windowMin > 0 {
			if bt, perr := time.Parse(time.RFC3339, p.Bucket); perr == nil && bt.Before(cutoff) {
				continue
			}
		}
		out.Points = append(out.Points, trafficPoint{Bucket: p.Bucket, Requests: p.Count, Errors: p.Errors})
		out.TotalRequests += p.Count
		out.TotalErrors += p.Errors
	}
	return out
}
