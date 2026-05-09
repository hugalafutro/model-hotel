// Package main provides a discovery test tool for benchmarking provider discovery.
package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type Result struct {
	ModelID string `json:"model_id"`
	Status  int    `json:"status"`
	TimeMs  int64  `json:"time_ms"`
	Err     string `json:"error,omitempty"`
}

//nolint:revive // CLI tool
func parseArgs() (string, string, int, error) {
	if len(os.Args) < 3 {
		return "", "", 0, fmt.Errorf("usage: discovery_test <base_url> <provider_id> [concurrency]")
	}
	base := os.Args[1]
	providerID := os.Args[2]
	concurrency := 2
	if len(os.Args) >= 4 {
		_, err := fmt.Sscanf(os.Args[3], "%d", &concurrency)
		if err != nil {
			return "", "", 0, fmt.Errorf("invalid concurrency: %w", err)
		}
	}
	return base, providerID, concurrency, nil
}

func main() {
	base, providerID, concurrency, err := parseArgs()
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}
	// Auth placeholder; in real tests you may pass an API key header
	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("%s/api/providers/%s/discover", base, providerID)

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	results := make([]Result, 0, concurrency*5)

	for i := 0; i < concurrency*5; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			start := time.Now()
			// no auth header in this helper; you may add if needed
			req, _ := http.NewRequest("POST", url, bytes.NewBuffer(nil))
			resp, err := client.Do(req)
			res := Result{ModelID: providerID, TimeMs: time.Since(start).Milliseconds()}
			if err != nil {
				res.Err = err.Error()
				results = append(results, res)
				return
			}
			res.Status = resp.StatusCode
			_ = resp.Body.Close()
			results = append(results, res)
		}()
	}
	wg.Wait()
	// print results
	byStatus := map[int]int{}
	total := 0
	for _, r := range results {
		total++
		byStatus[r.Status]++
		fmt.Printf("%s status=%d time=%dms err=%v\n", r.ModelID, r.Status, r.TimeMs, r.Err)
	}
	fmt.Printf("Total=%d, statuses=%v\n", total, byStatus)
}
