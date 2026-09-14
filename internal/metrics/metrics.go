// Package metrics exposes Model Hotel's Prometheus metrics: a private registry,
// the request-outcome collectors, a circuit-breaker-state collector, and the
// HTTP handler that serves the /metrics endpoint.
//
// Labels are low-cardinality: provider and model names yes, virtual-key IDs or
// request IDs never. No prompt, request or response content reaches a metric.
package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry is a private registry so metrics are isolated from any global
// default and tests can scrape a clean instance.
var registry = prometheus.NewRegistry()

var (
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_requests_total",
		Help: "Total proxied requests by provider, model, status class, and error kind.",
	}, []string{"provider", "model", "status_class", "error_kind"})

	// A generation runs for tens of seconds to minutes, so the buckets reach the
	// stall watchdog's range rather than stopping at the 10 s the Prometheus
	// defaults end at, where histogram_quantile would clamp every quantile to
	// 10 s once most requests take longer. The defaults stay as the lower
	// edges so every le series that existed before keeps its meaning across a
	// rolling upgrade.
	requestDurationBuckets = append(append([]float64{}, prometheus.DefBuckets...), 20, 30, 60, 120, 180, 300, 600)
	// First token lands within seconds when the provider is healthy and within
	// a minute or two when it queues; the top bucket marks a stall.
	ttftBuckets = append(append([]float64{}, prometheus.DefBuckets...), 20, 30, 60, 120)

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "modelhotel_request_duration_seconds",
		Help:    "End-to-end proxied request duration in seconds.",
		Buckets: requestDurationBuckets,
	}, []string{"provider", "model"})

	ttftSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "modelhotel_ttft_seconds",
		Help:    "Time to first token for streaming requests, in seconds.",
		Buckets: ttftBuckets,
	}, []string{"provider", "model"})

	tokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_tokens_total",
		Help: "Total tokens metered by provider, model, and kind. Reasoning is the share of completion a reasoning model spent thinking, not an extra count: sum prompt and completion for a total, never add reasoning to them.",
	}, []string{"provider", "model", "kind"})

	costUSDTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_cost_usd_total",
		Help: "Dollars spent on proxied requests by provider and model, the figure request_logs.cost_usd stores for the row, booked once the row has landed. A request whose model has no known price adds nothing and no series, so a sum is a floor where any model is unpriced; a free model adds 0. model is the name the client asked for (hotel/<group> for group traffic, priced from the member that served it), as modelhotel_requests_total carries it.",
	}, []string{"provider", "model"})

	failoverAttemptsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_failover_attempts_total",
		Help: "Failover attempts beyond the first try, by model (or hotel group) and the provider the attempt went to, hedged launches included. The fan-out to fallback entries per provider, not only per group.",
	}, []string{"model", "provider"})

	upstreamRateLimitTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_upstream_rate_limit_total",
		Help: "Upstream 429 responses to proxied requests by provider, model and class (probes and quota polls issue their own requests and are not counted here). saturated = slots or a per-minute budget busy, retry in seconds (the circuit is not charged); exhausted = the window or balance is spent (the circuit opens and pins); unknown = the classifier could not tell, or rate-limit failover is off (treated as an ordinary failure). model is the provider-side model id.",
	}, []string{"provider", "model", "class"})

	circuitBreakerOpensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_circuit_breaker_opens_total",
		Help: "Circuit-breaker open transitions by provider, model and cause, the breaker's own verdict phrase (\"upstream status 429 (exhausted)\", \"upstream status 503\", \"upstream request failed\"; a saturated 429 never opens a circuit). Pairs with modelhotel_circuit_breaker_state, which cannot show an open and a close inside one scrape interval.",
	}, []string{"provider", "model", "cause"})

	failoverExhaustedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_failover_exhausted_total",
		Help: "Requests to a failover group that no entry served, by group and reason. no_available_provider = the group resolved to zero candidates (every entry disabled, missing or skipped by the breaker); all_busy = the last candidate answered a saturated 429 or was at its in-flight limit; all_failed = it failed some other way, or the failover deadline passed.",
	}, []string{"group", "reason"})

	responsesRerouteTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_responses_reroute_total",
		Help: "Attempts routed via the OpenAI Responses API instead of chat completions, by provider, model, and mode (learned = healed from a live 400, preemptive = cache-driven).",
	}, []string{"provider", "model", "mode"})

	retirementProbesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_retirement_probes_total",
		Help: "Pre-retirement probes by provider, model, and verdict. refused = the provider refused the model by name, so a retirement was attempted (it can still be called off, so this is not a count of retirements; use the model.auto_disabled_gone event for those). served = the model answered and the retirement was called off. inconclusive = nothing was established and the retirement was postponed. model is the provider-side model id, not the name a client asked for.",
	}, []string{"provider", "model", "verdict"})
)

func init() {
	registry.MustRegister(
		requestsTotal,
		requestDuration,
		ttftSeconds,
		tokensTotal,
		costUSDTotal,
		failoverAttemptsTotal,
		upstreamRateLimitTotal,
		circuitBreakerOpensTotal,
		failoverExhaustedTotal,
		responsesRerouteTotal,
		retirementProbesTotal,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// Observation is one completed proxied request's metric-relevant outcome,
// derived from the request log entry at its single terminal-recording seam.
type Observation struct {
	Provider         string
	Model            string
	StatusCode       int
	ErrorKind        string // "" when none
	DurationSeconds  float64
	TTFTSeconds      float64 // 0 when not measured (non-streaming or no first token)
	Streaming        bool
	PromptTokens     int
	CompletionTokens int
	ReasoningTokens  int
	// CostUSD is what the request cost when Priced; a request the gateway could
	// not price (no provider served it, or the model has no prices) is not
	// counted at all rather than counted as free.
	CostUSD float64
	Priced  bool
	// FailoverProviders names the provider of every attempt after the first
	// (hedged launches included), in attempt order, one failover attempt each.
	FailoverProviders []string
}

// Record updates the request-outcome metrics from one completed request.
func Record(o Observation) {
	provider := labelOrUnknown(o.Provider)
	model := labelOrUnknown(o.Model)

	requestsTotal.WithLabelValues(provider, model, statusClass(o.StatusCode), o.ErrorKind).Inc()
	requestDuration.WithLabelValues(provider, model).Observe(o.DurationSeconds)
	if o.Streaming && o.TTFTSeconds > 0 {
		ttftSeconds.WithLabelValues(provider, model).Observe(o.TTFTSeconds)
	}
	if o.PromptTokens > 0 {
		tokensTotal.WithLabelValues(provider, model, "prompt").Add(float64(o.PromptTokens))
	}
	if o.CompletionTokens > 0 {
		tokensTotal.WithLabelValues(provider, model, "completion").Add(float64(o.CompletionTokens))
	}
	if o.ReasoningTokens > 0 {
		tokensTotal.WithLabelValues(provider, model, "reasoning").Add(float64(o.ReasoningTokens))
	}
	// A counter refuses a negative add with a panic, and a price is only range
	// checked on the admin API, not on catalog imports.
	if o.Priced && o.CostUSD >= 0 {
		costUSDTotal.WithLabelValues(provider, model).Add(o.CostUSD)
	}
	for _, p := range o.FailoverProviders {
		failoverAttemptsTotal.WithLabelValues(model, labelOrUnknown(p)).Inc()
	}
}

// RecordUpstreamRateLimit counts one upstream 429 by the class the classifier
// assigned it ("saturated", "exhausted", "unknown"). The caller owns that
// vocabulary, as with RecordRetirementProbe, because the class type lives in the
// proxy. It shows a provider's slot ceiling as a flat line of saturated, where
// the request counter shows only 429s.
func RecordUpstreamRateLimit(provider, model, class string) {
	upstreamRateLimitTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), class).Inc()
}

// RecordBreakerOpen counts one circuit opening, with the cause the breaker
// stamped on it. Cardinality is provider x model x cause, and cause is a small
// closed vocabulary: "upstream status <code>" with an optional qualifier, the
// transport failure, the exhausted-body verdict.
func RecordBreakerOpen(provider, model, cause string) {
	circuitBreakerOpensTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), labelOrUnknown(cause)).Inc()
}

// RecordFailoverExhausted counts one request a failover group could not serve,
// by group (the display model, without the hotel/ prefix) and reason
// ("no_available_provider", "all_busy", "all_failed").
func RecordFailoverExhausted(group, reason string) {
	failoverExhaustedTotal.WithLabelValues(labelOrUnknown(group), reason).Inc()
}

// RecordResponsesReroute counts one request issued to /v1/responses. mode is
// "learned" when the route was discovered by healing a live refusal,
// "preemptive" when the cached requirement redirected the attempt up front,
// and "param_retry" for each re-issue of either that the param self-heal
// makes on that route, so one attempt can count more than once.
func RecordResponsesReroute(provider, model, mode string) {
	responsesRerouteTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), mode).Inc()
}

// RecordRetirementProbe counts one completed pre-retirement probe. verdict is
// the probe's own name for what it established ("refused", "served",
// "inconclusive"); the caller owns that vocabulary, because the verdict type
// lives in the proxy and this package must stay importable by it.
//
// One series per (provider, model, verdict), and only for models the gateway
// probed: a probe is rate-limited to one per model per cooldown, and only a model
// drawing repeated gone-classified refusals is nominated.
//
// The model label is the PROVIDER-SIDE id, while requestsTotal carries the name
// the CLIENT asked for. For direct "provider/model" traffic those are the same
// string, so the two counters join. They diverge on two shapes: a request routed
// through a failover group is "hotel/<group>" on requestsTotal and the real id
// here, and a validation failure is collapsed there to "unresolved". A PromQL
// join is sound but silently misses the group-routed traffic.
//
// It counts VERDICTS rather than retirements. A retirement is visible in the
// model row and in the model.auto_disabled_gone event; a probe that retired
// nothing is not: a "served" means the classifier nominated a live model, and a
// run of "inconclusive" means the gateway is paying for an answer it is not
// getting.
//
// A refused verdict is not a completed retirement: the write that follows can be
// superseded by a success, refused by the repository, or reverted. Alert on the
// ratio between verdicts, not on refused as a retirement count.
func RecordRetirementProbe(provider, model, verdict string) {
	retirementProbesTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), verdict).Inc()
}

// Handler returns the HTTP handler that serves the metrics in Prometheus text
// exposition format. The caller is responsible for authenticating the route.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// statusClass buckets an HTTP status into a low-cardinality label. 499 (client
// closed request) stays distinct so client disconnects are not conflated with
// provider 4xx.
func statusClass(code int) string {
	switch {
	case code == 499:
		return "499"
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}

func labelOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// BreakerState is one provider's circuit-breaker state for the gauge: the
// provider identifier and the numeric state (0 closed / 1 half-open / 2 open).
type BreakerState struct {
	ProviderID   string
	ProviderName string // the operator's name for the provider; "" when unknown
	State        int
}

// Numeric state encoding for modelhotel_circuit_breaker_state.
const (
	BreakerClosed   = 0
	BreakerHalfOpen = 1
	BreakerOpen     = 2
)

// RegisterBreakerCollector registers a scrape-time collector that reports the
// circuit-breaker state per provider. collect runs on every scrape and must be
// cheap and non-blocking. Passing nil, or calling more than once, is a no-op
// after the first registration.
//
// Scrape-time rather than an event-updated gauge, because the open to half-open
// transition is time-based and an event gauge would miss it.
func RegisterBreakerCollector(collect func() []BreakerState) {
	if collect == nil {
		return
	}
	registerBreakerOnce.Do(func() {
		registry.MustRegister(&breakerCollector{collect: collect})
	})
}

var registerBreakerOnce sync.Once

type breakerCollector struct {
	collect func() []BreakerState
}

var breakerDesc = prometheus.NewDesc(
	"modelhotel_circuit_breaker_state",
	"Circuit breaker state per provider (0 closed, 1 half-open, 2 open). provider is the operator's name, as the other series carry it; provider_id is the row's id, stable across a rename.",
	[]string{"provider_id", "provider"}, nil,
)

func (c *breakerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- breakerDesc
}

func (c *breakerCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.collect() {
		ch <- prometheus.MustNewConstMetric(breakerDesc, prometheus.GaugeValue, float64(s.State), s.ProviderID, labelOrUnknown(s.ProviderName))
	}
}

// Compile-time guard: the collector implements prometheus.Collector.
var _ prometheus.Collector = (*breakerCollector)(nil)

// QuotaWindow is one provider quota window for the quota gauges: the share of
// the window consumed (0 untouched, 1 spent, above 1 in overage) and, when the
// provider dates it, the moment it rolls over.
type QuotaWindow struct {
	ProviderID   string
	ProviderName string // the operator's name for the provider; "" when unknown
	Window       string // the window's name as the quota modal shows it (5h, weekly, energy)
	Used         float64
	ResetsAt     time.Time // zero when the payload does not date the reset
}

// RegisterQuotaCollector registers a scrape-time collector that reports each
// provider's quota windows from its latest stored snapshot. collect runs on
// every scrape and must stay cheap; it may read the database, since the
// snapshots are what the poller last wrote. Passing nil, or calling more than
// once, is a no-op after the first registration.
func RegisterQuotaCollector(collect func() []QuotaWindow) {
	if collect == nil {
		return
	}
	registerQuotaOnce.Do(func() {
		registry.MustRegister(&quotaCollector{collect: collect})
	})
}

var registerQuotaOnce sync.Once

type quotaCollector struct {
	collect func() []QuotaWindow
}

var (
	quotaUsedDesc = prometheus.NewDesc(
		"modelhotel_provider_quota_used_ratio",
		"Share of a provider quota window consumed, from the latest stored quota snapshot: 0 untouched, 1 spent, above 1 in overage. window names the window as the quota modal does (5h, weekly, mcp, rolling, monthly, energy, credits, a Kimi span such as 5h, or a MiniMax model class with its span). Only providers whose quota endpoint states a measurable window appear.",
		[]string{"provider_id", "provider", "window"}, nil,
	)
	quotaResetDesc = prometheus.NewDesc(
		"modelhotel_provider_quota_resets_at_seconds",
		"Unix time at which a provider quota window rolls over, from the latest stored quota snapshot. Absent for a window the provider does not date (a prepaid balance).",
		[]string{"provider_id", "provider", "window"}, nil,
	)
)

func (c *quotaCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- quotaUsedDesc
	ch <- quotaResetDesc
}

func (c *quotaCollector) Collect(ch chan<- prometheus.Metric) {
	for _, w := range c.collect() {
		labels := []string{w.ProviderID, labelOrUnknown(w.ProviderName), labelOrUnknown(w.Window)}
		ch <- prometheus.MustNewConstMetric(quotaUsedDesc, prometheus.GaugeValue, w.Used, labels...)
		if !w.ResetsAt.IsZero() {
			ch <- prometheus.MustNewConstMetric(quotaResetDesc, prometheus.GaugeValue, float64(w.ResetsAt.Unix()), labels...)
		}
	}
}

var _ prometheus.Collector = (*quotaCollector)(nil)

// InflightState is one provider's adaptive in-flight window for the gauges:
// the learned allowance (0 = uncapped) and the requests currently in flight.
type InflightState struct {
	ProviderID string
	Limit      int
	Inflight   int
}

// RegisterInflightCollector registers a scrape-time collector for the adaptive
// in-flight limiter, mirroring the breaker collector: collect runs on every
// scrape and must be cheap and non-blocking. Scrape-time rather than
// event-updated because the forget-to-uncapped transition is time-based and an
// event gauge would report a stale cap until the next request touched it.
func RegisterInflightCollector(collect func() []InflightState) {
	if collect == nil {
		return
	}
	registerInflightOnce.Do(func() {
		registry.MustRegister(&inflightCollector{collect: collect})
	})
}

var registerInflightOnce sync.Once

type inflightCollector struct {
	collect func() []InflightState
}

var (
	inflightLimitDesc = prometheus.NewDesc(
		"modelhotel_provider_inflight_limit",
		"Learned in-flight allowance per provider on this member (0 = uncapped).",
		[]string{"provider_id"}, nil,
	)
	inflightDesc = prometheus.NewDesc(
		"modelhotel_provider_inflight",
		"Requests currently in flight to each provider on this member.",
		[]string{"provider_id"}, nil,
	)
)

func (c *inflightCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- inflightLimitDesc
	ch <- inflightDesc
}

func (c *inflightCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.collect() {
		ch <- prometheus.MustNewConstMetric(inflightLimitDesc, prometheus.GaugeValue, float64(s.Limit), s.ProviderID)
		ch <- prometheus.MustNewConstMetric(inflightDesc, prometheus.GaugeValue, float64(s.Inflight), s.ProviderID)
	}
}

var _ prometheus.Collector = (*inflightCollector)(nil)
