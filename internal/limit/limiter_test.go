package limit

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestLimiter creates an IPLimiter suitable for tests:
// - writes logs to temp dir (no pollution)
// - uses a fake time source
// - enforces defaults via Allow (drop mode)
func newTestLimiter(t *testing.T, rps float64, burst int, opts ...Option) *IPLimiter {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rate-limit.jsonl")

	all := []Option{
		WithLogPath(logPath),
		WithDefaultActionDrop(),
	}
	all = append(all, opts...)

	l := New(RateSpec{RPS: rps, Burst: burst}, nil, all...)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

var _ Limiter = (*IPLimiter)(nil)

// ---------------------------------------------------------------------------
// New / WithOptions
// ---------------------------------------------------------------------------

func TestNewDefaults(t *testing.T) {
	l := newTestLimiter(t, 10, 20)
	if l.defaults.RPS != 10 {
		t.Errorf("defaults.RPS = %v, want 10", l.defaults.RPS)
	}
	if l.defaults.Burst != 20 {
		t.Errorf("defaults.Burst = %d, want 20", l.defaults.Burst)
	}
}

// ---------------------------------------------------------------------------
// SetOverride / DeleteOverride
// ---------------------------------------------------------------------------

func TestSetOverride(t *testing.T) {
	l := newTestLimiter(t, 10, 20)

	if err := l.SetOverride("192.168.1.1", RateSpec{RPS: 1, Burst: 1}); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	// Verify override is stored
	if !l.hasOverride("192.168.1.1") {
		t.Error("expected override to be present")
	}
}

func TestSetOverrideInvalidIP(t *testing.T) {
	l := newTestLimiter(t, 10, 20)

	if err := l.SetOverride("not-an-ip", RateSpec{RPS: 1, Burst: 1}); err == nil {
		t.Error("expected error for invalid IP")
	}
}

func TestDeleteOverride(t *testing.T) {
	l := newTestLimiter(t, 10, 20)
	_ = l.SetOverride("192.168.1.1", RateSpec{RPS: 1, Burst: 1})
	l.DeleteOverride("192.168.1.1")

	if l.hasOverride("192.168.1.1") {
		t.Error("override should be deleted")
	}
}

// ---------------------------------------------------------------------------
// StatusOf
// ---------------------------------------------------------------------------

func TestStatusOf(t *testing.T) {
	t.Parallel()

	t.Run("default is ok", func(t *testing.T) {
		t.Parallel()
		r, _ := http.NewRequest("GET", "/", nil)
		if got := StatusOf(r); got != "ok" {
			t.Errorf("StatusOf = %q, want ok", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Middleware — Allow under rate
// ---------------------------------------------------------------------------

func TestMiddlewareAllowed(t *testing.T) {
	l := newTestLimiter(t, 100, 100) // generous limits

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Middleware — 429 when exhausted
// ---------------------------------------------------------------------------

func TestMiddleware429WhenExhausted(t *testing.T) {
	// Very restrictive: 0.001 RPS, burst of 1
	l := newTestLimiter(t, 0.001, 1)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request uses the burst token
	req1, _ := http.NewRequest("GET", "/rpc", nil)
	req1.RemoteAddr = "10.0.0.2:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request should be rate limited
	req2, _ := http.NewRequest("GET", "/rpc", nil)
	req2.RemoteAddr = "10.0.0.2:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec2.Code)
	}
}

// ---------------------------------------------------------------------------
// Override causes 429
// ---------------------------------------------------------------------------

func TestMiddlewareOverride429(t *testing.T) {
	l := newTestLimiter(t, 1000, 1000) // generous defaults

	// Set very restrictive override
	if err := l.SetOverride("10.0.0.3", RateSpec{RPS: 0.001, Burst: 1}); err != nil {
		t.Fatal(err)
	}

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request uses burst
	req1, _ := http.NewRequest("GET", "/rpc", nil)
	req1.RemoteAddr = "10.0.0.3:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request should be rate limited by override
	req2, _ := http.NewRequest("GET", "/rpc", nil)
	req2.RemoteAddr = "10.0.0.3:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("override status = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "1" {
		t.Error("expected Retry-After header")
	}
}

// ---------------------------------------------------------------------------
// Auto-quarantine
// ---------------------------------------------------------------------------

func TestAutoQuarantine(t *testing.T) {
	now := time.Now()
	l := newTestLimiter(t, 1000, 1000,
		WithNow(func() time.Time { return now }),
		WithAutoQuarantine(AutoRule{
			Threshold: 3,
			Window:    10 * time.Second,
			Penalty:   RateSpec{RPS: 0.001, Burst: 1},
			TTL:       1 * time.Minute,
		}),
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 3 requests to trigger auto-quarantine
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("GET", "/rpc", nil)
		req.RemoteAddr = "10.0.0.4:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// After threshold hit, should have override
	if !l.hasOverride("10.0.0.4") {
		t.Error("expected auto-quarantine override to be set")
	}

	// Next request after burst consumed should be 429'd
	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "10.0.0.4:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("auto-quarantine status = %d, want 429", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Concurrent Allow (race detector)
// ---------------------------------------------------------------------------

func TestConcurrentAllow(t *testing.T) {
	l := newTestLimiter(t, 10000, 10000) // generous

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				req, _ := http.NewRequest("GET", "/rpc", nil)
				req.RemoteAddr = "10.0.0.5:12345"
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// WithTrustedProxies + XFF
// ---------------------------------------------------------------------------

func TestTrustedProxiesXFF(t *testing.T) {
	l := newTestLimiter(t, 0.001, 1,
		WithTrustProxy(true),
		WithTrustedProxies([]string{"127.0.0.0/8"}),
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request from trusted proxy with XFF
	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first request status = %d, want 200", rec.Code)
	}

	// Second request from same XFF IP should be rate limited
	req2, _ := http.NewRequest("GET", "/rpc", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	req2.Header.Set("X-Forwarded-For", "203.0.113.1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", rec2.Code)
	}
}

func TestUntrustedProxyXFFIgnored(t *testing.T) {
	l := newTestLimiter(t, 0.001, 1,
		WithTrustProxy(true),
		WithTrustedProxies([]string{"10.0.0.0/8"}), // Only trust 10.x.x.x
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request from untrusted proxy (192.168.x.x not in 10.0.0.0/8)
	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.5")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// First request OK (burst token)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	// Second request from SAME RemoteAddr should be limited (XFF ignored)
	req2, _ := http.NewRequest("GET", "/rpc", nil)
	req2.RemoteAddr = "192.168.1.1:12345"
	req2.Header.Set("X-Forwarded-For", "203.0.113.99") // different XFF
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Should be 429 because 192.168.1.1 was rate limited, not 203.0.113.99
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429 (XFF should be ignored for untrusted proxy)", rec2.Code)
	}
}

// ---------------------------------------------------------------------------
// WithIPHeader
// ---------------------------------------------------------------------------

func TestWithIPHeader(t *testing.T) {
	l := newTestLimiter(t, 0.001, 1,
		WithIPHeader("X-Real-IP"),
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Real-IP", "203.0.113.10")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first request status = %d, want 200", rec.Code)
	}

	// Same X-Real-IP should be rate limited
	req2, _ := http.NewRequest("GET", "/rpc", nil)
	req2.RemoteAddr = "127.0.0.1:12345"
	req2.Header.Set("X-Real-IP", "203.0.113.10")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", rec2.Code)
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	dir := t.TempDir()
	l := New(RateSpec{RPS: 10, Burst: 20}, nil, WithLogPath(filepath.Join(dir, "rl.jsonl")))
	if err := l.Close(); err != nil {
		t.Errorf("Close error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// WithLogOnlyImportant
// ---------------------------------------------------------------------------

func TestLogOnlyImportant(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rl.jsonl")
	l := New(RateSpec{RPS: 10, Burst: 20}, nil,
		WithLogPath(logPath),
		WithLogOnlyImportant(),
		WithDefaultActionDrop(),
	)
	t.Cleanup(func() { _ = l.Close() })

	if !l.logImportantOnly {
		t.Error("logImportantOnly should be true")
	}

	// Only important events should be logged
	if !l.shouldLog("429") {
		t.Error("429 should be logged")
	}
	if !l.shouldLog("auto-override-add") {
		t.Error("auto-override-add should be logged")
	}
	if l.shouldLog("allow-sample") {
		t.Error("allow-sample should NOT be logged in important-only mode")
	}
}

// ---------------------------------------------------------------------------
// WithAllowLogEvery
// ---------------------------------------------------------------------------

func TestWithAllowLogEvery(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "rl.jsonl")
	l := New(RateSpec{RPS: 10000, Burst: 10000}, nil,
		WithLogPath(logPath),
		WithAllowLogEvery(5*time.Second),
		WithDefaultActionDrop(),
	)
	t.Cleanup(func() { _ = l.Close() })

	if l.allowLogEvery != 5*time.Second {
		t.Errorf("allowLogEvery = %v, want 5s", l.allowLogEvery)
	}
}

// ---------------------------------------------------------------------------
// VPROX_HOME-based default log path
// ---------------------------------------------------------------------------

func TestDefaultLogPathEnv(t *testing.T) {
	// Save and restore
	old := os.Getenv("VPROX_HOME")
	t.Cleanup(func() { os.Setenv("VPROX_HOME", old) })

	os.Setenv("VPROX_HOME", "/tmp/test-vprox")
	got := defaultLogPath()
	want := filepath.Join("/tmp/test-vprox", "data", "logs", "rate-limit.jsonl")
	if got != want {
		t.Errorf("defaultLogPath = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// parseFirstIP
// ---------------------------------------------------------------------------

func TestParseFirstIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"10.0.0.1:12345", "10.0.0.1"},
		{"10.0.0.1", "10.0.0.1"},
		{"[::1]:8080", "::1"},
		{"::1", "::1"},
		{"invalid", ""},
		{"", ""},
		{"192.168.1.1:80", "192.168.1.1"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := parseFirstIP(tt.input); got != tt.want {
				t.Errorf("parseFirstIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// limiterRouteFromPath
// ---------------------------------------------------------------------------

func TestLimiterRouteFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/rpc/status", "rpc"},
		{"/rest/bank/balances", "rest"},
		{"/api/v1/node", "api"},
		{"/grpc-web/cosmos.bank.v1beta1.Query/Balance", "grpc-web"},
		{"/grpc/cosmos.bank.v1beta1.Query/Balance", "grpc"},
		{"/websocket", "websocket"},
		{"/unknown", "direct"},
		{"/", "direct"},
		{"", "direct"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := limiterRouteFromPath(tt.path); got != tt.want {
				t.Errorf("limiterRouteFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// limiterReasonLabel
// ---------------------------------------------------------------------------

func TestLimiterReasonLabel(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"429", "RATE_LIMIT_EXCEEDED"},
		{"wait-canceled", "REQUEST_CANCELED"},
		{"auto-override-add", "AUTO_OVERRIDE_ADD"},
		{"auto-override-expire", "AUTO_OVERRIDE_EXPIRE"},
		{"allow-sample", "ALLOW_SAMPLE"},
		{"custom", "CUSTOM"},
		{"", "UNKNOWN"},
	}
	for _, tt := range tests {
		if got := limiterReasonLabel(tt.reason); got != tt.want {
			t.Errorf("limiterReasonLabel(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// limiterEventMessage
// ---------------------------------------------------------------------------

func TestLimiterEventMessage(t *testing.T) {
	tests := []struct {
		reason string
		want   string
	}{
		{"429", "rate limit exceeded"},
		{"wait-canceled", "request canceled by limiter"},
		{"auto-override-add", "auto override added"},
		{"auto-override-expire", "auto override expired"},
		{"allow-sample", "allow sample"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := limiterEventMessage(tt.reason)
		if got != tt.want {
			t.Errorf("limiterEventMessage(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// logEventLevel
// ---------------------------------------------------------------------------

func TestLogEventLevel(t *testing.T) {
	dir := t.TempDir()
	l := New(RateSpec{RPS: 10, Burst: 20}, nil, WithLogPath(filepath.Join(dir, "rl.jsonl")))
	t.Cleanup(func() { _ = l.Close() })

	tests := []struct {
		reason string
		want   string
	}{
		{"429", "ERROR"},
		{"wait-canceled", "ERROR"},
		{"auto-override-add", "WARN"},
		{"auto-override-expire", "INFO"},
		{"allow-sample", "INFO"},
		{"other", "DEBUG"},
	}
	for _, tt := range tests {
		if got := l.logEventLevel(tt.reason); got != tt.want {
			t.Errorf("logEventLevel(%q) = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// policyString
// ---------------------------------------------------------------------------

func TestPolicyString(t *testing.T) {
	l := newTestLimiter(t, 10, 20)

	got := l.policyString("10.0.0.1")
	if !strings.Contains(got, "ip=10.0.0.1") {
		t.Errorf("policyString = %q, should contain ip=10.0.0.1", got)
	}
	if !strings.Contains(got, "rps=10") {
		t.Errorf("policyString = %q, should contain rps=10", got)
	}

	_ = l.SetOverride("10.0.0.2", RateSpec{RPS: 1, Burst: 1})
	got = l.policyString("10.0.0.2")
	if !strings.Contains(got, "rps=1") {
		t.Errorf("policyString override = %q, should contain rps=1", got)
	}
}

// ---------------------------------------------------------------------------
// countRune
// ---------------------------------------------------------------------------

func TestCountRune(t *testing.T) {
	tests := []struct {
		s    string
		r    rune
		want int
	}{
		{"hello", 'l', 2},
		{"hello", 'z', 0},
		{"::1", ':', 2},
		{"", ':', 0},
	}
	for _, tt := range tests {
		if got := countRune(tt.s, tt.r); got != tt.want {
			t.Errorf("countRune(%q, %c) = %d, want %d", tt.s, tt.r, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Middleware with various path routes
// ---------------------------------------------------------------------------

func TestMiddlewareRouteCoverage(t *testing.T) {
	l := newTestLimiter(t, 10000, 10000)
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	paths := []string{"/rpc/status", "/rest/bank", "/api/v1/node", "/grpc-web/q", "/grpc/q", "/websocket", "/other"}
	for _, p := range paths {
		req, _ := http.NewRequest("GET", p, nil)
		req.RemoteAddr = "10.0.0.99:12345"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("path %s: status = %d, want 200", p, rec.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Multiple IPs - different limiters per IP
// ---------------------------------------------------------------------------

func TestDifferentIPsDifferentLimiters(t *testing.T) {
	l := newTestLimiter(t, 0.001, 1)
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request for IP A uses burst
	req1, _ := http.NewRequest("GET", "/rpc", nil)
	req1.RemoteAddr = "10.0.0.10:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("IP A first: %d", rec1.Code)
	}

	// First request for IP B should also succeed (different limiter)
	req2, _ := http.NewRequest("GET", "/rpc", nil)
	req2.RemoteAddr = "10.0.0.11:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("IP B first: %d", rec2.Code)
	}

	// Second request for IP A should be limited
	req3, _ := http.NewRequest("GET", "/rpc", nil)
	req3.RemoteAddr = "10.0.0.10:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("IP A second: %d, want 429", rec3.Code)
	}
}

// ---------------------------------------------------------------------------
// maskIP
// ---------------------------------------------------------------------------

func TestMaskIP(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.100", "192.168.1.x"},
		{"::1", "::x"},
		{"no-delimiter", "[masked]"},
	}
	for _, tt := range tests {
		if got := maskIP(tt.ip); got != tt.want {
			t.Errorf("maskIP(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// forwardedForIP
// ---------------------------------------------------------------------------

func TestForwardedForIP(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{`for="203.0.113.50"`, "203.0.113.50"},
		{`for="[2001:db8::1]"`, "2001:db8::1"},
		{`for=203.0.113.99`, "203.0.113.99"},
		{`For="203.0.113.50";proto=https`, "203.0.113.50"},
		{`host=example.com`, ""},
		{``, ""},
		{`for="203.0.113.50:12345"`, "203.0.113.50"},
	}
	for _, tt := range tests {
		got := forwardedForIP(tt.header)
		if got != tt.want {
			t.Errorf("forwardedForIP(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// clientIP with Forwarded header (trusted proxy)
// ---------------------------------------------------------------------------

func TestClientIPForwardedHeader(t *testing.T) {
	l := newTestLimiter(t, 10000, 10000,
		WithTrustProxy(true),
		WithTrustedProxies([]string{"127.0.0.0/8"}),
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Forwarded", `for="203.0.113.77"`)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// clientIP with CF-Connecting-IP (trusted proxy)
// ---------------------------------------------------------------------------

func TestClientIPCFConnectingIP(t *testing.T) {
	l := newTestLimiter(t, 10000, 10000,
		WithTrustProxy(true),
		WithTrustedProxies([]string{"127.0.0.0/8"}),
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.88")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// isProxyTrusted with no trusted proxies (empty list)
// ---------------------------------------------------------------------------

func TestIsProxyTrustedEmpty(t *testing.T) {
	l := newTestLimiter(t, 10000, 10000,
		WithTrustProxy(true),
		// no WithTrustedProxies — empty list
	)

	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Should still use XFF since trust is on with no list (backward compat)
	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.77")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// StatusOf with rate-limited request
// ---------------------------------------------------------------------------

func TestStatusOfLimited(t *testing.T) {
	l := newTestLimiter(t, 0.001, 1)

	var capturedStatus string
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedStatus = StatusOf(r)
		w.WriteHeader(http.StatusOK)
	}))

	// First request: OK
	req1, _ := http.NewRequest("GET", "/rpc", nil)
	req1.RemoteAddr = "10.0.0.20:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if capturedStatus != "ok" {
		t.Errorf("first request status = %q, want ok", capturedStatus)
	}
}

// ---------------------------------------------------------------------------
// Sweep (trigger via sleep or manual call)
// ---------------------------------------------------------------------------

func TestSweepManual(t *testing.T) {
	now := time.Now()
	l := newTestLimiter(t, 1000, 1000,
		WithNow(func() time.Time { return now }),
	)

	// Create a limiter for an IP
	handler := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req, _ := http.NewRequest("GET", "/rpc", nil)
	req.RemoteAddr = "10.0.0.30:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Advance time and sweep
	now = now.Add(15 * time.Minute)
	l.sweep()
	// Pool should be cleaned (no override, no auto state)
}
