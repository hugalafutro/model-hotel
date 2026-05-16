package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hugalafutro/model-hotel/tools/traffic-sim/simulator"
)

// excludedKeywords identifies model IDs that are not text-only chat models.
var excludedKeywords = []string{
	"vision", "embed", "tts", "audio", "speech", "whisper",
	"dall-e", "stable-diffusion", "midjourney", "image",
	"clip", "codestral-embed", "e5", "bge-", "nomic-embed",
	"m2m100", "seamless", "faster-whisper", "vl-", "-vl",
	"computer-use", "robotics", "lyria", "nano-banana",
	"multimodal", "dall-e", "sdxl",
}

// apiModel represents a model from the /api/models endpoint.
type apiModel struct {
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
}

func isTextOnlyModel(modelID string) bool {
	lower := strings.ToLower(modelID)
	for _, kw := range excludedKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}

// fetchModels queries the proxy API for available models and filters to text-only.
func fetchModels(proxyURL, adminToken string, allowedProviders map[string]bool) ([]simulator.ModelInfo, error) {
	url := proxyURL + "/api/models?limit=500"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+adminToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("fetch models: HTTP %d: %s", resp.StatusCode, string(body))
	}

	var apiModels []apiModel
	if err := json.NewDecoder(resp.Body).Decode(&apiModels); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	var result []simulator.ModelInfo
	for _, m := range apiModels {
		if !allowedProviders[m.ProviderName] {
			continue
		}
		if !allowedProviders[m.ProviderName] {
			continue
		}
		if isTextOnlyModel(m.ModelID) {
			result = append(result, simulator.ModelInfo{
				Provider: m.ProviderName,
				ID:       m.ModelID,
			})
		}
	}
	return result, nil
}

// pickRandomSubset shuffles the model list and returns a random subset of size n.
// Each call produces a different selection.
func pickRandomSubset(models []simulator.ModelInfo, n int) []simulator.ModelInfo {
	shuffled := make([]simulator.ModelInfo, len(models))
	copy(shuffled, models)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	if n > len(shuffled) {
		n = len(shuffled)
	}
	return shuffled[:n]
}

func parseDuration(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	// Support bare integer as minutes (e.g. "10" → 10m)
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil && fmt.Sprintf("%d", n) == s {
		return time.Duration(n) * time.Minute
	}
	log.Fatalf("invalid duration %q (use e.g. 10m, 30s, 1h, or bare integer for minutes)", s)
	return 0
}

func main() {
	url := flag.String("url", "http://localhost:8081", "Proxy base URL")
	key := flag.String("key", "", "Virtual API key (required)")
	adminToken := flag.String("admin-token", "", "Admin token for model discovery (optional, uses -key if empty)")
	users := flag.Int("users", 8, "Number of concurrent simulated users")
	duration := flag.String("duration", "10m", "Total run time (e.g. 10m, 30m, 1h)")
	convMin := flag.String("conv-min", "1m", "Min conversation duration")
	convMax := flag.String("conv-max", "3m", "Max conversation duration")
	streaming := flag.Bool("streaming", true, "Use streaming responses")
	maxTokensMin := flag.Int("max-tokens-min", 10, "Min tokens per response (>=1)")
	maxTokensMax := flag.Int("max-tokens-max", 500, "Max tokens per response (<=1000)")
	jitter := flag.Bool("jitter", true, "Random 2-8s delay between turns")
	modelsFlag := flag.String("models", "", "Comma-separated model list (overrides discovery, format: Provider/ModelID)")
	discover := flag.Bool("discover", true, "Auto-discover models from proxy API")
	pickCount := flag.Int("pick", 30, "Number of models to randomly pick from discovered pool")
	providersFlag := flag.String("providers", "Ollama Cloud,NanoGPT", "Comma-separated provider names to include in discovery")
	flag.Parse()

	if *key == "" {
		fmt.Fprintln(os.Stderr, "Error: -key is required")
		flag.Usage()
		os.Exit(1)
	}

	if *maxTokensMin < 1 {
		log.Fatalf("-max-tokens-min must be >= 1, got %d", *maxTokensMin)
	}
	if *maxTokensMax > 1000 {
		log.Fatalf("-max-tokens-max must be <= 1000, got %d", *maxTokensMax)
	}
	if *maxTokensMin > *maxTokensMax {
		log.Fatalf("-max-tokens-min (%d) must be <= -max-tokens-max (%d)", *maxTokensMin, *maxTokensMax)
	}

	dur := parseDuration(*duration)
	cMin := parseDuration(*convMin)
	cMax := parseDuration(*convMax)

	var models []simulator.ModelInfo

	if *modelsFlag != "" {
		// Explicit model list overrides everything
		for _, m := range strings.Split(*modelsFlag, ",") {
			m = strings.TrimSpace(m)
			parts := strings.SplitN(m, "/", 2)
			if len(parts) != 2 {
				log.Fatalf("invalid model format %q (expected Provider/ModelID)", m)
			}
			models = append(models, simulator.ModelInfo{Provider: parts[0], ID: parts[1]})
		}
	} else if *discover {
		// Auto-discover from API
		token := *adminToken
		if token == "" {
			token = *key // fallback to virtual key
		}
		fmt.Printf("Discovering models from %s ...\n", *url)
		allowedProviders := make(map[string]bool)
		for _, p := range strings.Split(*providersFlag, ",") {
			allowedProviders[strings.TrimSpace(p)] = true
		}
		discovered, err := fetchModels(*url, token, allowedProviders)
		if err != nil {
			log.Fatalf("model discovery failed: %v (use -discover=false or -models to override)", err)
		}
		fmt.Printf("Discovered %d text-only models\n", len(discovered))

		if len(discovered) == 0 {
			log.Fatal("no text-only models found")
		}

		models = pickRandomSubset(discovered, *pickCount)
		fmt.Printf("Randomly picked %d models for this run\n", len(models))
	} else {
		log.Fatal("no models specified: use -discover or -models")
	}

	cfg := simulator.Config{
		URL:          *url,
		Key:          *key,
		Users:        *users,
		Duration:     dur,
		ConvMin:      cMin,
		ConvMax:      cMax,
		Streaming:    *streaming,
		MaxTokensMin: *maxTokensMin,
		MaxTokensMax: *maxTokensMax,
		Jitter:       *jitter,
		Models:       models,
	}

	sim := simulator.New(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()

	// Handle SIGINT for early shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	fmt.Printf("Traffic Simulator\n")
	fmt.Printf("=================\n")
	fmt.Printf("  URL:            %s\n", *url)
	fmt.Printf("  Users:          %d\n", *users)
	fmt.Printf("  Duration:       %s\n", dur)
	fmt.Printf("  Conv range:     %s - %s\n", cMin, cMax)
	fmt.Printf("  Streaming:      %v\n", *streaming)
	fmt.Printf("  Max tokens:     %d - %d\n", *maxTokensMin, *maxTokensMax)
	fmt.Printf("  Jitter:         %v\n", *jitter)
	fmt.Printf("  Providers:      %s\n", *providersFlag)
	fmt.Printf("  Models:         %d\n", len(models))
	fmt.Printf("=================\n\n")

	// Launch user goroutines
	for i := range *users {
		go sim.RunUser(ctx, i+1)
	}

	// Status ticker
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			printFinalStats(sim)
			return
		case <-ticker.C:
			printStatus(sim)
		}
	}
}

func printStatus(sim *simulator.Simulator) {
	reqs, errs, models := sim.Stats.Snapshot()
	dead := sim.DeadCount()

	fmt.Printf("[%s] requests=%d errors=%d dead=%d | top models: ",
		time.Now().Format("15:04:05"), reqs, errs, dead)

	type kv struct {
		key string
		n   int
	}
	var sorted []kv
	for k, v := range models {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].n > sorted[j].n })

	limit := 5
	if len(sorted) < limit {
		limit = len(sorted)
	}
	for i := range limit {
		if i > 0 {
			fmt.Print(", ")
		}
		fmt.Printf("%s(%d)", sorted[i].key, sorted[i].n)
	}
	fmt.Println()
}

func printFinalStats(sim *simulator.Simulator) {
	reqs, errs, models := sim.Stats.Snapshot()
	dead := sim.DeadCount()

	fmt.Printf("\nFinal Stats\n")
	fmt.Printf("===========\n")
	fmt.Printf("  Total requests: %d\n", reqs)
	fmt.Printf("  Total errors:   %d\n", errs)
	fmt.Printf("  Dead models:    %d\n", dead)

	type kv struct {
		key string
		n   int
	}
	var sorted []kv
	for k, v := range models {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].n > sorted[j].n })

	fmt.Printf("  Models used:\n")
	for _, s := range sorted {
		fmt.Printf("    %-45s %d\n", s.key, s.n)
	}
	fmt.Println()
}
