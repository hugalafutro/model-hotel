// Package util provides helper functions for system utilities and Docker integration.
package util

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

var dockerSocketPath = "/var/run/docker.sock"

var (
	dockerAvailable  bool
	dockerCheckMu    sync.Once
	sharedDockerOnce sync.Once
	sharedDockerCli  *http.Client
)

// Overridable in tests for container ID detection.
var (
	procSelfCgroup = "/proc/self/cgroup"
	osHostname     = os.Hostname
)

// IsDockerAvailable checks if Docker socket is accessible and responsive.
func IsDockerAvailable() bool {
	dockerCheckMu.Do(func() {
		if _, err := os.Stat(dockerSocketPath); err != nil {
			dockerAvailable = false
			return
		}
		client := dockerHTTPClient()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/info", http.NoBody)
		resp, err := client.Do(req)
		if err != nil {
			debuglog.Info("docker: failed to connect to Docker API", "error", err)
			dockerAvailable = false
			return
		}
		_ = resp.Body.Close()
		dockerAvailable = resp.StatusCode == 200

	})
	return dockerAvailable
}

// dockerHTTPClient returns a singleton HTTP client for Docker socket
// communication.  Previously every caller constructed a fresh
// http.Transport, each of which spawns persistent readLoop/writeLoop
// goroutines per connection that only die after IdleConnTimeout (90 s
// default).  Reusing a single Transport avoids that unbounded goroutine
// growth while still pooling connections efficiently.
func dockerHTTPClient() *http.Client {
	sharedDockerOnce.Do(func() {
		sharedDockerCli = &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", dockerSocketPath)
				},
				IdleConnTimeout: 30 * time.Second,
			},
			Timeout: 5 * time.Second,
		}
	})
	return sharedDockerCli
}

// CloseDockerClient closes idle connections on the shared Docker HTTP
// client. Call during server shutdown so Transport goroutines are released.
func CloseDockerClient() {
	if sharedDockerCli != nil {
		if t, ok := sharedDockerCli.Transport.(*http.Transport); ok {
			t.CloseIdleConnections()
		}
	}
}

// DockerContainer represents a Docker container with basic metadata.
type DockerContainer struct {
	ID     string `json:"Id"`
	Name   string
	Labels map[string]string
	State  string
}

// ContainerStats holds resource usage statistics for a container.
type ContainerStats struct {
	Name        string
	CPUPercent  float64
	MemoryUsage int64
	MemoryLimit int64
	NetRxBytes  int64
	NetTxBytes  int64
	BlockRead   int64
	BlockWrite  int64
	Procs       int
	Pids        int
}

type dockerStatsResponse struct {
	Read     string `json:"read"`
	PreRead  string `json:"preread"`
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  int64   `json:"total_usage"`
			PerCPUUsage []int64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage int64 `json:"system_cpu_usage"`
		OnlineCPUs     int   `json:"online_cpus"`
		ThrottlingData struct {
			Periods          int64 `json:"periods"`
			ThrottledPeriods int64 `json:"throttled_periods"`
			ThrottledTime    int64 `json:"throttled_time"`
		} `json:"throttling_data"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage  int64   `json:"total_usage"`
			PerCPUUsage []int64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage int64 `json:"system_cpu_usage"`
		OnlineCPUs     int   `json:"online_cpus"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage int64            `json:"usage"`
		Limit int64            `json:"limit"`
		Stats map[string]int64 `json:"stats"`
		Cache int64            `json:"cache"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes int64 `json:"rx_bytes"`
		TxBytes int64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServicesRecursive []struct {
			Op    string `json:"op"`
			Major int64  `json:"major"`
			Minor int64  `json:"minor"`
			Value int64  `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	NumProcs  int `json:"num_procs"`
	PidsStats struct {
		Current int `json:"current"`
	} `json:"pids_stats"`
}

// ContainerFilter specifies which containers to include when listing.
// Both fields are optional; when set, only containers matching the
// respective label are returned.
type ContainerFilter struct {
	ComposeProject string // matches com.docker.compose.project
	AppGroup       string // matches app.group
}

// ListComposeContainers lists containers filtered by the given criteria.
func ListComposeContainers(filter ContainerFilter) ([]DockerContainer, error) {
	client := dockerHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "http://localhost/containers/json?all=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		debuglog.Info("docker: failed to list containers", "error", err)
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("docker API returned %d", resp.StatusCode)
	}

	var all []DockerContainer
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, err
	}

	return filterContainers(all, filter), nil
}

// filterContainers applies the ContainerFilter to a list of containers.
// When both ComposeProject and AppGroup are empty, no containers are
// returned (avoids accidentally including every container on the host).
func filterContainers(all []DockerContainer, filter ContainerFilter) []DockerContainer {
	var result []DockerContainer
	for _, c := range all {
		if filter.ComposeProject != "" {
			if project, ok := c.Labels["com.docker.compose.project"]; ok && project == filter.ComposeProject {
				result = append(result, c)
			}
			continue
		}
		if filter.AppGroup != "" {
			if group, ok := c.Labels["app.group"]; ok && group == filter.AppGroup {
				result = append(result, c)
			}
			continue
		}
	}
	return result
}

// GetContainerStats retrieves CPU and memory stats for a Docker container.
func GetContainerStats(containerID string) (*ContainerStats, error) {
	client := dockerHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://localhost/containers/%s/stats?stream=false", containerID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Info("docker: stats API returned non-200", "status", resp.StatusCode, "container", containerID[:12], "body", string(body[:min(len(body), 200)]))
		return nil, fmt.Errorf("docker stats API returned %d: %s", resp.StatusCode, string(body))
	}

	var raw dockerStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	stats := &ContainerStats{}

	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage - raw.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(raw.CPUStats.SystemCPUUsage - raw.PreCPUStats.SystemCPUUsage)
	onlineCPUs := raw.CPUStats.OnlineCPUs
	if onlineCPUs == 0 {
		onlineCPUs = len(raw.CPUStats.CPUUsage.PerCPUUsage)
	}
	if onlineCPUs > 0 && systemDelta > 0 && cpuDelta > 0 {
		stats.CPUPercent = (cpuDelta / systemDelta) * float64(onlineCPUs) * 100.0
		if stats.CPUPercent > 100.0*float64(onlineCPUs) {
			stats.CPUPercent = 100.0 * float64(onlineCPUs)
		}
	}

	stats.MemoryUsage = raw.MemoryStats.Usage
	stats.MemoryLimit = raw.MemoryStats.Limit
	if cache, ok := raw.MemoryStats.Stats["inactive_file"]; ok {
		stats.MemoryUsage -= cache
	} else if cache, ok := raw.MemoryStats.Stats["cache"]; ok {
		stats.MemoryUsage -= cache
	}

	for _, nw := range raw.Networks {
		stats.NetRxBytes += nw.RxBytes
		stats.NetTxBytes += nw.TxBytes
	}

	for _, io := range raw.BlkioStats.IOServicesRecursive {
		switch strings.ToLower(io.Op) {
		case "read":
			stats.BlockRead += io.Value
		case "write":
			stats.BlockWrite += io.Value
		}
	}

	stats.Procs = raw.NumProcs
	stats.Pids = raw.PidsStats.Current

	return stats, nil
}

// AggregatedDockerStats holds combined metrics from all containers in a project.
type AggregatedDockerStats struct {
	Available         bool    `json:"available"`
	CPUPercent        float64 `json:"cpu_percent"`
	MemoryUsage       int64   `json:"memory_usage_bytes"`
	MemoryLimit       int64   `json:"memory_limit_bytes"`
	NetRxBytesSec     float64 `json:"net_rx_bytes_sec"`
	NetTxBytesSec     float64 `json:"net_tx_bytes_sec"`
	DiskReadBytesSec  float64 `json:"disk_read_bytes_sec"`
	DiskWriteBytesSec float64 `json:"disk_write_bytes_sec"`
	Procs             int     `json:"procs"`
	ContainerCount    int     `json:"container_count"`
}

var (
	prevDockerNetRx    int64
	prevDockerNetTx    int64
	prevDockerBlkRead  int64
	prevDockerBlkWrite int64
	prevDockerTime     time.Time
	prevDockerMu       sync.Mutex
)

// CollectDockerStats aggregates resource usage across containers matching
// the given filter. It accepts a composeProject string for backward
// compatibility; prefer CollectDockerStatsWithFilter for new callers.
//
// Note: passing an empty string now returns no containers (changed from the
// previous behaviour of returning all containers with a compose label).
func CollectDockerStats(composeProject string) AggregatedDockerStats {
	filter := ContainerFilter{ComposeProject: composeProject}
	return CollectDockerStatsWithFilter(filter)
}

// CollectDockerStatsWithFilter aggregates resource usage across containers
// matching the provided ContainerFilter.
func CollectDockerStatsWithFilter(filter ContainerFilter) AggregatedDockerStats {
	result := AggregatedDockerStats{}

	if !IsDockerAvailable() {
		return result
	}

	containers, err := ListComposeContainers(filter)
	if err != nil || len(containers) == 0 {
		return result
	}

	result.Available = true
	result.ContainerCount = len(containers)

	var totalCPU float64
	var totalMemUsage int64
	var maxMemLimit int64
	var totalNetRx, totalNetTx int64
	var totalBlkRead, totalBlkWrite int64
	var totalProcs int

	for _, c := range containers {
		if c.State != "running" {
			continue
		}
		stats, err := GetContainerStats(c.ID)
		if err != nil {
			continue
		}
		totalCPU += stats.CPUPercent
		totalMemUsage += stats.MemoryUsage
		if stats.MemoryLimit > maxMemLimit {
			maxMemLimit = stats.MemoryLimit
		}
		totalNetRx += stats.NetRxBytes
		totalNetTx += stats.NetTxBytes
		totalBlkRead += stats.BlockRead
		totalBlkWrite += stats.BlockWrite
		if stats.Procs > 0 {
			totalProcs += stats.Procs
		} else {
			totalProcs += stats.Pids
		}
	}

	result.CPUPercent = totalCPU
	result.MemoryUsage = totalMemUsage
	result.MemoryLimit = maxMemLimit
	result.Procs = totalProcs

	prevDockerMu.Lock()
	defer prevDockerMu.Unlock()

	if prevDockerTime.IsZero() {
		prevDockerTime = time.Now()
		prevDockerNetRx = totalNetRx
		prevDockerNetTx = totalNetTx
		prevDockerBlkRead = totalBlkRead
		prevDockerBlkWrite = totalBlkWrite
		return result
	}

	now := time.Now()
	deltaSec := now.Sub(prevDockerTime).Seconds()
	deltaRx := totalNetRx - prevDockerNetRx
	deltaTx := totalNetTx - prevDockerNetTx
	deltaBlkRead := totalBlkRead - prevDockerBlkRead
	deltaBlkWrite := totalBlkWrite - prevDockerBlkWrite

	prevDockerTime = now
	prevDockerNetRx = totalNetRx
	prevDockerNetTx = totalNetTx
	prevDockerBlkRead = totalBlkRead
	prevDockerBlkWrite = totalBlkWrite

	if deltaSec > 0 {
		if deltaRx > 0 {
			result.NetRxBytesSec = float64(deltaRx) / deltaSec
		}
		if deltaTx > 0 {
			result.NetTxBytesSec = float64(deltaTx) / deltaSec
		}
		if deltaBlkRead > 0 {
			result.DiskReadBytesSec = float64(deltaBlkRead) / deltaSec
		}
		if deltaBlkWrite > 0 {
			result.DiskWriteBytesSec = float64(deltaBlkWrite) / deltaSec
		}
	}

	return result
}

func getOwnContainerID() string {
	// Try /proc/self/cgroup (Docker usually writes the container ID here)
	data, err := os.ReadFile(procSelfCgroup)
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// cgroup v2: 0::/system.slice/docker-<id>.scope
			// cgroup v1: 12:memory:/docker/<id>
			parts := strings.Split(line, "/")
			for _, part := range parts {
				part = strings.TrimSuffix(part, ".scope")
				if len(part) >= 12 && isHex(part) {
					return part
				}
			}
		}
	}

	// In many container setups (e.g. cgroup v2 with compose), the cgroup path is
	// just "/" but the hostname is set to the container's short ID by Docker.
	if hostname, err := osHostname(); err == nil && len(hostname) >= 12 && isHex(hostname) {
		return hostname
	}

	return ""
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return s != ""
}

// DetectContainerFilter inspects the current container's labels to
// determine which other containers belong to the same deployment.
// It prefers the Docker Compose project label; when absent (e.g. when
// deployed outside of docker-compose), it falls back to the app.group
// label.
func DetectContainerFilter() ContainerFilter {
	containerID := getOwnContainerID()
	if containerID == "" || !IsDockerAvailable() {
		return ContainerFilter{}
	}

	client := dockerHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/containers/"+containerID+"/json", http.NoBody)
	resp, err := client.Do(req)
	if err != nil {
		debuglog.Info("docker: failed to inspect own container", "error", err)
		return ContainerFilter{}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return ContainerFilter{}
	}

	body, _ := io.ReadAll(resp.Body)
	var info struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if json.Unmarshal(body, &info) != nil {
		return ContainerFilter{}
	}

	if project, ok := info.Config.Labels["com.docker.compose.project"]; ok {
		return ContainerFilter{ComposeProject: project}
	}
	if group, ok := info.Config.Labels["app.group"]; ok {
		return ContainerFilter{AppGroup: group}
	}
	return ContainerFilter{}
}

// DetectComposeProject returns the Docker Compose project name for the
// current container, or an empty string if unavailable.
//
// Deprecated: use DetectContainerFilter instead for broader label support.
func DetectComposeProject() string {
	return DetectContainerFilter().ComposeProject
}
