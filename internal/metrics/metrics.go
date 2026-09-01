// Package metrics exposes Model Hotel's Prometheus metrics: a private registry,
// the request-outcome collectors, a circuit-breaker-state collector, and the
// HTTP handler that serves the /metrics endpoint.
//
// Labels are deliberately low-cardinality — provider and model names yes,
// virtual-key IDs or request IDs never. No prompt/request/response content ever
// reaches a metric (consistent with the no-content logging rule).
package metrics

import (
	"net/http"
	"sync"

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

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "modelhotel_request_duration_seconds",
		Help:    "End-to-end proxied request duration in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider", "model"})

	ttftSeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "modelhotel_ttft_seconds",
		Help:    "Time to first token for streaming requests, in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider", "model"})

	tokensTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_tokens_total",
		Help: "Total tokens metered by provider, model, and kind (prompt/completion/reasoning).",
	}, []string{"provider", "model", "kind"})

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
	// FailoverProviders names the provider of every attempt after the first
	// (hedged launches included), in attempt order: one failover attempt each.
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
	for _, p := range o.FailoverProviders {
		failoverAttemptsTotal.WithLabelValues(model, labelOrUnknown(p)).Inc()
	}
}

// RecordUpstreamRateLimit counts one upstream 429 by the class the classifier
// assigned it ("saturated", "exhausted", "unknown"). The caller owns that
// vocabulary, as with RecordRetirementProbe: the class type lives in the proxy.
// This is the counter that shows a provider's slot ceiling as a flat line of
// saturated, where the request counter shows only 429s.
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

// RecordResponsesReroute counts one attempt routed to /v1/responses. mode is
// "learned" when the route was discovered by healing a live 400, "preemptive"
// when the cached requirement redirected the attempt up front.
func RecordResponsesReroute(provider, model, mode string) {
	responsesRerouteTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), mode).Inc()
}

// RecordRetirementProbe counts one completed pre-retirement probe. verdict is
// the probe's own name for what it established ("refused", "served",
// "inconclusive"); the caller owns that vocabulary, because the verdict type
// lives in the proxy and this package must stay importable by it.
//
// One series per (provider, model, verdict), and only for models the gateway
// actually probed: a probe is rate-limited to one per model per cooldown, and
// only a model drawing repeated gone-classified refusals is ever nominated. The
// bound is the catalog, and in practice a small fraction of it.
//
// The model label is the PROVIDER-SIDE id, while requestsTotal carries the name
// the CLIENT asked for. For direct "provider/model" traffic those are the same
// string — resolution matched the model row on that exact id, and the request
// log keeps the post-slash part — so the two counters join. They diverge on
// exactly two shapes: a request routed through a failover group is "hotel/<group>"
// on requestsTotal and the real id here, and a validation failure is collapsed
// there to "unresolved". A PromQL join is therefore sound but silently
// incomplete, missing precisely the group-routed traffic.
//
// Counting VERDICTS rather than retirements is deliberate. A retirement is
// visible in the model row and in the model.auto_disabled_gone event; what
// neither records is the probe that did NOT retire anything — a "served" is the
// classifier having nominated a live model, and a run of "inconclusive" is the
// gateway paying for an answer it is not getting. Those two are the reason this
// metric exists, and they leave no other trace than a log line.
//
// A refused verdict is not the same thing as a completed retirement: the write
// that follows can still be superseded by a success, refused by the repository,
// or reverted. Alert on the ratio between verdicts, not on refused as a
// retirement count.
func RecordRetirementProbe(provider, model, verdict string) {
	retirementProbesTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), verdict).Inc()
}

// Handler returns the HTTP handler that serves the metrics in Prometheus text
// exposition format. The caller is responsible for authenticating the route.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// statusClass buckets an HTTP status into a low-cardinality label. 499 (client
// closed request) is kept distinct so client disconnects are visible and not
// conflated with provider 4xx.
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
	ProviderID string
	State      int
}

// State numeric encoding for modelhotel_circuit_breaker_state.
const (
	BreakerClosed   = 0
	BreakerHalfOpen = 1
	BreakerOpen     = 2
)

// RegisterBreakerCollector registers a scrape-time collector that reports the
// circuit-breaker state per provider. collect is called on every scrape and
// must be cheap and non-blocking; it returns the current states. Passing nil,
// or calling more than once, is a no-op after the first registration.
//
// A scrape-time collector (rather than an event-updated gauge) is used because
// the open→half-open transition is time-based and would otherwise be missed.
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
	"Circuit breaker state per provider (0 closed, 1 half-open, 2 open).",
	[]string{"provider_id"}, nil,
)

func (c *breakerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- breakerDesc
}

func (c *breakerCollector) Collect(ch chan<- prometheus.Metric) {
	for _, s := range c.collect() {
		ch <- prometheus.MustNewConstMetric(breakerDesc, prometheus.GaugeValue, float64(s.State), s.ProviderID)
	}
}

// compile-time guard: the collector implements prometheus.Collector.
var _ prometheus.Collector = (*breakerCollector)(nil)

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
