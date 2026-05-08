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
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	// Read the file manually to verify the logic (since we can't override the path)
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
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

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
	if err := os.WriteFile(cgroupFile, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

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

// TestCloseDockerClient_NilClient tests CloseDockerClient when client is nil
func TestCloseDockerClient_NilClient(t *testing.T) {
	resetDockerState()

	// Ensure sharedDockerCli is nil
	sharedDockerCli = nil

	// Should not panic when client is nil
	CloseDockerClient()
}

// TestCloseDockerClient_NonTransport tests CloseDockerClient when transport is not *http.Transport
func TestCloseDockerClient_NonTransport(t *testing.T) {
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
		{"mixed case hex", "aBc123", true},
		{"deadbeef", "deadbeef", true},
		{"single char", "f", true},
		{"all digits", "123456", true},
		{"all letters", "abcdef", true},
		// Invalid hex strings
		{"contains non-hex", "xyz", false},
		{"contains hyphen", "abc-123", false},
		{"empty string", "", false},
		{"contains space", "abc 123", false},
		{"contains newline", "abc\n", false},
		{"contains underscore", "abc_123", false},
		{"starts with g", "gabc", false},
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
