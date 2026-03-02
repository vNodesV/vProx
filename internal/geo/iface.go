package geo

import "net/http"

// Reader is the interface satisfied by the geo package's public API
// for dependency injection and testing.
//
// The default implementation uses package-level functions backed by
// IP2Location MMDB and GeoLite2 databases. Test code can substitute
// a mock Reader without hitting real databases.
type Reader interface {
	// Lookup returns (countryISO2, "AS####") for the given IP string.
	Lookup(ipStr string) (countryCode string, asn string)

	// Country returns the ISO-3166 alpha-2 country code (e.g. "US").
	Country(ipStr string) string

	// Info returns a one-line status string for logging.
	Info() string

	// Close releases database resources.
	Close()
}

// LimiterReader is used by the limiter middleware to resolve geo data
// for rate-limit log entries. It is a subset of Reader.
type LimiterReader interface {
	Country(ipStr string) string
}

// ProbeReader is used by handlers that need proxy/threat metadata.
type ProbeReader interface {
	Reader
	LookupProxy(ipStr string) (ProxyMeta, bool)
}

// Middleware injects a Reader into request context for downstream handlers.
// This is a no-op placeholder — vProx currently uses package-level functions.
func Middleware(r Reader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return next // pass-through; Reader consumed via direct calls
	}
}
