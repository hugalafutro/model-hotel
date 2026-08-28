package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// withDiscoveryAgainst points one handler's discovery-service factory at a test
// server. Scoped to the handler rather than the package, so tests that run in
// parallel cannot overwrite each other's transport.
func withDiscoveryAgainst(h *Handler, client *http.Client) {
	h.newDiscovery = func() *provider.DiscoveryService {
		return provider.NewDiscoveryServiceWithHTTPClient(client)
	}
}

func gateBody(t *testing.T, rec *httptest.ResponseRecorder) providerTypeGateResponse {
	t.Helper()
	var body providerTypeGateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode gate body %q: %v", rec.Body.String(), err)
	}
	return body
}

// A cloud provider's address identifies it, so nothing is probed and the gate
// never blocks: a test server that would fail every fingerprint must not
// matter here.
func TestConfirmLocalServerType_SkipsNonLocalTypes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a cloud provider type must not be probed")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h := &Handler{}
	withDiscoveryAgainst(h, srv.Client())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)
	if !h.confirmLocalServerType(rec, req, "openai", "https://api.openai.com/v1", "") {
		t.Fatalf("expected the gate to pass, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestConfirmLocalServerType_Match(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/extra/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.119"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h := &Handler{}
	withDiscoveryAgainst(h, srv.Client())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)
	if !h.confirmLocalServerType(rec, req, "koboldcpp", srv.URL+"/v1", "") {
		t.Fatalf("expected the gate to pass, got %d %s", rec.Code, rec.Body.String())
	}
}

// Adding a KoboldCPP box as LM Studio must fail and name what answered, which
// is the whole point of confirming rather than guessing.
func TestConfirmLocalServerType_Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/extra/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.119"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h := &Handler{}
	withDiscoveryAgainst(h, srv.Client())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)
	if h.confirmLocalServerType(rec, req, "lmstudio", srv.URL+"/v1", "") {
		t.Fatal("expected the gate to block a mismatched server")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	body := gateBody(t, rec)
	if body.Code != codeProviderTypeMismatch {
		t.Errorf("code = %q, want %q", body.Code, codeProviderTypeMismatch)
	}
	if body.Expected != "lmstudio" || body.Detected != "koboldcpp" {
		t.Errorf("expected/detected = %q/%q, want lmstudio/koboldcpp", body.Expected, body.Detected)
	}
	if body.DetectedVersion != "1.119" {
		t.Errorf("detected_version = %q, want 1.119", body.DetectedVersion)
	}
}

// A server that is up but is not one of the three families cannot confirm the
// choice either, so it is refused rather than saved on trust.
func TestConfirmLocalServerType_Unconfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	h := &Handler{}
	withDiscoveryAgainst(h, srv.Client())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)
	if h.confirmLocalServerType(rec, req, "ollama", srv.URL+"/v1", "") {
		t.Fatal("expected the gate to block an unconfirmed server")
	}
	body := gateBody(t, rec)
	if body.Code != codeProviderTypeUnconfirmed {
		t.Errorf("code = %q, want %q", body.Code, codeProviderTypeUnconfirmed)
	}
	if body.Detected != "" {
		t.Errorf("detected = %q, want empty", body.Detected)
	}
}

func TestConfirmLocalServerType_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	client := srv.Client()
	srv.Close()

	h := &Handler{}
	withDiscoveryAgainst(h, client)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)
	if h.confirmLocalServerType(rec, req, "lmstudio", url+"/v1", "") {
		t.Fatal("expected the gate to block an unreachable server")
	}
	body := gateBody(t, rec)
	if body.Code != codeProviderUnreachable {
		t.Errorf("code = %q, want %q", body.Code, codeProviderUnreachable)
	}
}

// stubProviderList is a provider store that only answers List, which is all the
// duplicate-address guard reads.
type stubProviderList struct {
	ProviderStore
	rows []*provider.Provider
}

func (s *stubProviderList) List(context.Context) ([]*provider.Provider, error) {
	return s.rows, nil
}

func TestRejectDuplicateLocalServer(t *testing.T) {
	existing := &provider.Provider{
		ID:           uuid.New(),
		Name:         "KoboldCpp 141",
		BaseURL:      "http://192.168.1.141:5005/v1",
		ProviderType: "koboldcpp",
	}
	h := &Handler{providerRepo: &stubProviderList{rows: []*provider.Provider{existing}}}

	tests := []struct {
		name     string
		typ      string
		url      string
		exclude  uuid.UUID
		accepted bool
	}{
		// The address is the same server however it is spelled.
		{"same address, same type", "koboldcpp", "http://192.168.1.141:5005/v1", uuid.Nil, false},
		{"same address without the mount", "koboldcpp", "http://192.168.1.141:5005", uuid.Nil, false},
		{"same address with a trailing slash", "koboldcpp", "http://192.168.1.141:5005/", uuid.Nil, false},
		// A different self-hosted type on the same address is still the same
		// box: the probe would refuse it anyway, but it is a duplicate first.
		{"same address, other local type", "lmstudio", "http://192.168.1.141:5005", uuid.Nil, false},
		// A different port on the same host is a different server.
		{"different port", "koboldcpp", "http://192.168.1.141:11234", uuid.Nil, true},
		{"different host", "koboldcpp", "http://192.168.1.163:5005", uuid.Nil, true},
		// The operator's escape hatch: the same box added as a generic
		// OpenAI-compatible provider is not a self-hosted row, so it passes.
		{"same address as custom", "custom", "http://192.168.1.141:5005", uuid.Nil, true},
		// Editing the provider itself must not collide with itself.
		{"the row being edited", "koboldcpp", "http://192.168.1.141:5005", existing.ID, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)
			got := h.rejectDuplicateLocalServer(rec, req, tc.typ, tc.url, tc.exclude)
			if got != tc.accepted {
				t.Fatalf("accepted = %v, want %v (body %s)", got, tc.accepted, rec.Body.String())
			}
			if tc.accepted {
				return
			}
			if rec.Code != http.StatusConflict {
				t.Errorf("status = %d, want 409", rec.Code)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %q: %v", rec.Body.String(), err)
			}
			if body["code"] != codeProviderDuplicateAddress {
				t.Errorf("code = %q, want %q", body["code"], codeProviderDuplicateAddress)
			}
			// The operator needs to know which provider already holds it.
			if body["existing"] != existing.Name {
				t.Errorf("existing = %q, want %q", body["existing"], existing.Name)
			}
		})
	}
}

// erroringProviderList fails List, the way a flaky database would.
type erroringProviderList struct {
	ProviderStore
}

func (e *erroringProviderList) List(context.Context) ([]*provider.Provider, error) {
	return nil, errors.New("database unavailable")
}

// The duplicate check is a usability guard, not a correctness one: if the
// provider list cannot be read, the add proceeds rather than being blocked by
// an unrelated failure.
func TestRejectDuplicateLocalServer_ListFailureDoesNotBlock(t *testing.T) {
	h := &Handler{providerRepo: &erroringProviderList{}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", http.NoBody)

	if !h.rejectDuplicateLocalServer(rec, req, "koboldcpp", "http://192.168.1.141:5005", uuid.Nil) {
		t.Fatalf("expected the add to proceed, got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("expected no error response, got %d %s", rec.Code, rec.Body.String())
	}
}
