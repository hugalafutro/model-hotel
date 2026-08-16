package alert

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Reason codes carried by Status.Reason and DeliveryError.Reason. The UI
// translates these; Detail stays a human string for tooltips and logs.
const (
	ReasonNotConfigured = "not_configured" // no apprise-api URL / no targets
	ReasonInvalidURL    = "invalid_url"    // the URL cannot form a request
	ReasonUnreachable   = "unreachable"    // transport error talking to apprise-api
	ReasonUnhealthy     = "unhealthy"      // apprise-api answered with an unexpected non-2xx
	ReasonAppriseReject = "apprise_reject" // /notify answered 400: the target URL is malformed
	ReasonDeliverFailed = "deliver_failed" // /notify answered 424: apprise could not deliver
	ReasonUndecryptable = "undecryptable"  // stored target cannot be decrypted, e.g. MASTER_KEY rotated
)

// Status is the reachability of an apprise-api, surfaced in the Settings ->
// Alerts UI so a misconfigured or stopped container is visible instead of
// failing silently at dispatch time.
type Status struct {
	Configured bool   `json:"configured"`       // an apprise-api URL is set
	Reachable  bool   `json:"reachable"`        // the host answered an HTTP request
	Healthy    bool   `json:"healthy"`          // GET /status returned 2xx
	Reason     string `json:"reason,omitempty"` // one of the Reason* codes when not healthy
	Detail     string `json:"detail,omitempty"`
}

// Probe checks the configured apprise-api. It reads only the base URL, never
// the encrypted target, so a corrupt target secret or rotated MASTER_KEY cannot
// fail a reachability check. err is non-nil only when the config can't load.
func (d *Dispatcher) Probe(ctx context.Context) (Status, error) {
	rawBase, err := d.cfg.APIBaseURL(ctx)
	if err != nil {
		return Status{}, err
	}
	return d.ProbeURL(ctx, rawBase), nil
}

// ProbeURL checks an explicit apprise-api base URL by issuing GET {base}/status.
// It never returns a transport error as a Go error: a down host is a normal,
// reportable state. The setup wizard uses it to verify a URL before it is saved.
func (d *Dispatcher) ProbeURL(ctx context.Context, base string) Status {
	base = strings.TrimSpace(base)
	if base == "" {
		return Status{Configured: false, Reason: ReasonNotConfigured}
	}
	// Reject anything that isn't a real http(s) URL up front: url.Parse alone
	// accepts garbage like "apprise:8000" (parsed as scheme "apprise", opaque
	// "8000", empty Host) or "ftp://h", which would otherwise reach the HTTP
	// client and get misreported as "unreachable" instead of "invalid".
	if u, err := url.Parse(base); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Status{Configured: true, Reason: ReasonInvalidURL, Detail: "invalid apprise-api URL"}
	}
	endpoint := strings.TrimRight(base, "/") + "/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Status{Configured: true, Reason: ReasonInvalidURL, Detail: "invalid apprise-api URL"}
	}
	req.Header.Set("Accept", "text/plain")

	resp, err := d.client.Do(req)
	if err != nil {
		return Status{Configured: true, Reachable: false, Reason: ReasonUnreachable, Detail: "unreachable"}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Status{Configured: true, Reachable: true, Healthy: true, Detail: "ok"}
	}
	// apprise-api answers 417 when it is up but reporting an internal issue.
	return Status{
		Configured: true, Reachable: true, Healthy: false, Reason: ReasonUnhealthy,
		Detail: fmt.Sprintf("apprise-api returned status %d", resp.StatusCode),
	}
}
