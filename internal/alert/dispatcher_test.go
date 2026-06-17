package alert

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// fakeCfg is a static ConfigProvider for tests.
type fakeCfg struct {
	cfg Config
	err error
}

func (f fakeCfg) AlertConfig(_ context.Context) (Config, error) { return f.cfg, f.err }

// recordingServer captures the notify payloads apprise-api would receive.
type recordingServer struct {
	*httptest.Server
	mu       sync.Mutex
	payloads []notifyPayload
	status   int // response status to return (0 => 200)
}

func newRecordingServer() *recordingServer {
	rs := &recordingServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/notify" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var p notifyPayload
		_ = json.Unmarshal(body, &p)
		rs.mu.Lock()
		rs.payloads = append(rs.payloads, p)
		st := rs.status
		rs.mu.Unlock()
		if st != 0 {
			w.WriteHeader(st)
		}
	}))
	return rs
}

func (rs *recordingServer) count() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return len(rs.payloads)
}

func (rs *recordingServer) last() notifyPayload {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.payloads[len(rs.payloads)-1]
}

func enabledConfig(base, targets string, selected ...string) Config {
	ev := map[string]bool{}
	for _, e := range selected {
		ev[e] = true
	}
	return Config{Enabled: true, APIBaseURL: base, Targets: targets, Events: ev}
}

func TestAppriseType(t *testing.T) {
	cases := map[string]string{
		"error":   "failure",
		"warning": "warning",
		"success": "success",
		"info":    "info",
		"":        "info",
		"weird":   "info",
	}
	for in, want := range cases {
		if got := appriseType(in); got != want {
			t.Errorf("appriseType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPayloadForUsesMessageElseType(t *testing.T) {
	p := payloadFor(events.Event{Type: "circuit_breaker.open", Message: "Provider x: open", Severity: "warning"})
	if p.Body != "Provider x: open" {
		t.Errorf("body = %q", p.Body)
	}
	if p.Type != "warning" {
		t.Errorf("type = %q", p.Type)
	}
	p2 := payloadFor(events.Event{Type: "circuit_breaker.open", Severity: "warning"})
	if p2.Body != "circuit_breaker.open" {
		t.Errorf("empty-message body fallback = %q", p2.Body)
	}
}

func TestNormalizeTargets(t *testing.T) {
	cases := []struct{ in, want string }{
		{"tgram://tok/chat", "tgram://tok/chat"},
		{"tgram://a/b;discord://c/d", "tgram://a/b discord://c/d"},
		{" tgram://a/b ; discord://c/d ", "tgram://a/b discord://c/d"},
		// a comma inside a single URL (e.g. multi-recipient mailto) is preserved
		{"mailto://u:p@x?to=a@x,b@y;nfy:t", "mailto://u:p@x?to=a@x,b@y nfy:t"},
		{"", ""},
		{";;", ""},
	}
	for _, c := range cases {
		if got := normalizeTargets(c.in); got != c.want {
			t.Errorf("normalizeTargets(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPostNormalizesMultipleTargets(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	d := New(fakeCfg{cfg: enabledConfig(rs.URL, "tgram://a/b;discord://c/d", "circuit_breaker.open")}, rs.Client())

	if !d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning"}) {
		t.Fatal("expected dispatch")
	}
	waitForCount(t, rs, 1)
	// apprise-api parses whitespace/commas, not semicolons.
	if got := rs.last().URLs; got != "tgram://a/b discord://c/d" {
		t.Errorf("urls = %q, want space-separated", got)
	}
}

// waitForCount blocks until the recording server has seen exactly n POSTs (the
// dispatch is asynchronous) or fails after a short deadline.
func waitForCount(t *testing.T, rs *recordingServer, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rs.count() == n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d POSTs, got %d", n, rs.count())
}

func TestHandleDispatchesSelectedEvent(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	d := New(fakeCfg{cfg: enabledConfig(rs.URL, "tgram://tok/chat", "circuit_breaker.open")}, rs.Client())

	if !d.handle(context.Background(), events.Event{
		Type:     "circuit_breaker.open",
		Severity: "warning",
		Message:  "Provider openai circuit breaker: open",
	}) {
		t.Fatal("expected handle to dispatch")
	}

	waitForCount(t, rs, 1)
	got := rs.last()
	if got.URLs != "tgram://tok/chat" {
		t.Errorf("urls = %q", got.URLs)
	}
	if got.Type != "warning" {
		t.Errorf("type = %q", got.Type)
	}
	if got.Body != "Provider openai circuit breaker: open" {
		t.Errorf("body = %q", got.Body)
	}
}

func TestHandleSkipsUncataloguedEvent(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	d := New(fakeCfg{cfg: enabledConfig(rs.URL, "tgram://x", "request.completed")}, rs.Client())
	if d.handle(context.Background(), events.Event{Type: "request.completed", Severity: "info"}) {
		t.Error("uncatalogued event should not dispatch")
	}
}

func TestHandleSkipsWhenDisabledOrUnconfigured(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	cases := []Config{
		{Enabled: false, APIBaseURL: rs.URL, Targets: "tgram://x", Events: map[string]bool{"circuit_breaker.open": true}},
		{Enabled: true, APIBaseURL: "", Targets: "tgram://x", Events: map[string]bool{"circuit_breaker.open": true}},
		{Enabled: true, APIBaseURL: rs.URL, Targets: "   ", Events: map[string]bool{"circuit_breaker.open": true}},
	}
	for i, c := range cases {
		d := New(fakeCfg{cfg: c}, rs.Client())
		if d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning"}) {
			t.Errorf("case %d: expected no dispatch", i)
		}
	}
}

func TestHandleSkipsUnselectedEvent(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	// catalogued + enabled, but the operator did not select this event.
	d := New(fakeCfg{cfg: enabledConfig(rs.URL, "tgram://x", "circuit_breaker.closed")}, rs.Client())
	if d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning"}) {
		t.Error("unselected event should not dispatch")
	}
}

func TestHandleConfigErrorIsSwallowed(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	d := New(fakeCfg{err: context.DeadlineExceeded}, rs.Client())
	if d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning"}) {
		t.Error("config error should drop the event")
	}
}

func TestDebounceSuppressesFlapping(t *testing.T) {
	d := New(fakeCfg{cfg: enabledConfig("http://unused", "tgram://x", "circuit_breaker.open")}, nil)
	d.cooldown = time.Minute // large window: the decision is synchronous, no real time elapses

	ev := events.Event{Type: "circuit_breaker.open", Severity: "warning", Metadata: map[string]interface{}{"provider_id": "p1"}}
	if !d.handle(context.Background(), ev) {
		t.Fatal("first event should dispatch")
	}
	if d.handle(context.Background(), ev) {
		t.Error("immediate repeat for the same provider should be suppressed")
	}
	if d.handle(context.Background(), ev) {
		t.Error("a third repeat for the same provider should still be suppressed")
	}

	// Different provider => different debounce key => allowed.
	ev2 := events.Event{Type: "circuit_breaker.open", Severity: "warning", Metadata: map[string]interface{}{"provider_id": "p2"}}
	if !d.handle(context.Background(), ev2) {
		t.Error("a different provider should not be suppressed")
	}
}

// TestDebounceDistinctEntities guards the fix for events whose entity id is not
// "provider_id": discovery.provider_failed labels it "provider", failover.sync_error
// labels it "model_id". Distinct entities must alert independently.
func TestDebounceDistinctEntities(t *testing.T) {
	d := New(fakeCfg{cfg: enabledConfig("http://unused", "tgram://x", "discovery.provider_failed", "failover.sync_error")}, nil)
	d.cooldown = time.Minute

	disc := func(provider string) events.Event {
		return events.Event{Type: "discovery.provider_failed", Severity: "error", Metadata: map[string]interface{}{"provider": provider}}
	}
	if !d.handle(context.Background(), disc("openai")) {
		t.Fatal("first provider discovery failure should dispatch")
	}
	if !d.handle(context.Background(), disc("anthropic")) {
		t.Error("a different provider's discovery failure must not be suppressed")
	}
	if d.handle(context.Background(), disc("openai")) {
		t.Error("the same provider repeated should be suppressed")
	}

	sync := func(model string) events.Event {
		return events.Event{Type: "failover.sync_error", Severity: "warning", Metadata: map[string]interface{}{"model_id": model}}
	}
	if !d.handle(context.Background(), sync("gpt-4o")) || !d.handle(context.Background(), sync("claude")) {
		t.Error("distinct model sync errors must alert independently")
	}
}

func TestDebounceAllowsAfterCooldown(t *testing.T) {
	d := New(fakeCfg{cfg: enabledConfig("http://unused", "tgram://x", "circuit_breaker.open")}, nil)
	d.cooldown = 30 * time.Millisecond

	ev := events.Event{Type: "circuit_breaker.open", Severity: "warning", Metadata: map[string]interface{}{"provider_id": "p1"}}
	if !d.handle(context.Background(), ev) {
		t.Fatal("first event should dispatch")
	}
	if d.handle(context.Background(), ev) {
		t.Error("immediate repeat should be suppressed")
	}
	time.Sleep(60 * time.Millisecond) // exceed the cooldown
	if !d.handle(context.Background(), ev) {
		t.Error("after the cooldown elapses the event should dispatch again")
	}
}

func TestDebounceRecoveryNotSuppressedByFailure(t *testing.T) {
	d := New(fakeCfg{cfg: enabledConfig("http://unused", "tgram://x", "circuit_breaker.open", "circuit_breaker.closed")}, nil)
	d.cooldown = time.Minute

	meta := map[string]interface{}{"provider_id": "p1"}
	open := d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning", Metadata: meta})
	closed := d.handle(context.Background(), events.Event{Type: "circuit_breaker.closed", Severity: "success", Metadata: meta})
	if !open || !closed {
		t.Error("open then closed (different types) should both dispatch")
	}
}

func TestHandleSwallowsUpstreamError(t *testing.T) {
	rs := newRecordingServer()
	rs.status = http.StatusInternalServerError
	defer rs.Close()
	d := New(fakeCfg{cfg: enabledConfig(rs.URL, "tgram://x", "circuit_breaker.open")}, rs.Client())
	// Should not panic or propagate; the POST is attempted (recorded) but errors.
	if !d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning"}) {
		t.Fatal("handle should still dispatch even if the POST will fail")
	}
	waitForCount(t, rs, 1)
}

func TestTestSendValidatesConfig(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()

	if err := New(fakeCfg{cfg: Config{}}, rs.Client()).TestSend(context.Background()); err == nil {
		t.Error("expected error when apprise-api URL missing")
	}
	if err := New(fakeCfg{cfg: Config{APIBaseURL: rs.URL}}, rs.Client()).TestSend(context.Background()); err == nil {
		t.Error("expected error when target missing")
	}
	if err := New(fakeCfg{err: context.Canceled}, rs.Client()).TestSend(context.Background()); err == nil {
		t.Error("expected error when config load fails")
	}
}

func TestRunDispatchesFromBus(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	d := New(fakeCfg{cfg: enabledConfig(rs.URL, "tgram://x", "circuit_breaker.open")}, rs.Client())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	// Give Run a moment to subscribe before publishing.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events.Publish(events.Event{Type: "circuit_breaker.open", Severity: "warning"})
		if rs.count() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rs.count() == 0 {
		t.Fatal("Run did not dispatch a published event")
	}

	cancel()
	// Cancelling Run must not deadlock; a brief wait lets the goroutine return.
	time.Sleep(20 * time.Millisecond)
}

func TestTestSendHappyAndError(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	d := New(fakeCfg{cfg: Config{APIBaseURL: rs.URL, Targets: "tgram://x"}}, rs.Client())

	if err := d.TestSend(context.Background()); err != nil {
		t.Fatalf("happy TestSend: %v", err)
	}
	if rs.count() != 1 || rs.last().Type != "info" {
		t.Errorf("test notification not sent as info type: count=%d", rs.count())
	}

	rs.status = http.StatusBadGateway
	if err := d.TestSend(context.Background()); err == nil {
		t.Error("expected error when apprise-api returns 502")
	}
}
