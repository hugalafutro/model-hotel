package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/netguard"
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

// defaultTitlePrefix labels notifications from the main gateway. Front Desk
// overrides it via WithTitlePrefix so a fleet operator can tell the two apart.
const defaultTitlePrefix = "Model Hotel"

// defaultDebounceKeys are the metadata keys, most specific first, that identify
// the entity an event concerns so distinct entities debounce independently. The
// main app labels providers/models; Front Desk overrides this with member_id.
var defaultDebounceKeys = []string{"provider_id", "provider", "model_id"}

// Dispatcher consumes an events bus and forwards selected events to a
// stateless apprise-api container as outbound notifications.
type Dispatcher struct {
	cfg          ConfigProvider
	client       *http.Client
	bus          *events.Bus
	catalog      map[string]EventDef
	titlePrefix  string
	debounceKeys []string
	cooldown     time.Duration
	resultHook   func(ok bool) // observes each send attempt's outcome; nil disables

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// Option customizes a Dispatcher. Without any option the Dispatcher reproduces
// the main-app behavior (global bus, main catalog, "Model Hotel" title, provider/
// model debounce keys); Front Desk supplies its own via these options.
type Option func(*Dispatcher)

// WithBus subscribes the Dispatcher to a specific events bus instead of the
// package-global DefaultBus. Front Desk runs its own bus instance.
func WithBus(b *events.Bus) Option { return func(d *Dispatcher) { d.bus = b } }

// WithCatalog sets the alertable-event catalog (Type -> EventDef). Only events in
// the catalog can be alerted on, and the admin event picker is rendered from it.
func WithCatalog(defs []EventDef) Option {
	return func(d *Dispatcher) { d.catalog = catalogIndexOf(defs) }
}

// WithTitlePrefix sets the notification title prefix (e.g. "Front Desk").
func WithTitlePrefix(p string) Option { return func(d *Dispatcher) { d.titlePrefix = p } }

// WithDebounceKeys sets the metadata keys used to scope per-entity debounce. The
// slice is copied so a later mutation by the caller cannot change debounce behavior.
func WithDebounceKeys(keys []string) Option {
	return func(d *Dispatcher) { d.debounceKeys = append([]string(nil), keys...) }
}

// WithResultHook observes the outcome of every dispatched notification attempt
// (true on a successful POST, false on failure). It is called from the send
// goroutine, so it must be cheap and non-blocking; embedders use it to feed
// metrics without the dispatcher depending on a metrics package.
func WithResultHook(hook func(ok bool)) Option {
	return func(d *Dispatcher) { d.resultHook = hook }
}

// NewHTTPClient returns the SSRF-guarded client the alert handlers talk to
// apprise-api through, with this package's own timeout. A caller that serves
// several requests builds one and reuses it: each client owns a connection
// pool, so one per request means no keep-alive reuse and a pool of idle
// connections left behind with nothing to close them.
func NewHTTPClient() *http.Client {
	return netguard.NewClient(defaultTimeout)
}

// New constructs a Dispatcher. A nil client gets a sensible default. With no
// options it is the main-app dispatcher; pass options to embed it elsewhere.
func New(cfg ConfigProvider, client *http.Client, opts ...Option) *Dispatcher {
	if client == nil {
		// SSRF-guarded client: the apprise-api base URL is admin-configured, so a
		// hostile value must not reach cloud-metadata/link-local. It allows
		// private/loopback because apprise-api normally runs on the internal
		// docker network (e.g. http://apprise:8000).
		client = NewHTTPClient()
	}
	d := &Dispatcher{
		cfg:          cfg,
		client:       client,
		bus:          events.DefaultBus,
		catalog:      catalogIndex(),
		titlePrefix:  defaultTitlePrefix,
		debounceKeys: defaultDebounceKeys,
		cooldown:     defaultCooldown,
		lastSent:     make(map[string]time.Time),
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Run subscribes to the events bus and dispatches matching events until ctx is
// cancelled. Best-effort: a failed send is logged, never fatal.
func (d *Dispatcher) Run(ctx context.Context) {
	ch := d.bus.Subscribe()
	defer d.bus.Unsubscribe(ch)
	debuglog.Info("alert: dispatcher started")
	for {
		select {
		case <-ctx.Done():
			debuglog.Info("alert: dispatcher stopped")
			return
		case ev, ok := <-ch:
			if !ok {
				// Bus closed: the server is shutting down.
				debuglog.Info("alert: dispatcher stopped")
				return
			}
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
	payload := payloadFor(ev, d.titlePrefix)
	go func() {
		err := d.post(ctx, cfg, payload)
		if d.resultHook != nil {
			d.resultHook(err == nil)
		}
		if err != nil {
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
	if id := debounceID(ev.Metadata, d.debounceKeys); id != "" {
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
// second failure would be silently dropped. keys is the most-specific-first list
// to probe (the dispatcher's configured debounce keys).
func debounceID(meta map[string]any, keys []string) string {
	for _, k := range keys {
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
// titlePrefix labels the source app ("Model Hotel" or "Front Desk").
func payloadFor(ev events.Event, titlePrefix string) notifyPayload {
	body := ev.Message
	if body == "" {
		body = ev.Type
	}
	return notifyPayload{
		Title:  titlePrefix + ": " + ev.Type,
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

// DeliveryError is a failed test/dispatch POST with a stable Reason the UI can
// translate. HTTPStatus is apprise-api's answer (0 for transport failures).
type DeliveryError struct {
	Reason     string
	HTTPStatus int
	Err        error
}

func (e *DeliveryError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("apprise-api returned status %d", e.HTTPStatus)
}

func (e *DeliveryError) Unwrap() error { return e.Err }

// ReasonOf returns the reason code of a DeliveryError, or "" for nil / other
// errors, so handlers can attach it to their JSON error body without type
// assertions of their own.
func ReasonOf(err error) string {
	var de *DeliveryError
	if errors.As(err, &de) {
		return de.Reason
	}
	return ""
}

// SplitTargets splits the operator-facing ";"-joined target list into its
// trimmed, non-empty Apprise URLs. ";" is the documented separator because,
// unlike commas, it does not collide with commas inside one URL (a
// multi-recipient mailto://).
//
// Repeats are dropped, first occurrence winning so the operator's ordering
// survives: the same address listed twice only means apprise is asked to
// deliver the same notification to one phone twice, and every caller of this
// (delivery, the plaintext list the UI shows, the test endpoint) wants the set.
func SplitTargets(s string) []string {
	parts := strings.Split(s, ";")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			if _, dup := seen[p]; dup {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

// JoinTargets is the inverse of SplitTargets: the stored/operator form.
func JoinTargets(ts []string) string { return strings.Join(ts, "; ") }

// normalizeTargets converts the ";"-joined list into the whitespace-separated
// form apprise-api parses (it splits `urls` on whitespace/commas, never ";").
func normalizeTargets(s string) string { return strings.Join(SplitTargets(s), " ") }

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
		return &DeliveryError{Reason: ReasonUnreachable, Err: fmt.Errorf("post to apprise-api: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	// The apprise response body is deliberately never read into the error: it
	// can echo target URLs.
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusBadRequest:
		return &DeliveryError{Reason: ReasonAppriseReject, HTTPStatus: resp.StatusCode}
	case resp.StatusCode == http.StatusFailedDependency:
		return &DeliveryError{Reason: ReasonDeliverFailed, HTTPStatus: resp.StatusCode}
	default:
		return &DeliveryError{Reason: ReasonUnhealthy, HTTPStatus: resp.StatusCode}
	}
}

// TestSend fires a synthetic notification through the saved configuration.
func (d *Dispatcher) TestSend(ctx context.Context) error {
	cfg, err := d.cfg.AlertConfig(ctx)
	if err != nil {
		return fmt.Errorf("load alert config: %w", err)
	}
	return d.TestSendTo(ctx, cfg)
}

// TestBodyPrefix opens the body of every test notification. Bellhop matches on
// this exact prefix to tell a Front Desk test push apart from a real alert wake,
// so it acknowledges the test with its own notification; changing it here without
// changing TEST_BODY_PREFIX in Bellhop's push package silently breaks that.
const TestBodyPrefix = "Test notification from "

// TestSendTo fires the synthetic notification through an explicit config, so
// the setup wizard can test a URL/target pair before anything is saved. Errors
// are DeliveryErrors (reason-coded) so the caller can tell the operator what to
// fix.
func (d *Dispatcher) TestSendTo(ctx context.Context, cfg Config) error {
	if strings.TrimSpace(cfg.APIBaseURL) == "" {
		return &DeliveryError{Reason: ReasonNotConfigured, Err: fmt.Errorf("apprise-api URL is not configured")}
	}
	if strings.TrimSpace(cfg.Targets) == "" {
		return &DeliveryError{Reason: ReasonNotConfigured, Err: fmt.Errorf("notification target is not configured")}
	}
	return d.post(ctx, cfg, notifyPayload{
		Title:  d.titlePrefix + ": test notification",
		Body:   TestBodyPrefix + d.titlePrefix + ": if you can read this, alerting is wired up correctly.",
		Type:   "info",
		Format: "text",
	})
}
