# 📈 Observability

Model Hotel exposes three export surfaces: Prometheus metrics at `/metrics`, JSON logs on
stdout (`LOG_FORMAT=json`) and OpenTelemetry log export (`OTEL_EXPORTER_OTLP_ENDPOINT`). The
variables are documented in [[Configuration]]; the Settings page's Observability panel shows
which of the three is active. This page covers the piece most people want first: a Grafana
dashboard over the metrics.

<p align="center">
<img src="screenshots/grafana_dashboard.png" alt="Grafana: Model Hotel gateway dashboard" width="900"><br>
<em>The provisioned "Model Hotel gateway" dashboard: overview stats, traffic, latency, tokens and spend, reliability, process</em>
</p>

## What the dashboard shows

One dashboard, provisioned into the folder "Model Hotel", with a `provider` variable that
narrows the traffic, latency, spend and breaker panels (the fleet-wide tiles and the process
row ignore it):

- **Overview**: members up (the Up tile), requests/s, error share, p95 latency and TTFT, tokens/s, spend
  over the selected range (approximate at the range edges), providers with a breaker not
  closed anywhere in the fleet, time since the most recent member restart. Red means look
  here: only members up, error share and open breakers carry thresholds.
- **Traffic**: requests/s by status class and by provider, errors/s by kind, requests by model.
- **Latency**: duration and TTFT quantiles, p95 by provider.
- **Tokens & Spend**: tokens/s by kind, tokens by model, spend per hour by provider, spend by
  model. Spend is `modelhotel_cost_usd_total`, the same dollars the dashboard's `$` view and
  `request_logs.cost_usd` carry; a model with no known price adds nothing, so a sum is a floor
  wherever a model is unpriced.
- **Reliability**: breaker state per enabled provider, the worst state any member reports for it
  (a provider nothing has routed to yet reads closed; a blank lane is a provider that is disabled or gone), failover attempts, Responses API reroutes, retirement probes, upstream 429s
  by class, breaker opens by cause, failover exhaustion by reason.
- **Quota**: how much of each subscription window is used, from the latest quota poll (the
  same figures as the provider's quota modal), the same over time, and how long until each
  dated window rolls over. Only providers with a readable quota endpoint appear: Z.ai Coding
  Plan, Kimi Code, OpenCode Go, MiniMax and NeuralWatt. A provider with a quota reserve draws
  its line on the over-time panel as a dashed series, so a window pinned early reads as such
  (NeuralWatt's balances and Z.ai's MCP calls are shown but never pin, reserve or not).
- **Process**: goroutines, resident memory, CPU.

Every panel carries a description on hover. The full list of series and their labels is in
[[Failover and Hotel Routing]] (failover and breaker series) and [[Model Discovery]] (retirement
probes).

## Deploying Prometheus and Grafana

`deploy/observability/` in the repository is a self-contained compose stack: Prometheus
scraping every member, Grafana with the datasource and dashboard provisioned. It runs anywhere
that can reach the members over HTTPS; on a fleet, one copy next to any member is enough, since
the dashboard sums across members.

1. Copy the directory to the host, for example `/home/you/docker/observability`.
2. Put each member's token in `tokens/<member>` (`tokens/mh1`, `tokens/mh2`, ...), one line, mode
   `0600`. Set a dedicated `METRICS_TOKEN` on each member for this (see [[Configuration]]);
   until one is set, the member's admin token opens `/metrics` and works here too.
3. Edit `prometheus.yml`: one `scrape_config` block per member, with its public hostname as the
   target and its token file. The shipped file has `mh1` filled in as an example and `mh2`
   commented out; copy the block for each further member. A Prometheus on the same Docker host
   as a member may target the container directly instead (`model-hotel-app-1:8080` on the
   `model-hotel_default` network).
4. Copy `.env.example` to `.env` and set `GRAFANA_ADMIN_PASSWORD`; Grafana refuses to start
   without one. Set `OBS_UID`/`OBS_GID` to the uid and gid that own `tokens/` (`id -u`,
   `id -g`), so Prometheus runs as that user and can read the `0600` token files.
   `GRAFANA_PORT` (3000) and `PROMETHEUS_RETENTION` (30d) are optional. The compose file
   starts Prometheus with `--enable-feature=promql-experimental-functions`; the "Quota used"
   panel sorts its bars with `sort_by_label`, which Prometheus 3.x keeps behind that flag, so
   a Prometheus of your own that scrapes the members needs the same flag or that panel is empty.
5. `mkdir -p data/prometheus` before the first start (Prometheus stores its series there, as
   your user; if the stack was started first, Docker created that directory as root, so
   `sudo chown -R $(id -u):$(id -g) data` once), then `docker compose up -d` and open `http://<host>:3000`, sign in as `admin`, and find the
   dashboard under Dashboards → Model Hotel. Prometheus itself is not published; Grafana reaches
   it on the compose network.

Grafana listens on plain HTTP: keep it on the LAN or put it behind your reverse proxy with
TLS. The stack is small (Prometheus and Grafana together sit well under a gigabyte of memory
for a four-member fleet at a 15-second scrape interval).

The `Logs` row of the development overlay (Loki-backed request and application logs) is not
part of this stack, which is why the shipped dashboard has no Logs row. For logs, ship the
container output to your own collector with `LOG_FORMAT=json` or `OTEL_EXPORTER_OTLP_ENDPOINT`.

## Editing the dashboard

The dashboard is a plain Grafana JSON model, `grafana/dashboards/model-hotel-gateway.json`.
Grafana re-reads the provisioned file every 30 seconds, so an edit to the file lands without a
restart. Edits made in the Grafana UI are allowed (`allowUiUpdates`) and persist until the file
itself next changes, which overwrites them; export the JSON model and commit it to keep them.
