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
		Help: "Total failover attempts beyond the first try, by model (or hotel group).",
	}, []string{"model"})

	responsesRerouteTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "modelhotel_responses_reroute_total",
		Help: "Attempts routed via the OpenAI Responses API instead of chat completions, by provider, model, and mode (learned = healed from a live 400, preemptive = cache-driven).",
	}, []string{"provider", "model", "mode"})
)

func init() {
	registry.MustRegister(
		requestsTotal,
		requestDuration,
		ttftSeconds,
		tokensTotal,
		failoverAttemptsTotal,
		responsesRerouteTotal,
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
	FailoverAttempt  int // 0-based index of the attempt that ended the request
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
	// FailoverAttempt is the 0-based index of the terminal attempt; any value
	// above zero means the request failed over at least once.
	if o.FailoverAttempt > 0 {
		failoverAttemptsTotal.WithLabelValues(model).Add(float64(o.FailoverAttempt))
	}
}

// RecordResponsesReroute counts one attempt routed to /v1/responses. mode is
// "learned" when the route was discovered by healing a live 400, "preemptive"
// when the cached requirement redirected the attempt up front.
func RecordResponsesReroute(provider, model, mode string) {
	responsesRerouteTotal.WithLabelValues(labelOrUnknown(provider), labelOrUnknown(model), mode).Inc()
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
