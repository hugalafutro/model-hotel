# Synthetic Stress Test for Model Hotel

A black-box stress testing tool that fires concurrent streaming requests through the proxy against a mock OpenAI-compatible upstream — no real provider API keys or spending required.

## How It Works

1. **Starts a mock upstream server** that responds to `/v1/chat/completions` with realistic SSE streaming chunks and `/v1/models` for discovery
2. **Creates a provider** in the proxy pointing at the mock (via the admin API)
3. **Creates virtual keys** for distributing requests across multiple identities
4. **Runs the scenario matrix**: for each combination of concurrency / rate-limit mode / key count, fires requests through the proxy and collects metrics
5. **Reports** p50/p95/p99 latencies, TTFT (time to first token), throughput, error rates, and status code distribution

## Prerequisites

The proxy must already be running with:

```bash
ALLOW_HTTP_PROVIDERS=true \
ALLOWED_PROVIDER_HOSTS=localhost \
RATE_LIMIT_ENABLED=true \
MASTER_KEY=your-master-key \
DATABASE_URL=postgres://llmproxy:changeme@localhost:5432/llmproxy \
  ./server
```

`ALLOWED_PROVIDER_HOSTS=localhost` is required because the mock server runs on localhost. The proxy blocks loopback addresses by default, but allows them when explicitly listed in the allowlist (this was a small config change — see `internal/config/config.go`).

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
```

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-proxy-url` | `http://localhost:8081` | Proxy base URL |
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
| `-rps` | `10` | Rate limit RPS when enabled |
| `-burst` | `20` | Rate limit burst when enabled |
| `-output` | `markdown` | Output format: text, markdown, json |

## Test Matrix

The default flag values produce 16 scenarios:

| # | Concurrency | Rate Limit | Keys | What it tests |
|---|-------------|------------|------|---------------|
| 1 | 10 | OFF | 1 | Baseline proxy overhead |
| 2 | 50 | OFF | 1 | Moderate load, single key |
| 3 | 100 | OFF | 1 | High concurrency, single key |
| 4 | 1000 | OFF | 1 | Extreme concurrency — goroutine/transport/DB pool pressure |
| 5 | 10 | ON | 1 | Rate limiting correctness at low load |
| 6 | 50 | ON | 1 | Rate limiting under moderate load |
| 7 | 100 | ON | 1 | Rate limiting under high load |
| 8 | 1000 | ON | 1 | Rate limiting under extreme load — bucket exhaustion |
| 9 | 10 | OFF | 10 | Multi-key isolation — no cross-key interference |
| 10 | 50 | OFF | 10 | Multi-key at moderate scale |
| 11 | 100 | OFF | 10 | Multi-key at high scale |
| 12 | 1000 | OFF | 10 | Multi-key extreme — 10 independent token buckets |
| 13 | 10 | ON | 10 | Multi-key rate limiting |
| 14 | 50 | ON | 10 | Multi-key RL at moderate scale — 10 x 10 RPS = 100 RPS aggregate |
| 15 | 100 | ON | 10 | Multi-key RL at high scale |
| 16 | 1000 | ON | 10 | Multi-key RL extreme |

## Architecture

```
+---------------+     +---------------+     +---------------+
|  stress-test  |---->|  Model Hotel  |---->|  Mock Server   |
|    runner     |     |  (:8081)      |     |  (:9090)       |
+---------------+     +---------------+     +---------------+
     admin API           rate limiter          /v1/models
     virtual keys        failover              /v1/chat/completions
                        request logging        SSE streaming
```

The stress test is purely external — it talks to the proxy via HTTP, creates fixtures via the admin API, and measures end-to-end behaviour. No proxy code changes are needed to run it (beyond the loopback allowlist config).

## What Model Hotel Does Per Request

Understanding the internal path helps interpret results:

```
Request -> chi middleware (RequestID, RealIP, Logger, Recoverer, Compress, Security, CORS, MaxBytesReader)
  -> streamingAwareTimeout (5min for streaming)
  -> ProxyKeyMiddleware (SHA-256 hash lookup against virtual_keys -- 1 DB query)
  -> RateLimiter.Middleware (per-key token bucket from golang.org/x/time/rate)
  -> ChatCompletions handler:
      1. Parse body
      2. Resolve model (failover group or provider/model) -- DB + cache
      3. Decrypt provider API key (AES-256-GCM, cached)
      4. INSERT into request_logs (DB write)
      5. Forward to upstream provider (HTTP POST via shared Transport)
      6. Stream back SSE chunks line-by-line
      7. UPDATE request_logs (DB write)
      8. UPDATE virtual_keys SET tokens_used (DB write)
      9. Fire-and-forget TouchLastUsed (DB write)
```

Likely bottleneck candidates at high concurrency:

- **Postgres connection pool** (pgxpool MaxConns=25) — 4-5 DB writes per request
- **Shared http.Transport** — Go defaults (100 total idle conns, 2 per host) may cause connection churn
- **Rate limiter mutex** — single `sync.Mutex` for all key entries
- **Goroutine count** — each streaming request holds a goroutine for the full stream duration

## File Structure

```
tools/stress-test/
+-- main.go              # CLI entry point, scenario orchestration
+-- go.mod               # Standalone module (stdlib only)
+-- mock/
|   +-- server.go        # Mock OpenAI-compatible SSE upstream
+-- harness/
|   +-- admin.go         # Admin API helpers (create provider, keys, settings)
|   +-- proxy_client.go  # HTTP client that measures TTFT and streaming latency
|   +-- runner.go        # Scenario runner (goroutine pool + metrics collection)
+-- metrics/
    +-- collector.go     # Thread-safe metrics accumulator + percentile calculator
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

## Cleanup

The tool automatically cleans up all test fixtures (provider + virtual keys) when it finishes, even on errors. If you Ctrl-C, you may need to manually delete the `stress-mock` provider and `stress-key-*` virtual keys from the dashboard.