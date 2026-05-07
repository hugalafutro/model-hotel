# Provider Health

The dashboard provides tools to monitor provider health and verify that models are responding correctly.

## Model Testing

From the **Models** page, open any model's detail panel and click the **Test** button. This sends a minimal chat completion request directly to the provider to verify connectivity and model availability.

> **Note:** The Test button lives in `ModelDetailModal` on the **Models** page, not on the Providers page.

The test endpoint (`POST /api/models/{id}/test`) sends:
- `model`: the model's `model_id`
- `messages`: `[{"role": "user", "content": "Respond only with 'Hi'"}]`
- `max_tokens`: 10

With a **30-second timeout**. Returns: `success`, `duration_ms`, `response` (assistant content), `error` on failure.

The request goes directly to the provider's `/chat/completions` endpoint - it does **not** go through the model-hotel proxy (`/v1/chat/completions`). This means it validates:

- **API key decryption** - The key is decrypted using the same Argon2 + AES-256-GCM pipeline
- **Provider connectivity** - The base URL is reachable and returns a valid response
- **Model availability** - The model ID is accepted by the provider

The test reports:

- **Total duration** - End-to-end request time in milliseconds
- **Actual response** - The model's generated text (truncated)
- **Error details** - If the request fails, the full error message

> **Note:** TTFT is **not** reported by the test endpoint. Although the `TestModelResponse` struct includes a `ttft_ms` field, it is never populated in success responses. The test uses a non-streaming request, so there is no separate time-to-first-token measurement - only total `duration_ms` is meaningful.

### Masked API Keys

Provider API keys are shown in the UI with a `masked_key` field (e.g., `op***ky`). The full key is never exposed after creation - only the masked preview is available for identification purposes.

### Last Used Tracking

Each provider has a `last_used_at` timestamp that is updated on every proxy request (fire-and-forget with a 5-second timeout). This helps track which providers are actively being used and can inform deprecation or rotation decisions.

> 📸 **Screenshot needed:** Provider health indicators - showing enabled/disabled status, last used timestamps, and circuit breaker status indicators.

## Provider Quotas & Balances

For supported providers, the sidebar displays a **Quotas** panel with live account information. The data varies by provider type because each provider exposes different metrics:

### DeepSeek - Balance

- Fetches balance from the DeepSeek `/user/balance` API endpoint
- Shows remaining credit in **CNY** (total balance)
- Displayed as a pill: `$ X.XX`
- Click the pill to refresh

DeepSeek exposes account **balance** (how much credit remains), not usage quotas.

### NanoGPT - Usage Quota

- Fetches usage data from the NanoGPT API
- Shows **weekly token quota**: used vs. limit (e.g., `50K/500K`)
- Modal panel with detailed breakdown of input/output token usage
- Displayed as a pill: `used/limit`

NanoGPT exposes **quota/usage** - weekly limits on how many tokens you can consume.

### Z.AI - Quota Limits

- Fetches quota data from the Z.AI API
- Shows **remaining percentage** for two quota windows:
  - **5-hour window** - short-term rate limit
  - **Weekly window** - weekly usage limit
- Displayed as a pill: `5h% / weekly%`
- Modal panel with per-limit breakdown (limit type, unit, percentage remaining)

Z.AI exposes **quota limits** with percentage-based consumption tracking across different time windows.

### Auto-Refresh

- All quota/balance data is auto-refreshed on a configurable interval (default: 5 minutes, adjustable in Settings → Sidebar Quota Refresh)
- Collapsed panels pause auto-refresh to avoid unnecessary API calls
- Manual refresh is available via the refresh button (with a 10-second cooldown)
- The entire panel can be disabled in Settings if no providers support quota data

## System Status Sidebar

The left sidebar shows real-time system statistics, **polled every 10 seconds** via `/api/system`. This is a regular HTTP poll, not an SSE push - the system stats are fetched independently from the event bus.

### Metrics

| Metric | Source (Docker) | Source (Standalone) | Description |
|--------|----------------|--------------------|-------------|
| API Status | Health check | Health check | Whether the proxy API is responding (Online/Error) |
| Uptime | Process start time | Process start time | How long the server process has been running |
| CPU | Docker aggregate | cgroup cpuacct | CPU usage percentage |
| Processes | Docker container count | cgroup pids | Number of running processes |
| Memory | Docker memory stats | cgroup memory | Memory usage (with limit if available) |
| Network RX/TX | Docker network stats | cgroup network | Network throughput (receive/transmit) |
| Disk Read/Write | Docker I/O stats | cgroup blkio | Disk I/O throughput (read/write) |
| Goroutines | Go runtime | Go runtime | Active goroutine count |
| DB Size | PostgreSQL | PostgreSQL | Database size in MB |
| DB Connections | PostgreSQL | PostgreSQL | Active database connections |
| DB Cache Hit Ratio | PostgreSQL | PostgreSQL | Buffer cache hit percentage |
| Total Requests | request_logs count | request_logs count | Total proxied requests recorded |

When running under Docker Compose (detected via `docker.sock` mount), Docker API stats are aggregated across **all compose services** (app + database) - providing a more complete picture than the app container alone.

### Color Thresholds

System stats use threshold-based color coding to highlight issues at a glance:

| Metric | 🟢 Normal | 🟠 Warning | 🔴 Critical |
|--------|-----------|-----------|------------|
| CPU | < 75% | ≥ 75% | ≥ 90% |
| Memory | < 75% | ≥ 75% | ≥ 90% |
| Goroutines | < 300 | ≥ 300 | ≥ 1,000 |
| DB Cache Hit | ≥ 95% | < 95% | < 80% |

> **Note:** The memory warning/critical thresholds are 75%/90% (matching the CPU thresholds), not the 80%/95% that the original Home.md stated. The actual frontend code uses `dc(memUsagePct, 75, 90)` for memory, identical to CPU.

## In-Flight Request Monitoring

The **Requests** log shows streaming requests as they happen:

- Rows pulse with an animation while the request is active
- Status shows as in-progress (`status_code=0`)
- Updated in real-time as the stream completes with final metrics
- Useful for watching long-running generation requests in real-time