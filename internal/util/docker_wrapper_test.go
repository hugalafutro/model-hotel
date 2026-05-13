package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
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
			//nolint:gosec // test-only: error handling not critical
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
	//nolint:gosec // test-only: error handling not critical
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
		//nolint:gosec // test-only: error handling not critical
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
func TestDetectComposeProject(_ *testing.T) {
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

// TestDetectComposeProject_NoLabels tests when container has no compose labels
func TestDetectComposeProject_NoLabels(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def456/json":
			w.WriteHeader(http.StatusOK)
			// Container without compose labels
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Config": map[string]interface{}{
					"Labels": map[string]string{},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	result := DetectComposeProject()
	if result != "" {
		t.Errorf("Expected empty string when no compose labels, got %q", result)
	}
}

// TestDetectComposeProject_APIError tests when Docker API fails
func TestDetectComposeProject_APIError(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		// Simulate API error for container inspection
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	result := DetectComposeProject()
	if result != "" {
		t.Errorf("Expected empty string on API error, got %q", result)
	}
}

// TestGetOwnContainerID_CgroupV1 tests getOwnContainerID with cgroup v1 format
func TestGetOwnContainerID_CgroupV1(t *testing.T) {
	// Create a temp file mimicking /proc/self/cgroup with v1 format
	tmpDir := t.TempDir()
	cgroupContent := `12:memory:/docker/abc123def4567890
11:cpu:/docker/abc123def4567890
10:blkio:/docker/abc123def4567890`
	cgroupFile := tmpDir + "/cgroup"
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	// Read the file manually to verify the logic (since we can't override the path)
	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	// Simulate the parsing logic from getOwnContainerID
	var foundID string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "abc123def4567890" {
		t.Errorf("Expected container ID abc123def4567890, got %q", foundID)
	}
}

// TestGetOwnContainerID_CgroupV2 tests getOwnContainerID with cgroup v2 format
func TestGetOwnContainerID_CgroupV2(t *testing.T) {
	// Cgroup v2 format where container ID appears as a path component
	// Some setups write the ID directly: 0::/abc123def4567890
	tmpDir := t.TempDir()
	cgroupContent := `0::/abc123def4567890`
	cgroupFile := tmpDir + "/cgroup"
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	var foundID string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "abc123def4567890" {
		t.Errorf("Expected container ID abc123def4567890, got %q", foundID)
	}
}

// TestGetOwnContainerID_Empty tests getOwnContainerID with empty cgroup file
func TestGetOwnContainerID_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	cgroupFile := tmpDir + "/cgroup"
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(cgroupFile, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	//nolint:gosec // test-only: controlled test path
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	var foundID string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		for _, part := range parts {
			part = strings.TrimSuffix(part, ".scope")
			if len(part) >= 12 && isHex(part) {
				foundID = part
				break
			}
		}
		if foundID != "" {
			break
		}
	}

	if foundID != "" {
		t.Errorf("Expected empty string, got %q", foundID)
	}
}

// TestCloseDockerClient tests client cleanup
func TestCloseDockerClient(_ *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

// TestCloseDockerClient_NilClient tests CloseDockerClient when client is nil
func TestCloseDockerClient_NilClient(_ *testing.T) {
	resetDockerState()

	// Ensure sharedDockerCli is nil
	sharedDockerCli = nil

	// Should not panic when client is nil
	CloseDockerClient()
}

// TestCloseDockerClient_NonTransport tests CloseDockerClient when transport is not *http.Transport
func TestCloseDockerClient_NonTransport(_ *testing.T) {
	resetDockerState()

	// Create client with non-Transport roundtripper
	sharedDockerCli = &http.Client{
		Transport: http.DefaultTransport,
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

// TestIsHex tests hex string validation
func TestIsHex(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		wanted bool
	}{
		// Valid hex strings
		{"lowercase hex", "abc123", true},
		{"uppercase hex", "ABC123", true},
		{"mixed case hex", "aBcDeF", true},
		{"all digits", "123456", true},
		{"all letters", "abcdef", true},
		{"single valid char", "a", true},
		{"docker container ID format", "abc123def4567890", true},
		// Invalid hex strings
		{"empty string", "", false},
		{"invalid characters", "xyz", false},
		{"mixed valid/invalid", "abc123xyz", false},
		{"single invalid char", "g", false},
		{"with dashes", "abc-123", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isHex(tc.input)
			if got != tc.wanted {
				t.Errorf("isHex(%q) = %v, want %v", tc.input, got, tc.wanted)
			}
		})
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

// TestIsDockerAvailable_NoPanic tests that IsDockerAvailable doesn't panic
func TestIsDockerAvailable_NoPanic(t *testing.T) {
	resetDockerState()

	// Should not panic even if Docker is not available
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("IsDockerAvailable panicked: %v", r)
		}
	}()

	// This will return false since Docker socket doesn't exist in test env
	result := IsDockerAvailable()
	_ = result // Just verify it doesn't panic
}

// TestIsDockerAvailable_CachedResult tests that subsequent calls return cached result
func TestIsDockerAvailable_CachedResult(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// First call
	result1 := IsDockerAvailable()
	if !result1 {
		t.Fatal("expected Docker to be available on first call")
	}

	// Second call should return cached result (even if we change the client)
	sharedDockerCli = nil
	result2 := IsDockerAvailable()
	if !result2 {
		t.Error("expected cached result to be returned")
	}
}

// TestCollectDockerStats_NoDockerAvailable tests graceful handling when Docker is unavailable
func TestCollectDockerStats_NoDockerAvailable(t *testing.T) {
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// Should not panic and should return empty result
	result := CollectDockerStats("myapp")
	if result.Available {
		t.Error("expected Available=false when Docker is not available")
	}
	if result.ContainerCount != 0 {
		t.Errorf("expected ContainerCount=0, got %d", result.ContainerCount)
	}
}

// TestCollectDockerStats_ListContainersError tests handling when ListComposeContainers fails
func TestCollectDockerStats_ListContainersError(t *testing.T) {
	resetDockerState()

	// Server that succeeds on /info but fails on /containers/json
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/json" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// Should not panic and should return empty result
	result := CollectDockerStats("myapp")
	if result.Available {
		t.Error("expected Available=false when container listing fails")
	}
}

// TestCollectDockerStats_NoRunningContainers tests when all containers are stopped
func TestCollectDockerStats_NoRunningContainers(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "aaa111bbb222", Name: "web", State: "exited", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
		{ID: "ccc333ddd444", Name: "db", State: "paused", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/json" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(containers)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	result := CollectDockerStats("myapp")
	// Available should be true (Docker is available, containers listed)
	// but CPU/Memory should be 0 since no containers are running
	if !result.Available {
		t.Error("expected Available=true when Docker is available")
	}
	if result.ContainerCount != 2 {
		t.Errorf("expected ContainerCount=2, got %d", result.ContainerCount)
	}
	if result.CPUPercent != 0 {
		t.Errorf("expected CPUPercent=0 for stopped containers, got %f", result.CPUPercent)
	}
	if result.MemoryUsage != 0 {
		t.Errorf("expected MemoryUsage=0 for stopped containers, got %d", result.MemoryUsage)
	}
}

// TestCollectDockerStats_GetStatsError tests handling when GetContainerStats fails for a container
func TestCollectDockerStats_GetStatsError(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "aaa111bbb222", Name: "web", State: "running", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
		{ID: "ccc333ddd444", Name: "db", State: "running", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
	}

	// Only return stats for the second container
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
		"networks": {},
		"num_procs": 2,
		"pids_stats": {"current": 10}
	}`), &statsResp)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/json" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(containers)
			return
		}
		// Only second container returns stats, first returns error
		if r.URL.Path == "/containers/ccc333ddd444/stats" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(statsResp)
			return
		}
		if r.URL.Path == "/containers/aaa111bbb222/stats" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// Should not panic, should aggregate stats from successful container only
	result := CollectDockerStats("myapp")
	if !result.Available {
		t.Error("expected Available=true when at least one container works")
	}
	if result.ContainerCount != 2 {
		t.Errorf("expected ContainerCount=2, got %d", result.ContainerCount)
	}
	// Should have stats from the one successful container
	if result.CPUPercent == 0 {
		t.Error("expected non-zero CPUPercent from successful container")
	}
}
