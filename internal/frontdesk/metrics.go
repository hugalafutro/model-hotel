package frontdesk

// Prometheus /metrics for the Front Desk control plane. Front Desk is never in
// the proxied-request path, so none of the main gateway's request/token metrics
// apply here; the exposed series cover the control-plane domain instead: member
// fleet state, poll-loop timing, config-sync outcomes, and alert dispatch.
//
// Labels are deliberately low-cardinality (member names yes, never request
// identifiers) and no member secrets ever reach a metric, consistent with the
// no-content logging rule.

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// fdRegistry is a private registry so Front Desk's metrics are isolated from
// any global default and tests can scrape a known instance.
var fdRegistry = prometheus.NewRegistry()

var (
	fdPollDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "frontdesk_poll_duration_seconds",
		Help:    "Duration of one poller pass over all members, by poll kind.",
		Buckets: prometheus.DefBuckets,
	}, []string{"kind"})

	fdConfigSyncTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "frontdesk_config_sync_total",
		Help: "Per-member config-sync outcomes (wizard and auto-sync). \"superseded\" is a benign commit-fence refusal, not a failure.",
	}, []string{"result"})

	fdAlertsDispatchedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "frontdesk_alerts_dispatched_total",
		Help: "Outbound Apprise notification attempts by result.",
	}, []string{"result"})
)

func init() {
	fdRegistry.MustRegister(
		fdPollDuration,
		fdConfigSyncTotal,
		fdAlertsDispatchedTotal,
		fdMemberCollector,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// observePollDuration records one poll pass. kind is a fixed enum (health /
// traefik), never derived from member data.
func observePollDuration(kind string, d time.Duration) {
	fdPollDuration.WithLabelValues(kind).Observe(d.Seconds())
}

// recordConfigSync counts one member's sync outcome at the single seam both the
// wizard and the auto-syncer share (applyMemberConfig).
func recordConfigSync(result string) { fdConfigSyncTotal.WithLabelValues(result).Inc() }

// recordAlertDispatch counts one outbound notification attempt; wired into the
// shared alert dispatcher via alert.WithResultHook.
func recordAlertDispatch(ok bool) {
	result := "ok"
	if !ok {
		result = "err"
	}
	fdAlertsDispatchedTotal.WithLabelValues(result).Inc()
}

// memberMetricState is one member's scrape-time snapshot for the collector:
// store facts (state, last config sync) merged with the poller's live health
// cache. HealthKnown gates the up/latency series so a never-probed member does
// not report a fabricated "down".
type memberMetricState struct {
	Name             string
	State            MemberState
	HealthKnown      bool
	Up               bool
	LatencySeconds   float64
	LastConfigSyncAt *time.Time
}

// fdMemberCollector reports the member-fleet gauges at scrape time (rather
// than from events) so they always reflect current state; an event-updated
// gauge would go stale on missed transitions and removed members. The source
// func is swappable because it is bound to a Server's store and poller: the
// most recently constructed Server owns the series (one server per process in
// production; tests rebind freely).
var fdMemberCollector = &frontdeskCollector{}

type frontdeskCollector struct {
	mu      sync.RWMutex
	collect func() []memberMetricState
}

// setMemberMetricsSource binds the scrape-time source; nil is ignored.
func setMemberMetricsSource(collect func() []memberMetricState) {
	if collect == nil {
		return
	}
	fdMemberCollector.mu.Lock()
	fdMemberCollector.collect = collect
	fdMemberCollector.mu.Unlock()
}

var (
	fdMembersDesc = prometheus.NewDesc(
		"frontdesk_members_total",
		"Number of fleet members by state.",
		[]string{"state"}, nil,
	)
	fdMemberUpDesc = prometheus.NewDesc(
		"frontdesk_member_up",
		"Whether the member's last health probe succeeded (1 up, 0 down).",
		[]string{"member"}, nil,
	)
	fdMemberLatencyDesc = prometheus.NewDesc(
		"frontdesk_member_health_latency_seconds",
		"Latency of the member's last health probe in seconds.",
		[]string{"member"}, nil,
	)
	fdLastSyncDesc = prometheus.NewDesc(
		"frontdesk_last_config_sync_timestamp_seconds",
		"Unix timestamp of the member's last applied config sync.",
		[]string{"member"}, nil,
	)
)

func (c *frontdeskCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- fdMembersDesc
	ch <- fdMemberUpDesc
	ch <- fdMemberLatencyDesc
	ch <- fdLastSyncDesc
}

func (c *frontdeskCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.RLock()
	collect := c.collect
	c.mu.RUnlock()
	if collect == nil {
		return
	}

	states := collect()
	counts := map[MemberState]int{StateActive: 0, StateDrained: 0}
	for _, s := range states {
		counts[s.State]++
	}
	for state, n := range counts {
		ch <- prometheus.MustNewConstMetric(fdMembersDesc, prometheus.GaugeValue, float64(n), string(state))
	}
	for _, s := range states {
		if s.HealthKnown {
			up := 0.0
			if s.Up {
				up = 1.0
			}
			ch <- prometheus.MustNewConstMetric(fdMemberUpDesc, prometheus.GaugeValue, up, s.Name)
			ch <- prometheus.MustNewConstMetric(fdMemberLatencyDesc, prometheus.GaugeValue, s.LatencySeconds, s.Name)
		}
		if s.LastConfigSyncAt != nil {
			ch <- prometheus.MustNewConstMetric(fdLastSyncDesc, prometheus.GaugeValue, float64(s.LastConfigSyncAt.Unix()), s.Name)
		}
	}
}

// compile-time guard: the collector implements prometheus.Collector.
var _ prometheus.Collector = (*frontdeskCollector)(nil)

// collectMemberMetricsTimeout bounds the scrape-time member listing so a slow
// store read cannot hang a Prometheus scrape.
const collectMemberMetricsTimeout = 2 * time.Second

// collectMemberMetrics is the Server-bound scrape-time source: the store's
// member list joined with the poller's live health snapshot. It is called on
// every scrape; a store error yields an empty scrape rather than stale data.
func (s *Server) collectMemberMetrics() []memberMetricState {
	ctx, cancel := context.WithTimeout(context.Background(), collectMemberMetricsTimeout)
	defer cancel()

	members, err := s.store.ListMembers(ctx)
	if err != nil {
		return nil
	}
	statuses := s.poller.Snapshot()
	out := make([]memberMetricState, 0, len(members))
	for _, m := range members {
		st := statuses[m.ID]
		out = append(out, memberMetricState{
			Name:             m.Name,
			State:            m.State,
			HealthKnown:      st.Health.Known,
			Up:               st.Health.Healthy,
			LatencySeconds:   float64(st.Health.LatencyMs) / 1000,
			LastConfigSyncAt: m.LastConfigSyncAt,
		})
	}
	return out
}

// metricsHTTPHandler serves the registry in Prometheus text exposition format.
// The caller is responsible for authenticating the route.
func metricsHTTPHandler() http.Handler {
	return promhttp.HandlerFor(fdRegistry, promhttp.HandlerOpts{})
}
