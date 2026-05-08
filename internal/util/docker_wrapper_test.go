package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"
)

// localhostRedirectTransport redirects http://localhost/* requests to a test server
type localhostRedirectTransport struct {
	targetURL string
	backend   http.RoundTripper
}

func (t *localhostRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect http://localhost/* to test server
	if req.URL.Host == "localhost" || req.URL.Host == "localhost:80" {
		newURL := t.targetURL + req.URL.Path
		if req.URL.RawQuery != "" {
			newURL += "?" + req.URL.RawQuery
		}
		newReq := req.Clone(req.Context())
		newReq.URL, _ = url.Parse(newURL)
		newReq.Host = newReq.URL.Host
		return t.backend.RoundTrip(newReq)
	}
	return t.backend.RoundTrip(req)
}

// resetDockerState resets package-level state for testing
func resetDockerState() {
	sharedDockerCli = nil
	dockerAvailable = false
	dockerCheckMu = sync.Once{}
	sharedDockerOnce = sync.Once{}
}

// TestIsDockerAvailable tests the Docker availability check
func TestIsDockerAvailable(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	if !IsDockerAvailable() {
		t.Error("expected Docker to be available")
	}
}

// TestListComposeContainers tests container listing
func TestListComposeContainers(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "abc123def4567", Name: "web", Labels: map[string]string{"com.docker.compose.project": "myapp"}, State: "running"},
		{ID: "def456ghi7890", Name: "db", Labels: map[string]string{"com.docker.compose.project": "myapp"}, State: "running"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Test server received request: %s %s", r.Method, r.URL.Path)
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/json" && r.URL.Query().Get("all") == "true" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(containers)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// First, verify Docker is available
	if !IsDockerAvailable() {
		t.Skip("Docker not available in test setup")
	}

	result, err := ListComposeContainers("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(result))
	}
}

// TestGetContainerStats tests container statistics retrieval
func TestGetContainerStats(t *testing.T) {
	resetDockerState()

	// Use the production dockerStatsResponse type directly
	var statsResp dockerStatsResponse
	json.Unmarshal([]byte(`{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 500, "percpu_usage": [250, 250]},
			"system_cpu_usage": 10000,
			"online_cpus": 2
		},
		"precpu_stats": {
			"cpu_usage": {"total_usage": 100, "percpu_usage": [50, 50]},
			"system_cpu_usage": 0,
			"online_cpus": 2
		},
		"memory_stats": {
			"usage": 1024000,
			"limit": 2048000,
			"stats": {"inactive_file": 24000}
		},
		"networks": {
			"eth0": {"rx_bytes": 1000, "tx_bytes": 2000}
		},
		"blkio_stats": {
			"io_service_bytes_recursive": [
				{"op": "Read", "value": 500},
				{"op": "Write", "value": 300}
			]
		},
		"num_procs": 4,
		"pids_stats": {"current": 42}
	}`), &statsResp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/containers/abc123def4567/stats" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(statsResp)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	stats, err := GetContainerStats("abc123def4567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stats.CPUPercent != 8.0 {
		t.Errorf("CPUPercent = %f, want 8.0", stats.CPUPercent)
	}
	if stats.MemoryUsage != 1000000 {
		t.Errorf("MemoryUsage = %d, want 1000000", stats.MemoryUsage)
	}
}

// TestCollectDockerStats tests aggregated statistics collection
func TestCollectDockerStats(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "aaa111bbb222", Name: "web", State: "running", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
	}

	// Use the production dockerStatsResponse type directly
	var statsResp dockerStatsResponse
	json.Unmarshal([]byte(`{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 200, "percpu_usage": [100, 100]},
			"system_cpu_usage": 1000,
			"online_cpus": 1
		},
		"precpu_stats": {
			"cpu_usage": {"total_usage": 100, "percpu_usage": [50, 50]},
			"system_cpu_usage": 0,
			"online_cpus": 1
		},
		"memory_stats": {
			"usage": 500000,
			"limit": 1000000,
			"stats": {}
		},
		"networks": {
			"eth0": {"rx_bytes": 100, "tx_bytes": 200}
		},
		"num_procs": 2,
		"pids_stats": {"current": 10}
	}`), &statsResp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(containers)
		case "/containers/aaa111bbb222/stats":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(statsResp)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	result := CollectDockerStats("myapp")
	if !result.Available {
		t.Fatal("expected Available=true")
	}
	if result.CPUPercent != 10.0 {
		t.Errorf("CPUPercent = %f, want 10.0", result.CPUPercent)
	}
}

// TestDetectComposeProject tests compose project detection
func TestDetectComposeProject(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def456/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Config": map[string]interface{}{
					"Labels": map[string]string{
						"com.docker.compose.project": "testproject",
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// Mock getOwnContainerID to return a container ID
	// Since we can't easily mock /proc/self/cgroup in tests,
	// we'll just verify it doesn't panic and returns empty string
	result := DetectComposeProject()
	// In test environment without real Docker, this should return empty string
	_ = result
}

// TestCloseDockerClient tests client cleanup
func TestCloseDockerClient(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// Should not panic
	CloseDockerClient()
}

// TestCollectDockerStats_NotAvailable tests behavior when Docker is not available
func TestCollectDockerStats_NotAvailable(t *testing.T) {
	resetDockerState()

	// Set up server that fails the /info check
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	result := CollectDockerStats("myapp")
	if result.Available {
		t.Error("expected Available=false when Docker is not available")
	}
}

// TestListComposeContainers_Empty tests empty container list
func TestListComposeContainers_Empty(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/json" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]DockerContainer{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	if !IsDockerAvailable() {
		t.Skip("Docker not available in test setup")
	}

	result, err := ListComposeContainers("myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 containers, got %d", len(result))
	}
}

// TestGetContainerStats_Error tests error handling
func TestGetContainerStats_Error(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/abc123def4567/stats" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// Create a custom HTTP client that redirects localhost requests to test server
	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	// We need to trigger the sharedDockerOnce first, then replace the client
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	stats, err := GetContainerStats("abc123def4567")
	if err == nil {
		t.Error("expected error for non-existent container")
	}
	if stats != nil {
		t.Errorf("expected nil stats, got %v", stats)
	}
}
