package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"runtime/metrics"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// Server start time — used for uptime calculation.
var startedAt = time.Now()

// dockerStatsCollector is a function type matching util.CollectDockerStatsWithFilter.
type dockerStatsCollector func(filter util.ContainerFilter) util.AggregatedDockerStats

// SystemHandler provides system health and stats API endpoints.
type SystemHandler struct {
	pool                 *pgxpool.Pool
	settings             fleetSettings
	dockerStatsCollector dockerStatsCollector
}

// NewSystemHandler creates a new system handler. settings is read to surface HA
// fleet membership on the payload; it may be nil (fleet state is then omitted).
func NewSystemHandler(pool *pgxpool.Pool, settings fleetSettings) *SystemHandler {
	return &SystemHandler{
		pool:                 pool,
		settings:             settings,
		dockerStatsCollector: util.CollectDockerStatsWithFilter,
	}
}

// SetDockerStatsCollector overrides the Docker stats collector (for testing).
func (h *SystemHandler) SetDockerStatsCollector(fn dockerStatsCollector) {
	h.dockerStatsCollector = fn
}

// resetSystemCache clears the package-level system stats cache (for testing).
func resetSystemCache() {
	cachedSystemMu.Lock()
	cachedSystem = nil
	cachedSystemTime = time.Time{}
	cachedSystemSince = ""
	cachedSystemMu.Unlock()
}

// Register mounts system API routes.
func (h *SystemHandler) Register(r chi.Router) {
	r.Route("/system", func(r chi.Router) {
		r.Get("/", h.GetSystem)
	})
}

// SystemStats contains system-wide health metrics.
type SystemStats struct {
	App    AppStats                   `json:"app"`
	DB     DBStats                    `json:"db"`
	Docker util.AggregatedDockerStats `json:"docker"`
	// Fleet is this instance's HA fleet membership, or nil/omitted for a
	// standalone instance Front Desk has never contacted (see fleet.go).
	Fleet *FleetStatus `json:"fleet,omitempty"`
}

// AppStats contains application-level metrics (memory, CPU, network, disk).
type AppStats struct {
	HeapAllocMB       float64 `json:"heap_alloc_mb"`
	SysMemoryMB       float64 `json:"sys_memory_mb"`
	Goroutines        int     `json:"goroutines"`
	GCCycles          uint64  `json:"gc_cycles"`
	MemoryCurrent     int64   `json:"memory_current_bytes"`
	MemoryLimit       int64   `json:"memory_limit_bytes"`
	InContainer       bool    `json:"in_container"`
	UptimeSeconds     int64   `json:"uptime_seconds"`
	CPUPercent        float64 `json:"cpu_percent"`
	RequestsToday     int64   `json:"requests_today"`
	NetRxBytesSec     float64 `json:"net_rx_bytes_sec"`
	NetTxBytesSec     float64 `json:"net_tx_bytes_sec"`
	DiskReadBytesSec  float64 `json:"disk_read_bytes_sec"`
	DiskWriteBytesSec float64 `json:"disk_write_bytes_sec"`
	Procs             int     `json:"procs"`
}

// DBStats contains PostgreSQL database metrics.
type DBStats struct {
	SizeMB        float64 `json:"size_mb"`
	Connections   int     `json:"connections"`
	CacheHitRatio float64 `json:"cache_hit_ratio"`
	TxPerSec      float64 `json:"tx_per_sec"`
	DeadTuples    int64   `json:"dead_tuples"`
	LockWaits     int     `json:"lock_waits"`
}

var (
	cachedSystem      *SystemStats
	cachedSystemTime  time.Time
	cachedSystemSince string
	cachedSystemMu    sync.Mutex

	// For tx/sec delta calculation.
	prevTxCount int64
	prevTxTime  time.Time
	prevTxMu    sync.Mutex
)

const systemCacheTTL = 3 * time.Second

// GetSystem returns system health metrics (app, database, Docker).
func (h *SystemHandler) GetSystem(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")

	cachedSystemMu.Lock()
	if cachedSystem != nil && cachedSystemSince == since && time.Since(cachedSystemTime) < systemCacheTTL {
		result := *cachedSystem
		cachedSystemMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			respondError(w, "failed to encode response", err, http.StatusInternalServerError)
		}
		return
	}
	cachedSystemMu.Unlock()

	stats, err := h.collect(r.Context(), since)
	if err != nil {
		respondError(w, "failed to collect system stats", err, http.StatusInternalServerError)
		return
	}

	cachedSystemMu.Lock()
	cachedSystem = stats
	cachedSystemTime = time.Now()
	cachedSystemSince = since
	cachedSystemMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

func (h *SystemHandler) collect(ctx context.Context, sinceParam string) (*SystemStats, error) {
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

	var requestsToday int64

	since := time.Now().Truncate(24 * time.Hour) // fallback: UTC midnight
	if sinceParam != "" {
		parsed, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			return nil, fmt.Errorf("invalid since parameter: %w", err)
		}
		since = parsed
	}

	err := h.pool.QueryRow(ctx, `SELECT COUNT(*) FROM request_logs WHERE created_at >= $1`, since).Scan(&requestsToday)
	if err != nil {
		debuglog.Error("system: query failed", "query", "requestsToday", "error", err)
		requestsToday = 0
	}

	stats.App = AppStats{
		HeapAllocMB:       float64(heapAlloc) / 1024 / 1024,
		SysMemoryMB:       float64(sysMemory) / 1024 / 1024,
		Goroutines:        runtime.NumGoroutine(),
		GCCycles:          gcCycles,
		MemoryCurrent:     memCurrent,
		MemoryLimit:       memLimit,
		InContainer:       inContainer,
		UptimeSeconds:     int64(time.Since(startedAt).Seconds()),
		CPUPercent:        cpuPercent,
		RequestsToday:     requestsToday,
		NetRxBytesSec:     netRxPerSec,
		NetTxBytesSec:     netTxPerSec,
		DiskReadBytesSec:  diskReadPerSec,
		DiskWriteBytesSec: diskWritePerSec,
		Procs:             procs,
	}

	var dbSize int64
	err = h.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&dbSize)
	if err != nil {
		debuglog.Error("system: query failed", "query", "dbSize", "error", err)
		dbSize = 0
	}

	var connCount int
	err = h.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity`).Scan(&connCount)
	if err != nil {
		debuglog.Error("system: query failed", "query", "connCount", "error", err)
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
		debuglog.Error("system: query failed", "query", "cacheHitRatio", "error", err)
		cacheHitRatio = 0
	}

	// Transactions per second (delta from previous collection).
	// Note: the 3-second response cache means the rate is a rolling avg
	// across cache intervals rather than truly instantaneous.
	var txPerSec float64
	var totalTx int64
	err = h.pool.QueryRow(ctx,
		"SELECT xact_commit + xact_rollback FROM pg_stat_database WHERE datname = current_database()",
	).Scan(&totalTx)
	if err != nil {
		debuglog.Error("system: query failed", "query", "totalTx", "error", err)
		totalTx = 0
	}

	prevTxMu.Lock()
	if !prevTxTime.IsZero() {
		elapsed := time.Since(prevTxTime).Seconds()
		if elapsed > 0 {
			txPerSec = float64(totalTx-prevTxCount) / elapsed
			if txPerSec < 0 {
				txPerSec = 0
			}
		}
	}
	prevTxCount = totalTx
	prevTxTime = time.Now()
	prevTxMu.Unlock()

	// Dead tuples across all user tables.
	var deadTuples int64
	err = h.pool.QueryRow(ctx,
		"SELECT COALESCE(sum(n_dead_tup), 0) FROM pg_stat_user_tables",
	).Scan(&deadTuples)
	if err != nil {
		debuglog.Error("system: query failed", "query", "deadTuples", "error", err)
		deadTuples = 0
	}

	// Lock waits (granted=false).
	var lockWaits int
	err = h.pool.QueryRow(ctx,
		"SELECT count(*) FROM pg_locks WHERE NOT granted",
	).Scan(&lockWaits)
	if err != nil {
		debuglog.Error("system: query failed", "query", "lockWaits", "error", err)
		lockWaits = 0
	}

	stats.DB = DBStats{
		SizeMB:        float64(dbSize) / 1024 / 1024,
		Connections:   connCount,
		CacheHitRatio: cacheHitRatio,
		TxPerSec:      math.Round(txPerSec*10) / 10,
		DeadTuples:    deadTuples,
		LockWaits:     lockWaits,
	}

	stats.Docker = h.dockerStatsCollector(util.DetectContainerFilter())

	// HA fleet membership (nil/omitted for a standalone instance). Cheap reads
	// off the settings repo; the whole payload is response-cached for 3s.
	if h.settings != nil {
		stats.Fleet = computeFleetStatus(ctx, h.settings, time.Now())
	}

	return stats, nil
}

func getInt64(s metrics.Sample) int64 {
	switch s.Value.Kind() {
	case metrics.KindUint64:
		//nolint:gosec // value is memory size, always fits in int64 range
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
