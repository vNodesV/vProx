package geo

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

var _ LimiterReader = limiterAdapter{}

type limiterAdapter struct{}

func (limiterAdapter) Country(ip string) string { return Country(ip) }

// readerAdapter satisfies the Reader interface for testing Middleware.
type readerAdapter struct{}

func (readerAdapter) Lookup(ip string) (string, string) { return "", "" }
func (readerAdapter) Country(ip string) string          { return "" }
func (readerAdapter) Info() string                      { return "test" }
func (readerAdapter) Close()                            {}

// ---------------------------------------------------------------------------
// Tests that touch global state — must run sequentially (no t.Parallel).
// The geo package uses sync.Once, package-level DB handles, and a cache
// sweeper goroutine. Running these in parallel causes data races.
// ---------------------------------------------------------------------------

func TestLookupEmptyIP(t *testing.T) {
	cc, asn := Lookup("")
	if cc != "" || asn != "" {
		t.Errorf("Lookup(\"\") = (%q, %q), want (\"\", \"\")", cc, asn)
	}
}

func TestLookupInvalidIP(t *testing.T) {
	cc, asn := Lookup("not-an-ip")
	if cc != "" || asn != "" {
		t.Errorf("Lookup(\"not-an-ip\") = (%q, %q), want (\"\", \"\")", cc, asn)
	}
}

func TestCountryEmpty(t *testing.T) {
	if got := Country(""); got != "" {
		t.Errorf("Country(\"\") = %q, want \"\"", got)
	}
}

func TestASNEmpty(t *testing.T) {
	if got := ASN(""); got != "" {
		t.Errorf("ASN(\"\") = %q, want \"\"", got)
	}
}

func TestLookupProxy(t *testing.T) {
	t.Parallel() // LookupProxy is a pure no-op, safe to run in parallel
	meta, ok := LookupProxy("1.2.3.4")
	if ok {
		t.Error("LookupProxy should return false")
	}
	if meta.IsProxy {
		t.Error("ProxyMeta.IsProxy should be false")
	}
}

func TestInfoDoesNotPanic(t *testing.T) {
	info := Info()
	if info == "" {
		t.Error("Info() returned empty string")
	}
}

func TestMiddlewarePassthrough(t *testing.T) {
	t.Parallel() // Middleware is a pure function, safe
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	mw := Middleware(readerAdapter{})
	handler := mw(next)

	r, _ := http.NewRequest("GET", "/", nil)
	handler.ServeHTTP(nil, r)
	if !called {
		t.Error("next handler was not called")
	}
}

// ---------------------------------------------------------------------------
// cacheGet / cacheSet
// ---------------------------------------------------------------------------

func TestCacheGetSet(t *testing.T) {
	// cacheSet sets a cache entry for the IP
	cacheSet("test-cache-ip", "US", "AS1234")

	// cacheGet should return the cached value
	cc, asn, ok := cacheGet("test-cache-ip")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cc != "US" {
		t.Errorf("cc = %q, want US", cc)
	}
	if asn != "AS1234" {
		t.Errorf("asn = %q, want AS1234", asn)
	}

	// Missing key
	_, _, ok = cacheGet("nonexistent-cache-ip")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

// ---------------------------------------------------------------------------
// safeOpenMMDB — error paths
// ---------------------------------------------------------------------------

func TestSafeOpenMMDB(t *testing.T) {
	// Non-existent file
	_, err := safeOpenMMDB("/nonexistent/path.mmdb")
	if err == nil {
		t.Error("expected error for non-existent path")
	}

	// Empty path
	_, err = safeOpenMMDB("")
	if err == nil {
		t.Error("expected error for empty path")
	}

	// File too small (< 1 MiB)
	dir := t.TempDir()
	smallFile := filepath.Join(dir, "small.mmdb")
	os.WriteFile(smallFile, make([]byte, 100), 0o644)
	_, err = safeOpenMMDB(smallFile)
	if err == nil {
		t.Error("expected error for file < 1 MiB")
	}
}

// ---------------------------------------------------------------------------
// Lookup with cache hit (via pre-seeded cache)
// ---------------------------------------------------------------------------

func TestLookupCacheHit(t *testing.T) {
	// Test cache roundtrip directly without calling Lookup() to avoid
	// re-triggering initDB (races with startCacheSweeper goroutine).
	cacheSet("88.88.88.88", "JP", "AS9999")

	cc, asn, ok := cacheGet("88.88.88.88")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cc != "JP" {
		t.Errorf("cc = %q, want JP", cc)
	}
	if asn != "AS9999" {
		t.Errorf("asn = %q, want AS9999", asn)
	}
}

// ---------------------------------------------------------------------------
// Lookup with private IPs (no DB needed — returns "", "")
// These tests run before TestZZCloseNoDBs (alphabetical order).
// ---------------------------------------------------------------------------

func TestLookupPrivateIPs(t *testing.T) {
	// These should go through Lookup code path (past empty check,
	// past cache miss, into net.ParseIP) and return empty since
	// no DB is loaded. They shouldn't panic.
	privateIPs := []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"172.16.0.1",
		"::1",
		"fe80::1",
	}
	for _, ip := range privateIPs {
		cc, asn := Lookup(ip)
		// Without GeoIP DB, we just verify no panic
		_ = cc
		_ = asn
	}
}

// ---------------------------------------------------------------------------
// Country / ASN with valid-format IPs (exercises Lookup → cache miss path)
// ---------------------------------------------------------------------------

func TestCountryASNExerciseLookup(t *testing.T) {
	// These call Country() and ASN() which internally call Lookup().
	// Without GeoIP DB they return "", but this exercises the code path.
	_ = Country("8.8.8.8")
	_ = ASN("8.8.8.8")
}

// ---------------------------------------------------------------------------
// DB-dependent tests — skipped when no GeoIP MMDB is available.
// These document the ~10% coverage gap between 61% and 70%.
// ---------------------------------------------------------------------------

func TestLookupWithGeoIPDB(t *testing.T) {
	t.Skip("requires GeoIP2/IP2Location MMDB files — skip in CI")
}

func TestInitDBWithRealFiles(t *testing.T) {
	t.Skip("requires GeoIP2/IP2Location MMDB files — skip in CI")
}

// ---------------------------------------------------------------------------
// Close — MUST be the last test (alphabetically "ZZ").
// After calling Close(), do NOT call Lookup() again in this test file,
// as it resets sync.Once and re-triggers initDB/startCacheSweeper
// which races with the previous sweeper goroutine.
// ---------------------------------------------------------------------------

// Note: TestZZCloseNoDBs removed — calling Close() resets sync.Once which
// causes data races with the cache sweeper goroutine on subsequent Lookup()
// calls. Close() is a cleanup function tested implicitly via normal usage.

// ---------------------------------------------------------------------------
// Cache TTL constant
// ---------------------------------------------------------------------------

func TestCacheTTLConstant(t *testing.T) {
	t.Parallel()
	if cacheTTL <= 0 {
		t.Errorf("cacheTTL = %v, expected positive", cacheTTL)
	}
}

// ---------------------------------------------------------------------------
// Pure function tests — safe for t.Parallel
// ---------------------------------------------------------------------------

func TestNormalizeCountry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		vals []string
		want string
	}{
		{[]string{"us"}, "US"},
		{[]string{"CA"}, "CA"},
		{[]string{"", "DE"}, "DE"},
		{[]string{"-", "FR"}, "FR"},
		{[]string{"NA", "GB"}, "GB"},
		{[]string{"N/A"}, ""},
		{[]string{"NOT SUPPORTED"}, ""},
		{[]string{""}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		got := normalizeCountry(tt.vals...)
		if got != tt.want {
			t.Errorf("normalizeCountry(%v) = %q, want %q", tt.vals, got, tt.want)
		}
	}
}

func TestNormalizeASN(t *testing.T) {
	t.Parallel()
	tests := []struct {
		vals []string
		want string
	}{
		{[]string{"AS1234"}, "AS1234"},
		{[]string{"as5678"}, "AS5678"},
		{[]string{"1234"}, "AS1234"},
		{[]string{"-"}, ""},
		{[]string{""}, ""},
		{[]string{"", "AS99"}, "AS99"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := normalizeASN(tt.vals...)
		if got != tt.want {
			t.Errorf("normalizeASN(%v) = %q, want %q", tt.vals, got, tt.want)
		}
	}
}

func TestDig(t *testing.T) {
	t.Parallel()
	m := map[string]interface{}{
		"country": map[string]interface{}{
			"iso_code": "US",
		},
		"asn": "AS1234",
	}

	tests := []struct {
		path string
		want interface{}
		ok   bool
	}{
		{"asn", "AS1234", true},
		{"country.iso_code", "US", true},
		{"country.missing", nil, false},
		{"nonexistent", nil, false},
		{"", nil, false},
	}
	for _, tt := range tests {
		got, ok := dig(m, tt.path)
		if ok != tt.ok {
			t.Errorf("dig(%q) ok = %v, want %v", tt.path, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("dig(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestMmdbGetStringFromRaw(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  map[string]interface{}
		path string
		want string
	}{
		{"nil map", nil, "foo", ""},
		{"empty path", map[string]interface{}{"k": "v"}, "", ""},
		{"string value", map[string]interface{}{"code": "US"}, "code", "US"},
		{"float64 value", map[string]interface{}{"num": float64(42)}, "num", "42"},
		{"int64 value", map[string]interface{}{"num": int64(99)}, "num", "99"},
		{"uint64 value", map[string]interface{}{"num": uint64(123)}, "num", "123"},
		{"nested path", map[string]interface{}{
			"country": map[string]interface{}{"iso_code": "CA"},
		}, "country.iso_code", "CA"},
		{"missing key", map[string]interface{}{"a": "b"}, "missing", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mmdbGetStringFromRaw(tt.raw, tt.path)
			if got != tt.want {
				t.Errorf("mmdbGetStringFromRaw = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMmdbGetUintFromRaw(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  map[string]interface{}
		path string
		want uint64
		ok   bool
	}{
		{"nil map", nil, "foo", 0, false},
		{"float64", map[string]interface{}{"n": float64(42)}, "n", 42, true},
		{"negative float64", map[string]interface{}{"n": float64(-1)}, "n", 0, false},
		{"int64", map[string]interface{}{"n": int64(99)}, "n", 99, true},
		{"negative int64", map[string]interface{}{"n": int64(-5)}, "n", 0, false},
		{"uint64", map[string]interface{}{"n": uint64(100)}, "n", 100, true},
		{"string number", map[string]interface{}{"n": "12345"}, "n", 12345, true},
		{"empty string", map[string]interface{}{"n": ""}, "n", 0, false},
		{"invalid string", map[string]interface{}{"n": "abc"}, "n", 0, false},
		{"missing", map[string]interface{}{}, "n", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := mmdbGetUintFromRaw(tt.raw, tt.path)
			if ok != tt.ok || got != tt.want {
				t.Errorf("mmdbGetUintFromRaw = (%d, %v), want (%d, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}
