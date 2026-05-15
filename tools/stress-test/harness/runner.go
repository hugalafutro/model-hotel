package harness

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/tools/stress-test/metrics"
)

// ScenarioConfig defines a single test scenario.
type ScenarioConfig struct {
	Concurrency int
	NumKeys     int
	RateLimitOn bool
	RPS         float64
	Burst       int
	Streaming   bool
	// TotalRequests is the total number of requests to send. If 0, defaults to Concurrency * 10.
	TotalRequests int
	// IPRateLimitOn toggles the IP rate limiter. nil means "do not change".
	IPRateLimitOn *bool
	// PerKeyRPS and PerKeyBurst override per-key rate limits for each key.
	// nil means "use global setting" (no override).
	PerKeyRPS   *float64
	PerKeyBurst *int
}

// ScenarioResult holds the outcome of a scenario run.
type ScenarioResult struct {
	Config  ScenarioConfig
	Summary metrics.Summary
}

// ProviderConfig defines a single mock upstream provider for Setup.
type ProviderConfig struct {
	Name string // provider name (e.g. "stress-mock-0")
	URL  string // base URL as seen by the proxy (e.g. "http://host.docker.internal:9090/v1")
}

// Runner executes test scenarios against the proxy.
type Runner struct {
	proxyClient *ProxyClient
	admin       *AdminClient
	keys        []string // raw virtual key values
	keyIDs      []string // virtual key UUIDs (for cleanup)
	keyNames    []string // virtual key names (for update API)
	providerIDs []string // provider UUIDs (for cleanup)
	models      []string // model IDs to round-robin (e.g. "stress-mock-0/mock-model")
}

// SetExtraParams configures the proxy client to send additional request
// parameters in every chat completion request. This exercises the proxy's
// param-rejection auto-retry path when combined with mock server RejectParams.
func (r *Runner) SetExtraParams(params map[string]interface{}) {
	r.proxyClient.ExtraParams = params
}

// NewRunner creates a scenario runner. Call Setup to provision fixtures
// and Cleanup to tear them down.
func NewRunner(proxyClient *ProxyClient, admin *AdminClient) *Runner {
	return &Runner{
		proxyClient: proxyClient,
		admin:       admin,
	}
}

// Setup provisions the test fixtures: one or more providers pointing to mock
// upstream servers and the specified number of virtual keys. When multiple
// ProviderConfigs are given, requests are round-robined across providers.
func (r *Runner) Setup(providers []ProviderConfig, numKeys int) error {
	r.providerIDs = make([]string, len(providers))
	r.models = make([]string, len(providers))

	for i, pc := range providers {
		prov, err := r.admin.CreateProvider(pc.Name, pc.URL, "sk-mock-stress-test-key")
		if err != nil {
			// Cleanup already-created providers
			for j := 0; j < i; j++ {
				_ = r.admin.DeleteProvider(r.providerIDs[j])
			}
			return fmt.Errorf("setup: create provider %d: %w", i, err)
		}

		// Trigger discovery so the provider's models are registered.
		if err := r.admin.TriggerDiscovery(prov.ID); err != nil {
			debuglog.Warn("runner: discovery failed (will try to use model anyway)", "provider", pc.Name, "error", err)
		}

		r.providerIDs[i] = prov.ID
		r.models[i] = pc.Name + "/mock-model"
	}

	// Brief pause for model cache to populate after discovery.
	time.Sleep(500 * time.Millisecond)

	// Create virtual keys
	r.keys = make([]string, numKeys)
	r.keyIDs = make([]string, numKeys)
	r.keyNames = make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		vk, err := r.admin.CreateVirtualKey(fmt.Sprintf("stress-key-%d", i), nil, nil)
		if err != nil {
			// Cleanup partial keys
			for j := 0; j < i; j++ {
				_ = r.admin.DeleteVirtualKey(r.keyIDs[j])
			}
			for _, pid := range r.providerIDs {
				_ = r.admin.DeleteProvider(pid)
			}
			return fmt.Errorf("setup: create virtual key %d: %w", i, err)
		}
		r.keys[i] = vk.Key
		r.keyIDs[i] = vk.ID
		r.keyNames[i] = vk.Name
	}

	debuglog.Info("runner: setup complete", "providers", len(providers), "keys", numKeys, "models", r.models)
	return nil
}

// Cleanup removes all test fixtures.
func (r *Runner) Cleanup() {
	for _, id := range r.keyIDs {
		if err := r.admin.DeleteVirtualKey(id); err != nil {
			debuglog.Warn("runner: failed to delete key", "keyID", id, "error", err)
		}
	}
	for _, id := range r.providerIDs {
		if err := r.admin.DeleteProvider(id); err != nil {
			debuglog.Warn("runner: failed to delete provider", "providerID", id, "error", err)
		}
	}
	debuglog.Info("runner: cleanup complete")
}

// RunScenario executes a single test scenario and returns the results.
func (r *Runner) RunScenario(cfg ScenarioConfig) *ScenarioResult {
	totalReqs := cfg.TotalRequests
	if totalReqs == 0 {
		totalReqs = cfg.Concurrency * 10
	}

	label := fmt.Sprintf("%d-conc, RL=%v, %d-key, stream=%v",
		cfg.Concurrency, cfg.RateLimitOn, cfg.NumKeys, cfg.Streaming)
	if cfg.PerKeyRPS != nil || cfg.PerKeyBurst != nil {
		burstVal := 0
		if cfg.PerKeyBurst != nil {
			burstVal = *cfg.PerKeyBurst
		}
		label += fmt.Sprintf(", key-limits=%.0f/%d", floatPtrOr(cfg.PerKeyRPS, 0), burstVal)
	}
	debuglog.Info("runner: starting scenario", "label", label, "requests", totalReqs)

	// Configure rate limiting. Only send rps/burst when rate limiting is
	// enabled; the API validates burst >= 1 and will reject burst=0.
	settings := map[string]string{
		"rate_limit_enabled": fmt.Sprintf("%v", cfg.RateLimitOn),
	}
	if cfg.RateLimitOn {
		settings["rate_limit_rps"] = fmt.Sprintf("%.0f", cfg.RPS)
		burst := cfg.Burst
		if burst < 1 {
			burst = 1
		}
		settings["rate_limit_burst"] = fmt.Sprintf("%d", burst)
	}
	// IP rate limiter is a separate toggle. When RateLimitOn is false,
	// disable IP rate limiting too unless explicitly overridden.
	if cfg.IPRateLimitOn != nil {
		settings["rate_limit_ip_enabled"] = fmt.Sprintf("%v", *cfg.IPRateLimitOn)
	} else if !cfg.RateLimitOn {
		settings["rate_limit_ip_enabled"] = "false"
	}
	if err := r.admin.UpdateSettings(settings); err != nil {
		debuglog.Warn("runner: failed to update rate limit settings", "error", err)
	}

	// Brief pause for settings to propagate (the settings API is synchronous
	// but the rate limiter reads from cache on each request)
	time.Sleep(100 * time.Millisecond)

	// Apply per-key rate limit overrides if specified
	if cfg.PerKeyRPS != nil || cfg.PerKeyBurst != nil {
		numKeys := cfg.NumKeys
		if numKeys > len(r.keys) {
			numKeys = len(r.keys)
		}
		for i := 0; i < numKeys; i++ {
			if err := r.admin.UpdateVirtualKeyRateLimits(r.keyIDs[i], r.keyNames[i], cfg.PerKeyRPS, cfg.PerKeyBurst); err != nil {
				debuglog.Warn("runner: failed to update per-key rate limits", "keyID", r.keyIDs[i], "error", err)
			}
		}
		// Brief pause for per-key settings to propagate
		time.Sleep(100 * time.Millisecond)
	}

	collector := metrics.NewCollector(totalReqs)

	// Use a semaphore to cap concurrency
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup

	var requestsSent atomic.Int64

	wallStart := time.Now()

	for i := 0; i < totalReqs; i++ {
		wg.Add(1)
		sem <- struct{}{} // acquire slot

		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			// Pick a key (round-robin across first NumKeys keys)
			numKeys := cfg.NumKeys
			if numKeys > len(r.keys) {
				numKeys = len(r.keys)
			}
			keyIdx := idx % numKeys
			key := r.keys[keyIdx]

			// Pick a model (round-robin across providers)
			modelIdx := idx % len(r.models)
			model := r.models[modelIdx]

			result := r.proxyClient.SendChatCompletion(key, model, cfg.Streaming)
			result.KeyIndex = keyIdx
			collector.Record(result)

			sent := requestsSent.Add(1)
			if sent%100 == 0 {
				debuglog.Info("runner: requests sent", "label", label, "sent", sent, "total", totalReqs)
			}
		}(i)
	}

	wg.Wait()
	summary := collector.Summarize(wallStart)

	debuglog.Info("runner: scenario complete", "label", label, "success", summary.SuccessCount, "errors", summary.ErrorCount, "throughputRPS", summary.ThroughputRPS)

	return &ScenarioResult{
		Config:  cfg,
		Summary: summary,
	}
}

// Keys returns the raw virtual key values (for external use).
func (r *Runner) Keys() []string {
	return r.keys
}

// Models returns the model IDs available for requests.
func (r *Runner) Models() []string {
	return r.models
}

func floatPtrOr(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}
