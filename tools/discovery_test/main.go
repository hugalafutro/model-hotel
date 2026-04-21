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
    ModelID string        `json:"model_id"`
    Status  int           `json:"status"`
    TimeMs  int64         `json:"time_ms"`
    Err     string        `json:"error,omitempty"`
}

func main() {
    if len(os.Args) < 3 {
        fmt.Println("usage: discovery_test <base_url> <provider_id> [concurrency]")
        os.Exit(2)
    }
    base := os.Args[1]
    providerID := os.Args[2]
    concurrency := 2
    if len(os.Args) >= 4 {
        fmt.Sscanf(os.Args[3], "%d", &concurrency)
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
        go func(i int) {
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
            resp.Body.Close()
            results = append(results, res)
        }(i)
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
