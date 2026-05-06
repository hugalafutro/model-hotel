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
	CircuitBreakerEnabled string  // empty = don't change, "true"/"false"
	RequestTimeout        string  // empty = don't change, e.g. "30s", "1m0s"
	FailoverOnRateLimit   string  // empty = don't change, "true"/"false"
}

// ScenarioResult holds the outcome of a scenario run.
type ScenarioResult struct {
	Config  ScenarioConfig
	Summary metrics.Summary
}

// Runner executes test scenarios against the proxy.
type Runner struct {
	proxyClient *ProxyClient
	admin       *AdminClient
	keys        []string // raw virtual key values
	keyIDs      []string // virtual key UUIDs (for cleanup)
	providerID  string   // provider UUID (for cleanup)
	model       string   // model ID to use (e.g. "stress-mock/mock-model")
}

// SetExtraParams configures the proxy client to send additional request
// parameters in every chat completion request. This exercises the proxy's
// param-rejection auto-retry path when combined with mock server RejectParams.
func (r *Runner) SetExtraParams(params map[string]interface{}) {
	r.proxyClient.ExtraParams = params
}

// ApplySettings pushes arbitrary settings to the proxy via the admin API.
// This is used to configure circuit breaker, request timeout, and failover
// behaviour before scenarios start.
func (r *Runner) ApplySettings(settings map[string]string) error {
	return r.admin.UpdateSettings(settings)
}

// NewRunner creates a scenario runner. Call Setup to provision fixtures
// and Cleanup to tear them down.
func NewRunner(proxyClient *ProxyClient, admin *AdminClient) *Runner {
	return &Runner{
		proxyClient: proxyClient,
		admin:       admin,
	}
}

// Setup provisions the test fixtures: a provider pointing to the mock
// upstream and the specified number of virtual keys.
func (r *Runner) Setup(mockURL string, numKeys int) error {
	// Create provider
	prov, err := r.admin.CreateProvider("stress-mock", mockURL, "sk-mock-stress-test-key")
	if err != nil {
		return fmt.Errorf("setup: create provider: %w", err)
	}

	// Trigger discovery so the provider's models are registered.
	// The mock server has a /v1/models endpoint that returns "mock-model".
	if err := r.admin.TriggerDiscovery(prov.ID); err != nil {
		debuglog.Warn("runner: discovery failed (will try to use model anyway)", "error", err)
	}

	r.providerID = prov.ID
	r.model = "stress-mock/mock-model"

	// Create virtual keys
	r.keys = make([]string, numKeys)
	r.keyIDs = make([]string, numKeys)

	for i := 0; i < numKeys; i++ {
		vk, err := r.admin.CreateVirtualKey(fmt.Sprintf("stress-key-%d", i))
		if err != nil {
			// Cleanup partial keys
			for j := 0; j < i; j++ {
				r.admin.DeleteVirtualKey(r.keyIDs[j])
			}
			r.admin.DeleteProvider(prov.ID)
			return fmt.Errorf("setup: create virtual key %d: %w", i, err)
		}
		r.keys[i] = vk.Key
		r.keyIDs[i] = vk.ID
	}

	debuglog.Info("runner: setup complete", "provider", prov.ID, "keys", numKeys, "model", r.model)
	return nil
}

// Cleanup removes all test fixtures.
func (r *Runner) Cleanup() {
	for _, id := range r.keyIDs {
		if err := r.admin.DeleteVirtualKey(id); err != nil {
			debuglog.Warn("runner: failed to delete key", "keyID", id, "error", err)
		}
	}
	if r.providerID != "" {
		if err := r.admin.DeleteProvider(r.providerID); err != nil {
			debuglog.Warn("runner: failed to delete provider", "providerID", r.providerID, "error", err)
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
	debuglog.Info("runner: starting scenario", "label", label, "requests", totalReqs)

	// Configure rate limiting
	settings := map[string]string{
		"rate_limit_enabled": fmt.Sprintf("%v", cfg.RateLimitOn),
		"rate_limit_rps":     fmt.Sprintf("%.0f", cfg.RPS),
		"rate_limit_burst":   fmt.Sprintf("%d", cfg.Burst),
	}
	if cfg.CircuitBreakerEnabled != "" {
		settings["circuit_breaker_enabled"] = cfg.CircuitBreakerEnabled
	}
	if cfg.RequestTimeout != "" {
		settings["request_timeout"] = cfg.RequestTimeout
	}
	if cfg.FailoverOnRateLimit != "" {
		settings["failover_on_rate_limit"] = cfg.FailoverOnRateLimit
	}
	if err := r.admin.UpdateSettings(settings); err != nil {
		debuglog.Warn("runner: failed to update rate limit settings", "error", err)
	}

	// Brief pause for settings to propagate (the settings API is synchronous
	// but the rate limiter reads from cache on each request)
	time.Sleep(100 * time.Millisecond)

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

			result := r.proxyClient.SendChatCompletion(key, r.model, cfg.Streaming)
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

// Model returns the model ID to use for requests.
func (r *Runner) Model() string {
	return r.model
}
