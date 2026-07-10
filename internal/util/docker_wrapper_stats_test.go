package util

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
	result := CollectDockerStatsWithFilter(ContainerFilter{ComposeProject: "myapp"})
	if !result.Available {
		t.Fatal("expected Available=true")
	}
	if result.ContainerCount != 2 {
		t.Errorf("expected ContainerCount=2, got %d", result.ContainerCount)
	}
	// Should have stats from the one successful container
	if result.CPUPercent == 0 {
		t.Error("expected non-zero CPUPercent from successful container")
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
