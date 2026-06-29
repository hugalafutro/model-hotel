package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/settings"
)

// fakeFleetSettings is an in-memory fleetSettings for tests: a plain map with a
// recorded write order so assertions can check exactly what Announce persisted.
type fakeFleetSettings struct {
	values  map[string]string
	setErr  error
	written []string // keys in the order Set was called
}

func newFakeFleetSettings() *fakeFleetSettings {
	return &fakeFleetSettings{values: map[string]string{}}
}

func (f *fakeFleetSettings) GetWithDefault(_ context.Context, key, def string) string {
	if v, ok := f.values[key]; ok {
		return v
	}
	return def
}

func (f *fakeFleetSettings) Set(_ context.Context, key, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.values[key] = value
	f.written = append(f.written, key)
	return nil
}

func TestFleetAnnounce_PersistsContact(t *testing.T) {
	fs := newFakeFleetSettings()
	h := NewFleetHandler(fs)

	body := `{"is_primary":true,"primary_name":"hotel-a","frontdesk_id":"fd-1"}`
	req := httptest.NewRequest(http.MethodPost, "/fleet/announce", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Announce(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%q", rec.Code, rec.Body.String())
	}
	if got := fs.values[keyFleetIsPrimary]; got != "true" {
		t.Errorf("%s = %q, want true", keyFleetIsPrimary, got)
	}
	if got := fs.values[keyFleetPrimaryName]; got != "hotel-a" {
		t.Errorf("%s = %q, want hotel-a", keyFleetPrimaryName, got)
	}
	if got := fs.values[keyFleetFrontdeskID]; got != "fd-1" {
		t.Errorf("%s = %q, want fd-1", keyFleetFrontdeskID, got)
	}
	seen := fs.values[keyFleetManagedSeenAt]
	if _, err := time.Parse(time.RFC3339, seen); err != nil {
		t.Errorf("%s = %q, not RFC3339: %v", keyFleetManagedSeenAt, seen, err)
	}
}

func TestFleetAnnounce_NonPrimaryWritesFalse(t *testing.T) {
	fs := newFakeFleetSettings()
	h := NewFleetHandler(fs)

	req := httptest.NewRequest(http.MethodPost, "/fleet/announce",
		strings.NewReader(`{"is_primary":false}`))
	rec := httptest.NewRecorder()
	h.Announce(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := fs.values[keyFleetIsPrimary]; got != "false" {
		t.Errorf("%s = %q, want false", keyFleetIsPrimary, got)
	}
}

func TestFleetAnnounce_RejectsBadJSON(t *testing.T) {
	fs := newFakeFleetSettings()
	h := NewFleetHandler(fs)

	req := httptest.NewRequest(http.MethodPost, "/fleet/announce", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	h.Announce(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if len(fs.written) != 0 {
		t.Errorf("wrote %v on bad body; want no writes", fs.written)
	}
}

func TestComputeFleetStatus(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	rfc := func(d time.Duration) string { return now.Add(-d).Format(time.RFC3339) }

	tests := []struct {
		name      string
		values    map[string]string
		wantNil   bool
		wantState string
		wantPrim  bool
	}{
		{
			name:    "standalone: no contact",
			values:  map[string]string{},
			wantNil: true,
		},
		{
			name: "forgotten: contact older than forget window",
			values: map[string]string{
				keyFleetManagedSeenAt: rfc(fleetForgetTTL + time.Hour),
			},
			wantNil: true,
		},
		{
			name: "primary: fresh and flagged",
			values: map[string]string{
				keyFleetManagedSeenAt: rfc(10 * time.Second),
				keyFleetIsPrimary:     "true",
			},
			wantState: "primary",
			wantPrim:  true,
		},
		{
			name: "member: fresh, not primary",
			values: map[string]string{
				keyFleetManagedSeenAt: rfc(10 * time.Second),
				keyFleetIsPrimary:     "false",
			},
			wantState: "member",
		},
		{
			name: "warning: stale heartbeat (member)",
			values: map[string]string{
				keyFleetManagedSeenAt: rfc(fleetManagedTTL + time.Minute),
				keyFleetIsPrimary:     "false",
			},
			wantState: "warning",
		},
		{
			name: "warning: stale heartbeat even when last seen as primary",
			values: map[string]string{
				keyFleetManagedSeenAt: rfc(fleetManagedTTL + time.Minute),
				keyFleetIsPrimary:     "true",
			},
			wantState: "warning",
			wantPrim:  true,
		},
		{
			name: "unparseable timestamp: treated as standalone",
			values: map[string]string{
				keyFleetManagedSeenAt: "not-a-timestamp",
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &fakeFleetSettings{values: tt.values}
			got := computeFleetStatus(context.Background(), fs, now)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("computeFleetStatus = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("computeFleetStatus = nil, want state %q", tt.wantState)
			}
			if got.State != tt.wantState {
				t.Errorf("State = %q, want %q", got.State, tt.wantState)
			}
			if got.IsPrimary != tt.wantPrim {
				t.Errorf("IsPrimary = %v, want %v", got.IsPrimary, tt.wantPrim)
			}
		})
	}
}

// TestFleetKeysNeverSyncable is the guard that protects the whole design: the
// _fleet_* keys must stay out of the settings allowlist so config-sync's
// declarative replace (apply) never writes or deletes them. If someone adds one
// of these to AllowedSettings, the synced marker would be wiped on every sync
// and a managed member could clobber a primary's heartbeat — this test fails
// loudly before that ships.
func TestFleetKeysNeverSyncable(t *testing.T) {
	for _, k := range []string{
		keyFleetManagedSeenAt,
		keyFleetIsPrimary,
		keyFleetPrimaryName,
		keyFleetFrontdeskID,
		keyFleetConfigSyncedAt,
		keyFleetLastSourceGen,
	} {
		if isSyncableSetting(k) {
			t.Errorf("%q is syncable; _fleet_* keys must never be in the sync envelope", k)
		}
	}
}

// TestConfigSyncApplyStampsFleetMarker is the end-to-end proof of the synced
// marker contract: a config-sync apply (a) stamps _fleet_config_synced_at, (b)
// leaves a pre-existing instance-local _fleet_* key untouched through the
// declarative replace, and (c) still declaratively removes a syncable setting
// the envelope omits. (a)+(b) together are what the dashboard's "synced from
// primary" readout depends on.
func TestConfigSyncApplyStampsFleetMarker(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	sr := settings.NewRepository(apiTestDB.Pool())
	h := NewConfigSyncHandler(apiTestDB, sr, "", "v-test", nil)

	// Instance-local fleet key (must survive) + a syncable key the envelope omits
	// (must be deleted by the declarative replace).
	if err := sr.Set(ctx, keyFleetManagedSeenAt, "2026-06-26T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := sr.Set(ctx, "request_timeout", "123"); err != nil {
		t.Fatal(err)
	}

	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			Providers: []ExportProvider{
				{Name: "p1", BaseURL: "https://p1.example", Enabled: true, AutodiscoveryEnabled: true},
			},
		},
	}
	if err := h.apply(ctx, env, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := sr.GetWithDefault(ctx, keyFleetConfigSyncedAt, ""); got == "" {
		t.Error("synced marker not stamped")
	} else if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("synced marker = %q, not RFC3339: %v", got, err)
	}
	if got := sr.GetWithDefault(ctx, keyFleetManagedSeenAt, ""); got != "2026-06-26T00:00:00Z" {
		t.Errorf("instance-local fleet key = %q, want preserved through replace", got)
	}
	if got := sr.GetWithDefault(ctx, "request_timeout", "MISSING"); got != "MISSING" {
		t.Errorf("request_timeout = %q, want declaratively removed", got)
	}
}

// TestFleetStatusJSONOmitsWhenStandalone confirms a nil Fleet drops out of the
// system payload entirely, so a standalone dashboard sees no `fleet` key.
func TestFleetStatusJSONOmitsWhenStandalone(t *testing.T) {
	b, err := json.Marshal(SystemStats{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "fleet") {
		t.Errorf("standalone payload contains fleet key: %s", b)
	}
}
