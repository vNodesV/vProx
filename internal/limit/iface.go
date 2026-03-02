package limit

import "net/http"

// Limiter is the interface satisfied by *IPLimiter for dependency injection
// and testing.
//
// In production, New() returns an *IPLimiter that implements this interface.
// Test code can substitute a mock Limiter to avoid real rate limiting.
type Limiter interface {
	// Middleware wraps an http.Handler with IP rate limiting.
	Middleware(next http.Handler) http.Handler

	// SetOverride adds or updates a per-IP RateSpec at runtime.
	SetOverride(ip string, spec RateSpec) error

	// DeleteOverride removes a per-IP override (falls back to defaults).
	DeleteOverride(ip string)

	// Close releases resources (log files, sweeper goroutine).
	Close() error
}
