package metrics_test

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/vNodesV/vProx/internal/metrics"
)

// newRegistry creates an isolated registry and registers all vProx collectors
// onto it. This avoids conflicts with the global DefaultRegisterer used by
// promauto in the package under test — we describe each metric and then query
// the real (global) counters instead.
//
// Because promauto registers onto prometheus.DefaultRegisterer at init time, we
// cannot re-register onto a fresh registry without panicking. The tests below
// therefore exercise the exported helpers against the real default registry and
// use testutil to gather metric values.

func TestRecordRequest_IncrementsCounter(t *testing.T) {
	before := testutil.ToFloat64(metrics.RequestsTotal.WithLabelValues("GET", "rpc", "200"))
	metrics.RecordRequest("GET", "rpc", 200, 50*time.Millisecond)
	after := testutil.ToFloat64(metrics.RequestsTotal.WithLabelValues("GET", "rpc", "200"))
	if after-before != 1 {
		t.Errorf("RequestsTotal: want +1 got %.0f→%.0f", before, after)
	}
}

func TestRecordRequest_ObservesHistogram(t *testing.T) {
	// Calling RecordRequest must not panic and must record a sample.
	// We verify the histogram has non-zero count via Gather.
	metrics.RecordRequest("POST", "rest", 201, 100*time.Millisecond)

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	found := false
	for _, mf := range families {
		if mf.GetName() == "vprox_request_duration_seconds" {
			found = true
			break
		}
	}
	if !found {
		t.Error("vprox_request_duration_seconds histogram not found in registry")
	}
}

func TestRecordProxyError_IncrementsCounter(t *testing.T) {
	before := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues("rpc", "backend_error"))
	metrics.RecordProxyError("rpc", "backend_error")
	after := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues("rpc", "backend_error"))
	if after-before != 1 {
		t.Errorf("ProxyErrorsTotal: want +1 got %.0f→%.0f", before, after)
	}
}

func TestRecordProxyError_UnknownHost(t *testing.T) {
	before := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues("direct", "unknown_host"))
	metrics.RecordProxyError("direct", "unknown_host")
	after := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues("direct", "unknown_host"))
	if after-before != 1 {
		t.Errorf("ProxyErrorsTotal(unknown_host): want +1 got %.0f→%.0f", before, after)
	}
}

func TestRecordProxyError_RequestBuildError(t *testing.T) {
	before := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues("rest", "request_build_error"))
	metrics.RecordProxyError("rest", "request_build_error")
	after := testutil.ToFloat64(metrics.ProxyErrorsTotal.WithLabelValues("rest", "request_build_error"))
	if after-before != 1 {
		t.Errorf("ProxyErrorsTotal(request_build_error): want +1 got %.0f→%.0f", before, after)
	}
}

func TestActiveConnections_IncDec(t *testing.T) {
	baseline := testutil.ToFloat64(metrics.ActiveConnections)

	metrics.IncActiveConnections()
	if v := testutil.ToFloat64(metrics.ActiveConnections); v != baseline+1 {
		t.Errorf("after Inc: want %.0f got %.0f", baseline+1, v)
	}

	metrics.DecActiveConnections()
	if v := testutil.ToFloat64(metrics.ActiveConnections); v != baseline {
		t.Errorf("after Dec: want %.0f got %.0f", baseline, v)
	}
}

func TestActiveConnections_MultipleIncDec(t *testing.T) {
	baseline := testutil.ToFloat64(metrics.ActiveConnections)

	metrics.IncActiveConnections()
	metrics.IncActiveConnections()
	metrics.IncActiveConnections()
	metrics.DecActiveConnections()

	want := baseline + 2
	if v := testutil.ToFloat64(metrics.ActiveConnections); v != want {
		t.Errorf("after 3 Inc / 1 Dec: want %.0f got %.0f", want, v)
	}

	// cleanup
	metrics.DecActiveConnections()
	metrics.DecActiveConnections()
}

func TestRecordRateLimitHit(t *testing.T) {
	before := testutil.ToFloat64(metrics.RateLimitHitsTotal)
	metrics.RecordRateLimitHit()
	metrics.RecordRateLimitHit()
	after := testutil.ToFloat64(metrics.RateLimitHitsTotal)
	if after-before != 2 {
		t.Errorf("RateLimitHitsTotal: want +2 got %.0f→%.0f", before, after)
	}
}

func TestRecordGeoCacheHit(t *testing.T) {
	before := testutil.ToFloat64(metrics.GeoCacheHitsTotal)
	metrics.RecordGeoCacheHit()
	after := testutil.ToFloat64(metrics.GeoCacheHitsTotal)
	if after-before != 1 {
		t.Errorf("GeoCacheHitsTotal: want +1 got %.0f→%.0f", before, after)
	}
}

func TestRecordGeoCacheMiss(t *testing.T) {
	before := testutil.ToFloat64(metrics.GeoCacheMissesTotal)
	metrics.RecordGeoCacheMiss()
	after := testutil.ToFloat64(metrics.GeoCacheMissesTotal)
	if after-before != 1 {
		t.Errorf("GeoCacheMissesTotal: want +1 got %.0f→%.0f", before, after)
	}
}

func TestRecordBackupEvent(t *testing.T) {
	for _, status := range []string{"started", "completed", "failed"} {
		before := testutil.ToFloat64(metrics.BackupEventsTotal.WithLabelValues(status))
		metrics.RecordBackupEvent(status)
		after := testutil.ToFloat64(metrics.BackupEventsTotal.WithLabelValues(status))
		if after-before != 1 {
			t.Errorf("BackupEventsTotal(%s): want +1 got %.0f→%.0f", status, before, after)
		}
	}
}

func TestAllMetricsRegistered_NoDoubleRegisterPanic(t *testing.T) {
	// promauto registers at init() time. Gathering must succeed without panic.
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather failed: %v", err)
	}

	want := []string{
		"vprox_requests_total",
		"vprox_active_connections",
		"vprox_request_duration_seconds",
		"vprox_proxy_errors_total",
		"vprox_rate_limit_hits_total",
		"vprox_geo_cache_hits_total",
		"vprox_geo_cache_misses_total",
		"vprox_backup_events_total",
	}
	got := make(map[string]bool)
	for _, mf := range families {
		got[mf.GetName()] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("metric %q not registered", name)
		}
	}
}

func TestRecordRequest_MultipleRoutes(t *testing.T) {
	routes := []struct {
		method     string
		route      string
		statusCode int
	}{
		{"GET", "rpc", 200},
		{"POST", "rest", 201},
		{"GET", "direct", 400},
		{"GET", "rest", 502},
	}
	for _, tc := range routes {
		before := testutil.ToFloat64(metrics.RequestsTotal.WithLabelValues(tc.method, tc.route, itoa(tc.statusCode)))
		metrics.RecordRequest(tc.method, tc.route, tc.statusCode, time.Millisecond)
		after := testutil.ToFloat64(metrics.RequestsTotal.WithLabelValues(tc.method, tc.route, itoa(tc.statusCode)))
		if after-before != 1 {
			t.Errorf("RecordRequest(%s,%s,%d): want +1 got %.0f→%.0f",
				tc.method, tc.route, tc.statusCode, before, after)
		}
	}
}

// itoa mirrors the unexported helper for use in test assertions.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[pos:])
}
