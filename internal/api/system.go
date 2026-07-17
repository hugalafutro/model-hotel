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
	"golang.org/x/sync/singleflight"

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

	reqTodayMu.Lock()
	reqTodayVal = 0
	reqTodayTime = time.Time{}
	reqTodaySince = ""
	reqTodayMu.Unlock()

	prevBlksMu.Lock()
	prevBlksHit = 0
	prevBlksRead = 0
	prevBlksSeen = false
	lastCacheHitRatio = 0
	prevBlksMu.Unlock()
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
	// InstanceID is this instance's stable identity (migration 056). Front Desk
	// uses it to recognise the same instance reached under a different URL, so it
	// can refuse to add a host that is already the primary or already a member.
	// Omitted only on a pre-056 build that never generated one.
	InstanceID string `json:"instance_id,omitempty"`
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
	// CacheWindowBlocks is how many block accesses (hits + reads) the window
	// behind CacheHitRatio contained. Zero/omitted means the ratio is not backed
	// by fresh activity (first sample, counter reset, or an idle window), so
	// consumers can grey the ratio out instead of colour-coding stale history.
	CacheWindowBlocks int64   `json:"cache_window_blocks,omitempty"`
	TxPerSec          float64 `json:"tx_per_sec"`
	DeadTuples        int64   `json:"dead_tuples"`
	LockWaits         int     `json:"lock_waits"`
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

	// For cache-hit-ratio delta calculation. Lifetime pg_stat_database counters
	// are useless as an indicator once history dominates them (an idle instance
	// reads ~93% forever off its cold-start misses), so the served ratio covers
	// only the window between two collects. lastCacheHitRatio carries the
	// previous windowed value through an idle window so consumers never see the
	// ratio collapse to a hard 0 between samples.
	prevBlksHit       int64
	prevBlksRead      int64
	prevBlksSeen      bool
	lastCacheHitRatio float64
	prevBlksMu        sync.Mutex
)

const systemCacheTTL = 3 * time.Second

// systemCollectTimeout bounds a single detached collect so a wedged query or
// docker call cannot pin a background collect open indefinitely.
const systemCollectTimeout = 15 * time.Second

// systemCollectGroup coalesces concurrent cold collects for the same `since`, so
// a burst of pollers hitting an expired cache triggers one collect, not a herd.
var systemCollectGroup singleflight.Group

// requestsTodayTTL caches the requests-today COUNT apart from the 3s system
// payload. That COUNT is a live aggregate over request_logs which, on a freshly
// restarted instance whose planner stats are stale, can seq-scan the whole table
// and dominate the collect (the same heap-scan trap documented in applogs.go). A
// status widget tolerates a slightly stale count, so run the query at most once
// per this window instead of on every collect.
const requestsTodayTTL = 30 * time.Second

var (
	reqTodayVal   int64
	reqTodayTime  time.Time
	reqTodaySince string
	reqTodayMu    sync.Mutex
)

// GetSystem returns system health metrics (app, database, Docker).
func (h *SystemHandler) GetSystem(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")

	if stats, ok := cachedSystemFor(since); ok {
		writeSystemJSON(w, stats)
		return
	}

	// Cold miss. Coalesce concurrent collects for the same `since`, and run the
	// shared collect on a context detached from THIS request's cancellation. A
	// caller that gives up early (e.g. Front Desk's 4s probe timing out while a
	// freshly restarted, still-slow instance collects) must not abort the collect:
	// if it did, the 3s cache would never be written and the instance would stay
	// stuck cold on every poll. Detached, the collect still finishes and warms the
	// cache, so the next poll is served from it instantly.
	v, err, _ := systemCollectGroup.Do(since, func() (any, error) {
		// A concurrent collect may have filled the cache while callers coalesced.
		if stats, ok := cachedSystemFor(since); ok {
			return stats, nil
		}
		cctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), systemCollectTimeout)
		defer cancel()
		stats, err := h.collect(cctx, since)
		if err != nil {
			return nil, err
		}
		cachedSystemMu.Lock()
		cachedSystem = stats
		cachedSystemTime = time.Now()
		cachedSystemSince = since
		cachedSystemMu.Unlock()
		return stats, nil
	})
	if err != nil {
		respondError(w, "failed to collect system stats", err, http.StatusInternalServerError)
		return
	}
	writeSystemJSON(w, v.(*SystemStats))
}

// cachedSystemFor returns a copy of the cached payload for `since` when it is
// still within the TTL, so callers never share the cached pointer.
func cachedSystemFor(since string) (*SystemStats, bool) {
	cachedSystemMu.Lock()
	defer cachedSystemMu.Unlock()
	if cachedSystem != nil && cachedSystemSince == since && time.Since(cachedSystemTime) < systemCacheTTL {
		result := *cachedSystem
		return &result, true
	}
	return nil, false
}

func writeSystemJSON(w http.ResponseWriter, stats *SystemStats) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// requestsSince returns the number of request_logs rows at or after `since`,
// cached for requestsTodayTTL under sinceKey. A query error yields 0 and is not
// cached, so a transient failure is retried on the next collect rather than
// pinned. Keeping this COUNT off every collect is what stops a stale-planner-stats
// seq-scan on a freshly restarted primary from tipping the whole status endpoint
// past a caller's timeout.
func (h *SystemHandler) requestsSince(ctx context.Context, since time.Time, sinceKey string) int64 {
	reqTodayMu.Lock()
	if reqTodaySince == sinceKey && time.Since(reqTodayTime) < requestsTodayTTL {
		v := reqTodayVal
		reqTodayMu.Unlock()
		return v
	}
	reqTodayMu.Unlock()

	var n int64
	if err := h.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM request_logs WHERE created_at >= $1`, since).Scan(&n); err != nil {
		debuglog.Error("system: query failed", "query", "requestsToday", "error", err)
		return 0
	}

	reqTodayMu.Lock()
	reqTodayVal = n
	reqTodayTime = time.Now()
	reqTodaySince = sinceKey
	reqTodayMu.Unlock()
	return n
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

	since := time.Now().Truncate(24 * time.Hour) // fallback: UTC midnight
	if sinceParam != "" {
		parsed, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			return nil, fmt.Errorf("invalid since parameter: %w", err)
		}
		since = parsed
	}
	requestsToday := h.requestsSince(ctx, since, sinceParam)

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
	err := h.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&dbSize)
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

	// Cache hit ratio over the window since the previous collect (see
	// windowedCacheHitRatio). A query error leaves the snapshot untouched so a
	// transient failure cannot poison the next window's baseline.
	var cacheHitRatio float64
	var cacheWindowBlocks int64
	var blksHit, blksRead int64
	err = h.pool.QueryRow(ctx,
		"SELECT blks_hit, blks_read FROM pg_stat_database WHERE datname = current_database()",
	).Scan(&blksHit, &blksRead)
	if err != nil {
		debuglog.Error("system: query failed", "query", "cacheHitRatio", "error", err)
	} else {
		cacheHitRatio, cacheWindowBlocks = windowedCacheHitRatio(blksHit, blksRead)
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
		SizeMB:            float64(dbSize) / 1024 / 1024,
		Connections:       connCount,
		CacheHitRatio:     cacheHitRatio,
		CacheWindowBlocks: cacheWindowBlocks,
		TxPerSec:          math.Round(txPerSec*10) / 10,
		DeadTuples:        deadTuples,
		LockWaits:         lockWaits,
	}

	stats.Docker = h.dockerStatsCollector(util.DetectContainerFilter())

	// HA fleet membership (nil/omitted for a standalone instance). Cheap reads
	// off the settings repo; the whole payload is response-cached for 3s. A read
	// failure (e.g. the client canceled this request mid-flight) is returned as
	// an error so GetSystem responds 500 and, crucially, does NOT cache the
	// half-read payload: a canceled request can no longer poison the 3s cache and
	// report the primary as a demoted member to everyone.
	if h.settings != nil {
		fleet, err := computeFleetStatus(ctx, h.settings, time.Now())
		if err != nil {
			return stats, fmt.Errorf("compute fleet status: %w", err)
		}
		stats.Fleet = fleet
		// Stable instance identity. A read miss leaves it empty (an older instance
		// that never generated one); it is display/identity metadata, not a gate,
		// so unlike the fleet role a transient miss need not fail the whole payload.
		stats.InstanceID = h.settings.GetWithDefault(ctx, "instance_id", "")
	}

	return stats, nil
}

// windowedCacheHitRatio folds one raw pg_stat_database counter sample into the
// package snapshot and returns the buffer-cache hit ratio over the window since
// the previous sample, plus how many block accesses that window contained.
// Lifetime counters are the fallback whenever no window exists: the first
// sample after process start, and a sample taken right after Postgres restarted
// (its stats reset, so a delta turns negative). An idle window (zero accesses)
// carries the previous windowed value forward instead of reporting 0%; in every
// no-window case windowBlocks is 0 so consumers know the ratio is not backed by
// fresh activity.
func windowedCacheHitRatio(blksHit, blksRead int64) (ratio float64, windowBlocks int64) {
	lifetime := 0.0
	if total := blksHit + blksRead; total > 0 {
		lifetime = 100 * float64(blksHit) / float64(total)
	}

	prevBlksMu.Lock()
	defer prevBlksMu.Unlock()

	deltaHit, deltaRead := blksHit-prevBlksHit, blksRead-prevBlksRead
	hadBaseline := prevBlksSeen
	prevBlksHit, prevBlksRead, prevBlksSeen = blksHit, blksRead, true

	if !hadBaseline || deltaHit < 0 || deltaRead < 0 {
		lastCacheHitRatio = lifetime
		return round2(lifetime), 0
	}
	window := deltaHit + deltaRead
	if window == 0 {
		return round2(lastCacheHitRatio), 0
	}
	lastCacheHitRatio = 100 * float64(deltaHit) / float64(window)
	return round2(lastCacheHitRatio), window
}

// round2 matches the round(x, 2) the cache-hit SQL used to apply.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
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
