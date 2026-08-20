package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// koboldcppFingerprintServer answers exactly like a real KoboldCPP: the version
// endpoint names the product, everything else 404s.
func koboldcppFingerprintServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/extra/version" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.119","llm":true,"vision":false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

// lmStudioFingerprintServer answers like LM Studio, including its habit of
// returning HTTP 200 with an error body for routes it does not serve. That is
// the case a status-only check would misread as a match.
func lmStudioFingerprintServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v0/models" {
			_, _ = w.Write([]byte(`{"data":[{"id":"google/gemma-4-26b-a4b","type":"vlm","publisher":"google","arch":"gemma4","compatibility_type":"gguf","max_context_length":262144}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"error":"Unexpected endpoint or method. (GET ` + r.URL.Path + `)"}`))
	}))
}

func ollamaFingerprintServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3:8b"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestIdentifyLocalServer_KoboldCPP(t *testing.T) {
	srv := koboldcppFingerprintServer(t)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "koboldcpp" {
		t.Errorf("type = %q, want koboldcpp", got.Type)
	}
	if got.Version != "1.119" {
		t.Errorf("version = %q, want 1.119", got.Version)
	}
}

func TestIdentifyLocalServer_LMStudio(t *testing.T) {
	srv := lmStudioFingerprintServer(t)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "lmstudio" {
		t.Errorf("type = %q, want lmstudio", got.Type)
	}
}

// LM Studio answers the KoboldCPP probe with a 200 and an error body. Matching
// on the body rather than the status is what keeps it from being called
// KoboldCPP, so this is the regression that guards the fail-closed rule.
func TestIdentifyLocalServer_LMStudioNotMistakenForKoboldCPP(t *testing.T) {
	srv := lmStudioFingerprintServer(t)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, _ := svc.IdentifyLocalServer(context.Background(), srv.URL, "")
	if got.Type == "koboldcpp" {
		t.Fatal("LM Studio's 200-with-error body was read as a KoboldCPP fingerprint")
	}
}

func TestIdentifyLocalServer_Ollama(t *testing.T) {
	srv := ollamaFingerprintServer(t)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "ollama" {
		t.Errorf("type = %q, want ollama", got.Type)
	}
}

// A plain OpenAI-compatible server answers none of the three fingerprints: it
// is reachable, so this is "not one of ours", not "unreachable".
func TestIdentifyLocalServer_GenericOpenAIServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"some-model"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "" {
		t.Errorf("type = %q, want empty", got.Type)
	}
}

func TestIdentifyLocalServer_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	client := srv.Client()
	srv.Close() // nothing is listening now

	svc := &DiscoveryService{httpClient: client}
	_, err := svc.IdentifyLocalServer(context.Background(), url+"/v1", "")
	if !errors.Is(err, ErrLocalServerUnreachable) {
		t.Fatalf("err = %v, want ErrLocalServerUnreachable", err)
	}
}

// A server that answers with something structurally similar but not ours must
// not match: the Ollama check needs a models array, not just any 200 JSON.
func TestIdentifyLocalServer_FailsClosedOnForeignJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tags":["a","b"]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "" {
		t.Errorf("type = %q, want empty", got.Type)
	}
}

// A self-hosted server behind a password (KoboldCPP --password, Ollama behind
// an authenticating proxy) answers 401 without a key. Discovery sends the key,
// so the probe must too, or such a server could never be added.
func TestIdentifyLocalServer_SendsAPIKey(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-local" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3:8b"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "sk-local")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "ollama" {
		t.Errorf("type = %q, want ollama (a 401 would have made it unconfirmed)", got.Type)
	}
	if sawAuth == "" {
		t.Error("the probe sent no Authorization header")
	}

	// Same server, no key: it cannot be confirmed, which is what made the
	// unauthenticated probe a regression.
	unauth, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer without key: %v", err)
	}
	if unauth.Type != "" {
		t.Errorf("unauthenticated probe type = %q, want empty", unauth.Type)
	}
}

func TestNormalizeLocalBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		typ      string
		in       string
		expected string
	}{
		{"adds the mount", "ollama", "http://192.168.1.5:11434", "http://192.168.1.5:11434/v1"},
		{"keeps an existing mount", "lmstudio", "http://192.168.1.5:11234/v1", "http://192.168.1.5:11234/v1"},
		{"never doubles the mount", "koboldcpp", "http://host:5001/v1/", "http://host:5001/v1"},
		{"trims a trailing slash", "ollama", "http://host:11434/", "http://host:11434/v1"},
		{"leaves cloud types alone", "openai", "https://api.openai.com/v1", "https://api.openai.com/v1"},
		// The mount belongs on the path: appending to the raw string would put
		// it after the query and produce a URL that resolves nowhere.
		{"appends to the path, not the query", "ollama", "http://box:11434?x=1", "http://box:11434/v1?x=1"},
		{"does not confuse a similar path", "lmstudio", "http://box:11434/v1beta", "http://box:11434/v1beta/v1"},
		{"leaves custom alone", "custom", "https://gateway.example.com", "https://gateway.example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeLocalBaseURL(tc.typ, tc.in); got != tc.expected {
				t.Errorf("NormalizeLocalBaseURL(%q, %q) = %q, want %q", tc.typ, tc.in, got, tc.expected)
			}
		})
	}
}

func TestLocalServerOrigin(t *testing.T) {
	tests := []struct{ in, expected string }{
		{"http://host:5001/v1", "http://host:5001"},
		{"http://host:5001/v1/", "http://host:5001"},
		{"http://host:5001", "http://host:5001"},
		{"http://host:5001/openai/v1", "http://host:5001/openai"},
	}
	for _, tc := range tests {
		if got := localServerOrigin(tc.in); got != tc.expected {
			t.Errorf("localServerOrigin(%q) = %q, want %q", tc.in, got, tc.expected)
		}
	}
}

func TestTypeOf(t *testing.T) {
	stored := &Provider{ProviderType: "lmstudio", BaseURL: "http://host:5001/v1"}
	if got := TypeOf(stored); got != "lmstudio" {
		t.Errorf("stored type = %q, want lmstudio", got)
	}

	// A row that has not been backfilled yet keeps behaving as it did before
	// the column existed.
	unstored := &Provider{BaseURL: "https://api.deepseek.com/v1"}
	if got := TypeOf(unstored); got != "deepseek" {
		t.Errorf("fallback type = %q, want deepseek", got)
	}

	if got := TypeOf(nil); got != "openai" {
		t.Errorf("nil provider type = %q, want openai", got)
	}
}

func TestIsKnownType(t *testing.T) {
	for _, typ := range []string{"custom", "openai", "koboldcpp", "lmstudio", "ollama", "vertex-express", "anthropic-messages"} {
		if !IsKnownType(typ) {
			t.Errorf("IsKnownType(%q) = false, want true", typ)
		}
	}
	for _, typ := range []string{"", "openai ", "gpt", "OLLAMA"} {
		if IsKnownType(typ) {
			t.Errorf("IsKnownType(%q) = true, want false", typ)
		}
	}
}

// Every type the discovery switch drives natively has to be part of the
// vocabulary, or a provider added as that type could never be created.
func TestKnownTypesCoverNativeDiscoverers(t *testing.T) {
	for _, r := range hostTypeRules {
		if !IsKnownType(r.typ) {
			t.Errorf("host rule type %q is missing from KnownTypes", r.typ)
		}
	}
	for _, typ := range LocalServerTypes {
		if !IsKnownType(typ) {
			t.Errorf("local server type %q is missing from KnownTypes", typ)
		}
	}
}

func TestTypeFromHostname(t *testing.T) {
	tests := []struct{ in, expected string }{
		{"https://api.deepseek.com/v1", "deepseek"},
		{"https://opencode.ai/zen/go/v1", "opencode-go"},
		// A self-hosted server's port must not imply a type here: a client that
		// does not name one gets the generic path, not a guess.
		{"http://192.168.1.9:11434/v1", "openai"},
		{"http://localhost:1234/v1", "openai"},
		{"https://gateway.example.com/v1", "openai"},
		{"", "openai"},
		{"://nonsense", "openai"},
	}
	for _, tc := range tests {
		if got := TypeFromHostname(tc.in); got != tc.expected {
			t.Errorf("TypeFromHostname(%q) = %q, want %q", tc.in, got, tc.expected)
		}
	}
}

func TestNormalizeLocalBaseURL_EmptyInput(t *testing.T) {
	if got := NormalizeLocalBaseURL("ollama", "   "); got != "   " {
		t.Errorf("NormalizeLocalBaseURL on blank input = %q, want it unchanged", got)
	}
}

// The two listing checks are the fail-closed half of the fingerprint: an error
// body, a foreign shape or unparseable JSON must never count as a match.
func TestListingFingerprintsFailClosed(t *testing.T) {
	lmStudioCases := map[string]bool{
		`{"data":[]}`: true,
		`{"data":[{"compatibility_type":"gguf"}]}`:   true,
		`{"error":"Unexpected endpoint or method."}`: false,
		`{"data":[{"id":"m","object":"model"}]}`:     false,
		`{"object":"list"}`:                          false,
		`not json`:                                   false,
	}
	for body, want := range lmStudioCases {
		if got := isLMStudioModelListing([]byte(body)); got != want {
			t.Errorf("isLMStudioModelListing(%s) = %v, want %v", body, got, want)
		}
	}

	ollamaCases := map[string]bool{
		`{"models":[{"name":"llama3:8b"}]}`: true,
		`{"models":[]}`:                     true,
		`{"error":"nope","models":[]}`:      false,
		`{"tags":["a"]}`:                    false,
		`not json`:                          false,
	}
	for body, want := range ollamaCases {
		if got := isOllamaTagListing([]byte(body)); got != want {
			t.Errorf("isOllamaTagListing(%s) = %v, want %v", body, got, want)
		}
	}
}

func TestSameLocalAddress(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		expected bool
	}{
		// The same server however either side spells it.
		{"identical", "http://box:5001/v1", "http://box:5001/v1", true},
		{"one side without the mount", "http://box:5001", "http://box:5001/v1", true},
		{"trailing slash", "http://box:5001/", "http://box:5001/v1", true},
		{"host case", "http://BOX:5001/v1", "http://box:5001", true},
		// Different servers.
		{"different port", "http://box:5001/v1", "http://box:11234/v1", false},
		{"different host", "http://box-a:5001", "http://box-b:5001", false},
		{"different path", "http://box:5001/one/v1", "http://box:5001/two/v1", false},
		// Nothing matches an unusable address, or the comparison would make
		// every malformed entry a duplicate of every other.
		{"both empty", "", "", false},
		{"one empty", "", "http://box:5001", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameLocalAddress(tc.a, tc.b); got != tc.expected {
				t.Errorf("SameLocalAddress(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.expected)
			}
		})
	}
}

// A base URL too malformed to parse is handed back untouched rather than
// mangled into something that looks valid.
func TestLocalURLHelpers_MalformedInput(t *testing.T) {
	const broken = "http://box:5001:bad]url"
	if got := NormalizeLocalBaseURL("ollama", broken); got != broken {
		t.Errorf("NormalizeLocalBaseURL = %q, want it unchanged", got)
	}
	if got := localServerOrigin("not a url/v1"); got != "not a url" {
		t.Errorf("localServerOrigin = %q, want the /v1 mount trimmed off the raw string", got)
	}
}

// A request the HTTP client cannot even build counts as "did not reach the
// server", not as a server that answered.
func TestIdentifyLocalServer_UnbuildableRequest(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}
	got, err := svc.IdentifyLocalServer(context.Background(), "http://invalid host with spaces", "")
	if !errors.Is(err, ErrLocalServerUnreachable) {
		t.Fatalf("err = %v, want ErrLocalServerUnreachable", err)
	}
	if got.Type != "" {
		t.Errorf("type = %q, want empty", got.Type)
	}
}
