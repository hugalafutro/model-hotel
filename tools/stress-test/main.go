package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
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
	proxyURL := flag.String("proxy-url", "http://localhost:8080", "Proxy base URL")
	adminToken := flag.String("admin-token", "", "Admin token for API calls (required)")
	mockPort := flag.Int("mock-port", 9090, "Base port for the mock upstream server(s)")
	mockURL := flag.String("mock-url", "", "Override URL for the mock server as seen by the proxy (e.g. http://host.docker.internal:9090/v1 when proxy runs in Docker)")
	mockWorkers := flag.Int("mock-workers", 1, "Number of mock upstream servers to start (distributes load across multiple providers)")

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
	streamDuration := flag.String("stream-duration", "", "Random stream duration range in seconds (e.g. 3-13). Overrides -chunk-delay with per-request random variation")
	rejectParams := flag.String("reject-params", "", "Comma-separated param names the mock server rejects with 400 (e.g. top_p,frequency_penalty)")
	extraParams := flag.String("extra-params", "", "Comma-separated param names to include in requests (set to 0.5 for floats, e.g. top_p=0.5,frequency_penalty=1.0)")

	// Rate limit defaults (used when RL is on)
	rps := flag.Float64("rps", 10, "Rate limit RPS when enabled")
	burst := flag.Int("burst", 20, "Rate limit burst when enabled")

	// Per-key rate limit overrides
	keyRPS := flag.Float64("key-rps", 0, "Per-key rate limit RPS override (0 = use global setting, no override)")
	keyBurst := flag.Int("key-burst", 0, "Per-key rate limit burst override (0 = use global setting, no override)")

	// IP rate limiter override
	ipRateLimit := flag.String("ip-ratelimit", "", "Override IP rate limiter: true or false (empty = do not change)")

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

	// Build per-key rate limit overrides
	var perKeyRPS *float64
	var perKeyBurst *int
	if *keyRPS > 0 {
		perKeyRPS = keyRPS
	}
	if *keyBurst > 0 {
		perKeyBurst = keyBurst
	}

	var ipRateLimitOn *bool
	if *ipRateLimit != "" {
		v, err := strconv.ParseBool(*ipRateLimit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid -ip-ratelimit value %q (use true or false)\n", *ipRateLimit)
			os.Exit(1)
		}
		ipRateLimitOn = &v
	}

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
	if perKeyRPS != nil || perKeyBurst != nil {
		burstVal := 0
		if perKeyBurst != nil {
			burstVal = *perKeyBurst
		}
		debuglog.Info("main: Per-key rate limits", "rps", floatPtrVal(perKeyRPS), "burst", burstVal)
	}
	debuglog.Info("main: Streaming", "enabled", *streaming)
	debuglog.Info("main: Chunk config", "chunkDelay", *chunkDelay, "chunkCount", *chunkCount, "tokensPerChunk", *tokensPerChunk)

	// ── Start mock server(s) ─────────────────────────────────────────
	if *mockWorkers < 1 {
		*mockWorkers = 1
	}

	type mockInstance struct {
		server *mock.Server
		port   int
	}
	var mockInstances []mockInstance

	for w := 0; w < *mockWorkers; w++ {
		port := *mockPort + w
		mockAddr := fmt.Sprintf(":%d", port)
		mockServer := mock.NewServer(mockAddr)
		mockServer.ChunkDelay = time.Duration(*chunkDelay) * time.Millisecond
		mockServer.ChunkCount = *chunkCount
		mockServer.TokensPerChunk = *tokensPerChunk
		mockServer.InitialDelay = time.Duration(*initialDelay) * time.Millisecond

		// Parse stream-duration range (e.g. "3-13" → 3s–13s per request)
		if *streamDuration != "" {
			if w == 0 { // parse once, apply to all
				min, max, err := parseDurationRange(*streamDuration)
				if err != nil {
					log.Fatalf("Invalid -stream-duration: %v (use format like 3-13)", err)
				}
				mockServer.StreamDurationMin = time.Duration(min) * time.Second
				mockServer.StreamDurationMax = time.Duration(max) * time.Second
				debuglog.Info("main: stream duration range", "min", fmt.Sprintf("%ds", min), "max", fmt.Sprintf("%ds", max))
			} else {
				// Copy from first instance (already parsed)
				mockServer.StreamDurationMin = mockInstances[0].server.StreamDurationMin
				mockServer.StreamDurationMax = mockInstances[0].server.StreamDurationMax
			}
		}

		debuglog.Info("main: starting mock upstream server...", "worker", w, "port", port)
		if err := mockServer.StartAsync(); err != nil {
			log.Fatalf("Failed to start mock server %d: %v", w, err)
		}

		// Parse reject params (apply to all workers)
		if *rejectParams != "" {
			for _, p := range strings.Split(*rejectParams, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					mockServer.RejectParams = append(mockServer.RejectParams, p)
				}
			}
		}

		mockInstances = append(mockInstances, mockInstance{server: mockServer, port: port})
	}

	// Wait for all mock servers to be ready
	for _, mi := range mockInstances {
		<-mi.server.Ready()
	}
	if len(mockInstances) > 0 && len(mockInstances[0].server.RejectParams) > 0 {
		debuglog.Info("main: mock servers will reject params", "params", mockInstances[0].server.RejectParams)
	}

	// Defer cleanup for all mock servers
	defer func() {
		for _, mi := range mockInstances {
			mi.server.Stop()
		}
	}()

	// ── Create clients ─────────────────────────────────────────────
	admin := harness.NewAdminClient(*proxyURL, *adminToken)

	// Use the max stream duration to determine the HTTP client timeout.
	// Each request takes roughly the stream duration plus proxy overhead.
	// Multiply by 2 for safety headroom.
	var maxStreamDuration time.Duration
	if len(mockInstances) > 0 && mockInstances[0].server.StreamDurationMax > 0 {
		maxStreamDuration = mockInstances[0].server.StreamDurationMax
	} else {
		maxStreamDuration = time.Duration(*chunkDelay**chunkCount) * time.Millisecond
	}
	clientTimeout := maxStreamDuration * 2
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

	// ── Build provider configs ──────────────────────────────────────
	var providerConfigs []harness.ProviderConfig
	for w := 0; w < *mockWorkers; w++ {
		name := "stress-mock"
		if *mockWorkers > 1 {
			name = fmt.Sprintf("stress-mock-%d", w)
		}

		// Determine the URL the proxy should use to reach this mock server.
		// When --mock-url is set, it overrides the base URL. For multi-worker,
		// we replace the port in the mock URL with the worker's port.
		var providerURL string
		if *mockURL != "" {
			if *mockWorkers > 1 {
				// Replace port in mock URL with this worker's port
				providerURL = replacePortInURL(*mockURL, *mockPort+w)
			} else {
				providerURL = *mockURL
			}
		} else {
			providerURL = mockInstances[w].server.URL()
		}

		providerConfigs = append(providerConfigs, harness.ProviderConfig{
			Name: name,
			URL:  providerURL,
		})
	}

	// ── Setup test fixtures ────────────────────────────────────────
	if err := runner.Setup(providerConfigs, maxKeys); err != nil {
		log.Fatalf("Failed to set up test fixtures: %v", err)
	}
	defer func() {
		debuglog.Info("main: cleaning up test fixtures...")
		runner.Cleanup()
	}()

	// ── Build scenario matrix ──────────────────────────────────────
	var scenarios []harness.ScenarioConfig
	for _, conc := range concurrencyLevels {
		for _, numKeys := range keyCounts {
			for _, rlOn := range rlModes {
				// Use only the keys needed for this scenario
				scenario := harness.ScenarioConfig{
					Concurrency:   conc,
					NumKeys:       numKeys,
					RateLimitOn:   rlOn,
					RPS:           *rps,
					Burst:         *burst,
					Streaming:     *streaming,
					TotalRequests: *requestsPerScenario,
					PerKeyRPS:     perKeyRPS,
					PerKeyBurst:   perKeyBurst,
					IPRateLimitOn: ipRateLimitOn,
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
		MockURL:   providerConfigs[0].URL,
		Scenarios: scenarioReports,
	}

	fmt.Println()
	if err := report.Write(os.Stdout, format); err != nil {
		log.Fatalf("Failed to write report: %v", err)
	}

	// ── Mock server stats ──────────────────────────────────────────
	var totalServed, totalFailed int64
	for _, mi := range mockInstances {
		s, f := mi.server.Stats()
		totalServed += s
		totalFailed += f
	}
	debuglog.Info("main: mock server stats", "workers", len(mockInstances), "served", totalServed, "failed", totalFailed)
}

// replacePortInURL replaces the port number in a URL string.
// e.g. "http://host.docker.internal:9090/v1" with port 9091 → "http://host.docker.internal:9091/v1"
func replacePortInURL(urlStr string, newPort int) string {
	u, err := url.Parse(urlStr)
	if err != nil || u.Scheme == "" {
		return urlStr
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(newPort))
	return u.String()
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

func floatPtrVal(p *float64) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%.0f", *p)
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

// parseDurationRange parses a "min-max" string (e.g. "3-13") and returns
// (min, max) in seconds.
func parseDurationRange(s string) (int, int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected min-max format")
	}
	min, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid min: %w", err)
	}
	max, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid max: %w", err)
	}
	if min <= 0 || max <= 0 {
		return 0, 0, fmt.Errorf("both min and max must be positive")
	}
	if min > max {
		return 0, 0, fmt.Errorf("min (%d) must be <= max (%d)", min, max)
	}
	return min, max, nil
}
