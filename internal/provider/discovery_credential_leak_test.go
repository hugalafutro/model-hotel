package provider

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Discovery decrypts the provider credential to talk to the upstream, and an
// upstream that quotes it back in an auth failure used to reach app_logs
// verbatim through these error paths. The error a discovery function returns
// is logged by its caller, so the credential must not be in it.
// Deliberately shapeless: no known prefix, no digit, so MaskKeyShapedTokens
// cannot see it and only the exact-match layer can remove it. A self-hosted
// gateway's key looks like this, and it is the case the exact layer exists
// for. With an sk- key these tests passed even with MaskCredential removed,
// because the shape layer inside SanitizeLogBody caught it on its own.
const leakedKey = "selfhosted-gateway-secret"

func echoKeyHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + leakedKey + `"}}`))
	}
}

// The native endpoint answers an auth failure quoting the key; the returned
// error reaches the app log.
func TestDiscoverLMStudioNative_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverLMStudioNative(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	assertScrubbed(t, err.Error())
}

// The OpenAI-compatible fallback listing does the same.
func TestDiscoverLMStudioOpenAI_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverLMStudioOpenAI(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	assertScrubbed(t, err.Error())
}

func TestKoboldCPPLoadedModel_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, leakedKey)
	if err == nil {
		t.Fatal("expected an error from the 401")
	}
	assertScrubbed(t, err.Error())
}

func assertScrubbed(t *testing.T, msg string) {
	t.Helper()
	if strings.Contains(msg, leakedKey) {
		t.Errorf("the provider key survived into the error: %q", msg)
	}
	if !strings.Contains(msg, "[redacted]") {
		t.Errorf("no redaction marker, so the body was not scrubbed at all: %q", msg)
	}
}

// The shared helpers behind every provider family that lists models over a
// plain OpenAI-shaped endpoint. The vendor-specific paths above each learned
// to run MaskCredential over what the upstream said back; fetchURL and
// doDiscoveryRequest, which the openai/custom/azure/deepseek/... families all
// go through, still scrubbed with the shape layer only. A self-hosted gateway
// (provider type custom or openai, the arbitrary-endpoint fallback) whose error
// body quotes the bearer back therefore put the decrypted key into the returned
// error, and from there into app_logs, stdout and the discovery SSE event.
// Strix vuln-0005 (2026-09-01), the fourth site of the #836 class.
func TestDiscoverOpenAI_Non200DoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}

	_, err := svc.discoverOpenAI(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, leakedKey)

	assertNoKey(t, err)
}

// A retryable status (429/5xx) is read into lastErr on every attempt and the
// final error wraps it, so the key rode out through a different string than the
// non-200 branch and needs its own assertion.
func TestFetchURL_RetryableStatusDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusServiceUnavailable))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()} // zero retryBaseDelay: instant backoffs
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/models", headers)

	assertNoKey(t, err)
}

// A transport error quotes the request URL, and one provider family (Google)
// authenticates by query parameter. The site logged that error at Info with the
// shape layer alone and returned it verbatim when it was not transient, so a
// custom-format key in ?key= reached the log line and the caller's error text.
func TestDoDiscoveryRequest_TransportErrorDoesNotCarryQueryKey(t *testing.T) {
	var logged strings.Builder
	prev := slog.Default()
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	defer slog.SetDefault(prev)

	// 127.0.0.1:1 refuses immediately on every platform CI runs on.
	svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
	_, err := svc.fetchURL(context.Background(), http.MethodGet, "http://127.0.0.1:1/v1beta/models?key="+leakedKey, http.Header{})

	assertNoKey(t, err)
	if strings.Contains(logged.String(), leakedKey) {
		t.Errorf("the query-parameter key reached the discovery log:\n%s", logged.String())
	}
}

func assertNoKey(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error from the upstream refusal")
	}
	if strings.Contains(err.Error(), leakedKey) {
		t.Errorf("error carries the decrypted key: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error should show the key was redacted, not silently dropped: %s", err.Error())
	}
}

// The review of the first cut found that masking AFTER SanitizeLogBody's cut
// left the head of a key that straddled it, for exactly the custom-format keys
// the exact pass exists for. The retryable branch bounds at 200, so a JSON
// error body that quotes the key late is the realistic case.
func TestFetchURL_RetryableBodyKeyAcrossTheCutIsRedactedWhole(t *testing.T) {
	// 140 + the 22-byte JSON prefix + the 23-byte phrase puts the key's first
	// byte at 185 and its last past 200: fifteen bytes of it sit before the
	// retryable branch's cut, which the inverted order leaves behind (the
	// second review found the first version of this test placed only six
	// there, fewer than it asserted on, so it passed on the old code).
	pad := strings.Repeat("x", 140)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"` + pad + ` auth failed for token ` + leakedKey + `"}}`))
	}))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/models", headers)

	// Not assertNoKey: the "[redacted]" marker can itself sit across the cut,
	// so its presence is not the property. The property is that no run of the
	// key survives, head included.
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), leakedKey[:5]) {
		t.Errorf("a prefix of the key survived the cut: %s", err.Error())
	}
}

// A URL that fails to parse never becomes a request, and the parse error
// prints the raw URL whole. The fallback must still scrub the query.
func TestFetchURL_UnparseableURLDoesNotCarryQueryKey(t *testing.T) {
	svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
	_, err := svc.fetchURL(context.Background(), http.MethodGet, "http://example.invalid/v1beta/models?key="+leakedKey+"\n", http.Header{})
	if err == nil {
		t.Fatal("expected a parse error")
	}
	if strings.Contains(err.Error(), leakedKey) {
		t.Errorf("the query key survived the unparseable-URL fallback: %s", err.Error())
	}
}

// A url.Error quotes the URL as sent, so a query key with characters that
// Query() decodes ('+' to a space, %2F to '/') must be matched in its raw
// rendering too.
func TestDoDiscoveryRequest_TransportErrorDoesNotCarryRawQueryKey(t *testing.T) {
	const rawKey = "selfhosted+gateway%2Fsecret-abcdefghij"
	svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
	_, err := svc.fetchURL(context.Background(), http.MethodGet, "http://127.0.0.1:1/v1beta/models?key="+rawKey, http.Header{})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), rawKey) || strings.Contains(err.Error(), "selfhosted+gateway") {
		t.Errorf("the raw query rendering of the key leaked: %s", err.Error())
	}
}

// Only credential-bearing query parameters are secrets. Azure lists deployments
// with ?api-version=..., and redacting that out of a "version not supported"
// body would destroy the one diagnostic the operator needs.
func TestFetchURL_NonCredentialQueryValueIsNotRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"api-version 2023-03-15-preview is not supported"}`))
	}))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/deployments?api-version=2023-03-15-preview", headers)
	if err == nil || !strings.Contains(err.Error(), "2023-03-15-preview") {
		t.Errorf("a non-credential query value must survive in the diagnostic, got %v", err)
	}
}

// The masked transport error keeps its cause reachable, so callers can still
// tell a cancelled context or a timeout apart from a refusal, even when the
// text was rewritten (a UUID in the path is enough to rewrite it).
func TestMaskedRequestError_KeepsTheCauseForErrorsIs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
	_, err := svc.fetchURL(ctx, http.MethodGet, "http://127.0.0.1:1/orgs/793ac38b-0211-43e6-baa7-aa7054c39931/v1/models?key="+leakedKey, http.Header{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(context.Canceled) must survive the mask, got %v", err)
	}
	if strings.Contains(err.Error(), leakedKey) || strings.Contains(err.Error(), "793ac38b-0211-43e6-baa7-aa7054c39931") {
		t.Errorf("masked text must carry neither the key nor the UUID: %s", err.Error())
	}
}

// The quota helper is the same pre-fix code as doDiscoveryRequest with a worse
// sink: its error is persisted as the provider's quota failure and rendered on
// the dashboard. Both of its sites.
func TestDoQuotaRequestWithRetry_DoesNotCarryTheKey(t *testing.T) {
	t.Run("retryable body", func(t *testing.T) {
		srv := httptest.NewServer(echoKeyHandler(http.StatusServiceUnavailable))
		defer srv.Close()
		svc := &DiscoveryService{httpClient: srv.Client()}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/quota", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+leakedKey)

		_, err := svc.doQuotaRequestWithRetry(context.Background(), req, uuid.NewString(), "prov", "openrouter")

		assertNoKey(t, err)
	})
	t.Run("transport error quoting a query key", func(t *testing.T) {
		svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1/v1beta/quota?key="+leakedKey, http.NoBody)

		_, err := svc.doQuotaRequestWithRetry(context.Background(), req, uuid.NewString(), "prov", "google")

		assertNoKey(t, err)
	})
}

// Three vendor non-200 log sites were still shape-only. Each logs the body at
// Error; the swapped handler captures what would have reached app_logs.
func TestVendorNon200Logs_DoNotCarryTheKey(t *testing.T) {
	cases := []struct {
		name string
		run  func(svc *DiscoveryService, base string) error
	}{
		{"anthropic", func(svc *DiscoveryService, base string) error {
			_, err := svc.discoverAnthropic(context.Background(), &Provider{ID: uuid.New(), Name: "a", BaseURL: base}, leakedKey)
			return err
		}},
		{"opencode-go", func(svc *DiscoveryService, base string) error {
			_, err := svc.discoverOpenCodeGo(context.Background(), &Provider{ID: uuid.New(), Name: "o", BaseURL: base}, leakedKey)
			return err
		}},
		{"xai", func(svc *DiscoveryService, base string) error {
			_, err := svc.discoverXAI(context.Background(), &Provider{ID: uuid.New(), Name: "x", BaseURL: base}, leakedKey)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logged strings.Builder
			prev := slog.Default()
			debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
			defer slog.SetDefault(prev)
			srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
			defer srv.Close()
			svc := &DiscoveryService{httpClient: srv.Client()}

			err := tc.run(svc, srv.URL)

			if err == nil {
				t.Fatal("expected the 401 to surface as an error")
			}
			if strings.Contains(logged.String(), leakedKey) || strings.Contains(err.Error(), leakedKey) {
				t.Errorf("key reached the log or the error:\nlog: %s\nerr: %v", logged.String(), err)
			}
		})
	}
}

// Every header a discovery family folds its key into must be in
// credentialHeaders, or a refactor of that family onto the shared helpers would
// silently un-cover it. The list here is the one the discoverers use today;
// add to both when a family authenticates through a new header.
func TestRequestSecrets_CoversEveryCredentialHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "bearer  "+leakedKey) // lowercase and a double space still strip
	h.Set("X-Api-Key", "anthropic-"+leakedKey)
	h.Set("Api-Key", "azure-"+leakedKey)
	h.Set("X-Goog-Api-Key", "vertex-"+leakedKey)
	got := strings.Join(secretsOf(h, nil), "\n")
	for _, want := range []string{leakedKey, "anthropic-" + leakedKey, "azure-" + leakedKey, "vertex-" + leakedKey} {
		if !strings.Contains(got, want) {
			t.Errorf("secretsOf missed %q; collected:\n%s", want, got)
		}
	}
	if lines := strings.Split(got, "\n"); lines[0] != "bearer  "+leakedKey || lines[1] != leakedKey {
		t.Errorf("a bearer must be listed raw first, then stripped, got %v", lines[:2])
	}
}

// The four quota readers that talk to a fixed vendor host log a non-200 body
// at Error (five sites: OpenRouter has a second, for its key-info call, which
// this stub never reaches because the credits call fails first; it takes the
// same one-line fix). They were shape-only too; a sweep for the pattern found
// them after the vendor listing sites above. Same stub, same custom-format
// key, the same captured log.
func TestQuotaNon200Logs_DoNotCarryTheKey(t *testing.T) {
	const masterKey = "test-master-key-for-testing-only-32bytes!"
	kp, err := auth.Encrypt(leakedKey, masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	cases := []struct {
		name, baseURL string
		run           func(svc *DiscoveryService, p *Provider) error
	}{
		{"kimi-code", "https://api.kimi.com/coding/v1", func(svc *DiscoveryService, p *Provider) error {
			_, err := svc.GetKimiCodeQuota(context.Background(), p, masterKey)
			return err
		}},
		{"zai-coding", "https://api.z.ai", func(svc *DiscoveryService, p *Provider) error {
			_, err := svc.GetZAICodingQuota(context.Background(), p, masterKey)
			return err
		}},
		{"openrouter", "https://openrouter.ai/api/v1", func(svc *DiscoveryService, p *Provider) error {
			_, err := svc.GetOpenRouterBalance(context.Background(), p, masterKey)
			return err
		}},
		{"neuralwatt", "https://api.neuralwatt.com/v1", func(svc *DiscoveryService, p *Provider) error {
			_, err := svc.GetNeuralWattQuota(context.Background(), p, masterKey)
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logged strings.Builder
			prev := slog.Default()
			debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
			defer slog.SetDefault(prev)
			// A 400, not a 401: the readers route an auth refusal to their own
			// (already masked) "quota rejected" site, and the non-200 site under
			// test is what every other refusal reaches.
			srv := httptest.NewServer(echoKeyHandler(http.StatusBadRequest))
			defer srv.Close()
			svc := &DiscoveryService{httpClient: &http.Client{Transport: &testTransport{url: srv.URL}}}
			p := &Provider{ID: uuid.New(), Name: tc.name, BaseURL: tc.baseURL, EncryptedKey: kp.Ciphertext, KeyNonce: kp.Nonce, KeySalt: kp.Salt}

			err := tc.run(svc, p)

			if err == nil {
				t.Fatal("expected the 400 to surface as an error")
			}
			if strings.Contains(logged.String(), leakedKey) || strings.Contains(err.Error(), leakedKey) {
				t.Errorf("key reached the log or the error:\nlog: %s\nerr: %v", logged.String(), err)
			}
			if !strings.Contains(logged.String(), "non-200") {
				t.Errorf("the non-200 log site was not reached, so this case proves nothing:\nerr: %v\nlog: %s", err, logged.String())
			}
		})
	}
}
