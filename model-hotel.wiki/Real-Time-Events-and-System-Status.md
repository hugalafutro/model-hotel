# Real-Time Events & System Status

Model Hotel provides real-time visibility into system state through two mechanisms: an SSE event bus (for toast notifications) and live system statistics (for the sidebar).

## Event Bus (SSE)

The backend publishes events to an in-memory pub/sub bus (`internal/events/bus.go`). The frontend subscribes via Server-Sent Events (SSE) at `/api/events` using the admin token.

### Architecture

The event bus is simple:

- **In-memory** — No persistence; events are lost on server restart
- **Pub/sub** — Subscribers get a buffered channel (capacity 64)
- **Non-blocking** — If a subscriber's channel is full, the event is dropped for that subscriber (with a warning logged)
- **Heartbeat** — The server sends `: heartbeat` comments every 30 seconds to keep the connection alive
- **JSON payloads** — Each event is serialized as `data: {JSON}\n\n`

### Event Types

These are the actual events published by the codebase. The event bus schema supports any `type`/`severity`/`message` combination, but only these specific event types are emitted:

| Type | Severity | When Published |
|------|----------|---------------|
| `discovery.complete` | success | Discovery completed successfully for all providers |
| `discovery.complete` | warning | Discovery partially failed (some providers OK, some errored) |
| `discovery.complete` | error | Discovery failed entirely (no providers scanned) |
| `discovery.models_disabled` | warning | Models no longer found at a provider were auto-disabled |
| `failover.sync_error` | warning | Failover group sync failed for a model after discovery |
| `tokens.error` | error | Token counting failed for a virtual key during proxying |
| `logs.stale_startup` | warning | Server restart interrupted pending requests (stale cleanup on startup) |
| `logs.stale_cleanup` | warning | Periodic stale request cleanup marked requests as interrupted |

> **Important:** There are NO events for `provider.created`, `provider.updated`, `provider.deleted`, `model.discovered`, `failover.triggered`, `rate_limit.hit`, `virtual_key.created`, `virtual_key.deleted`, `settings.changed`, or a generic `error` type. The event bus is not a general-purpose CRUD notification system — it only publishes events for the specific situations listed above.

### Failover Retries Are Not SSE Events

When a proxy request triggers a failover retry (the upstream provider returned 5xx or timed out), the retry happens transparently and is **logged to stdout** only. Failover retries are **not** pushed as SSE events — the client receives the successful response from the fallback provider without any toast notification.

### Toast Notifications

Events are displayed as toast notifications in the dashboard:

- **Configurable position** — top-left, top-center, top-right, bottom-left, bottom-center, bottom-right
- **Configurable timeout** — 1–30 seconds (default 4s)
- **Severity-based colors** — success=green, info=blue, warning=amber, error=red
- **Duplicate suppression** — Same message type won't stack multiple toasts

The frontend `EventProvider` component subscribes to the SSE stream and calls `toast(event.message, event.severity)` for each received event. It does not use the event type or metadata — only the message and severity are rendered.

### SSE Reconnection

The frontend handles SSE connection drops with exponential backoff:

- Starts at 1 second
- Doubles each failure (2s, 4s, 8s, 16s, 30s...)
- Caps at 30 seconds
- Resets to 1s on successful connection

The connection is established via `fetch()` with the admin token in the `Authorization` header (not in the query parameter). On disposal (component unmount), the `AbortController` aborts the connection.

## System Status

The sidebar displays live system statistics, **polled every 10 seconds** via a standard HTTP GET to `/api/system`. This is not an SSE push — the frontend uses `react-query` with `refetchInterval: 10000`. SSE is only for toast notifications.

### Metrics

The `/api/system` endpoint returns an object with `app`, `db`, and `docker` fields. The sidebar displays:

| Metric | Source | Description |
|--------|--------|-------------|
| API Status | HTTP health check | Whether the proxy API is responding (Online/Error) |
| Uptime | Process start time | How long the server has been running |
| CPU | cgroup / Docker aggregate | CPU usage percentage |
| Processes | cgroup / Docker | Number of running processes |
| Memory | cgroup / Docker | Memory usage vs. limit |
| Network RX/TX | cgroup / Docker | Network throughput (↓ receive / ↑ transmit) |
| Disk Read/Write | cgroup / Docker | Disk I/O throughput (↓ read / ↑ write) |
| Goroutines | Go runtime | Active goroutine count |
| Total Requests | request_logs count | Total proxied requests recorded |
| DB Size | PostgreSQL `pg_database_size` | Database size in MB/GB |
| DB Connections | PostgreSQL `pg_stat_activity` | Active database connections |
| DB Cache Hit Ratio | PostgreSQL `pg_stat_database` | Buffer cache hit percentage |

### Docker Container Stats

When the app detects it's running under Docker Compose (via `docker.sock` mount), it queries the Docker API for **aggregated resource metrics** across all compose services — CPU, memory, network, disk I/O, and process count for the entire stack (app + database).

The `AggregatedDockerStats` struct provides:

| Field | Description |
|-------|-------------|
| `cpu_percent` | Aggregate CPU across all compose containers |
| `memory_usage_bytes` | Total memory usage across containers |
| `memory_limit_bytes` | Total memory limit across containers |
| `net_rx_bytes_sec` | Aggregate network receive throughput |
| `net_tx_bytes_sec` | Aggregate network transmit throughput |
| `disk_read_bytes_sec` | Aggregate disk read throughput |
| `disk_write_bytes_sec` | Aggregate disk write throughput |
| `procs` | Total process count across containers |
| `container_count` | Number of compose containers |

> **Note:** Docker stats show **resource metrics** (CPU, memory, network, disk I/O), not container health status (healthy/unhealthy). The dashboard does not monitor Docker health checks.

When Docker is not available, the sidebar falls back to cgroup-based stats from the app container/process.

### Color Thresholds

The sidebar uses color coding to highlight issues at a glance:

| Metric | 🟢 Normal | 🟠 Warning | 🔴 Critical |
|--------|-----------|-----------|------------|
| CPU | < 75% | ≥ 75% | ≥ 90% |
| Memory | < 75% | ≥ 75% | ≥ 90% |
| Goroutines | < 300 | ≥ 300 | ≥ 1,000 |
| DB Cache Hit | ≥ 95% | < 95% | < 80% |

> **Note:** The memory thresholds are 75%/90%, matching the CPU thresholds — not the 80%/95% that was previously documented. The actual frontend function `dc(v, w, c)` uses `dc(cpuPct, 75, 90)` for CPU and `dc(memUsagePct, 75, 90)` for memory.

### Backend Caching

The `/api/system` endpoint caches its results for 3 seconds (`systemCacheTTL`) to avoid hammering cgroup/Docker APIs on rapid concurrent requests. The frontend's 10-second poll interval combined with the 3-second stale time ensures data is fresh without excessive load.