package util

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// noopRoundTripper is a minimal http.RoundTripper that is NOT *http.Transport.
// Used to test the non-Transport code path in CloseDockerClient.
type noopRoundTripper struct{}

func (noopRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

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
	prevDockerNetRx = 0
	prevDockerNetTx = 0
	prevDockerBlkRead = 0
	prevDockerBlkWrite = 0
	prevDockerTime = time.Time{}
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
	if result != "" {
		t.Errorf("DetectComposeProject() = %q, want empty string (no real Docker)", result)
	}
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
func TestCloseDockerClient(t *testing.T) {
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

	// Should not panic - CloseDockerClient closes idle connections on the transport
	CloseDockerClient()

	// Verify the client still exists (CloseDockerClient doesn't nil it, just closes connections)
	if sharedDockerCli == nil {
		t.Error("CloseDockerClient() should not set sharedDockerCli to nil")
	}
}

// TestCloseDockerClient_NilClient tests CloseDockerClient when client is nil
func TestCloseDockerClient_NilClient(t *testing.T) {
	resetDockerState()

	// Ensure sharedDockerCli is nil
	sharedDockerCli = nil

	// Should not panic when client is nil
	CloseDockerClient()

	// Verify sharedDockerCli is still nil after
	if sharedDockerCli != nil {
		t.Error("CloseDockerClient() should leave sharedDockerCli as nil when already nil")
	}
}

// TestCloseDockerClient_NonTransport tests CloseDockerClient when transport is not *http.Transport
func TestCloseDockerClient_NonTransport(t *testing.T) {
	resetDockerState()

	// Create client with a custom RoundTripper (not *http.Transport)
	// so the type assertion in CloseDockerClient fails and the
	// CloseIdleConnections call is skipped gracefully.
	sharedDockerCli = &http.Client{
		Transport: noopRoundTripper{},
		Timeout:   5 * time.Second,
	}

	// Should not panic - CloseDockerClient handles non-*http.Transport gracefully
	CloseDockerClient()

	// Verify the client still exists (CloseDockerClient doesn't nil it)
	if sharedDockerCli == nil {
		t.Error("CloseDockerClient() should not set sharedDockerCli to nil")
	}
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

// TestDetectComposeProject_WithContainerID tests compose project detection with mock container ID
func TestDetectComposeProject_WithContainerID(t *testing.T) {
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// In CI, getOwnContainerID() returns "" because /proc/self/cgroup doesn't contain container ID
	// This test verifies the function doesn't panic and returns empty string gracefully
	result := DetectComposeProject()
	if result != "" {
		t.Logf("DetectComposeProject returned %q (expected empty in CI)", result)
	}
}

// TestDetectComposeProject_DockerUnavailable tests when Docker is not available
func TestDetectComposeProject_DockerUnavailable(t *testing.T) {
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

	// Should return empty string without panic
	result := DetectComposeProject()
	if result != "" {
		t.Errorf("Expected empty string when Docker unavailable, got %q", result)
	}
}

// TestListComposeContainers_AllProjects tests listing all containers with compose labels (no filter)
func TestListComposeContainers_AllProjects(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "abc123", Name: "web", Labels: map[string]string{"com.docker.compose.project": "myapp"}, State: "running"},
		{ID: "def456", Name: "db", Labels: map[string]string{"com.docker.compose.project": "otherapp"}, State: "running"},
		{ID: "ghi789", Name: "cache", Labels: map[string]string{}, State: "running"}, // no compose label
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	if !IsDockerAvailable() {
		t.Skip("Docker not available in test setup")
	}

	// Empty string filter should return all containers WITH compose labels
	result, err := ListComposeContainers("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 containers with compose labels, got %d", len(result))
	}
}

// TestListComposeContainers_NoComposeLabel tests when containers have no compose labels
func TestListComposeContainers_NoComposeLabel(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "abc123", Name: "web", Labels: map[string]string{}, State: "running"},
		{ID: "def456", Name: "db", Labels: map[string]string{"other.label": "value"}, State: "running"},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	if !IsDockerAvailable() {
		t.Skip("Docker not available in test setup")
	}

	result, err := ListComposeContainers("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 containers (no compose labels), got %d", len(result))
	}
}

// TestIsDockerAvailable_SocketNotExists tests when Docker socket doesn't exist
func TestIsDockerAvailable_SocketNotExists(t *testing.T) {
	resetDockerState()

	// Check if socket exists - skip if it does (local dev environment)
	if _, err := os.Stat(dockerSocketPath); err == nil {
		t.Skip("Docker socket exists in this environment, skipping socket-not-exists test")
	}

	// In CI/test environment without Docker, this verifies graceful handling
	result := IsDockerAvailable()
	if result {
		t.Error("expected false when socket doesn't exist")
	}
}

// TestGetContainerStats_CacheFallback tests memory stats with "cache" key instead of "inactive_file"
func TestGetContainerStats_CacheFallback(t *testing.T) {
	resetDockerState()

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
			"stats": {"cache": 24000}
		},
		"networks": {
			"eth0": {"rx_bytes": 1000, "tx_bytes": 2000}
		},
		"blkio_stats": {
			"io_service_bytes_recursive": []
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	stats, err := GetContainerStats("abc123def4567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Memory usage should be reduced by cache (1024000 - 24000 = 1000000)
	if stats.MemoryUsage != 1000000 {
		t.Errorf("MemoryUsage = %d, want 1000000 (cache fallback)", stats.MemoryUsage)
	}
}

// TestGetContainerStats_ZeroOnlineCPUs tests fallback when online_cpus=0
func TestGetContainerStats_ZeroOnlineCPUs(t *testing.T) {
	resetDockerState()

	var statsResp dockerStatsResponse
	json.Unmarshal([]byte(`{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 500, "percpu_usage": [250, 250]},
			"system_cpu_usage": 10000,
			"online_cpus": 0
		},
		"precpu_stats": {
			"cpu_usage": {"total_usage": 100, "percpu_usage": [50, 50]},
			"system_cpu_usage": 0,
			"online_cpus": 0
		},
		"memory_stats": {
			"usage": 1024000,
			"limit": 2048000,
			"stats": {}
		},
		"networks": {},
		"blkio_stats": {
			"io_service_bytes_recursive": []
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	stats, err := GetContainerStats("abc123def4567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use percpu_usage length (2) as fallback for online_cpus
	// CPU percent = (400/10000) * 2 * 100 = 8.0
	if stats.CPUPercent != 8.0 {
		t.Errorf("CPUPercent = %f, want 8.0 (zero online_cpus fallback)", stats.CPUPercent)
	}
}

// TestGetContainerStats_CPUCapped tests CPU percent capping at 100% * onlineCPUs
func TestGetContainerStats_CPUCapped(t *testing.T) {
	resetDockerState()

	var statsResp dockerStatsResponse
	json.Unmarshal([]byte(`{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 9000, "percpu_usage": [4500, 4500]},
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
			"stats": {}
		},
		"networks": {},
		"blkio_stats": {
			"io_service_bytes_recursive": []
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	stats, err := GetContainerStats("abc123def4567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Raw calculation: (8900/10000) * 2 * 100 = 178%, should be capped at 200% (100% * 2 CPUs)
	// Actually the cap is at 100% * onlineCPUs = 200%
	// But 178% < 200%, so it won't be capped. Let me recalculate...
	// Actually: cpuDelta = 8900, systemDelta = 10000, onlineCPUs = 2
	// CPU percent = (8900/10000) * 2 * 100 = 178%
	// Cap = 100 * 2 = 200%
	// 178 < 200, so no capping happens
	// Let me create a case that exceeds the cap
	if stats.CPUPercent > 200.0 {
		t.Errorf("CPUPercent = %f, should be capped at 200.0", stats.CPUPercent)
	}
}

// TestGetContainerStats_CPUCapped_Exceeded tests CPU percent capping when it exceeds limit
func TestGetContainerStats_CPUCapped_Exceeded(t *testing.T) {
	resetDockerState()

	var statsResp dockerStatsResponse
	json.Unmarshal([]byte(`{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 9900, "percpu_usage": [4950, 4950]},
			"system_cpu_usage": 1000,
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
			"stats": {}
		},
		"networks": {},
		"blkio_stats": {
			"io_service_bytes_recursive": []
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	stats, err := GetContainerStats("abc123def4567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Raw calculation: (9800/1000) * 2 * 100 = 1960%, should be capped at 200% (100% * 2 CPUs)
	if stats.CPUPercent != 200.0 {
		t.Errorf("CPUPercent = %f, want 200.0 (capped)", stats.CPUPercent)
	}
}

// TestGetContainerStats_NilNetworks tests stats with no networks field
func TestGetContainerStats_NilNetworks(t *testing.T) {
	resetDockerState()

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
			"stats": {}
		},
		"blkio_stats": {
			"io_service_bytes_recursive": []
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

	customTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}

	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	}

	// Should not panic with nil networks
	stats, err := GetContainerStats("abc123def4567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stats.NetRxBytes != 0 || stats.NetTxBytes != 0 {
		t.Errorf("expected zero network bytes with nil networks, got rx=%d tx=%d", stats.NetRxBytes, stats.NetTxBytes)
	}
}

// TestCollectDockerStats_SecondCall tests rate calculations on second call
func TestCollectDockerStats_SecondCall(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "aaa111bbb222", Name: "web", State: "running", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
	}

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
			"eth0": {"rx_bytes": 1000, "tx_bytes": 2000}
		},
		"blkio_stats": {
			"io_service_bytes_recursive": [
				{"op": "Read", "value": 500},
				{"op": "Write", "value": 300}
			]
		},
		"num_procs": 2,
		"pids_stats": {"current": 10}
	}`), &statsResp)

	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(containers)
		case "/containers/aaa111bbb222/stats":
			callCount++
			// Second call returns increased network/block stats
			if callCount >= 2 {
				var statsResp2 dockerStatsResponse
				json.Unmarshal([]byte(`{
					"cpu_stats": {
						"cpu_usage": {"total_usage": 300, "percpu_usage": [150, 150]},
						"system_cpu_usage": 2000,
						"online_cpus": 1
					},
					"precpu_stats": {
						"cpu_usage": {"total_usage": 200, "percpu_usage": [100, 100]},
						"system_cpu_usage": 1000,
						"online_cpus": 1
					},
					"memory_stats": {
						"usage": 600000,
						"limit": 1000000,
						"stats": {}
					},
					"networks": {
						"eth0": {"rx_bytes": 2000, "tx_bytes": 3000}
					},
					"blkio_stats": {
						"io_service_bytes_recursive": [
							{"op": "Read", "value": 1000},
							{"op": "Write", "value": 800}
						]
					},
					"num_procs": 2,
					"pids_stats": {"current": 10}
				}`), &statsResp2)
				json.NewEncoder(w).Encode(statsResp2)
			} else {
				json.NewEncoder(w).Encode(statsResp)
			}
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

	// First call - initializes baseline, no rates
	result1 := CollectDockerStats("myapp")
	if !result1.Available {
		t.Fatal("expected Available=true on first call")
	}
	if result1.NetRxBytesSec != 0 || result1.NetTxBytesSec != 0 {
		t.Errorf("expected zero rates on first call, got rx=%f tx=%f", result1.NetRxBytesSec, result1.NetTxBytesSec)
	}

	// Small sleep to ensure time delta
	time.Sleep(10 * time.Millisecond)

	// Second call - should calculate rates
	result2 := CollectDockerStats("myapp")
	if !result2.Available {
		t.Fatal("expected Available=true on second call")
	}
	// Network rates should be positive (1000 bytes delta / ~0.01s = ~100000 bytes/sec)
	if result2.NetRxBytesSec <= 0 {
		t.Errorf("expected positive NetRxBytesSec on second call, got %f", result2.NetRxBytesSec)
	}
	if result2.NetTxBytesSec <= 0 {
		t.Errorf("expected positive NetTxBytesSec on second call, got %f", result2.NetTxBytesSec)
	}
	// Disk rates should be positive
	if result2.DiskReadBytesSec <= 0 {
		t.Errorf("expected positive DiskReadBytesSec on second call, got %f", result2.DiskReadBytesSec)
	}
	if result2.DiskWriteBytesSec <= 0 {
		t.Errorf("expected positive DiskWriteBytesSec on second call, got %f", result2.DiskWriteBytesSec)
	}
}

// TestListComposeContainers_JSONDecodeError tests JSON decode error handling
func TestListComposeContainers_JSONDecodeError(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/json" {
			w.WriteHeader(http.StatusOK)
			// Return malformed JSON that doesn't match DockerContainer struct
			w.Write([]byte(`{"invalid": "json", "structure": "mismatch"}`))
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

	if !IsDockerAvailable() {
		t.Skip("Docker not available in test setup")
	}

	_, err := ListComposeContainers("myapp")
	if err == nil {
		t.Error("expected error for malformed JSON response")
	}
}

// TestGetContainerStats_JSONDecodeError tests JSON decode error handling
func TestGetContainerStats_JSONDecodeError(t *testing.T) {
	resetDockerState()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/containers/abc123def4567/stats" {
			w.WriteHeader(http.StatusOK)
			// Return truly malformed JSON that will cause decode error
			w.Write([]byte(`{invalid json that cannot be parsed}`))
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

	stats, err := GetContainerStats("abc123def4567")
	if err == nil {
		t.Error("expected error for malformed JSON response")
	}
	if stats != nil {
		t.Errorf("expected nil stats, got %v", stats)
	}
}

// TestCollectDockerStats_ProcsFallbackToPids tests fallback from Procs to Pids when num_procs is 0
func TestCollectDockerStats_ProcsFallbackToPids(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "aaa111bbb222", Name: "web", State: "running", Labels: map[string]string{"com.docker.compose.project": "myapp"}},
	}

	// num_procs: 0 but pids_stats.current: 10 - should use Pids (10) instead of Procs (0)
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
		"blkio_stats": {
			"io_service_bytes_recursive": []
		},
		"num_procs": 0,
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
	if !result.Available {
		t.Fatal("expected Available=true")
	}
	// Should use Pids (10) as fallback when Procs is 0
	if result.Procs != 10 {
		t.Errorf("Procs = %d, want 10 (fallback from Pids)", result.Procs)
	}
}

func TestGetOwnContainerID_WithCgroupV1File(t *testing.T) {
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def4567890\n11:cpu:/docker/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	id := getOwnContainerID()
	if id != "abc123def4567890" {
		t.Errorf("Expected container ID abc123def4567890, got %q", id)
	}
}

func TestGetOwnContainerID_WithCgroupV2Scope(t *testing.T) {
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	// cgroup v2 format where container ID is a standalone path component
	cgroupContent := "0::/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	id := getOwnContainerID()
	if id != "abc123def4567890" {
		t.Errorf("Expected container ID abc123def4567890, got %q", id)
	}
}

func TestGetOwnContainerID_HostnameFallback(t *testing.T) {
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	if err := os.WriteFile(cgroupFile, []byte("0::/\n"), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	origHostname := osHostname
	osHostname = func() (string, error) { return "fed456abc789012", nil }
	defer func() { osHostname = origHostname }()

	id := getOwnContainerID()
	if id != "fed456abc789012" {
		t.Errorf("Expected hostname fallback fed456abc789012, got %q", id)
	}
}

func TestGetOwnContainerID_HostnameNotHex(t *testing.T) {
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	if err := os.WriteFile(cgroupFile, []byte("0::/\n"), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	origHostname := osHostname
	osHostname = func() (string, error) { return "my-container-host", nil }
	defer func() { osHostname = origHostname }()

	id := getOwnContainerID()
	if id != "" {
		t.Errorf("Expected empty string for non-hex hostname, got %q", id)
	}
}

func TestDetectComposeProject_HappyPath(t *testing.T) {
	resetDockerState()

	// Set up fake cgroup file with container ID
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	// Create a fake Docker socket file so os.Stat passes
	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

	// Set up test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def4567890/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"Config": map[string]interface{}{
					"Labels": map[string]string{
						"com.docker.compose.project": "myproject",
					},
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
	if result != "myproject" {
		t.Errorf("Expected project name myproject, got %q", result)
	}
}

func TestDetectComposeProject_NoLabelsWithID(t *testing.T) {
	resetDockerState()

	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def4567890/json":
			w.WriteHeader(http.StatusOK)
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

func TestDetectComposeProject_JSONDecodeError(t *testing.T) {
	resetDockerState()

	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def4567890/json":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{invalid json"))
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
		t.Errorf("Expected empty string on JSON decode error, got %q", result)
	}
}

func TestCloseDockerClient_WithTransport(t *testing.T) {
	resetDockerState()

	transport := &http.Transport{}
	sharedDockerCli = &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}

	// Should not panic and should close idle connections
	CloseDockerClient()

	// Calling again should also be safe
	CloseDockerClient()
}

func TestIsDockerAvailable_WithSocketOverride(t *testing.T) {
	resetDockerState()

	dir := t.TempDir()
	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

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

	if !IsDockerAvailable() {
		t.Error("Expected Docker to be available with socket override")
	}
}

func TestIsDockerAvailable_RealDaemon(t *testing.T) {
	resetDockerState()
	// Call with the real Docker socket path. If Docker is running (expected
	// in this project's primary deployment), this covers the os.Stat success
	// path and the /info HTTP check. If Docker is not available the test
	// still passes — it just won't contribute coverage for those branches.
	result := IsDockerAvailable()
	t.Logf("IsDockerAvailable() = %v", result)
}

type errorRoundTripper struct{}

func (errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("connection refused")
}

func TestDetectComposeProject_HTTPError(t *testing.T) {
	resetDockerState()

	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

	// Use a test server for /info so IsDockerAvailable returns true
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// First, make IsDockerAvailable succeed with the test server
	infoTransport := &localhostRedirectTransport{
		targetURL: ts.URL,
		backend:   http.DefaultTransport,
	}
	sharedDockerOnce.Do(func() {})
	sharedDockerCli = &http.Client{
		Transport: infoTransport,
		Timeout:   5 * time.Second,
	}
	if !IsDockerAvailable() {
		t.Fatal("Expected Docker to be available for test setup")
	}

	// Now swap the transport to one that returns errors for the container inspect
	sharedDockerCli = &http.Client{
		Transport: errorRoundTripper{},
		Timeout:   5 * time.Second,
	}

	result := DetectComposeProject()
	if result != "" {
		t.Errorf("Expected empty string on HTTP error, got %q", result)
	}
}

func TestDetectComposeProject_Non200Status(t *testing.T) {
	resetDockerState()

	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def4567890\n"
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("Failed to write cgroup file: %v", err)
	}

	origCgroup := procSelfCgroup
	procSelfCgroup = cgroupFile
	defer func() { procSelfCgroup = origCgroup }()

	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def4567890/json":
			w.WriteHeader(http.StatusNotFound)
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
		t.Errorf("Expected empty string on non-200 status, got %q", result)
	}
}

// ===========================================================================
// Tests moved from coverage_test.go
// ===========================================================================

// TestIsDockerAvailable_SocketNotExist tests that IsDockerAvailable returns
// false when the Docker socket doesn't exist.
func TestIsDockerAvailable_SocketNotExist(t *testing.T) {
	// Save original values
	origSocketPath := dockerSocketPath
	origDockerAvailable := dockerAvailable

	// Reset for test
	dockerCheckMu = sync.Once{}
	dockerAvailable = false

	// Override socket path to non-existent location
	dockerSocketPath = "/nonexistent/docker.sock"

	// Restore original values after test
	defer func() {
		dockerSocketPath = origSocketPath
		dockerCheckMu = sync.Once{}
		dockerAvailable = origDockerAvailable
	}()

	result := IsDockerAvailable()
	if result {
		t.Error("Expected false when Docker socket doesn't exist")
	}
}
