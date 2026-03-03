package limit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// newBenchLimiter creates an IPLimiter suitable for benchmarks:
// - writes logs to temp dir
// - enforces defaults via Allow (drop mode)
// - high burst to avoid 429s during allow benchmarks
func newBenchLimiter(b *testing.B, rps float64, burst int, opts ...Option) *IPLimiter {
	b.Helper()
	dir := b.TempDir()
	logPath := filepath.Join(dir, "rate-limit.jsonl")

	all := []Option{
		WithLogPath(logPath),
		WithDefaultActionDrop(),
	}
	all = append(all, opts...)

	l := New(RateSpec{RPS: rps, Burst: burst}, nil, all...)
	b.Cleanup(func() { _ = l.Close() })
	return l
}

// dummyHandler is a no-op handler used as the next handler in middleware chains.
var dummyHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

// BenchmarkLimiterAllow measures the hot path: single-IP Allow check
// through the full middleware. Uses a high burst to avoid 429s.
func BenchmarkLimiterAllow(b *testing.B) {
	b.ReportAllocs()
	l := newBenchLimiter(b, 1e9, 1<<30) // effectively unlimited
	handler := l.Middleware(dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
	req.RemoteAddr = "10.0.0.1:12345"

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

// BenchmarkLimiterAllow_Parallel measures concurrent Allow throughput
// from GOMAXPROCS goroutines against distinct IPs.
func BenchmarkLimiterAllow_Parallel(b *testing.B) {
	b.ReportAllocs()
	l := newBenchLimiter(b, 1e9, 1<<30)
	handler := l.Middleware(dummyHandler)

	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		id := counter.Add(1)
		ip := fmt.Sprintf("10.0.%d.%d", (id/256)%256, id%256)
		req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
		req.RemoteAddr = ip + ":12345"

		for pb.Next() {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}

// BenchmarkLimiterNewIP measures the cold path: first request from a
// never-before-seen IP (creates a new rate.Limiter via LoadOrStore).
func BenchmarkLimiterNewIP(b *testing.B) {
	b.ReportAllocs()
	l := newBenchLimiter(b, 1e9, 1<<30)
	handler := l.Middleware(dummyHandler)

	b.ResetTimer()
	for i := range b.N {
		ip := fmt.Sprintf("10.%d.%d.%d", (i/65536)%256, (i/256)%256, i%256)
		req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
		req.RemoteAddr = ip + ":12345"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

// BenchmarkLimiterAllow_WithTrustedProxy measures the overhead of
// X-Forwarded-For resolution when a trusted proxy is configured.
func BenchmarkLimiterAllow_WithTrustedProxy(b *testing.B) {
	b.ReportAllocs()
	l := newBenchLimiter(b, 1e9, 1<<30,
		WithTrustProxy(true),
		WithTrustedProxies([]string{"192.168.0.0/16"}),
	)
	handler := l.Middleware(dummyHandler)

	req := httptest.NewRequest(http.MethodGet, "/rpc", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.42, 192.168.1.1")

	b.ResetTimer()
	for b.Loop() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}
}

// BenchmarkLimiterCleanup measures sweep/eviction cost under a
// populated pool of stale entries.
func BenchmarkLimiterCleanup(b *testing.B) {
	b.ReportAllocs()
	l := newBenchLimiter(b, 1e9, 1<<30)

	// populate 1000 IPs in the pool
	for i := range 1000 {
		ip := fmt.Sprintf("10.%d.%d.%d", (i/65536)%256, (i/256)%256, i%256)
		_ = l.limiterFor(ip)
	}

	b.ResetTimer()
	for b.Loop() {
		l.sweep()
	}
}
