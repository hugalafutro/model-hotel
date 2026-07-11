package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

func TestCreatePairedDeviceValidation(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.CreatePairedDevice(ctx, "x", "hash", DeviceRole("root")); !errors.Is(err, ErrValidation) {
		t.Errorf("invalid role err = %v, want ErrValidation", err)
	}
	if _, err := store.CreatePairedDevice(ctx, "x", "", RoleMonitor); !errors.Is(err, ErrValidation) {
		t.Errorf("empty hash err = %v, want ErrValidation", err)
	}

	// Blank label defaults; oversized label is truncated.
	d, err := store.CreatePairedDevice(ctx, "   ", "hash-1", RoleMonitor)
	if err != nil {
		t.Fatalf("CreatePairedDevice: %v", err)
	}
	if d.Label != "Paired device" {
		t.Errorf("default label = %q", d.Label)
	}
	long := strings.Repeat("a", maxDeviceLabelLen+50)
	d2, err := store.CreatePairedDevice(ctx, long, "hash-2", RoleOperator)
	if err != nil {
		t.Fatalf("CreatePairedDevice long label: %v", err)
	}
	if len(d2.Label) != maxDeviceLabelLen {
		t.Errorf("label len = %d, want %d", len(d2.Label), maxDeviceLabelLen)
	}
	if d2.Role != RoleOperator {
		t.Errorf("role = %q", d2.Role)
	}
}

func TestPairedDeviceLookupRevokeAndTouch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	d, err := store.CreatePairedDevice(ctx, "phone", "hash-a", RoleOperator)
	if err != nil {
		t.Fatalf("CreatePairedDevice: %v", err)
	}

	got, err := store.DeviceByTokenHash(ctx, "hash-a")
	if err != nil || got.ID != d.ID {
		t.Fatalf("DeviceByTokenHash = %+v, %v", got, err)
	}
	if got.LastSeenAt != nil {
		t.Errorf("fresh device has last_seen_at set")
	}
	if _, err := store.DeviceByTokenHash(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown hash err = %v, want ErrNotFound", err)
	}

	if err := store.TouchPairedDevice(ctx, d.ID); err != nil {
		t.Fatalf("TouchPairedDevice: %v", err)
	}
	got, err = store.DeviceByTokenHash(ctx, "hash-a")
	if err != nil || got.LastSeenAt == nil {
		t.Fatalf("after touch: %+v, %v", got, err)
	}

	// List shows the device until it is revoked.
	list, err := store.ListPairedDevices(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPairedDevices = %v, %v", list, err)
	}
	if err := store.RevokePairedDevice(ctx, d.ID); err != nil {
		t.Fatalf("RevokePairedDevice: %v", err)
	}
	if _, err := store.DeviceByTokenHash(ctx, "hash-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked device still authenticates: %v", err)
	}
	list, err = store.ListPairedDevices(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("list after revoke = %v, %v", list, err)
	}
	// Double revoke and unknown id are ErrNotFound.
	if err := store.RevokePairedDevice(ctx, d.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("double revoke err = %v, want ErrNotFound", err)
	}
	if err := store.RevokePairedDevice(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown revoke err = %v, want ErrNotFound", err)
	}
}

func TestListPairedDevicesNewestFirst(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if _, err := store.CreatePairedDevice(ctx, "first", "h1", RoleMonitor); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // created_at is nanosecond wall clock
	if _, err := store.CreatePairedDevice(ctx, "second", "h2", RoleMonitor); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListPairedDevices(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("list = %v, %v", list, err)
	}
	if list[0].Label != "second" || list[1].Label != "first" {
		t.Errorf("order = %q, %q; want newest first", list[0].Label, list[1].Label)
	}
}

// ---------------------------------------------------------------------------
// Pairing codes
// ---------------------------------------------------------------------------

func TestPairingCodesSingleUseAndExpiry(t *testing.T) {
	p := newPairingCodes()
	now := time.Now()
	p.now = func() time.Time { return now }

	code, expiresAt, err := p.mint(RoleOperator)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if want := now.Add(pairingCodeTTL); !expiresAt.Equal(want) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, want)
	}
	if _, ok := p.consume("bogus"); ok {
		t.Error("bogus code consumed")
	}
	pc, ok := p.consume(code)
	if !ok || pc.role != RoleOperator {
		t.Fatalf("consume = %q, %v", pc.role, ok)
	}
	// Single-use: the same code never works twice.
	if _, ok := p.consume(code); ok {
		t.Error("code consumed twice")
	}

	// restore puts a burned code back so a failed pairing can be retried, but
	// only while it is still within its TTL.
	p.restore(code, pc)
	if got, ok := p.consume(code); !ok || got.role != RoleOperator {
		t.Errorf("restored code consume = %q, %v; want operator, true", got.role, ok)
	}
	p.restore(code, pairingCode{role: RoleMonitor, expiresAt: now.Add(-time.Second)})
	if _, ok := p.consume(code); ok {
		t.Error("expired code restored")
	}

	// Expired codes are pruned and refused.
	code2, _, err := p.mint(RoleMonitor)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// outstanding tracks a code's live/consumed/expired state.
	if !p.outstanding(code2) {
		t.Error("freshly minted code not outstanding")
	}
	now = now.Add(pairingCodeTTL + time.Second)
	if p.outstanding(code2) {
		t.Error("expired code still outstanding")
	}
	if _, ok := p.consume(code2); ok {
		t.Error("expired code consumed")
	}

	// A consumed code is no longer outstanding.
	now = time.Now()
	code3, _, err := p.mint(RoleOperator)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, ok := p.consume(code3); !ok {
		t.Fatal("mint/consume code3")
	}
	if p.outstanding(code3) {
		t.Error("consumed code still outstanding")
	}
	if p.outstanding("never-minted") {
		t.Error("unknown code reported outstanding")
	}
}

func TestPairingCodesCapEviction(t *testing.T) {
	p := newPairingCodes()
	base := time.Now()
	tick := 0
	// Each mint stamps a later expiry so "oldest" is deterministic.
	p.now = func() time.Time { tick++; return base.Add(time.Duration(tick) * time.Second) }

	first, _, err := p.mint(RoleMonitor)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < maxOutstandingPairingCodes+1; i++ {
		if _, _, err := p.mint(RoleMonitor); err != nil {
			t.Fatal(err)
		}
	}
	p.mu.Lock()
	n := len(p.codes)
	p.mu.Unlock()
	if n > maxOutstandingPairingCodes {
		t.Errorf("outstanding codes = %d, want <= %d", n, maxOutstandingPairingCodes)
	}
	// The earliest-expiring code was evicted by the overflow mint.
	if _, ok := p.consume(first); ok {
		t.Error("oldest code survived past the cap")
	}
}

// ---------------------------------------------------------------------------
// HTTP surface
// ---------------------------------------------------------------------------

// doDevice issues a request bearing a device token.
func doDevice(t *testing.T, srv *Server, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// pairDevice runs the full admin-mints-code, device-exchanges-token flow and
// returns the device token and id.
func pairDevice(t *testing.T, srv *Server, role DeviceRole, label string) (token, id string) {
	t.Helper()
	rec := do(t, srv, http.MethodPost, "/api/pair/start", `{"role":"`+string(role)+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("pair/start = %d: %s", rec.Code, rec.Body.String())
	}
	var start struct {
		Code      string `json:"code"`
		Role      string `json:"role"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode pair/start: %v", err)
	}
	if start.Code == "" || start.Role != string(role) || start.ExpiresAt == "" {
		t.Fatalf("pair/start body = %+v", start)
	}

	rec = do(t, srv, http.MethodPost, "/api/pair", `{"code":"`+start.Code+`","label":"`+label+`"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("pair = %d: %s", rec.Code, rec.Body.String())
	}
	var paired struct {
		Token  string       `json:"token"`
		Device PairedDevice `json:"device"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &paired); err != nil {
		t.Fatalf("decode pair: %v", err)
	}
	if paired.Token == "" || paired.Device.ID == "" || paired.Device.Role != role {
		t.Fatalf("pair body = %+v", paired)
	}
	return paired.Token, paired.Device.ID
}

func TestPairStartValidation(t *testing.T) {
	srv, _ := newTestServer(t)
	// Unauthenticated and device-token callers cannot mint codes.
	if rec := do(t, srv, http.MethodPost, "/api/pair/start", `{"role":"monitor"}`, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauth pair/start = %d, want 401", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/pair/start", `{"role":"root"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad role = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodPost, "/api/pair/start", `not json`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json = %d, want 400", rec.Code)
	}
}

func TestPairStatusEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)
	// Status is admin-only.
	if rec := do(t, srv, http.MethodPost, "/api/pair/status", `{"code":"x"}`, false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauth pair/status = %d, want 401", rec.Code)
	}

	// Mint a code; it reports outstanding.
	rec := do(t, srv, http.MethodPost, "/api/pair/start", `{"role":"monitor"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("pair/start = %d: %s", rec.Code, rec.Body.String())
	}
	var mint struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &mint); err != nil {
		t.Fatalf("decode mint: %v", err)
	}
	outstanding := func() bool {
		r := do(t, srv, http.MethodPost, "/api/pair/status", `{"code":"`+mint.Code+`"}`, true)
		if r.Code != http.StatusOK {
			t.Fatalf("pair/status = %d: %s", r.Code, r.Body.String())
		}
		var out struct {
			Outstanding bool `json:"outstanding"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return out.Outstanding
	}
	if !outstanding() {
		t.Error("freshly minted code not outstanding via endpoint")
	}

	// Consuming it (public exchange) flips it to not outstanding.
	if r := do(t, srv, http.MethodPost, "/api/pair", `{"code":"`+mint.Code+`","label":"x"}`, false); r.Code != http.StatusOK {
		t.Fatalf("pair = %d: %s", r.Code, r.Body.String())
	}
	if outstanding() {
		t.Error("consumed code still outstanding via endpoint")
	}
}

func TestPairExchangeFlow(t *testing.T) {
	srv, store := newTestServer(t)
	token, _ := pairDevice(t, srv, RoleMonitor, "Pixel 8")

	// The token authenticates reads and the code is burned: replaying the
	// exchange with any bad/used code is refused with a stable code.
	if rec := doDevice(t, srv, http.MethodGet, "/api/members", "", token); rec.Code != http.StatusOK {
		t.Errorf("device GET /api/members = %d: %s", rec.Code, rec.Body.String())
	}
	rec := do(t, srv, http.MethodPost, "/api/pair", `{"code":"WRONG","label":"x"}`, false)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "invalid_pairing_code") {
		t.Errorf("bad code = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPost, "/api/pair", `bad`, false); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json pair = %d", rec.Code)
	}

	// last_seen_at is stamped by authenticated device requests.
	devices, err := store.ListPairedDevices(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("devices = %v, %v", devices, err)
	}
	if devices[0].LastSeenAt == nil {
		t.Error("last_seen_at not stamped by device request")
	}
	if devices[0].Label != "Pixel 8" {
		t.Errorf("label = %q", devices[0].Label)
	}
}

func TestDeviceRoleCeiling(t *testing.T) {
	srv, _ := newTestServer(t)
	monitor, _ := pairDevice(t, srv, RoleMonitor, "watcher")
	operator, _ := pairDevice(t, srv, RoleOperator, "own phone")

	// Monitor devices are refused on mutating routes with a stable code.
	rec := doDevice(t, srv, http.MethodPost, "/api/members/mid/state", `{"state":"drained"}`, monitor)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "device_role_forbidden") {
		t.Errorf("monitor mutate = %d: %s", rec.Code, rec.Body.String())
	}
	// Operator devices pass the role gate (404 proves the handler ran).
	if rec := doDevice(t, srv, http.MethodPost, "/api/members/mid/state", `{"state":"drained"}`, operator); rec.Code != http.StatusNotFound {
		t.Errorf("operator mutate on unknown member = %d, want 404: %s", rec.Code, rec.Body.String())
	}

	// Admin-only routes refuse every device token, regardless of role.
	adminOnly := []struct{ method, path, body string }{
		{http.MethodPost, "/api/members", `{"name":"x","url":"https://x"}`},
		{http.MethodGet, "/api/settings", ""},
		{http.MethodPost, "/api/pair/start", `{"role":"monitor"}`},
		{http.MethodGet, "/api/devices", ""},
	}
	for _, c := range adminOnly {
		rec := doDevice(t, srv, c.method, c.path, c.body, operator)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "device_forbidden") {
			t.Errorf("%s %s with device token = %d: %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

func TestDeviceListAndAdminRevoke(t *testing.T) {
	srv, _ := newTestServer(t)
	token, id := pairDevice(t, srv, RoleOperator, "lost phone")

	rec := do(t, srv, http.MethodGet, "/api/devices", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/devices = %d", rec.Code)
	}
	var devices []PairedDevice
	if err := json.Unmarshal(rec.Body.Bytes(), &devices); err != nil {
		t.Fatalf("decode devices: %v", err)
	}
	if len(devices) != 1 || devices[0].ID != id {
		t.Fatalf("devices = %+v", devices)
	}

	// Remote unlink: the token dies immediately.
	if rec := do(t, srv, http.MethodDelete, "/api/devices/"+id, "", true); rec.Code != http.StatusOK {
		t.Fatalf("revoke = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doDevice(t, srv, http.MethodGet, "/api/members", "", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked device request = %d, want 401", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/devices/"+id, "", true); rec.Code != http.StatusNotFound {
		t.Errorf("double revoke = %d, want 404", rec.Code)
	}

	// An empty list serializes as [] rather than null.
	rec = do(t, srv, http.MethodGet, "/api/devices", "", true)
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("empty devices body = %q, want []", body)
	}
}

func TestDeviceSelfRevoke(t *testing.T) {
	srv, _ := newTestServer(t)
	token, _ := pairDevice(t, srv, RoleMonitor, "unlinking phone")

	// The admin bearer has no "self" device.
	if rec := do(t, srv, http.MethodDelete, "/api/devices/self", "", true); rec.Code != http.StatusBadRequest {
		t.Errorf("admin self-revoke = %d, want 400", rec.Code)
	}
	// Bellhop Unlink: works for monitor role too, and kills the token.
	if rec := doDevice(t, srv, http.MethodDelete, "/api/devices/self", "", token); rec.Code != http.StatusOK {
		t.Fatalf("self-revoke = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doDevice(t, srv, http.MethodGet, "/api/members", "", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("post-unlink request = %d, want 401", rec.Code)
	}
}

// TestDeviceStoreMethodsErrorWhenDBClosed exercises the DB-error branches of the
// paired-device store methods, mirroring TestStoreMethodsErrorWhenDBClosed.
func TestDeviceStoreMethodsErrorWhenDBClosed(t *testing.T) {
	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()

	if _, err := s.CreatePairedDevice(ctx, "l", "h", RoleMonitor); err == nil {
		t.Error("CreatePairedDevice: want error")
	}
	if _, err := s.ListPairedDevices(ctx); err == nil {
		t.Error("ListPairedDevices: want error")
	}
	if _, err := s.DeviceByTokenHash(ctx, "h"); err == nil {
		t.Error("DeviceByTokenHash: want error")
	}
	if err := s.RevokePairedDevice(ctx, "x"); err == nil {
		t.Error("RevokePairedDevice: want error")
	}
	if err := s.TouchPairedDevice(ctx, "x"); err == nil {
		t.Error("TouchPairedDevice: want error")
	}
}

// TestDeviceEndpointsSurfaceStoreErrors exercises the 500 branches of the
// pairing HTTP surface when the store is unavailable.
func TestDeviceEndpointsSurfaceStoreErrors(t *testing.T) {
	srv, store := newTestServer(t)
	// Mint a valid code first (no DB involved), then kill the store so the
	// exchange fails at CreatePairedDevice.
	rec := do(t, srv, http.MethodPost, "/api/pair/start", `{"role":"monitor"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("pair/start = %d", rec.Code)
	}
	var start struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	if rec := do(t, srv, http.MethodPost, "/api/pair", `{"code":"`+start.Code+`","label":"x"}`, false); rec.Code != http.StatusInternalServerError {
		t.Errorf("pair with dead store = %d, want 500", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/devices", "", true); rec.Code != http.StatusInternalServerError {
		t.Errorf("devices with dead store = %d, want 500", rec.Code)
	}
	// A device-token bearer whose lookup errors (not merely misses) falls
	// through to the admin/session gate rather than 500-ing: a broken
	// paired_devices table must not take down the whole control plane. The
	// bogus bearer then simply fails admin auth with 401.
	rec = doDevice(t, srv, http.MethodGet, "/api/members", "", "sometoken")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("device lookup with dead store = %d, want 401 (fall through to admin gate)", rec.Code)
	}
}

// TestRevokeSelfRaceSurfacesNotFound covers revokeSelf when the device vanishes
// between authentication and the revoke write (e.g. a concurrent admin revoke):
// the handler is invoked directly with a device in context that has no row.
func TestRevokeSelfRaceSurfacesNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/devices/self", http.NoBody)
	ghost := &PairedDevice{ID: "gone", Label: "ghost", Role: RoleMonitor}
	req = req.WithContext(context.WithValue(req.Context(), deviceCtxKey{}, ghost))
	rec := httptest.NewRecorder()
	srv.revokeSelf(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("revokeSelf on vanished device = %d, want 404", rec.Code)
	}
}
