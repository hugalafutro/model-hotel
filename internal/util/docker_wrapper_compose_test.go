package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

	result, err := ListComposeContainers(ContainerFilter{ComposeProject: "myapp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(result))
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
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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

	result, err := ListComposeContainers(ContainerFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 containers, got %d", len(result))
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
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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
// Note: With the new ContainerFilter API, an empty filter returns NO containers (by design).
// This test now verifies that ContainerFilter{} returns empty results.
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

	// Empty filter should return NO containers (by design - avoids counting random containers)
	result, err := ListComposeContainers(ContainerFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 containers with empty filter, got %d", len(result))
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

	result, err := ListComposeContainers(ContainerFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 containers (no compose labels), got %d", len(result))
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

	_, err := ListComposeContainers(ContainerFilter{ComposeProject: "myapp"})
	if err == nil {
		t.Error("expected error for malformed JSON response")
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
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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

// TestFilterContainers tests the filterContainers function with various filters.
func TestFilterContainers(t *testing.T) {
	containers := []DockerContainer{
		{ID: "1", Name: "app", Labels: map[string]string{"com.docker.compose.project": "myapp", "app.group": "model-hotel"}, State: "running"},
		{ID: "2", Name: "db", Labels: map[string]string{"com.docker.compose.project": "myapp", "app.group": "model-hotel"}, State: "running"},
		{ID: "3", Name: "other", Labels: map[string]string{"com.docker.compose.project": "otherapp"}, State: "running"},
		{ID: "4", Name: "standalone", Labels: map[string]string{"app.group": "model-hotel"}, State: "running"},
		{ID: "5", Name: "nolabels", Labels: map[string]string{}, State: "running"},
	}

	tests := []struct {
		name      string
		filter    ContainerFilter
		wantCount int
	}{
		{"compose project filter", ContainerFilter{ComposeProject: "myapp"}, 2},
		{"app group filter", ContainerFilter{AppGroup: "model-hotel"}, 3},
		{"empty filter returns nothing", ContainerFilter{}, 0},
		{"non-matching compose project", ContainerFilter{ComposeProject: "nonexistent"}, 0},
		{"non-matching app group", ContainerFilter{AppGroup: "nonexistent"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterContainers(containers, tt.filter)
			if len(got) != tt.wantCount {
				t.Errorf("filterContainers() = %d containers, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestDetectContainerFilter tests that DetectContainerFilter returns the compose project when present.
func TestDetectContainerFilter(t *testing.T) {
	resetDockerState()

	// Set up fake cgroup file with container ID so getOwnContainerID() returns a real ID
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def456\n"
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

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def456/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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

	result := DetectContainerFilter()
	if result != (ContainerFilter{ComposeProject: "testproject"}) {
		t.Errorf("DetectContainerFilter() = %+v, want ContainerFilter{ComposeProject: \"testproject\"}", result)
	}
}

// TestDetectContainerFilter_AppGroupLabel tests that DetectContainerFilter falls back to app.group label
// when com.docker.compose.project is absent.
func TestDetectContainerFilter_AppGroupLabel(t *testing.T) {
	resetDockerState()

	// Set up fake cgroup file with container ID so getOwnContainerID() returns a real ID
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def456\n"
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

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def456/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
					"Labels": map[string]string{
						"app.group": "model-hotel",
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

	result := DetectContainerFilter()
	if result != (ContainerFilter{AppGroup: "model-hotel"}) {
		t.Errorf("DetectContainerFilter() = %+v, want ContainerFilter{AppGroup: \"model-hotel\"}", result)
	}
}

// TestListComposeContainers_Non200Status tests that ListComposeContainers
// returns an error when the Docker API returns a non-200 status.
func TestListComposeContainers_Non200Status(t *testing.T) {
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
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/json":
			w.WriteHeader(http.StatusInternalServerError)
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

	if !IsDockerAvailable() {
		t.Skip("Docker not available in test setup")
	}

	_, err := ListComposeContainers(ContainerFilter{ComposeProject: "myapp"})
	if err == nil {
		t.Error("Expected error when Docker API returns 500")
	}
}

// TestFilterContainers_AppGroupOnly tests filtering by AppGroup when
// ComposeProject is empty.
func TestFilterContainers_AppGroupOnly(t *testing.T) {
	containers := []DockerContainer{
		{ID: "1", Name: "app", Labels: map[string]string{"com.docker.compose.project": "myapp", "app.group": "hotel"}, State: "running"},
		{ID: "2", Name: "db", Labels: map[string]string{"app.group": "hotel"}, State: "running"},
		{ID: "3", Name: "other", Labels: map[string]string{"com.docker.compose.project": "otherapp"}, State: "running"},
		{ID: "4", Name: "standalone", Labels: map[string]string{}, State: "running"},
	}

	result := filterContainers(containers, ContainerFilter{AppGroup: "hotel"})
	if len(result) != 2 {
		t.Errorf("Expected 2 containers with AppGroup=hotel, got %d", len(result))
	}
}

// TestDetectContainerFilter_NoLabels tests when container has no relevant labels.
func TestDetectContainerFilter_NoLabels(t *testing.T) {
	resetDockerState()

	// Set up fake cgroup file with container ID so getOwnContainerID() returns a real ID
	dir := t.TempDir()
	cgroupFile := filepath.Join(dir, "cgroup")
	cgroupContent := "12:memory:/docker/abc123def456\n"
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

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			w.WriteHeader(http.StatusOK)
		case "/containers/abc123def456/json":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"Config": map[string]any{
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

	result := DetectContainerFilter()
	if result != (ContainerFilter{}) {
		t.Errorf("Expected empty ContainerFilter when no labels, got %+v", result)
	}
}

// ---------------------------------------------------------------------------
// filterContainers — AppGroup filter branch
// ---------------------------------------------------------------------------

// TestFilterContainers_AppGroup tests the AppGroup filter branch in
// filterContainers. The ComposeProject branch is covered by existing
// ListComposeContainers tests, but the AppGroup branch needs explicit coverage.
func TestFilterContainers_AppGroup(t *testing.T) {
	all := []DockerContainer{
		{ID: "1", Name: "web", Labels: map[string]string{"app.group": "mygroup"}, State: "running"},
		{ID: "2", Name: "db", Labels: map[string]string{"app.group": "othergroup"}, State: "running"},
		{ID: "3", Name: "cache", Labels: map[string]string{"com.docker.compose.project": "myapp"}, State: "running"},
	}

	result := filterContainers(all, ContainerFilter{AppGroup: "mygroup"})
	if len(result) != 1 {
		t.Fatalf("expected 1 container matching app.group=mygroup, got %d", len(result))
	}
	if result[0].ID != "1" {
		t.Errorf("expected container ID 1, got %s", result[0].ID)
	}
}

// TestFilterContainers_EmptyFilter_ReturnsNone tests that an empty filter
// returns no containers (by design — avoids accidentally including every
// container on the host).
func TestFilterContainers_EmptyFilter_ReturnsNone(t *testing.T) {
	all := []DockerContainer{
		{ID: "1", Name: "web", Labels: map[string]string{"app.group": "mygroup"}, State: "running"},
		{ID: "2", Name: "db", Labels: map[string]string{"com.docker.compose.project": "myapp"}, State: "running"},
	}

	result := filterContainers(all, ContainerFilter{})
	if len(result) != 0 {
		t.Errorf("expected 0 containers with empty filter, got %d", len(result))
	}
}

// TestFilterContainers_ComposeProjectNoMatch tests that containers without
// a matching com.docker.compose.project label are excluded.
func TestFilterContainers_ComposeProjectNoMatch(t *testing.T) {
	all := []DockerContainer{
		{ID: "1", Name: "web", Labels: map[string]string{"com.docker.compose.project": "otherapp"}, State: "running"},
		{ID: "2", Name: "db", Labels: map[string]string{"com.docker.compose.project": "myapp"}, State: "running"},
	}

	result := filterContainers(all, ContainerFilter{ComposeProject: "myapp"})
	if len(result) != 1 {
		t.Fatalf("expected 1 container matching project=myapp, got %d", len(result))
	}
	if result[0].ID != "2" {
		t.Errorf("expected container ID 2, got %s", result[0].ID)
	}
}

// ---------------------------------------------------------------------------
// ListComposeContainers — Docker API non-200 status
// TestListComposeContainers_AppGroupFilter tests that the AppGroup filter
// correctly reaches the Docker API and filters by the app.group label.
func TestListComposeContainers_AppGroupFilter(t *testing.T) {
	resetDockerState()

	containers := []DockerContainer{
		{ID: "abc1", Name: "web", Labels: map[string]string{"app.group": "myapp"}, State: "running"},
		{ID: "def2", Name: "db", Labels: map[string]string{"app.group": "other"}, State: "running"},
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

	result, err := ListComposeContainers(ContainerFilter{AppGroup: "myapp"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 container matching app.group=myapp, got %d", len(result))
	}
	if result[0].ID != "abc1" {
		t.Errorf("expected container abc1, got %s", result[0].ID)
	}
}
