package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/auth"
)

// TestOIDCStatusRouteMountedUnauthenticated proves the shared adminauth OIDC
// handler is wired into Front Desk's public login group: GET /api/auth/oidc/status
// answers without a bearer (it gates the login button) and reports disabled by
// default. The start/callback routes share the same Register, so mounting status
// confirms the group.
func TestOIDCStatusRouteMountedUnauthenticated(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/api/auth/oidc/status", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/oidc/status = %d, want 200 (public); body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if resp.Enabled {
		t.Errorf("default OIDC status enabled=true, want false")
	}
}

// TestSettingsOIDCSecretMaskRoundTrip mirrors the Apprise-target test for the OIDC
// client secret: it is encrypted at rest, masked on read, preserved when the mask
// is re-submitted, and cleared by a blank submission.
func TestSettingsOIDCSecretMaskRoundTrip(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	get := func() Settings {
		rec := do(t, srv, http.MethodGet, "/api/settings", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET settings = %d", rec.Code)
		}
		var s Settings
		if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	put := func(s Settings) {
		b, _ := json.Marshal(s)
		if rec := do(t, srv, http.MethodPut, "/api/settings", string(b), true); rec.Code != http.StatusOK {
			t.Fatalf("PUT settings = %d (%s)", rec.Code, rec.Body.String())
		}
	}
	storedSecret := func() string {
		set, err := store.GetSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return set.OidcClientSecret
	}

	// Configure OIDC with a fresh client secret.
	s := get()
	s.OidcEnabled = true
	s.OidcIssuerURL = "https://auth.example.com"
	s.OidcClientID = "frontdesk"
	s.OidcClientSecret = "s3cr3t-value"
	s.OidcPublicBaseURL = "https://frontdesk.example.com"
	s.OidcAllowedEmails = "admin@example.com"
	put(s)

	// Stored value is encrypted and decrypts to the plaintext.
	if raw := storedSecret(); !auth.IsEncryptedString(raw) {
		t.Errorf("stored client secret not encrypted: %q", raw)
	}
	if got, _ := auth.DecryptString(storedSecret(), testMasterKey); got != "s3cr3t-value" {
		t.Errorf("stored client secret decrypts to %q", got)
	}
	// GET masks it (never the ciphertext or the plaintext).
	if m := get().OidcClientSecret; m != alertMaskValue {
		t.Errorf("GET client secret = %q, want mask", m)
	}

	// Re-submitting the mask preserves the stored secret while other fields change.
	s2 := get() // secret == mask
	s2.OidcClientID = "frontdesk-2"
	put(s2)
	if got, _ := auth.DecryptString(storedSecret(), testMasterKey); got != "s3cr3t-value" {
		t.Errorf("after mask resubmit, secret = %q, want preserved", got)
	}
	if cur := get().OidcClientID; cur != "frontdesk-2" {
		t.Errorf("client id = %q, want frontdesk-2", cur)
	}

	// Blanking the secret clears it.
	s3 := get()
	s3.OidcClientSecret = ""
	put(s3)
	if raw := storedSecret(); raw != "" {
		t.Errorf("after blank submit, stored secret = %q, want cleared", raw)
	}
}

// TestOIDCSettingsAdapter checks the SQLite-backed adapter maps the exported
// adminauth key strings onto the Settings row (and returns the stored, still
// encrypted, client secret for the handler to decrypt).
func TestOIDCSettingsAdapter(t *testing.T) {
	_, store := newTestServer(t)
	ctx := context.Background()

	enc, err := auth.EncryptString("the-secret", testMasterKey)
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set.OidcEnabled = true
	set.OidcIssuerURL = "https://idp.example"
	set.OidcClientID = "cid"
	set.OidcClientSecret = enc
	set.OidcPublicBaseURL = "https://fd.example"
	set.OidcAllowedEmails = "a@example.com"
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatal(err)
	}

	a := newOIDCSettings(store)
	if !a.GetBool(ctx, adminauth.OIDCEnabledKey, false) {
		t.Error("GetBool(OIDCEnabledKey) = false, want true")
	}
	if got := a.GetWithDefault(ctx, adminauth.OIDCIssuerURLKey, ""); got != "https://idp.example" {
		t.Errorf("issuer = %q", got)
	}
	if got := a.GetWithDefault(ctx, adminauth.OIDCClientIDKey, ""); got != "cid" {
		t.Errorf("client id = %q", got)
	}
	// The adapter returns the stored ciphertext; the handler decrypts it.
	if got := a.GetWithDefault(ctx, adminauth.OIDCClientSecretKey, ""); got != enc {
		t.Errorf("client secret = %q, want stored ciphertext", got)
	}
	if got := a.GetWithDefault(ctx, adminauth.OIDCPublicBaseURLKey, ""); got != "https://fd.example" {
		t.Errorf("base url = %q", got)
	}
	if got := a.GetWithDefault(ctx, adminauth.OIDCAllowedEmailsKey, ""); got != "a@example.com" {
		t.Errorf("allowed emails = %q", got)
	}
	// Unknown keys fall back to the supplied default.
	if got := a.GetWithDefault(ctx, "nope", "def"); got != "def" {
		t.Errorf("unknown key = %q, want default", got)
	}
}
