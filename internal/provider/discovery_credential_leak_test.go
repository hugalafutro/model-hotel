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
// carries the upstream status only, and the body it no longer carries goes to
// the debug log, so the credential must be in neither.
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

	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverLMStudioNative(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, leakedKey)
	assertBodyScrubbedIntoLog(t, err, logged)
}

// The OpenAI-compatible fallback listing does the same.
func TestDiscoverLMStudioOpenAI_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.discoverLMStudioOpenAI(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, leakedKey)
	assertBodyScrubbedIntoLog(t, err, logged)
}

func TestKoboldCPPLoadedModel_ErrorDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusUnauthorized))
	defer srv.Close()

	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, leakedKey)
	assertBodyScrubbedIntoLog(t, err, logged)
}

// captureDebuglog routes the debug log into a buffer for the rest of the test.
func captureDebuglog(t *testing.T) *strings.Builder {
	t.Helper()
	var logged strings.Builder
	debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() { debuglog.SetHandler(debuglog.StdoutHandler()) })
	return &logged
}

// assertBodyScrubbedIntoLog checks the split an upstream refusal takes: the
// returned error carries neither the key nor the body that quoted it, and the
// debug log carries the body with the key redacted out of it.
func assertBodyScrubbedIntoLog(t *testing.T, err error, logged *strings.Builder) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error from the upstream refusal")
	}
	if strings.Contains(err.Error(), leakedKey) || strings.Contains(err.Error(), "Incorrect API key") {
		t.Errorf("the upstream body survived into the error: %q", err.Error())
	}
	if strings.Contains(logged.String(), leakedKey) {
		t.Errorf("the provider key reached the debug log:\n%s", logged.String())
	}
	if !strings.Contains(logged.String(), "[redacted]") {
		t.Errorf("no redaction marker in the debug log, so the body was not scrubbed at all:\n%s", logged.String())
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
	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()}

	_, err := svc.discoverOpenAI(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, leakedKey)

	assertBodyScrubbedIntoLog(t, err, logged)
}

// A retryable status (429/5xx) is read on every attempt and logged with each
// retry, and the final error wraps the last attempt's, so it takes a different
// path than the non-200 branch and needs its own assertion.
func TestFetchURL_RetryableStatusDoesNotCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(echoKeyHandler(http.StatusServiceUnavailable))
	defer srv.Close()
	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()} // zero retryBaseDelay: instant backoffs
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/models", headers)

	assertBodyScrubbedIntoLog(t, err, logged)
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
	// 140 + the 21-byte JSON prefix + the 23-byte phrase puts the key's first
	// byte at 184 and its last past 200: sixteen bytes of it sit before the
	// retryable branch's cut, which the inverted order leaves behind (the
	// second review found the first version of this test placed only six
	// there, fewer than it asserted on, so it passed on the old code).
	pad := strings.Repeat("x", 140)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"` + pad + ` auth failed for token ` + leakedKey + `"}}`))
	}))
	defer srv.Close()
	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/models", headers)

	// Not assertBodyScrubbedIntoLog: the "[redacted]" marker can itself sit
	// across the cut, so its presence is not the property. The property is that
	// no run of the key survives in the logged body, head included.
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(logged.String(), "auth failed") {
		t.Fatalf("setup: the retryable body never reached the debug log:\n%s", logged.String())
	}
	if strings.Contains(logged.String(), leakedKey[:5]) {
		t.Errorf("a prefix of the key survived the cut:\n%s", logged.String())
	}
}

// A URL that fails to parse never becomes a request, and the parse error
// prints the raw URL whole. The fallback must still scrub the query.
func TestFetchURL_UnparseableURLDoesNotCarryQueryKey(t *testing.T) {
	// Three ways a pasted key breaks the URL: a trailing newline, a trailing
	// DEL (which no whitespace trim touches), and a wrap in the middle of the
	// key (the realistic one for a long key). The visible text is the %q
	// rendering, so the assertion is on the readable halves, not the raw key.
	for name, key := range map[string]string{
		"trailing newline": leakedKey + "\n",
		"trailing DEL":     leakedKey + "\x7f",
		"wrapped":          leakedKey[:12] + "\n" + leakedKey[12:],
	} {
		t.Run(name, func(t *testing.T) {
			svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
			_, err := svc.fetchURL(context.Background(), http.MethodGet, "http://example.invalid/v1beta/models?key="+key, http.Header{})
			if err == nil {
				t.Fatal("expected a parse error")
			}
			if strings.Contains(err.Error(), leakedKey[:12]) || strings.Contains(err.Error(), leakedKey[12:]) {
				t.Errorf("a readable part of the key survived the unparseable-URL fallback: %s", err.Error())
			}
		})
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
	logged := captureDebuglog(t)
	svc := &DiscoveryService{httpClient: srv.Client()}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+leakedKey)

	_, err := svc.fetchURL(context.Background(), http.MethodGet, srv.URL+"/deployments?api-version=2023-03-15-preview", headers)
	if err == nil {
		t.Fatal("expected an error from the 400")
	}
	if !strings.Contains(logged.String(), "2023-03-15-preview is not supported") {
		t.Errorf("a non-credential query value must survive in the logged diagnostic:\n%s", logged.String())
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
		logged := captureDebuglog(t)
		svc := &DiscoveryService{httpClient: srv.Client()}
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/quota", http.NoBody)
		req.Header.Set("Authorization", "Bearer "+leakedKey)

		_, err := svc.doQuotaRequestWithRetry(context.Background(), req, uuid.NewString(), "prov", "openrouter")

		assertBodyScrubbedIntoLog(t, err, logged)
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

// A failing discovery scan is stored as the provider's last error, published in
// the discovery.provider_failed event and returned in the API's 500 body, so
// the upstream's own response body must not travel in it, whatever the family.
// The body stays in the debug log, where an operator can read it in context.
func TestDiscoveryErrors_DropUpstreamBody(t *testing.T) {
	const upstreamBody = "upstream-html-error-page-marker"

	tests := []struct {
		name   string
		invoke func(*DiscoveryService, *Provider) error
	}{
		{"ollama", func(d *DiscoveryService, p *Provider) error {
			_, err := d.discoverOllama(context.Background(), p, leakedKey)
			return err
		}},
		{"koboldcpp-version", func(d *DiscoveryService, p *Provider) error {
			_, err := d.koboldcppVersion(context.Background(), p.BaseURL, leakedKey)
			return err
		}},
		{"openai", func(d *DiscoveryService, p *Provider) error {
			_, err := d.discoverOpenAI(context.Background(), p, leakedKey)
			return err
		}},
		{"deepseek", func(d *DiscoveryService, p *Provider) error {
			_, err := d.discoverDeepSeek(context.Background(), p, leakedKey)
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(upstreamBody))
			}))
			defer server.Close()

			var logged strings.Builder
			debuglog.SetHandler(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
			t.Cleanup(func() { debuglog.SetHandler(debuglog.StdoutHandler()) })

			d := &DiscoveryService{httpClient: server.Client()}
			p := &Provider{ID: uuid.New(), Name: "leaky", BaseURL: server.URL}

			err := tc.invoke(d, p)
			if err == nil {
				t.Fatal("expected an error for a 400 response")
			}
			if strings.Contains(err.Error(), upstreamBody) {
				t.Errorf("upstream body reached the returned error: %v", err)
			}
			if !strings.Contains(err.Error(), "400") {
				t.Errorf("error = %q, want it to carry the status", err)
			}
			if !strings.Contains(logged.String(), upstreamBody) {
				t.Errorf("upstream body missing from the debug log: %s", logged.String())
			}
		})
	}
}

// encoding/json quotes the offending literal in a type error (a fractional
// number for an integer field reads "cannot unmarshal number 12345678.5"), and
// a decode failure's error text reaches the discovery result, the
// discovery.provider_failed event and the stored quota failure. The error says
// where the document broke, never what it held.
func TestDecodeErrors_DropUpstreamLiteral(t *testing.T) {
	const masterKey = "test-master-key-for-testing-only-32bytes!"
	kp, err := auth.Encrypt(leakedKey, masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	tests := []struct {
		name, body string
		invoke     func(*DiscoveryService, *Provider) error
	}{
		{"discovery", `{"data":[{"id":"m","created":12345678.5}]}`, func(d *DiscoveryService, p *Provider) error {
			_, err := d.discoverOpenAI(context.Background(), p, leakedKey)
			return err
		}},
		{"quota", `{"code":12345678.5}`, func(d *DiscoveryService, p *Provider) error {
			_, err := d.GetZAICodingQuota(context.Background(), p, masterKey)
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			d := &DiscoveryService{httpClient: &http.Client{Transport: &testTransport{url: srv.URL}}}
			p := &Provider{ID: uuid.New(), Name: "leaky", BaseURL: srv.URL, EncryptedKey: kp.Ciphertext, KeyNonce: kp.Nonce, KeySalt: kp.Salt}

			err := tc.invoke(d, p)
			if err == nil {
				t.Fatal("expected a decode error")
			}
			if strings.Contains(err.Error(), "12345678.5") {
				t.Errorf("decoder literal reached the returned error: %v", err)
			}
			if !strings.Contains(err.Error(), "unexpected JSON value") {
				t.Errorf("error = %q, want the content-free decode description", err)
			}
		})
	}
}

// The local-server probe's failure log carries the host and a masked error,
// never the base URL's user:password or a key a proxy quotes back.
func TestIdentifyLocalServer_ProbeFailureLogDoesNotCarryTheKey(t *testing.T) {
	logged := captureDebuglog(t)
	failing := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("proxy refused " + r.Header.Get("Authorization"))
	})
	svc := &DiscoveryService{httpClient: &http.Client{Transport: failing}}

	_, err := svc.IdentifyLocalServer(context.Background(), "http://operator:"+leakedKey+"@127.0.0.1:1/v1", leakedKey, "")
	if !errors.Is(err, ErrLocalServerUnreachable) {
		t.Fatalf("err = %v, want ErrLocalServerUnreachable", err)
	}
	if !strings.Contains(logged.String(), "local server probe failed") {
		t.Fatalf("the probe failure was not logged:\n%s", logged.String())
	}
	if strings.Contains(logged.String(), leakedKey) {
		t.Errorf("the key reached the probe log:\n%s", logged.String())
	}
}

// An unparseable URL prints whole, userinfo included, in both the fetch error
// and the provider-type fallback's warning. Neither may carry the password or a
// query key. Each case holds one credential, so masking one cannot hide a leak
// of the other.
func TestUnparseableURL_UserinfoAndQueryKeyAreScrubbed(t *testing.T) {
	for name, tc := range map[string]struct{ raw, secret string }{
		// A space and a quote stop the userinfo pattern, and they are also what
		// makes the URL fail to parse.
		"userinfo with space and quote": {"http://operator:pass word\"" + leakedKey + "@example.invalid/v1", leakedKey},
		"query key":                     {"http://example.invalid\x7f/v1?key=" + leakedKey, leakedKey},
		"percent-encoded query name":    {"http://example.invalid\x7f/v1?%6bey=" + leakedKey, leakedKey},
		// No key shape, so only the parse-reason mask stands between it and the log.
		"unprefixed key in the port": {"http://example.invalid:PORTSECRETVALUE99/v1", "PORTSECRETVALUE99"},
	} {
		t.Run(name, func(t *testing.T) {
			logged := captureDebuglog(t)

			svc := &DiscoveryService{httpClient: &http.Client{Timeout: 2 * time.Second}}
			_, err := svc.fetchURL(context.Background(), http.MethodGet, tc.raw, http.Header{})
			if err == nil {
				t.Fatal("expected a parse error")
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Errorf("fetch error carries the secret: %s", err.Error())
			}

			if got := TypeFromHostname(tc.raw); got != "openai" {
				t.Errorf("TypeFromHostname = %q, want the openai fallback", got)
			}
			if !strings.Contains(logged.String(), "failed to parse base URL") {
				t.Fatalf("the parse failure was not logged:\n%s", logged.String())
			}
			if strings.Contains(logged.String(), tc.secret) {
				t.Errorf("the secret reached the parse-failure log:\n%s", logged.String())
			}
		})
	}
}

// A legacy row may separate its query with ";", which url.ParseQuery refuses,
// so the hand split covers it too.
func TestRawURLSecrets_SemicolonSeparatedQuery(t *testing.T) {
	got := maskRawURLText(rawURLSecrets("http://example.invalid/v1?a=1;key="+leakedKey), "GET http://example.invalid/v1?a=1;key="+leakedKey)
	if strings.Contains(got, leakedKey) {
		t.Errorf("the ;-separated key survived: %s", got)
	}
}
