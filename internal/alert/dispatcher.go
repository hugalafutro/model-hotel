package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// defaultCooldown is the per-(event-type, provider) debounce window: repeat
// alerts for the same key inside this window are suppressed so a flapping
// circuit breaker cannot spam the operator.
const defaultCooldown = 5 * time.Minute

// defaultTimeout bounds a single outbound POST to apprise-api.
const defaultTimeout = 5 * time.Second

// Config is the resolved alerting configuration used for one dispatch decision.
type Config struct {
	Enabled    bool
	APIBaseURL string          // base URL of the apprise-api container, e.g. http://apprise:8000
	Targets    string          // resolved (decrypted) Apprise URL(s), ";"-joined
	Events     map[string]bool // enabled event Types (the operator's picker)
}

// ConfigProvider resolves the live alerting config: it reads settings and
// decrypts the target. Abstracted behind an interface so the dispatcher core is
// testable without a database or the master key.
type ConfigProvider interface {
	AlertConfig(ctx context.Context) (Config, error)
	// APIBaseURL returns just the apprise-api base URL without decrypting the
	// target secret. Probing reachability must not fail on a corrupt target or
	// a rotated MASTER_KEY when the URL itself is valid.
	APIBaseURL(ctx context.Context) (string, error)
}

// Dispatcher consumes the events bus and forwards selected events to a
// stateless apprise-api container as outbound notifications.
type Dispatcher struct {
	cfg      ConfigProvider
	client   *http.Client
	catalog  map[string]EventDef
	cooldown time.Duration

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// New constructs a Dispatcher. A nil client gets a sensible default.
func New(cfg ConfigProvider, client *http.Client) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Dispatcher{
		cfg:      cfg,
		client:   client,
		catalog:  catalogIndex(),
		cooldown: defaultCooldown,
		lastSent: make(map[string]time.Time),
	}
}

// Run subscribes to the events bus and dispatches matching events until ctx is
// cancelled. Best-effort: a failed send is logged, never fatal.
func (d *Dispatcher) Run(ctx context.Context) {
	ch := events.Subscribe()
	defer events.Unsubscribe(ch)
	debuglog.Info("alert: dispatcher started")
	for {
		select {
		case <-ctx.Done():
			debuglog.Info("alert: dispatcher stopped")
			return
		case ev := <-ch:
			d.handle(ctx, ev)
		}
	}
}

// handle applies the filter chain and, if every gate passes, dispatches the
// notification asynchronously. It returns true when a send was dispatched and
// false when the event was filtered out — the boolean is the synchronous,
// deterministic decision (the actual POST happens on its own goroutine). It
// never panics the caller — a misbehaving event or config is logged and dropped.
func (d *Dispatcher) handle(ctx context.Context, ev events.Event) bool {
	defer func() {
		if r := recover(); r != nil {
			debuglog.Warn("alert: recovered from panic while handling event", "type", ev.Type)
		}
	}()

	// Gate 1: only catalogued events are alertable.
	if _, ok := d.catalog[ev.Type]; !ok {
		return false
	}
	// Gate 2: alerting must be enabled and fully configured.
	cfg, err := d.cfg.AlertConfig(ctx)
	if err != nil {
		debuglog.Warn("alert: failed to load config", "error", err.Error())
		return false
	}
	if !cfg.Enabled || cfg.APIBaseURL == "" || strings.TrimSpace(cfg.Targets) == "" {
		return false
	}
	// Gate 3: the operator must have selected this event.
	if !cfg.Events[ev.Type] {
		return false
	}
	// Gate 4: debounce flapping.
	if d.suppressed(ev) {
		return false
	}

	// Dispatch on a separate goroutine: a slow or hanging apprise-api must never
	// block the event-drain loop, which would overflow the bus subscriber buffer
	// and drop unrelated events. Debounce above bounds how often we get here, so
	// the goroutine rate is naturally limited.
	payload := payloadFor(ev)
	go func() {
		if err := d.post(ctx, cfg, payload); err != nil {
			debuglog.Warn("alert: notify failed", "type", ev.Type, "error", err.Error())
			return
		}
		debuglog.Info("alert: notification sent", "type", ev.Type)
	}()
	return true
}

// suppressed implements per-(type, entity) debounce. Recovery events carry a
// different Type from their failure counterpart, so an "all clear" is never
// suppressed by a preceding failure. The send time is recorded on attempt
// (not on success) so a broken apprise-api is not hammered every event.
func (d *Dispatcher) suppressed(ev events.Event) bool {
	key := ev.Type
	if id := debounceID(ev.Metadata); id != "" {
		key += "|" + id
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if last, seen := d.lastSent[key]; seen && now.Sub(last) < d.cooldown {
		return true
	}
	d.lastSent[key] = now
	return false
}

// debounceID picks the most specific entity identifier present in an event's
// metadata, so failures for distinct providers/models debounce independently.
// Different event types label the entity differently: circuit_breaker.* carry
// "provider_id", discovery.provider_failed carries "provider" (a name), and
// failover.sync_error carries "model_id". Without this, two different providers
// failing inside the cooldown window would collapse to a single alert and the
// second failure would be silently dropped.
func debounceID(meta map[string]interface{}) string {
	for _, k := range []string{"provider_id", "provider", "model_id"} {
		if v, ok := meta[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// notifyPayload is the apprise-api stateless /notify request body.
type notifyPayload struct {
	URLs   string `json:"urls"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Type   string `json:"type"`
	Format string `json:"format"`
}

// payloadFor builds the notification for an event (target URLs filled in later).
func payloadFor(ev events.Event) notifyPayload {
	body := ev.Message
	if body == "" {
		body = ev.Type
	}
	return notifyPayload{
		Title:  "Model Hotel — " + ev.Type,
		Body:   body,
		Type:   appriseType(ev.Severity),
		Format: "text",
	}
}

// appriseType maps an internal event severity to an Apprise notification type.
func appriseType(severity string) string {
	switch severity {
	case "error":
		return "failure"
	case "warning":
		return "warning"
	case "success":
		return "success"
	default:
		return "info"
	}
}

// normalizeTargets converts the operator-facing ";"-separated target list into
// the whitespace-separated form apprise-api parses. ";" is the documented
// separator because — unlike commas — it does not collide with commas used
// inside a single Apprise URL (e.g. a multi-recipient mailto://). But apprise-api
// splits the `urls` field on whitespace/commas, not semicolons, so a raw
// ";"-joined string would be treated as one malformed URL and only the first
// destination (if any) would fire. Splitting on ";" and rejoining with spaces
// preserves both single targets and intra-URL commas.
func normalizeTargets(s string) string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// post sends a single notification to apprise-api's /notify endpoint.
func (d *Dispatcher) post(ctx context.Context, cfg Config, p notifyPayload) error {
	p.URLs = normalizeTargets(cfg.Targets)
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	endpoint := strings.TrimRight(cfg.APIBaseURL, "/") + "/notify"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("post to apprise-api: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("apprise-api returned status %d", resp.StatusCode)
	}
	return nil
}

// TestSend fires a synthetic notification through the same POST path, returning
// any error so the caller (the Settings "Send test" button) can surface
// success/failure to the operator. Unlike event dispatch, errors are returned.
func (d *Dispatcher) TestSend(ctx context.Context) error {
	cfg, err := d.cfg.AlertConfig(ctx)
	if err != nil {
		return fmt.Errorf("load alert config: %w", err)
	}
	if cfg.APIBaseURL == "" {
		return fmt.Errorf("apprise-api URL is not configured")
	}
	if strings.TrimSpace(cfg.Targets) == "" {
		return fmt.Errorf("notification target is not configured")
	}
	return d.post(ctx, cfg, notifyPayload{
		Title:  "Model Hotel — test notification",
		Body:   "If you can read this, Model Hotel alerting is wired up correctly.",
		Type:   "info",
		Format: "text",
	})
}
