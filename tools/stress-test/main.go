package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/tools/stress-test/harness"
	"github.com/hugalafutro/model-hotel/tools/stress-test/metrics"
	"github.com/hugalafutro/model-hotel/tools/stress-test/mock"
)

func main() {
	// Connection settings
	proxyURL := flag.String("proxy-url", "http://localhost:8081", "Proxy base URL")
	adminToken := flag.String("admin-token", "", "Admin token for API calls (required)")
	mockPort := flag.Int("mock-port", 9090, "Port for the mock upstream server")

	// Scenario settings
	concurrencyStr := flag.String("concurrency", "10,50,100,1000", "Comma-separated concurrency levels")
	keysStr := flag.String("keys", "1,10", "Comma-separated number of virtual keys to test with")
	rateLimitModes := flag.String("rate-limit", "false,true", "Comma-separated rate limit on/off values")
	streaming := flag.Bool("streaming", true, "Use streaming requests")
	requestsPerScenario := flag.Int("requests", 0, "Total requests per scenario (0 = concurrency * 10)")

	// Mock server settings
	chunkDelay := flag.Int("chunk-delay", 20, "Delay between SSE chunks in milliseconds")
	chunkCount := flag.Int("chunk-count", 15, "Number of SSE chunks per response")
	tokensPerChunk := flag.Int("tokens-per-chunk", 3, "Completion tokens per SSE chunk")
	initialDelay := flag.Int("initial-delay", 10, "Initial delay before first chunk in ms (simulates TTFT)")
	errorRate := flag.Float64("error-rate", 0, "Probability of mock server returning 500 error (0.0-1.0, 0 = disabled)")
	rejectParams := flag.String("reject-params", "", "Comma-separated param names the mock server rejects with 400 (e.g. top_p,frequency_penalty)")
	extraParams := flag.String("extra-params", "", "Comma-separated param names to include in requests (set to 0.5 for floats, e.g. top_p=0.5,frequency_penalty=1.0)")

	// Rate limit defaults (used when RL is on)
	rps := flag.Float64("rps", 10, "Rate limit RPS when enabled")
	burst := flag.Int("burst", 20, "Rate limit burst when enabled")

	// Circuit breaker & timeout settings (applied before each scenario via UpdateSettings)
	circuitBreakerEnabled := flag.String("circuit-breaker-enabled", "", "Enable circuit breaker: true or false (empty = don't change)")
	circuitBreakerThreshold := flag.Int("circuit-breaker-threshold", 0, "Consecutive failures before circuit opens (0 = don't change)")
	circuitBreakerCooldown := flag.String("circuit-breaker-cooldown", "", "Circuit breaker cooldown duration, e.g. 30s, 1m (empty = don't change)")
	requestTimeout := flag.String("request-timeout", "", "Proxy request timeout, e.g. 30s, 1m0s, 2m0s (empty = don't change)")
	failoverOnRateLimit := flag.String("failover-on-rate-limit", "", "Enable rate-limit failover: true or false (empty = don't change)")

	// Output
	outputFormat := flag.String("output", "markdown", "Output format: text, markdown, json")

	flag.Parse()

	if *adminToken == "" {
		fmt.Fprintln(os.Stderr, "Error: -admin-token is required")
		flag.Usage()
		os.Exit(1)
	}

	// Parse comma-separated values
	concurrencyLevels := parseIntList(*concurrencyStr)
	keyCounts := parseIntList(*keysStr)
	rlModes := parseBoolList(*rateLimitModes)

	if len(concurrencyLevels) == 0 {
		concurrencyLevels = []int{10, 50, 100, 1000}
	}
	if len(keyCounts) == 0 {
		keyCounts = []int{1}
	}
	if len(rlModes) == 0 {
		rlModes = []bool{false, true}
	}

	// Validate output format
	format := metrics.Format(*outputFormat)
	switch format {
	case metrics.FormatText, metrics.FormatMarkdown, metrics.FormatJSON:
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported output format %q (use text, markdown, or json)\n", *outputFormat)
		os.Exit(1)
	}

	debuglog.Info("main: ╔══════════════════════════════════════════════════════════════╗")
	debuglog.Info("main: ║  Model Hotel Synthetic Stress Test                          ║")
	debuglog.Info("main: ╚══════════════════════════════════════════════════════════════╝")
	debuglog.Info("main: Proxy", "url", *proxyURL)
	debuglog.Info("main: Mock", "port", *mockPort)
	debuglog.Info("main: Concurrency levels", "levels", concurrencyLevels)
	debuglog.Info("main: Key counts", "counts", keyCounts)
	debuglog.Info("main: Rate limit modes", "modes", rlModes)
	debuglog.Info("main: Streaming", "enabled", *streaming)
	debuglog.Info("main: Chunk config", "chunkDelay", *chunkDelay, "chunkCount", *chunkCount, "tokensPerChunk", *tokensPerChunk)
	debuglog.Info("main: Circuit breaker", "enabled", *circuitBreakerEnabled, "threshold", *circuitBreakerThreshold, "cooldown", *circuitBreakerCooldown)
	debuglog.Info("main: Request timeout", "timeout", *requestTimeout)
	debuglog.Info("main: Failover on rate limit", "enabled", *failoverOnRateLimit)

	// ── Start mock server ──────────────────────────────────────────
	mockAddr := fmt.Sprintf(":%d", *mockPort)
	mockServer := mock.NewServer(mockAddr)
	mockServer.ChunkDelay = time.Duration(*chunkDelay) * time.Millisecond
	mockServer.ChunkCount = *chunkCount
	mockServer.TokensPerChunk = *tokensPerChunk
	mockServer.InitialDelay = time.Duration(*initialDelay) * time.Millisecond
	mockServer.ErrorRate = *errorRate

	debuglog.Info("main: starting mock upstream server...")
	if err := mockServer.StartAsync(); err != nil {
		log.Fatalf("Failed to start mock server: %v", err)
	}
	defer mockServer.Stop()
	debuglog.Info("main: mock server listening", "addr", mockAddr)

	// Wait for mock to be ready
	<-mockServer.Ready()

	// Parse reject/extra params
	if *rejectParams != "" {
		for _, p := range strings.Split(*rejectParams, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				mockServer.RejectParams = append(mockServer.RejectParams, p)
			}
		}
		debuglog.Info("main: mock server will reject params", "params", mockServer.RejectParams)
	}

	// ── Create clients ─────────────────────────────────────────────
	admin := harness.NewAdminClient(*proxyURL, *adminToken)

	// Use the max concurrency to determine the HTTP client timeout.
	// Each request takes roughly chunkCount*chunkDelay ms of streaming time,
	// plus proxy overhead. Multiply by 2 for safety headroom.
	maxConc := maxInt(concurrencyLevels)
	clientTimeoutMs := int64(maxConc) * int64(*chunkDelay) * int64(*chunkCount) * 2
	clientTimeout := time.Duration(clientTimeoutMs) * time.Millisecond
	if clientTimeout < 30*time.Second {
		clientTimeout = 30 * time.Second
	}
	if clientTimeout > 10*time.Minute {
		clientTimeout = 10 * time.Minute
	}
	debuglog.Info("main: per-request client timeout", "timeout", clientTimeout)

	proxyClient := harness.NewProxyClient(*proxyURL, clientTimeout)
	runner := harness.NewRunner(proxyClient, admin)

	// Parse extra params (e.g. "top_p=0.5,frequency_penalty=1.0") and configure
	// the proxy client to include them in every request. This exercises the
	// proxy's param-rejection auto-retry path when combined with -reject-params.
	if *extraParams != "" {
		extraMap := make(map[string]interface{})
		for _, kv := range strings.Split(*extraParams, ",") {
			kv = strings.TrimSpace(kv)
			if kv == "" {
				continue
			}
			parts := strings.SplitN(kv, "=", 2)
			key := strings.TrimSpace(parts[0])
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					extraMap[key] = f
				} else {
					extraMap[key] = val
				}
			} else {
				extraMap[key] = true
			}
		}
		runner.SetExtraParams(extraMap)
		debuglog.Info("main: extra request params", "params", extraMap)
	}

	// ── Determine max keys needed ──────────────────────────────────
	maxKeys := maxInt(keyCounts)

	// ── Setup test fixtures ────────────────────────────────────────
	mockURL := mockServer.URL()
	if err := runner.Setup(mockURL, maxKeys); err != nil {
		log.Fatalf("Failed to set up test fixtures: %v", err)
	}
	defer func() {
		debuglog.Info("main: cleaning up test fixtures...")
		runner.Cleanup()
	}()

	// ── Apply circuit breaker & timeout settings ──────────────────
	settingsUpdates := map[string]string{}
	if *circuitBreakerEnabled != "" {
		settingsUpdates["circuit_breaker_enabled"] = *circuitBreakerEnabled
	}
	if *circuitBreakerThreshold > 0 {
		settingsUpdates["circuit_breaker_threshold"] = fmt.Sprintf("%d", *circuitBreakerThreshold)
	}
	if *circuitBreakerCooldown != "" {
		settingsUpdates["circuit_breaker_cooldown"] = *circuitBreakerCooldown
	}
	if *requestTimeout != "" {
		settingsUpdates["request_timeout"] = *requestTimeout
	}
	if *failoverOnRateLimit != "" {
		settingsUpdates["failover_on_rate_limit"] = *failoverOnRateLimit
	}
	if len(settingsUpdates) > 0 {
		debuglog.Info("main: applying circuit breaker & timeout settings", "updates", settingsUpdates)
		if err := runner.ApplySettings(settingsUpdates); err != nil {
			debuglog.Warn("main: failed to apply circuit breaker & timeout settings", "error", err)
		}
		time.Sleep(100 * time.Millisecond) // let settings propagate
	}

	// ── Build scenario matrix ──────────────────────────────────────
	var scenarios []harness.ScenarioConfig
	for _, conc := range concurrencyLevels {
		for _, numKeys := range keyCounts {
			for _, rlOn := range rlModes {
				// Use only the keys needed for this scenario
				scenario := harness.ScenarioConfig{
					Concurrency:           conc,
					NumKeys:               numKeys,
					RateLimitOn:           rlOn,
					RPS:                   *rps,
					Burst:                 *burst,
					Streaming:             *streaming,
					TotalRequests:         *requestsPerScenario,
					CircuitBreakerEnabled: *circuitBreakerEnabled,
					RequestTimeout:        *requestTimeout,
					FailoverOnRateLimit:   *failoverOnRateLimit,
				}
				scenarios = append(scenarios, scenario)
			}
		}
	}

	debuglog.Info("main: running scenarios", "count", len(scenarios))

	// ── Execute scenarios ──────────────────────────────────────────
	var scenarioReports []metrics.ScenarioReport

	for i, sc := range scenarios {
		label := fmt.Sprintf("%d-conc, RL=%v, %d-key, stream=%v",
			sc.Concurrency, sc.RateLimitOn, sc.NumKeys, sc.Streaming)

		debuglog.Info("main: scenario", "index", i+1, "total", len(scenarios), "label", label)

		result := runner.RunScenario(sc)

		scenarioReports = append(scenarioReports, metrics.ScenarioReport{
			Label:       label,
			Concurrency: sc.Concurrency,
			RateLimitOn: sc.RateLimitOn,
			NumKeys:     sc.NumKeys,
			Streaming:   sc.Streaming,
			Summary:     result.Summary,
		})

		// Cool-down between scenarios
		if i < len(scenarios)-1 {
			debuglog.Info("main: cooling down for 2 seconds...")
			time.Sleep(2 * time.Second)
		}
	}

	// ── Print report ───────────────────────────────────────────────
	report := &metrics.Report{
		ProxyURL:  *proxyURL,
		MockURL:   mockURL,
		Scenarios: scenarioReports,
	}

	fmt.Println()
	if err := report.Write(os.Stdout, format); err != nil {
		log.Fatalf("Failed to write report: %v", err)
	}

	// ── Mock server stats ──────────────────────────────────────────
	served, failed := mockServer.Stats()
	debuglog.Info("main: mock server stats", "served", served, "failed", failed)
}

func parseIntList(s string) []int {
	parts := strings.Split(s, ",")
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil {
			debuglog.Warn("main: ignoring invalid concurrency value", "value", p, "error", err)
			continue
		}
		result = append(result, v)
	}
	return result
}

func parseBoolList(s string) []bool {
	parts := strings.Split(s, ",")
	var result []bool
	for _, p := range parts {
		p = strings.TrimSpace(p)
		b, err := strconv.ParseBool(p)
		if err != nil {
			debuglog.Warn("main: ignoring invalid bool value", "value", p, "error", err)
			continue
		}
		result = append(result, b)
	}
	return result
}

func maxInt(vals []int) int {
	m := 0
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}
