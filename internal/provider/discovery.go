package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// DiscoveryService handles model discovery across different LLM providers.
type DiscoveryService struct {
	httpClient *http.Client
	// quotaBreaker tracks per-provider circuit breaker state for quota fetches.
	// Key: providerID string, Value: *quotaCircuitState.
	quotaBreaker   sync.Map
	retryBaseDelay time.Duration // configurable retry backoff base delay
}

// NewDiscoveryService creates a new discovery service instance with optional
// SSRF protection. Pass nil for both parameters to use default HTTP client
// (useful for tests).
func NewDiscoveryService(dialCtx func(ctx context.Context, network, addr string) (net.Conn, error), checkRedirect func(req *http.Request, via []*http.Request) error) *DiscoveryService {
	transport := &http.Transport{
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	if dialCtx != nil {
		transport.DialContext = dialCtx
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
	if checkRedirect != nil {
		client.CheckRedirect = checkRedirect
	}
	return &DiscoveryService{
		httpClient:     client,
		retryBaseDelay: 3 * time.Second,
	}
}

// NewDiscoveryServiceWithHTTPClient creates a discovery service with a custom
// HTTP client. This is intended for tests that need to inject a mock transport.
func NewDiscoveryServiceWithHTTPClient(client *http.Client) *DiscoveryService {
	return &DiscoveryService{
		httpClient:     client,
		retryBaseDelay: 3 * time.Second,
	}
}

// SetRetryBaseDelay configures the base delay for quota fetch retry backoff.
// Shorter values are useful in tests.
func (d *DiscoveryService) SetRetryBaseDelay(dur time.Duration) {
	d.retryBaseDelay = dur
}

// maxDiscoveryRetries bounds attempts for a single discovery HTTP call.
const maxDiscoveryRetries = 3

// credentialHeaders are the request headers a discovery family folds its key
// into. secretsOf reads the key back off these, so the shared helpers can
// scrub what an upstream says back without ever being handed the key as a
// value. A family that authenticates through another header must add it here.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key"}

// credentialQueryParams are the query parameter names a key may travel in
// (a custom gateway may authenticate by ?key=). Only these are treated as secrets: a
// sweep of every query value would redact Azure's ?api-version=... out of the
// one diagnostic an operator needs when a version is refused.
var credentialQueryParams = map[string]bool{
	"key": true, "api_key": true, "apikey": true, "api-key": true,
	"token": true, "access_token": true, "secret": true, "password": true,
}

// secretsOf collects every credential the given headers and URL carry, so text
// the upstream sends back (or a transport error that quotes the URL) can be
// scrubbed of it exactly, before the shape layer. The helpers below never
// receive the key as a value: the caller has already folded it into a header
// or the query string. Reading it back is what lets the shared path cover
// every family, including the custom and self-hosted gateways whose key
// format no prefix regex anticipates.
//
// A header value is listed raw and, for a bearer, stripped, since an upstream
// may quote either. A query value is listed decoded, percent-escaped, and as
// its raw "name=value" segment, because a url.Error quotes the URL exactly as
// it was sent while Query() hands back the decoded form. Short values fall
// out inside the mask.
func secretsOf(h http.Header, u *url.URL) []string {
	var secrets []string
	for _, name := range credentialHeaders {
		for _, v := range h.Values(name) {
			secrets = append(secrets, v)
			if f := strings.Fields(v); len(f) == 2 && strings.EqualFold(f[0], "bearer") {
				secrets = append(secrets, f[1])
			}
		}
	}
	if u == nil {
		return secrets
	}
	return append(secrets, querySecrets(u.RawQuery)...)
}

// querySecrets is the query half of secretsOf, over a raw query string, so a
// URL that failed to parse (and whose error therefore prints it whole) can
// still be scrubbed from the text after its '?'.
func querySecrets(rawQuery string) []string {
	var secrets []string
	for _, seg := range strings.Split(rawQuery, "&") {
		name, raw, ok := strings.Cut(seg, "=")
		if !ok || !credentialQueryParams[strings.ToLower(name)] {
			continue
		}
		secrets = append(secrets, seg, raw)
		if dec, err := url.QueryUnescape(raw); err == nil && dec != raw {
			secrets = append(secrets, dec, url.QueryEscape(dec))
		}
		// A key pasted with a stray control byte (a newline at the end, a
		// wrap in the middle, a DEL) is what makes a URL unparseable in the
		// first place, and url.Error renders the URL with %q, so that byte
		// shows escaped and the exact match on the raw value misses the
		// visible text. The %q rendering IS the visible text, so it is listed
		// too, for a control byte anywhere in the value.
		for _, v := range []string{seg, raw} {
			if q := strconv.Quote(v); q[1:len(q)-1] != v {
				secrets = append(secrets, q[1:len(q)-1])
			}
		}
	}
	return secrets
}

// requestSecrets is secretsOf for a built request.
func requestSecrets(req *http.Request) []string {
	if req == nil {
		return nil
	}
	return secretsOf(req.Header, req.URL)
}

// maskRequestSecrets scrubs text of everything req carried, then of anything
// key-shaped, then bounds it: in that order, so a key straddling the cut is
// still redacted whole. Used for every upstream body or transport error the
// shared helpers turn into an error or a log line.
func maskRequestSecrets(req *http.Request, text string, maxLen int) string {
	return util.MaskCredentialsBounded(requestSecrets(req), text, maxLen)
}

// doDiscoveryRequest executes a discovery HTTP request with retries for
// transient network errors (DNS flaps, timeouts, connection resets) and
// retryable HTTP statuses (429, 5xx), so one network hiccup cannot turn into
// a failed or partial model listing. newReq rebuilds the request for each
// attempt so request bodies (e.g. the ollama /api/show POST) replay correctly.
// A response with a non-retryable status is returned as-is for the caller to
// interpret; only the request host is logged (query strings can carry keys).
func (d *DiscoveryService) doDiscoveryRequest(ctx context.Context, newReq func() (*http.Request, error)) (*http.Response, error) {
	var lastErr error
	for attempt := range maxDiscoveryRetries {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		if attempt > 0 {
			backoff := retryBackoff(d.retryBaseDelay, attempt)
			debuglog.Info("discovery: retrying fetch",
				"host", req.URL.Host, "backoff", backoff, "attempt", attempt+1, "max_attempts", maxDiscoveryRetries)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during discovery retry: %w", lastErr)
			case <-time.After(backoff):
			}
		}

		resp, err := d.httpClient.Do(req)
		if err != nil {
			if isTransientNetworkError(err) {
				// A transport error quotes the request URL, and one provider
				// family authenticates by query parameter, so the key IS in
				// scope here: it is in the request that just failed. Masked
				// exactly off that request, then by shape, before it reaches
				// the log or the caller.
				lastErr = maskedRequestError(req, err)
				debuglog.Info("discovery: transient fetch error, will retry",
					"host", req.URL.Host, "attempt", attempt+1, "error", lastErr.Error())
				continue
			}
			return nil, maskedRequestError(req, err)
		}
		if isRetryableStatus(resp.StatusCode) {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("retryable HTTP status %d: %s", resp.StatusCode, maskRequestSecrets(req, string(body), 200))
			debuglog.Info("discovery: retryable fetch status, will retry",
				"host", req.URL.Host, "status", resp.StatusCode, "attempt", attempt+1)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("discovery fetch failed after %d attempts: %w", maxDiscoveryRetries, lastErr)
}

// maskedError is a transport error whose text has been scrubbed of what the
// request carried, with the original still reachable through Unwrap. So
// errors.Is on a cancelled context or a deadline, and errors.As on a net.Error,
// keep working for every caller, while nothing that prints the error (%v, %s,
// slog, a stored column) can reach the unscrubbed text: those all go through
// Error(). The masked text is computed once, at construction.
type maskedError struct {
	text  string
	cause error
}

func (e *maskedError) Error() string { return e.text }
func (e *maskedError) Unwrap() error { return e.cause }

// maskedRequestError wraps err so its text is scrubbed of everything req
// carried (a url.Error quotes the request URL, and one family authenticates
// by query parameter) and of anything key-shaped, bounded to 500 runes.
func maskedRequestError(req *http.Request, err error) error {
	if err == nil {
		return nil
	}
	return &maskedError{text: maskRequestSecrets(req, err.Error(), 500), cause: err}
}

// doDiscoveryRequestPrebuilt retries a prebuilt body-less request (GETs).
// Requests carrying a body must go through doDiscoveryRequest with a factory
// so the body replays on retry.
func (d *DiscoveryService) doDiscoveryRequestPrebuilt(ctx context.Context, req *http.Request) (*http.Response, error) {
	return d.doDiscoveryRequest(ctx, func() (*http.Request, error) { return req, nil })
}

// fetchURL makes an HTTP request with the given headers, reads the full
// response body, and checks for a 200 OK status. Returns the response body
// bytes on success. The caller is responsible for unmarshaling the result.
// Transient network errors and 429/5xx are retried via doDiscoveryRequest.
func (d *DiscoveryService) fetchURL(ctx context.Context, method, rawURL string, headers http.Header) ([]byte, error) {
	// The last request built is kept for the scrub below: the credential is in
	// its headers or URL, and this function never sees it any other way.
	var last *http.Request
	resp, err := d.doDiscoveryRequest(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		for k, vs := range headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		last = req
		return req, nil
	})
	if err != nil {
		// A request that never got built (a URL that fails to parse) reaches
		// here with last == nil, and its error quotes the URL. Scrub from the
		// inputs this function was handed instead of from a request.
		if last == nil {
			// url.Parse fails for the same reason NewRequest did, and the error
			// prints the raw URL whole, so the query is split off by hand.
			secrets := secretsOf(headers, nil)
			if _, q, ok := strings.Cut(rawURL, "?"); ok {
				secrets = append(secrets, querySecrets(q)...)
			}
			err = &maskedError{text: util.MaskCredentialsBounded(secrets, err.Error(), 500), cause: err}
		}
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, maskRequestSecrets(last, string(bodyBytes), 2000))
	}

	return bodyBytes, nil
}

// hostTypeRules maps provider hostnames to provider types: exact host names
// plus suffixes for subdomain matches (api.foo.deepseek.com, custom.nano-gpt.com).
// Suffix matching (rather than strings.Contains) ensures
// "https://my-proxy.deepseek.com" resolves to "deepseek" without substring
// false positives. Providers needing path- or contains-based detection
// (opencode, google) are handled separately in detectByHost.
var hostTypeRules = []struct {
	typ      string
	exact    []string
	suffixes []string
}{
	{"nanogpt", []string{"api.nano-gpt.com", "nano-gpt.com"}, []string{".nano-gpt.com"}},
	{"zai-coding", []string{"api.z.ai", "z.ai"}, []string{".z.ai"}},
	{"deepseek", []string{"api.deepseek.com", "deepseek.com"}, []string{".deepseek.com"}},
	{"anthropic", []string{"api.anthropic.com", "anthropic.com"}, []string{".anthropic.com"}},
	{"xai", []string{"api.x.ai", "x.ai"}, []string{".x.ai"}},
	{"cohere", []string{"api.cohere.com", "api.cohere.ai"}, []string{".cohere.com", ".cohere.ai"}},
	{"openrouter", []string{"openrouter.ai"}, []string{".openrouter.ai"}},
	{"ollama-cloud", []string{"ollama.com"}, []string{".ollama.com"}},
	{"neuralwatt", []string{"api.neuralwatt.com", "neuralwatt.com"}, []string{".neuralwatt.com"}},
	// Azure AI Foundry ({res}.services.ai.azure.com) and classic Azure OpenAI
	// ({res}.openai.azure.com) resources. Both expose the same OpenAI v1
	// surface under /openai/v1, with Bearer auth.
	{"azure", nil, []string{".services.ai.azure.com", ".openai.azure.com"}},
	// Kimi Code subscription endpoint (api.kimi.com/coding). Subscription
	// sk-kimi- keys ONLY work here: the pay-per-token platform
	// (api.moonshot.ai) is a separate key namespace and stays generic openai.
	{"kimi-code", []string{"api.kimi.com", "kimi.com"}, []string{".kimi.com"}},
	// MiniMax intl platform (api.minimax.io). Token Plan subscription sk-cp-
	// keys and pay-as-you-go sk-api- keys share the same OpenAI-compatible
	// endpoint; the CN twin (api.minimaxi.com) stays generic openai.
	{"minimax", []string{"api.minimax.io", "minimax.io"}, []string{".minimax.io"}},
}

// detectByHost resolves a provider type from a lowercased hostname (and URL
// path, for opencode). Returns "" when no rule matches.
func detectByHost(host, path string) string {
	for _, r := range hostTypeRules {
		if slices.Contains(r.exact, host) {
			return r.typ
		}
		for _, s := range r.suffixes {
			if strings.HasSuffix(host, s) {
				return r.typ
			}
		}
	}
	// AWS Bedrock's OpenAI-optimized bedrock-mantle endpoint
	// (bedrock-mantle.{region}.api.aws). Prefix+suffix must both match so
	// bedrock-named hosts on unrelated domains stay generic. The classic
	// bedrock-runtime endpoint is deliberately not detected: it serves chat
	// only under /openai/v1 and has no /models listing at all, so discovery
	// can never succeed against it.
	if strings.HasPrefix(host, "bedrock-mantle.") && strings.HasSuffix(host, ".api.aws") {
		return "bedrock"
	}
	if host == "generativelanguage.googleapis.com" {
		return "google"
	}
	// Vertex AI native surface (aiplatform.googleapis.com, incl. regional
	// hosts): express-mode API keys only work on the native generateContent
	// routes, served through the internal/gemini egress adapter.
	if strings.HasSuffix(host, ".googleapis.com") && strings.Contains(host, "aiplatform") {
		return "vertex-express"
	}
	if strings.HasSuffix(host, ".googleapis.com") && strings.Contains(host, "generativelanguage") {
		return "google"
	}
	if host == "opencode.ai" || strings.HasSuffix(host, ".opencode.ai") {
		// Path-based detection: Go URL contains /zen/go/, Zen contains /zen/.
		// Must check Go before Zen since /zen/go/ is a subpath of /zen/.
		if strings.Contains(path, "/zen/go") {
			return "opencode-go"
		}
		if strings.Contains(path, "/zen") {
			return "opencode-zen"
		}
	}
	return ""
}

// TypeFromHostname derives a provider type from the vendor hostname alone.
// It is the fallback for API clients that create a provider without naming a
// type: a vendor host still identifies itself unambiguously, while an address
// that does not match one is generic OpenAI-compatible rather than a guess at
// which self-hosted server might be listening on that port.
func TypeFromHostname(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		debuglog.Warn("discovery: failed to parse base URL", "url", baseURL)
		return "openai"
	}
	if typ := detectByHost(strings.ToLower(u.Hostname()), strings.ToLower(u.Path)); typ != "" {
		return typ
	}
	return "openai"
}

// LegacyTypeFromURL derives a provider type from a base URL, including the
// default-port rules for self-hosted servers. It serves rows with no stored
// provider_type: the startup backfill and TypeOf's fallback are its only
// callers. Provider type is chosen by the operator when the provider is
// added, never guessed at request time.
func LegacyTypeFromURL(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		return TypeFromHostname(baseURL)
	}
	if typ := detectByHost(strings.ToLower(u.Hostname()), strings.ToLower(u.Path)); typ != "" {
		return typ
	}
	switch u.Port() {
	case "11434":
		return "ollama"
	case "5001":
		return "koboldcpp"
	case "1234":
		return "lmstudio"
	}
	return "openai"
}

// TypeOf returns the provider's stored type. Rows that have not been backfilled
// yet (a restored dump, an import that landed after startup) fall back to the
// legacy URL derivation.
func TypeOf(p *Provider) string {
	if p == nil {
		return "openai"
	}
	if p.ProviderType != "" {
		return p.ProviderType
	}
	return LegacyTypeFromURL(p.BaseURL)
}

// DiscoverModels discovers available models from a provider.
func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
	providerType := TypeOf(provider)
	debuglog.Info("discovery: starting discovery", "provider", provider.Name, "provider_id", provider.ID, "type", providerType)

	// Keyless providers (e.g. OpenCode Zen free models) store nil encrypted
	// key bytes. When the key is empty, skip decryption and use empty string.
	var apiKey string
	if len(provider.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		var err error
		apiKey, err = auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
		if err != nil {
			debuglog.Error("discovery: failed to decrypt API key", "provider", provider.Name, "provider_id", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to decrypt API key: %w", err)
		}
	}

	models, err := func() ([]*model.Model, error) {
		switch providerType {
		case "nanogpt":
			return d.discoverNanoGPT(ctx, provider, apiKey)
		case "zai-coding":
			return d.discoverZAICoding(ctx, provider, apiKey)
		case "kimi-code":
			return d.discoverKimiCode(ctx, provider, apiKey)
		case "minimax":
			return d.discoverMiniMax(ctx, provider, apiKey)
		case "bedrock":
			return d.discoverBedrock(ctx, provider, apiKey)
		case "azure":
			return d.discoverAzure(ctx, provider, apiKey)
		case "deepseek":
			return d.discoverDeepSeek(ctx, provider, apiKey)
		case "anthropic", "anthropic-messages":
			// Anthropic's models listing is part of the Messages API surface
			// (GET /v1/models, x-api-key), so any endpoint serving that API
			// answers it in the same shape. An operator-entered one that does
			// not fails discovery loudly rather than adding a provider whose
			// models were never confirmed to exist.
			return d.discoverAnthropic(ctx, provider, apiKey)
		case "ollama":
			return d.discoverOllama(ctx, provider, apiKey)
		case "ollama-cloud":
			// Ollama Cloud (ollama.com) serves the same /api/tags + /api/show
			// discovery endpoints as local Ollama.
			return d.discoverOllama(ctx, provider, apiKey)
		case "opencode-zen":
			return d.discoverOpenCodeZen(ctx, provider, apiKey)
		case "opencode-go":
			return d.discoverOpenCodeGo(ctx, provider, apiKey)
		case "xai":
			return d.discoverXAI(ctx, provider, apiKey)
		case "google":
			return d.discoverGoogleAIStudio(ctx, provider, apiKey)
		case "vertex-express":
			return d.discoverVertexExpress(ctx, provider, apiKey)
		case "cohere":
			return d.discoverCohere(ctx, provider, apiKey)
		case "openrouter":
			return d.discoverOpenRouter(ctx, provider, apiKey)
		case "koboldcpp":
			return d.discoverKoboldCPP(ctx, provider, apiKey)
		case "lmstudio":
			return d.discoverLMStudio(ctx, provider, apiKey)
		default:
			return d.discoverOpenAI(ctx, provider, apiKey)
		}
	}()
	if err != nil {
		debuglog.Error("discovery: discovery failed", "provider", provider.Name, "provider_id", provider.ID, "type", providerType, "error", err)
		return nil, err
	}

	debuglog.Info("discovery: completed", "provider", provider.Name, "provider_id", provider.ID, "models", len(models))
	return models, nil
}

const maxQuotaRetries = 3

// Circuit breaker thresholds.
const (
	// quotaBreakerThreshold is the number of consecutive failures before the
	// circuit opens and further quota fetches are short-circuited.
	quotaBreakerThreshold = 5
	// quotaBreakerResetAfter is how long an open circuit stays open before
	// transitioning to half-open, allowing one probe request through.
	quotaBreakerResetAfter = 5 * time.Minute
)

// quotaCircuitState tracks consecutive failures for a single provider.
type quotaCircuitState struct {
	mu             sync.Mutex
	consecFailures int
	openUntil      time.Time // zero means closed; set when circuit opens
}

// isCircuitOpen returns true if the circuit is open (requests should be
// short-circuited). If the open window has expired, it transitions to
// half-open and returns false (allowing one probe).
func (s *quotaCircuitState) isCircuitOpen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.openUntil.IsZero() {
		return false
	}
	if time.Now().Before(s.openUntil) {
		return true
	}
	// Half-open: allow one probe. Don't reset consecFailures yet; the
	// probe success will do that.
	s.openUntil = time.Time{}
	return false
}

// recordSuccess resets the circuit breaker state on a successful fetch and
// reports whether the circuit had been failing/open (so the caller can log the
// recovery transition).
func (s *quotaCircuitState) recordSuccess() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Only report recovery for a circuit that actually tripped (open) or is
	// probing after a trip (half-open keeps consecFailures at/above threshold
	// until this success). A sub-threshold blip never opened, so its recovery
	// isn't worth a line.
	wasFailing := s.consecFailures >= quotaBreakerThreshold || !s.openUntil.IsZero()
	s.consecFailures = 0
	s.openUntil = time.Time{}
	return wasFailing
}

// recordFailure increments the failure counter and opens the circuit if the
// threshold is reached. Returns true if the circuit just opened.
func (s *quotaCircuitState) recordFailure() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecFailures++
	if s.consecFailures >= quotaBreakerThreshold && s.openUntil.IsZero() {
		s.openUntil = time.Now().Add(quotaBreakerResetAfter)
		return true
	}
	return false
}

// getOrCreateCircuit returns the circuit breaker state for a provider,
// creating one if it doesn't exist yet.
func (d *DiscoveryService) getOrCreateCircuit(providerID string) *quotaCircuitState {
	val, _ := d.quotaBreaker.LoadOrStore(providerID, &quotaCircuitState{})
	circuit, ok := val.(*quotaCircuitState)
	if !ok {
		debuglog.Error("quotaBreaker: unexpected type", "provider_id", providerID, "type", fmt.Sprintf("%T", val))
		return &quotaCircuitState{}
	}
	return circuit
}

// isTransientNetworkError returns true for DNS failures, timeouts, and
// connection errors that are likely to succeed on retry.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	// url.Error wraps underlying network errors
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		return isTransientNetworkError(urlErr.Err)
	}
	return false
}

// isRetryableStatus returns true for HTTP status codes that warrant a retry.
func isRetryableStatus(statusCode int) bool {
	switch {
	case statusCode == http.StatusTooManyRequests: // 429
		return true
	case statusCode >= 500 && statusCode < 600: // 5xx
		return true
	default:
		return false
	}
}

// retryBackoff computes a linear backoff with jitter: base × attempt + random
// jitter in [0, base). This prevents thundering herd when multiple providers
// fail simultaneously.
func retryBackoff(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	delay := time.Duration(attempt) * base
	jitter := time.Duration(rand.Int64N(int64(base)))
	return delay + jitter
}

// doQuotaRequestWithRetry executes an HTTP request with retries for transient
// network errors (DNS failures, timeouts, connection issues) and retryable
// HTTP statuses (429, 5xx). The circuit breaker short-circuits requests to
// providers that have failed consecutively beyond the threshold.
// On success, the circuit breaker is reset automatically.
// On final failure, the circuit breaker failure counter is incremented.
func (d *DiscoveryService) doQuotaRequestWithRetry(ctx context.Context, req *http.Request, providerID, providerName, providerType string) (*http.Response, error) {
	circuit := d.getOrCreateCircuit(providerID)
	if circuit.isCircuitOpen() {
		debuglog.Warn("discovery: circuit breaker open, skipping quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID)
		return nil, fmt.Errorf("quota fetch circuit breaker open for provider %s (consecutive failures threshold reached)", providerName)
	}

	var lastErr error
	for attempt := range maxQuotaRetries {
		if attempt > 0 {
			backoff := retryBackoff(d.retryBaseDelay, attempt)
			debuglog.Info("discovery: retrying quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "backoff", backoff, "attempt", attempt+1, "max_attempts", maxQuotaRetries)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context cancelled during retry: %w", lastErr)
			case <-time.After(backoff):
			}
		}
		//nolint:gosec // provider URL is admin-configured, not arbitrary user input
		resp, err := d.httpClient.Do(req)
		if err != nil {
			// Same scrub as doDiscoveryRequest, for the same reason, and with a
			// worse sink: this error is persisted as the provider's quota
			// failure and rendered on the dashboard, not only logged.
			if isTransientNetworkError(err) {
				lastErr = maskedRequestError(req, err)
				continue
			}
			if opened := circuit.recordFailure(); opened {
				debuglog.Warn("discovery: circuit breaker opened for quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "threshold", quotaBreakerThreshold)
			}
			return nil, maskedRequestError(req, err)
		}
		// Retry on 429 (rate-limited) and 5xx (server error) responses.
		if isRetryableStatus(resp.StatusCode) {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("retryable HTTP %d: %s", resp.StatusCode, maskRequestSecrets(req, string(body), 200))
			debuglog.Info("discovery: retryable HTTP status for quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "status", resp.StatusCode, "attempt", attempt+1)
			continue
		}
		// Success or non-retryable status: return as-is.
		if recovered := circuit.recordSuccess(); recovered {
			debuglog.Info("discovery: quota circuit breaker recovered", "type", providerType, "provider", providerName, "provider_id", providerID)
		}
		return resp, nil
	}
	if opened := circuit.recordFailure(); opened {
		debuglog.Warn("discovery: circuit breaker opened for quota fetch", "type", providerType, "provider", providerName, "provider_id", providerID, "threshold", quotaBreakerThreshold)
	}
	return nil, fmt.Errorf("quota fetch failed for provider %s (type=%s) after %d attempts: %w", providerName, providerType, maxQuotaRetries, lastErr)
}
