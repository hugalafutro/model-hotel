# Synthetic Stress Test for Model Hotel

A black-box stress testing tool that fires concurrent streaming requests through the proxy against a mock OpenAI-compatible upstream - no real provider API keys or spending required.

## How It Works

1. **Starts a mock upstream server** that responds to `/v1/chat/completions` with realistic SSE streaming chunks and `/v1/models` for discovery
2. **Creates a provider** in the proxy pointing at the mock (via the admin API)
3. **Creates virtual keys** for distributing requests across multiple identities
4. **Runs the scenario matrix**: for each combination of concurrency / rate-limit mode / key count, fires requests through the proxy and collects metrics
5. **Reports** p50/p95/p99 latencies, TTFT (time to first token), throughput, error rates, and status code distribution

## Prerequisites

### Docker Compose (Recommended)

Use the stress-test compose override to start the proxy with the correct settings without modifying the main `docker-compose.yml`:

```bash
# From the project root
docker compose -f docker-compose.yml -f docker-compose.stress-test.yml up -d
```

This override sets `ALLOW_HTTP_PROVIDERS=true`, `RATE_LIMIT_ENABLED=false`, adds `host.docker.internal` to `ALLOWED_PROVIDER_HOSTS`, and maps `host.docker.internal` so the proxy can reach the mock server on the host.

When the proxy runs in Docker, use `--mock-url http://host.docker.internal:9090/v1` so the provider points at the host-reachable address instead of `localhost`.

### Manual Setup

The proxy must already be running with:

```bash
ALLOW_HTTP_PROVIDERS=true \
ALLOWED_PROVIDER_HOSTS=localhost \
RATE_LIMIT_ENABLED=true \
RATE_LIMIT_IP_RPS=30 \
RATE_LIMIT_IP_BURST=60 \
MASTER_KEY=your-master-key \
POSTGRES_PASSWORD=changeme \
  ./server
```

`ALLOWED_PROVIDER_HOSTS=localhost` is required because the mock server runs on localhost. The proxy blocks loopback addresses by default, but allows them when explicitly listed in the allowlist (see `internal/config/config.go`).

### IP Rate Limiter

The proxy has an **IP rate limiter** (separate from per-key rate limiting) that acts as a DoS safety net. It is configured via environment variables (`RATE_LIMIT_IP_RPS` and `RATE_LIMIT_IP_BURST`, defaults: 30 RPS / 60 burst) and can now be toggled at runtime via the settings API (setting key: `rate_limit_ip_enabled`, default: `true`). The IP limiter also implements graceful backpressure, sleeping up to `rate_limit_max_wait_ms` instead of immediately returning 429.

The stress test controls **per-key rate limiting** via the `/api/settings` endpoint (the `-rate-limit`, `-rps`, and `-burst` flags) and can override the IP limiter via the `-ip-ratelimit` flag. If you see unexpected 429 responses at high concurrency, the IP rate limiter may be the cause. Increase `RATE_LIMIT_IP_RPS`/`RATE_LIMIT_IP_BURST` above your expected aggregate request rate, or disable IP rate limiting during tests using `-ip-ratelimit false`.

## Quick-Start Checklist

Before running, verify each item:

1. **Proxy is running** - `curl http://localhost:8080/api/settings -H "Authorization: Bearer <ADMIN_TOKEN>"` returns settings
2. **PostgreSQL is up** - `pg_isready -h localhost -p 5432`
3. **Environment vars set** - `MASTER_KEY`, `POSTGRES_PASSWORD`, `ALLOW_HTTP_PROVIDERS=true`, `ALLOWED_PROVIDER_HOSTS=localhost`
4. **IP rate limiter is high enough** - `RATE_LIMIT_IP_RPS` / `RATE_LIMIT_IP_BURST` exceed your max aggregate RPS (default 30/60 is fine for ≤100 concurrency)
5. **Port 9090 is free** - the mock server binds to `:9090` by default
6. **Admin token** - check `data/admin-token` file or server startup logs

## Running

```bash
cd tools/stress-test
go run . -admin-token <YOUR_ADMIN_TOKEN>
```

### Examples

```bash
# Run all default scenarios (10, 50, 100, 1000 concurrent x RL on/off x 1 and 10 keys)
go run . -admin-token abc123

# Run a single scenario: 100 concurrent, rate limit off, 1 key, streaming
go run . -admin-token abc123 -concurrency 100 -rate-limit false -keys 1

# Quick test with fewer requests
go run . -admin-token abc123 -concurrency 10,50 -requests 50 -output json

# Simulate slower upstream (100ms between chunks)
go run . -admin-token abc123 -chunk-delay 100 -chunk-count 20

# Non-streaming test
go run . -admin-token abc123 -streaming false

# Test proxy's param-rejection auto-retry (mock rejects top_p, proxy should strip and retry)
go run . -admin-token abc123 -reject-params top_p -extra-params top_p=0.5 -concurrency 10

# Floodgates test: high concurrency, all rate limiting off
go run . -admin-token abc123 -concurrency 200,500,1000,2000 -keys 10 -requests 10000 -rate-limit false -key-rps 1000000 -key-burst 1000000 -ip-ratelimit false

# Test with per-key rate limits (10 RPS / 20 burst per key)
go run . -admin-token abc123 -key-rps 10 -key-burst 20 -concurrency 50,100 -keys 10 -rate-limit true

# Sustained streams: each request streams for 3-13 seconds (random per request)
go run . -admin-token abc123 -stream-duration 3-13 -chunk-count 50 -concurrency 50,100 -requests 200
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-proxy-url` | `http://localhost:8080` | Proxy base URL |
| `-admin-token` | *(required)* | Admin token for API calls |
| `-mock-port` | `9090` | Port for the mock upstream server |
| `-concurrency` | `10,50,100,1000` | Comma-separated concurrency levels |
| `-keys` | `1,10` | Comma-separated number of virtual keys |
| `-rate-limit` | `false,true` | Comma-separated RL on/off values |
| `-streaming` | `true` | Use streaming requests |
| `-requests` | `0` | Total requests per scenario (0 = concurrency x 10) |
| `-chunk-delay` | `20` | Delay between SSE chunks (ms) |
| `-chunk-count` | `15` | Number of SSE chunks per response |
| `-tokens-per-chunk` | `3` | Completion tokens per SSE chunk |
| `-initial-delay` | `10` | Initial delay before first chunk (ms, simulates TTFT) |
| `-stream-duration` | `""` | Random stream duration range in seconds (e.g. `3-13`). Overrides `-chunk-delay` with per-request random variation for sustained, realistic streams |
| `-reject-params` | `""` | Comma-separated param names the mock server rejects with 400 (e.g. `top_p,frequency_penalty`) |
| `-extra-params` | `""` | Comma-separated params to include in requests, with optional values (e.g. `top_p=0.5,frequency_penalty=1.0`) |
| `-rps` | `10` | Rate limit RPS when enabled |
| `-burst` | `20` | Rate limit burst when enabled |
| `-key-rps` | `0` | Per-key rate limit RPS override (0 = use global setting, no override) |
| `-key-burst` | `0` | Per-key rate limit burst override (0 = use global setting, no override) |
| `-ip-ratelimit` | `""` | Override IP rate limiter: `"true"` or `"false"` (empty = do not change setting) |
| `-mock-url` | `""` | Override provider base URL (use `http://host.docker.internal:9090/v1` when proxy runs in Docker) |
| `-output` | `markdown` | Output format: text, markdown, json |

## Test Matrix

The default flag values produce 16 scenarios:

| # | Concurrency | Rate Limit | Keys | What it tests |
|---|-------------|------------|------|---------------|
| 1 | 10 | OFF | 1 | Baseline proxy overhead |
| 2 | 50 | OFF | 1 | Moderate load, single key |
| 3 | 100 | OFF | 1 | High concurrency, single key |
| 4 | 1000 | OFF | 1 | Extreme concurrency - goroutine/transport/DB pool pressure |
| 5 | 10 | ON | 1 | Rate limiting correctness at low load |
| 6 | 50 | ON | 1 | Rate limiting under moderate load |
| 7 | 100 | ON | 1 | Rate limiting under high load |
| 8 | 1000 | ON | 1 | Rate limiting under extreme load - bucket exhaustion |
| 9 | 10 | OFF | 10 | Multi-key isolation - no cross-key interference |
| 10 | 50 | OFF | 10 | Multi-key at moderate scale |
| 11 | 100 | OFF | 10 | Multi-key at high scale |
| 12 | 1000 | OFF | 10 | Multi-key extreme - 10 independent token buckets |
| 13 | 10 | ON | 10 | Multi-key rate limiting |
| 14 | 50 | ON | 10 | Multi-key RL at moderate scale - 10 x 10 RPS = 100 RPS aggregate |
| 15 | 100 | ON | 10 | Multi-key RL at high scale |
| 16 | 1000 | ON | 10 | Multi-key RL extreme |

## Architecture

```
+---------------+     +---------------+     +---------------+
|  stress-test  |---->|  Model Hotel  |---->|  Mock Server   |
|    runner     |     |  (:8080)      |     |  (:9090)       |
+---------------+     +---------------+     +---------------+
      admin API           IP rate limiter
      virtual keys        per-key rate limiter
                         failover (exponential backoff)
                         request logging          SSE streaming
```

The stress test is purely external - it talks to the proxy via HTTP, creates fixtures via the admin API, and measures end-to-end behaviour. No proxy code changes are needed to run it (beyond the loopback allowlist config).

## What Model Hotel Does Per Request

Understanding the internal path helps interpret results:

```
Request -> chi middleware (RequestID, RealIP, Logger, Recoverer, Compress, Security, CORS, MaxBytesReader)
  -> streamingAwareTimeout (5min for streaming)
  -> IPLimiter.Middleware (per-IP token bucket, always-on DoS safety net, config: RATE_LIMIT_IP_RPS/RATE_LIMIT_IP_BURST)
  -> ProxyKeyMiddleware (SHA-256 hash lookup against virtual_keys -- 1 DB query)
  -> RateLimiter.Middleware (per-key token bucket from golang.org/x/time/rate, config: rate_limit_rps/rate_limit_burst via settings API)
  -> ChatCompletions handler:
      1. Parse body
      2. Resolve model (failover group or provider/model) -- DB + cache
      3. Decrypt provider API key (AES-256-GCM, cached)
      4. INSERT into request_logs (DB write)
      5. For each candidate in failover chain:
         a. Strip provider-unsupported params preemptively
         b. Forward to upstream provider (HTTP POST via shared Transport)
         c. On 400: parse error for rejected params, cache them, strip and retry once
         d. On 5xx/429/401/403: exponential backoff (100ms base, 2s cap), try next
         e. On 200: stream/return response
      6. UPDATE request_logs (DB write)
      7. UPDATE virtual_keys SET tokens_used (DB write)
      8. Fire-and-forget TouchLastUsed (DB write)
```

### Two-Layer Rate Limiting

The proxy has **two independent rate limiting layers**:

| Layer | Scope | Config | Controls |
|-------|-------|--------|----------|
| **IP Rate Limiter** | Per IP address | `RATE_LIMIT_IP_RPS` / `RATE_LIMIT_IP_BURST` env vars; `rate_limit_ip_enabled` via settings API | DoS safety net. Can be toggled via `rate_limit_ip_enabled` setting. Implements graceful backpressure (sleeps up to `rate_limit_max_wait_ms`). |
| **Per-Key Rate Limiter** | Per virtual key | `rate_limit_rps` / `rate_limit_burst` via `PUT /api/settings` | Can be toggled on/off via `rate_limit_enabled`. Stress test controls this with `-rate-limit`, `-rps`, `-burst`, `-key-rps`, `-key-burst`. |

At high concurrency, the IP limiter may trigger before the per-key limiter. If running scenarios with >30 RPS aggregate, increase `RATE_LIMIT_IP_RPS` and `RATE_LIMIT_IP_BURST` accordingly.

Likely bottleneck candidates at high concurrency:

- **Postgres connection pool** (pgxpool MaxConns=25) - 4-5 DB writes per request
- **Shared http.Transport** - Go defaults (100 total idle conns, 2 per host) may cause connection churn
- **Rate limiter mutex** - single `sync.Mutex` for all key entries
- **Goroutine count** - each streaming request holds a goroutine for the full stream duration

## File Structure

```
tools/stress-test/
+-- main.go              # CLI entry point, scenario orchestration
+-- main_test.go         # Unit tests for CLI parsers (parseIntList, parseBoolList, maxInt)
+-- go.mod               # Standalone module (stdlib only, replace directive to ../../)
+-- .golangci.yml        # Linter config
+-- ../../docker-compose.stress-test.yml  # Docker Compose override for stress testing
+-- mock/
|   +-- server.go        # Mock OpenAI-compatible SSE upstream
|   +-- server_test.go   # Mock server endpoint tests
+-- harness/
|   +-- admin.go         # Admin API client (create/delete providers, keys, settings)
|   +-- proxy_client.go  # HTTP client that measures TTFT and streaming latency
|   +-- runner.go        # Scenario runner (goroutine pool + metrics collection)
|   +-- harness_test.go  # Unit tests for AdminClient + ProxyClient
+-- metrics/
    +-- collector.go     # Thread-safe metrics accumulator + percentile calculator
    +-- collector_test.go # Comprehensive collector + percentile tests
    +-- report.go        # Aggregate stats + formatting (text/markdown/json)
```

## Sample Output (Markdown)

```markdown
# Model Hotel Synthetic Stress Test Report

- **Proxy:** `http://localhost:8081`
- **Mock upstream:** `http://localhost:9090`

| # | Scenario | Requests | Success | Errors | Throughput | p50 | p95 | p99 | TTFT p50 | TTFT p95 | Status codes |
|---|----------|----------|---------|--------|------------|-----|-----|-----|----------|----------|-------------|
| 1 | 10-conc, RL=false, 1-key, stream=true | 100 | 100 | 0 | 89.3/s | 312ms | 354ms | 378ms | 18ms | 22ms | 200: 100 |
| 4 | 1000-conc, RL=false, 1-key, stream=true | 10000 | 9987 | 13 | 423.1/s | 2.1s | 3.8s | 5.2s | 890ms | 2.1s | 200: 9987, 502: 13 |
| 8 | 1000-conc, RL=true, 1-key, stream=true | 10000 | 4200 | 5800 | 42.1/s | 1.2s | 3.4s | 5.0s | 890ms | 2.1s | 200: 4200, 429: 5800 |
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `Failed to set up test fixtures: create provider: 500` | Proxy can't reach mock or `ALLOW_HTTP_PROVIDERS` not set | Set `ALLOW_HTTP_PROVIDERS=true` and `ALLOWED_PROVIDER_HOSTS=localhost`. If proxy runs in Docker, use `--mock-url http://host.docker.internal:9090/v1` |
| `Failed to start mock server: listen tcp :9090: bind: address already in use` | Another process on port 9090 | Use `-mock-port 9091` or kill the process |
| All requests return 429 at high concurrency | IP rate limiter is throttling | Increase `RATE_LIMIT_IP_RPS` / `RATE_LIMIT_IP_BURST` env vars on the proxy. Or disable IP rate limiting during tests using `-ip-ratelimit false`, or via the settings API (`rate_limit_ip_enabled`). |
| Discovery fails (`discovery failed`) | Mock server not ready or unreachable | Check mock server logs; ensure it started before the proxy call |
| `Error: -admin-token is required` | Missing required flag | Pass `-admin-token <TOKEN>` (find it in `data/admin-token` or startup logs) |
| Stale `stress-mock` provider / `stress-key-*` keys after Ctrl-C | Cleanup defer didn't run | Delete manually via dashboard or API |
| `connection refused` on proxy | Proxy not running | Start proxy first with required env vars |

## Cleanup

The tool automatically cleans up all test fixtures (provider + virtual keys) when it finishes, even on errors. If you Ctrl-C, you may need to manually delete the `stress-mock` provider and `stress-key-*` virtual keys from the dashboard.