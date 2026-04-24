package api

import (
	"context"
	"encoding/json"
	"net/http"
	"runtime"
	"runtime/metrics"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/user/llm-proxy/internal/util"
)

// Server start time — used for uptime calculation.
var startedAt = time.Now()

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
	App     AppStats           `json:"app"`
	DB      DBStats            `json:"db"`
	Docker  util.AggregatedDockerStats `json:"docker"`
}

type AppStats struct {
	HeapAllocMB      float64 `json:"heap_alloc_mb"`
	SysMemoryMB      float64 `json:"sys_memory_mb"`
	Goroutines       int     `json:"goroutines"`
	GCCycles         uint64  `json:"gc_cycles"`
	MemoryCurrent    int64   `json:"memory_current_bytes"`
	MemoryLimit      int64   `json:"memory_limit_bytes"`
	InContainer      bool    `json:"in_container"`
	UptimeSeconds    int64   `json:"uptime_seconds"`
	CpuPercent       float64 `json:"cpu_percent"`
	TotalRequests    int64   `json:"total_requests"`
	NetRxBytesSec    float64 `json:"net_rx_bytes_sec"`
	NetTxBytesSec    float64 `json:"net_tx_bytes_sec"`
	DiskReadBytesSec float64 `json:"disk_read_bytes_sec"`
	DiskWriteBytesSec float64 `json:"disk_write_bytes_sec"`
	Procs            int     `json:"procs"`
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

	memCurrent, memLimit, inContainer := util.ReadCgroupMemory()
	cpuPercent := util.ReadCgroupCPU()
	netRxPerSec, netTxPerSec := util.ReadNetworkStats()
	diskReadPerSec, diskWritePerSec := util.ReadCgroupDiskIO()
	procs := util.ReadCgroupProcs()

	var totalRequests int64
	err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_logs`).Scan(&totalRequests)
	if err != nil {
		totalRequests = 0
	}

	stats.App = AppStats{
		HeapAllocMB:      float64(heapAlloc) / 1024 / 1024,
		SysMemoryMB:      float64(sysMemory) / 1024 / 1024,
		Goroutines:       runtime.NumGoroutine(),
		GCCycles:         gcCycles,
		MemoryCurrent:    memCurrent,
		MemoryLimit:      memLimit,
		InContainer:      inContainer,
		UptimeSeconds:    int64(time.Since(startedAt).Seconds()),
		CpuPercent:       cpuPercent,
		TotalRequests:    totalRequests,
		NetRxBytesSec:    netRxPerSec,
		NetTxBytesSec:    netTxPerSec,
		DiskReadBytesSec: diskReadPerSec,
		DiskWriteBytesSec: diskWritePerSec,
		Procs:            procs,
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

	stats.Docker = util.CollectDockerStats(util.DetectComposeProject())

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
