package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hugalafutro/model-hotel/tools/traffic-sim/simulator"
)

var defaultModels = []simulator.ModelInfo{
	// Ollama Cloud (fast, small models)
	{Provider: "Ollama Cloud", ID: "gemma3:4b"},
	{Provider: "Ollama Cloud", ID: "gemma3:12b"},
	{Provider: "Ollama Cloud", ID: "gemma3:27b"},
	{Provider: "Ollama Cloud", ID: "gemma4:31b"},
	{Provider: "Ollama Cloud", ID: "glm-5.1"},
	{Provider: "Ollama Cloud", ID: "glm-5"},
	{Provider: "Ollama Cloud", ID: "glm-4.7"},
	{Provider: "Ollama Cloud", ID: "deepseek-v4-flash"},
	{Provider: "Ollama Cloud", ID: "ministral-3:3b"},
	{Provider: "Ollama Cloud", ID: "ministral-3:8b"},
	{Provider: "Ollama Cloud", ID: "qwen3-coder"},
	{Provider: "Ollama Cloud", ID: "qwen3-coder-next"},
	// NanoGPT (popular/reliable)
	{Provider: "NanoGPT", ID: "deepseek-chat"},
	{Provider: "NanoGPT", ID: "deepseek-r1"},
	{Provider: "NanoGPT", ID: "deepseek/deepseek-v4-flash"},
	{Provider: "NanoGPT", ID: "meta-llama/llama-3.1-8b-instruct"},
	{Provider: "NanoGPT", ID: "meta-llama/llama-3.2-3b-instruct"},
	{Provider: "NanoGPT", ID: "qwen/qwen3-14b"},
	{Provider: "NanoGPT", ID: "qwen/qwen3-32b"},
	{Provider: "NanoGPT", ID: "qwen/qwen3-coder"},
	{Provider: "NanoGPT", ID: "google/gemma-4-26b-a4b-it"},
	{Provider: "NanoGPT", ID: "google/gemma-4-31b-it"},
	{Provider: "NanoGPT", ID: "mistralai/mistral-tiny"},
	{Provider: "NanoGPT", ID: "mistralai/mistral-small-31-24b-instruct"},
	{Provider: "NanoGPT", ID: "zai-org/glm-5.1"},
	{Provider: "NanoGPT", ID: "zai-org/glm-5"},
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

func mustAtoi(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func main() {
	url := flag.String("url", "http://localhost:8081", "Proxy base URL")
	key := flag.String("key", "", "Virtual API key (required)")
	users := flag.Int("users", 8, "Number of concurrent simulated users")
	duration := flag.String("duration", "10m", "Total run time (e.g. 10m, 30m, 1h)")
	convMin := flag.String("conv-min", "1m", "Min conversation duration")
	convMax := flag.String("conv-max", "3m", "Max conversation duration")
	streaming := flag.Bool("streaming", true, "Use streaming responses")
	maxTokens := flag.Int("max-tokens", 150, "Max tokens per response")
	jitter := flag.Bool("jitter", true, "Random 2-8s delay between turns")
	modelsFlag := flag.String("models", "", "Comma-separated model list (overrides defaults, format: Provider/ModelID)")
	flag.Parse()

	if *key == "" {
		fmt.Fprintln(os.Stderr, "Error: -key is required")
		flag.Usage()
		os.Exit(1)
	}

	dur := parseDuration(*duration)
	cMin := parseDuration(*convMin)
	cMax := parseDuration(*convMax)

	var models []simulator.ModelInfo
	if *modelsFlag != "" {
		for _, m := range strings.Split(*modelsFlag, ",") {
			m = strings.TrimSpace(m)
			parts := strings.SplitN(m, "/", 2)
			if len(parts) != 2 {
				log.Fatalf("invalid model format %q (expected Provider/ModelID)", m)
			}
			models = append(models, simulator.ModelInfo{Provider: parts[0], ID: parts[1]})
		}
	} else {
		models = defaultModels
	}

	cfg := simulator.Config{
		URL:       *url,
		Key:       *key,
		Users:     *users,
		Duration:  dur,
		ConvMin:   cMin,
		ConvMax:   cMax,
		Streaming: *streaming,
		MaxTokens: *maxTokens,
		Jitter:    *jitter,
		Models:    models,
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
	fmt.Printf("  URL:        %s\n", *url)
	fmt.Printf("  Users:      %d\n", *users)
	fmt.Printf("  Duration:   %s\n", dur)
	fmt.Printf("  Conv range: %s - %s\n", cMin, cMax)
	fmt.Printf("  Streaming:  %v\n", *streaming)
	fmt.Printf("  Max tokens: %d\n", *maxTokens)
	fmt.Printf("  Jitter:     %v\n", *jitter)
	fmt.Printf("  Models:     %d\n", len(models))
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
