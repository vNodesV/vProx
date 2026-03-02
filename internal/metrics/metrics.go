// Package metrics provides Prometheus instrumentation for vProx.
// All metrics are registered with prometheus.DefaultRegisterer via promauto.
// Other packages MUST NOT import this package directly — use the hook pattern
// exposed in internal/limit, internal/geo, and internal/backup instead.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// --------------------- COUNTERS ---------------------

// RequestsTotal counts every proxied HTTP request by method, route, and status code.
var RequestsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vprox_requests_total",
		Help: "Total number of proxied HTTP requests.",
	},
	[]string{"method", "route", "status_code"},
)

// ProxyErrorsTotal counts proxy-level errors by route and error type.
// error_type is one of: backend_error, request_build_error, unknown_host.
var ProxyErrorsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vprox_proxy_errors_total",
		Help: "Total proxy errors by route and error type.",
	},
	[]string{"route", "error_type"},
)

// RateLimitHitsTotal counts requests that received a 429 response.
var RateLimitHitsTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "vprox_rate_limit_hits_total",
		Help: "Total rate-limited (429) responses served.",
	},
)

// GeoCacheHitsTotal counts geo lookup cache hits.
var GeoCacheHitsTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "vprox_geo_cache_hits_total",
		Help: "Total geo lookup cache hits.",
	},
)

// GeoCacheMissesTotal counts geo lookup cache misses.
var GeoCacheMissesTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "vprox_geo_cache_misses_total",
		Help: "Total geo lookup cache misses.",
	},
)

// BackupEventsTotal counts backup lifecycle events.
// status is one of: started, completed, failed.
var BackupEventsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vprox_backup_events_total",
		Help: "Total backup lifecycle events by status.",
	},
	[]string{"status"},
)

// --------------------- GAUGE ---------------------

// ActiveConnections tracks the number of in-flight proxy connections.
var ActiveConnections = promauto.NewGauge(
	prometheus.GaugeOpts{
		Name: "vprox_active_connections",
		Help: "Number of currently active proxy connections.",
	},
)

// --------------------- HISTOGRAM ---------------------

// RequestDuration records the latency distribution of proxied requests
// by method and route.
var RequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "vprox_request_duration_seconds",
		Help:    "Proxy request duration in seconds.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	},
	[]string{"method", "route"},
)

// --------------------- HELPERS ---------------------

// RecordRequest increments RequestsTotal and observes RequestDuration.
// statusCode is the HTTP response status code returned to the client.
func RecordRequest(method, route string, statusCode int, duration time.Duration) {
	sc := itoa(statusCode)
	RequestsTotal.WithLabelValues(method, route, sc).Inc()
	RequestDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}

// RecordProxyError increments ProxyErrorsTotal for the given route and error type.
// Recognized error types: backend_error, request_build_error, unknown_host.
func RecordProxyError(route, errorType string) {
	ProxyErrorsTotal.WithLabelValues(route, errorType).Inc()
}

// IncActiveConnections increments the active connections gauge.
func IncActiveConnections() {
	ActiveConnections.Inc()
}

// DecActiveConnections decrements the active connections gauge.
func DecActiveConnections() {
	ActiveConnections.Dec()
}

// RecordRateLimitHit increments the rate-limit counter.
func RecordRateLimitHit() {
	RateLimitHitsTotal.Inc()
}

// RecordGeoCacheHit increments the geo cache hit counter.
func RecordGeoCacheHit() {
	GeoCacheHitsTotal.Inc()
}

// RecordGeoCacheMiss increments the geo cache miss counter.
func RecordGeoCacheMiss() {
	GeoCacheMissesTotal.Inc()
}

// RecordBackupEvent increments the backup events counter.
// status should be one of: started, completed, failed.
func RecordBackupEvent(status string) {
	BackupEventsTotal.WithLabelValues(status).Inc()
}

// itoa converts an integer to its decimal string representation.
// Inlined to avoid importing strconv solely for this.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte(n%10) + '0'
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
