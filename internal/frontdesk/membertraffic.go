package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// This file proxies a member's own request/error time series for the Traffic
// tab. Front Desk calls the member's admin-authenticated /api/stats/timeseries
// (last hour, 5-minute buckets) with the stored admin token and reshapes it to
// the small payload the UI charts. No request or prompt content is read: only
// per-bucket request and error counts, which are routing/metering metadata.

const memberTimeSeriesPath = "/api/stats/timeseries?period=1h"

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
	m, token, err := s.memberTokenOrErr(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, err)
			return
		}
		// No stored token (ErrValidation) or a decrypt failure: not chartable,
		// but a normal state, so report unreachable rather than erroring.
		writeJSON(w, http.StatusOK, memberTrafficResponse{MemberID: id, Reachable: false, WindowMinutes: 60})
		return
	}

	resp := s.fetchMemberTraffic(r.Context(), m, token)
	writeJSON(w, http.StatusOK, resp)
}

// fetchMemberTraffic proxies and reshapes one member's time series, returning a
// reachable=false response on any transport, status, or parse failure (the
// member may simply be down; the Traffic tab degrades gracefully per member).
func (s *Server) fetchMemberTraffic(ctx context.Context, m *Member, token string) memberTrafficResponse {
	out := memberTrafficResponse{MemberID: m.ID, WindowMinutes: 60, Points: []trafficPoint{}}

	status, body, err := s.callMember(ctx, http.MethodGet, m.URL, memberTimeSeriesPath, token, nil)
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

	out.Reachable = true
	out.Points = make([]trafficPoint, 0, len(ts.Points))
	for _, p := range ts.Points {
		out.Points = append(out.Points, trafficPoint{Bucket: p.Bucket, Requests: p.Count, Errors: p.Errors})
		out.TotalRequests += p.Count
		out.TotalErrors += p.Errors
	}
	return out
}
