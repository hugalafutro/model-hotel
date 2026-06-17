package alert

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Status is the reachability of the operator's apprise-api, surfaced in the
// Settings → Alerts UI so a misconfigured or stopped container is visible
// instead of failing silently at dispatch time.
type Status struct {
	Configured bool   `json:"configured"` // an apprise-api URL is set
	Reachable  bool   `json:"reachable"`  // the host answered an HTTP request
	Healthy    bool   `json:"healthy"`    // GET /status returned 2xx
	Detail     string `json:"detail,omitempty"`
}

// Probe checks the configured apprise-api by issuing GET {base}/status. It does
// not require the notification target — this is purely "can we reach the
// container". It never returns a transport error as a Go error (a down host is
// a normal, reportable state); err is non-nil only when the config can't load.
func (d *Dispatcher) Probe(ctx context.Context) (Status, error) {
	// Read only the base URL — never the encrypted target — so a corrupt target
	// secret or rotated MASTER_KEY cannot fail a reachability check.
	rawBase, err := d.cfg.APIBaseURL(ctx)
	if err != nil {
		return Status{}, err
	}
	base := strings.TrimSpace(rawBase)
	if base == "" {
		return Status{Configured: false}, nil
	}

	endpoint := strings.TrimRight(base, "/") + "/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Status{Configured: true, Detail: "invalid apprise-api URL"}, nil
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := d.client.Do(req)
	if err != nil {
		return Status{Configured: true, Reachable: false, Detail: "unreachable"}, nil
	}
	defer func() { _ = resp.Body.Close() }()

	healthy := resp.StatusCode >= 200 && resp.StatusCode < 300
	detail := "ok"
	if !healthy {
		// apprise-api answers 417 when it is up but reporting an internal issue.
		detail = fmt.Sprintf("apprise-api returned status %d", resp.StatusCode)
	}
	return Status{Configured: true, Reachable: true, Healthy: healthy, Detail: detail}, nil
}
