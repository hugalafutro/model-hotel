package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// identityHandler wires an update handler around one stored provider, with the
// discovery probe pointed at whatever test server the caller stood up.
func identityHandler(t *testing.T, stored *provider.Provider, captured *provider.UpdateProviderRequest) (*Handler, uuid.UUID) {
	t.Helper()
	store := &mockProviderStore{
		getFn: func(context.Context, uuid.UUID) (*provider.Provider, error) { return stored, nil },
		listFn: func(context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{stored}, nil
		},
		updateFn: func(_ context.Context, _ uuid.UUID, req provider.UpdateProviderRequest, _, _, _ []byte) (*provider.Provider, error) {
			*captured = req
			return stored, nil
		},
	}
	h := &Handler{
		cfg: &config.Config{
			AllowHTTPProviders:   true,
			AllowedProviderHosts: []string{"127.0.0.1"},
		},
		providerRepo: store,
		adminMgr:     &mockAdminAuth{validateFn: func(string) bool { return true }},
	}
	return h, stored.ID
}

func doUpdate(t *testing.T, h *Handler, id uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, w := newChiRequest(http.MethodPut, "/providers/"+id.String(), bytes.NewReader([]byte(body)))
	req = setChiURLParam(req, "id", id.String())
	h.UpdateProvider(w, req)
	return w
}

// A row the startup backfill typed from the old URL rules can carry a type its
// operator never chose: a self-hosted server on a non-default port was filed as
// generic OpenAI. Correcting it must be possible without deleting the provider,
// which would cascade away its models.
func TestUpdateProvider_CorrectsABackfilledType(t *testing.T) {
	srv := koboldcppTestServer(t)
	defer srv.Close()

	stored := &provider.Provider{
		ID:           uuid.New(),
		Name:         "KoboldCpp on an odd port",
		BaseURL:      srv.URL + "/v1",
		ProviderType: "openai", // what the legacy port rules made of it
	}
	var captured provider.UpdateProviderRequest
	h, id := identityHandler(t, stored, &captured)
	withDiscoveryAgainst(h, srv.Client())

	w := doUpdate(t, h, id, `{"provider_type":"koboldcpp"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if captured.ProviderType == nil || *captured.ProviderType != "koboldcpp" {
		t.Fatalf("stored type = %v, want koboldcpp", captured.ProviderType)
	}
}

// The corrected type is confirmed the same way a new one is: claiming a
// KoboldCPP box is LM Studio must fail even when the address does not change.
func TestUpdateProvider_TypeCorrectionIsProbed(t *testing.T) {
	srv := koboldcppTestServer(t)
	defer srv.Close()

	stored := &provider.Provider{
		ID:           uuid.New(),
		Name:         "KoboldCpp",
		BaseURL:      srv.URL + "/v1",
		ProviderType: "openai",
	}
	var captured provider.UpdateProviderRequest
	h, id := identityHandler(t, stored, &captured)
	withDiscoveryAgainst(h, srv.Client())

	w := doUpdate(t, h, id, `{"provider_type":"lmstudio"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
	}
	var body providerTypeGateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	if body.Code != codeProviderTypeMismatch || body.Detected != "koboldcpp" {
		t.Errorf("body = %+v, want a koboldcpp mismatch", body)
	}
}

func TestUpdateProvider_RejectsUnknownType(t *testing.T) {
	stored := &provider.Provider{ID: uuid.New(), BaseURL: "https://api.openai.com/v1", ProviderType: "openai"}
	var captured provider.UpdateProviderRequest
	h, id := identityHandler(t, stored, &captured)

	w := doUpdate(t, h, id, `{"provider_type":"not-a-type"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body %s", w.Code, w.Body.String())
	}
}

// An update that changes neither the address nor the type must not probe: a
// rename has to work while the server is switched off. The test server is
// closed, so any probe at all would surface as an unreachable error.
func TestUpdateProvider_RenameDoesNotProbe(t *testing.T) {
	srv := koboldcppTestServer(t)
	url := srv.URL
	client := srv.Client()
	srv.Close()

	stored := &provider.Provider{
		ID:           uuid.New(),
		Name:         "KoboldCpp",
		BaseURL:      url + "/v1",
		ProviderType: "koboldcpp",
	}
	var captured provider.UpdateProviderRequest
	h, id := identityHandler(t, stored, &captured)
	withDiscoveryAgainst(h, client)

	for _, body := range []string{
		`{"name":"KoboldCpp renamed"}`,
		// The stored address echoed back, and the same address in the form the
		// operator originally typed it.
		`{"base_url":"` + url + `/v1"}`,
		`{"base_url":"` + url + `"}`,
		// The type it already has.
		`{"provider_type":"koboldcpp"}`,
	} {
		w := doUpdate(t, h, id, body)
		if w.Code != http.StatusOK {
			t.Errorf("update %s: status = %d, body %s", body, w.Code, w.Body.String())
		}
	}
}

// koboldcppTestServer answers the KoboldCPP fingerprint and nothing else.
func koboldcppTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/extra/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.119"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
