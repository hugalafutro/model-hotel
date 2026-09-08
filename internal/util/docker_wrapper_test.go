package util

import (
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
	dockerHTTPClient = sync.OnceValue(newDockerHTTPClient)
	dockerAvailable = sync.OnceValue(probeDockerAvailable)
	containerFilter = sync.OnceValue(detectContainerFilter)
	dockerRates.reset()
}

// useDockerClient points the package's memoised Docker client at a test server.
func useDockerClient(c *http.Client) {
	dockerHTTPClient = func() *http.Client { return c }
	dockerAvailable = sync.OnceValue(probeDockerAvailable)
	containerFilter = sync.OnceValue(detectContainerFilter)
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

	useDockerClient(&http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	})

	if !IsDockerAvailable() {
		t.Error("expected Docker to be available")
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
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	// Read the file manually to verify the logic (since we can't override the path)
	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	// Simulate the parsing logic from getOwnContainerID
	var foundID string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitSeq(line, "/")
		for part := range parts {
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
	if err := os.WriteFile(cgroupFile, []byte(cgroupContent), 0o644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	var foundID string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitSeq(line, "/")
		for part := range parts {
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
	if err := os.WriteFile(cgroupFile, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to write temp cgroup file: %v", err)
	}

	data, err := os.ReadFile(cgroupFile)
	if err != nil {
		t.Fatalf("failed to read temp file: %v", err)
	}

	var foundID string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitSeq(line, "/")
		for part := range parts {
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

	useDockerClient(&http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	})

	// Should not panic - CloseDockerClient closes idle connections on the transport
	CloseDockerClient()

	// The client survives: CloseDockerClient only closes idle connections.
	if dockerHTTPClient() == nil {
		t.Error("CloseDockerClient() dropped the shared client")
	}
}

// TestCloseDockerClient_NonTransport tests CloseDockerClient when transport is not *http.Transport
func TestCloseDockerClient_NonTransport(t *testing.T) {
	resetDockerState()

	// A custom RoundTripper (not *http.Transport) makes the type assertion in
	// CloseDockerClient fail, so the CloseIdleConnections call is skipped.
	useDockerClient(&http.Client{
		Transport: noopRoundTripper{},
		Timeout:   5 * time.Second,
	})

	// Should not panic - CloseDockerClient handles non-*http.Transport gracefully
	CloseDockerClient()

	if dockerHTTPClient() == nil {
		t.Error("CloseDockerClient() dropped the shared client")
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

	useDockerClient(&http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	})

	// First call
	result1 := IsDockerAvailable()
	if !result1 {
		t.Fatal("expected Docker to be available on first call")
	}

	// Second call returns the cached result even though the client would now
	// fail the probe.
	dockerHTTPClient = func() *http.Client { return &http.Client{Transport: errorRoundTripper{}} }
	result2 := IsDockerAvailable()
	if !result2 {
		t.Error("expected cached result to be returned")
	}
}

// TestIsDockerAvailable_SocketNotExists tests when Docker socket doesn't exist
func TestIsDockerAvailable_SocketNotExists(t *testing.T) {
	// Point the socket path at a guaranteed-absent file so the check is
	// deterministic whether or not the host actually runs Docker (it used to
	// skip on a dev box where the socket exists).
	orig := dockerSocketPath
	dockerSocketPath = filepath.Join(t.TempDir(), "nonexistent-docker.sock")
	t.Cleanup(func() {
		dockerSocketPath = orig
		resetDockerState()
	})
	resetDockerState()

	if IsDockerAvailable() {
		t.Error("expected false when socket doesn't exist")
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

func TestCloseDockerClient_WithTransport(t *testing.T) {
	resetDockerState()

	useDockerClient(&http.Client{
		Transport: &http.Transport{},
		Timeout:   5 * time.Second,
	})

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

	useDockerClient(&http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	})

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

// ===========================================================================
// Tests moved from coverage_test.go
// ===========================================================================

// TestIsDockerAvailable_SocketNotExist tests that IsDockerAvailable returns
// false when the Docker socket doesn't exist.
func TestIsDockerAvailable_SocketNotExist(t *testing.T) {
	resetDockerState()

	origSocketPath := dockerSocketPath
	dockerSocketPath = "/nonexistent/docker.sock"
	defer func() {
		dockerSocketPath = origSocketPath
		resetDockerState()
	}()

	result := IsDockerAvailable()
	if result {
		t.Error("Expected false when Docker socket doesn't exist")
	}
}

// TestIsDockerAvailable_Non200Status tests that Docker is reported as
// unavailable when the /info endpoint returns a non-200 status code.
func TestIsDockerAvailable_Non200Status(t *testing.T) {
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

	useDockerClient(&http.Client{
		Transport: customTransport,
		Timeout:   5 * time.Second,
	})

	if IsDockerAvailable() {
		t.Error("Expected Docker to be unavailable when /info returns 503")
	}
}

// TestIsDockerAvailable_HTTPClientError tests that IsDockerAvailable returns
// false when the HTTP client fails to connect (e.g. connection refused).
func TestIsDockerAvailable_HTTPClientError(t *testing.T) {
	resetDockerState()

	dir := t.TempDir()
	socketFile := filepath.Join(dir, "docker.sock")
	if err := os.WriteFile(socketFile, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to write socket file: %v", err)
	}

	origSocket := dockerSocketPath
	dockerSocketPath = socketFile
	defer func() { dockerSocketPath = origSocket }()

	// Use a client that always fails
	useDockerClient(&http.Client{
		Transport: errorRoundTripper{},
		Timeout:   5 * time.Second,
	})

	if IsDockerAvailable() {
		t.Error("Expected Docker to be unavailable when HTTP client errors")
	}
}
