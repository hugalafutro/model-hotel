package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Server start time — used for uptime calculation.
var startedAt = time.Now()

// CPU tracking state for computing container CPU percentage from cgroup v2.
var (
	cpuPrevUsage int64
	cpuPrevTime  time.Time
	cpuPrevMu    sync.Mutex
)

type SystemHandler struct {
	pool *pgxpool.Pool
}

func NewSystemHandler(pool *pgxpool.Pool) *SystemHandler {
	return &SystemHandler{pool: pool}
}

func (h *SystemHandler) Register(r chi.Router) {
	r.Route("/system", func(r chi.Router) {
		r.Get("/", h.GetSystem)
	})
}

type SystemStats struct {
	App AppStats `json:"app"`
	DB  DBStats  `json:"db"`
}

type AppStats struct {
	HeapAllocMB   float64 `json:"heap_alloc_mb"`
	SysMemoryMB   float64 `json:"sys_memory_mb"`
	Goroutines    int     `json:"goroutines"`
	GCCycles      uint64  `json:"gc_cycles"`
	MemoryCurrent int64   `json:"memory_current_bytes"`
	MemoryLimit   int64   `json:"memory_limit_bytes"`
	InContainer   bool    `json:"in_container"`
	UptimeSeconds int64   `json:"uptime_seconds"`
	CpuPercent    float64 `json:"cpu_percent"`
	TotalRequests int64   `json:"total_requests"`
}

type DBStats struct {
	SizeMB        float64 `json:"size_mb"`
	Connections   int     `json:"connections"`
	CacheHitRatio float64 `json:"cache_hit_ratio"`
}

var (
	cachedSystem     *SystemStats
	cachedSystemTime time.Time
	cachedSystemMu   sync.Mutex
)

const systemCacheTTL = 5 * time.Second

func (h *SystemHandler) GetSystem(w http.ResponseWriter, r *http.Request) {
	cachedSystemMu.Lock()
	if cachedSystem != nil && time.Since(cachedSystemTime) < systemCacheTTL {
		result := *cachedSystem
		cachedSystemMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
		return
	}
	cachedSystemMu.Unlock()

	stats, err := h.collect(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cachedSystemMu.Lock()
	cachedSystem = stats
	cachedSystemTime = time.Now()
	cachedSystemMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (h *SystemHandler) collect(ctx context.Context) (*SystemStats, error) {
	stats := &SystemStats{}

	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/memory/classes/total:bytes"},
		{Name: "/gc/cycles/total:gc-cycles"},
	}
	metrics.Read(samples)

	heapAlloc := getInt64(samples[0])
	sysMemory := getInt64(samples[1])
	gcCycles := getUint64(samples[2])

	memCurrent, memLimit, inContainer := readCgroupMemory()
	cpuPercent := readCgroupCPU()

	var totalRequests int64
	err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_logs`).Scan(&totalRequests)
	if err != nil {
		totalRequests = 0
	}

	stats.App = AppStats{
		HeapAllocMB:   float64(heapAlloc) / 1024 / 1024,
		SysMemoryMB:   float64(sysMemory) / 1024 / 1024,
		Goroutines:    runtime.NumGoroutine(),
		GCCycles:      gcCycles,
		MemoryCurrent: memCurrent,
		MemoryLimit:   memLimit,
		InContainer:   inContainer,
		UptimeSeconds: int64(time.Since(startedAt).Seconds()),
		CpuPercent:    cpuPercent,
		TotalRequests: totalRequests,
	}

	var dbSize int64
	err = h.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&dbSize)
	if err != nil {
		dbSize = 0
	}

	var connCount int
	err = h.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity`).Scan(&connCount)
	if err != nil {
		connCount = 0
	}

	var cacheHitRatio float64
	err = h.pool.QueryRow(ctx, `
		SELECT CASE WHEN blks_hit + blks_read = 0 THEN 0
			    ELSE round(100.0 * blks_hit / (blks_hit + blks_read), 2)
			END
		FROM pg_stat_database WHERE datname = current_database()
	`).Scan(&cacheHitRatio)
	if err != nil {
		cacheHitRatio = 0
	}

	stats.DB = DBStats{
		SizeMB:        float64(dbSize) / 1024 / 1024,
		Connections:   connCount,
		CacheHitRatio: cacheHitRatio,
	}

	return stats, nil
}

func getInt64(s metrics.Sample) int64 {
	switch s.Value.Kind() {
	case metrics.KindUint64:
		return int64(s.Value.Uint64())
	case metrics.KindFloat64:
		return int64(s.Value.Float64())
	default:
		return 0
	}
}

func getUint64(s metrics.Sample) uint64 {
	if s.Value.Kind() == metrics.KindUint64 {
		return s.Value.Uint64()
	}
	return 0
}

func readCgroupMemory() (current, limit int64, inContainer bool) {
	currentBytes, err := os.ReadFile("/sys/fs/cgroup/memory.current")
	if err == nil {
		val := strings.TrimSpace(string(currentBytes))
		if v, e := parseInt(val); e == nil {
			current = v
			inContainer = true
		}
	}

	limitBytes, err := os.ReadFile("/sys/fs/cgroup/memory.max")
	if err == nil {
		val := strings.TrimSpace(string(limitBytes))
		if val == "max" {
			limit = 0
		} else if v, e := parseInt(val); e == nil {
			limit = v
		}
	}

	return current, limit, inContainer
}

// readCgroupCPU returns container CPU usage percentage from cgroup v2 cpu.stat.
// It reads the cumulative usage_usec value and computes a delta-based percentage.
// Returns -1 if cgroup CPU stats are not available (not in a container).
// First call always returns 0 since it establishes the baseline.
func readCgroupCPU() float64 {
	f, err := os.Open("/sys/fs/cgroup/cpu.stat")
	if err != nil {
		return -1
	}
	defer f.Close()

	var usageUsec int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "usage_usec ") {
			val := strings.TrimPrefix(line, "usage_usec ")
			if v, e := strconv.ParseInt(strings.TrimSpace(val), 10, 64); e == nil {
				usageUsec = v
			}
			break
		}
	}

	if usageUsec == 0 {
		return -1
	}

	cpuPrevMu.Lock()
	defer cpuPrevMu.Unlock()

	if cpuPrevTime.IsZero() {
		cpuPrevTime = time.Now()
		cpuPrevUsage = usageUsec
		return 0
	}

	now := time.Now()
	deltaTime := now.Sub(cpuPrevTime).Seconds()
	deltaUsage := usageUsec - cpuPrevUsage

	cpuPrevTime = now
	cpuPrevUsage = usageUsec

	if deltaTime <= 0 || deltaUsage < 0 {
		return 0
	}

	// CPU percent = (cpu time used / wall time) * 100
	// usage_usec is cumulative CPU microseconds across all cores.
	// On a multi-core system this can exceed 100%.
	percent := (float64(deltaUsage) / (deltaTime * 1_000_000)) * 100
	if percent < 0 {
		percent = 0
	}
	if percent > 999 {
		percent = 999
	}
	return percent
}

func parseInt(s string) (int64, error) {
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
