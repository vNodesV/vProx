package limit

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vNodesV/vApp/modules/vProx/internal/geo"
	"golang.org/x/time/rate"
)

// RateSpec defines a requests-per-second (RPS) budget and a burst size.
type RateSpec struct {
	RPS   float64
	Burst int
}

// AutoRule controls automatic IP quarantine (auto-override) behavior.
type AutoRule struct {
	Threshold int           // N requests
	Window    time.Duration // within this time window
	Penalty   RateSpec      // apply this override
	TTL       time.Duration // for this long
}

// IPLimiter is an IP-aware rate limiter middleware with per-IP overrides.
type IPLimiter struct {
	defaults  RateSpec
	overrides sync.Map // ip(string) -> RateSpec (manual + auto)

	// limiter pool per ip.
	pool sync.Map // ip(string) -> *rate.Limiter

	// auto-quarantine
	autoRule    *AutoRule
	autoState   sync.Map // ip -> *strikeState
	autoExpiry  sync.Map // ip -> time.Time (when to remove override)
	enforceAuto bool     // true when autoRule != nil

	// sampled "allow" logs
	allowLogEvery time.Duration
	lastAllowLog  sync.Map // ip -> time.Time

	// logging
	logger     *log.Logger
	logFile    *os.File
	mirrorMain bool // also echo important events into main log (global log package)

	// client IP extraction
	trustProxy bool   // prefer CF-Connecting-IP / Forwarded / X-Forwarded-For
	ipHeader   string // explicit header (e.g., X-Real-IP)

	// behavior
	enforceDefaults bool // if true, defaults also 429 via Allow(); else Wait()

	// JSONL filter
	logImportantOnly bool

	// time source
	now func() time.Time
}

type strikeState struct {
	mu        sync.Mutex
	count     int
	windowEnd time.Time
}

// Option configures an IPLimiter.
type Option func(*IPLimiter)

// WithLogPath sets the JSONL log path (default: $VPROX_HOME/data/logs/rate-limit.jsonl).
func WithLogPath(p string) Option {
	return func(l *IPLimiter) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Printf("[limit] warn: cannot open log file %q: %v", p, err)
			return
		}
		l.logFile = f
		l.logger = log.New(f, "", 0)
	}
}

// WithTrustProxy enables proxy-aware IP detection (default: false).
func WithTrustProxy(trust bool) Option {
	return func(l *IPLimiter) { l.trustProxy = trust }
}

// WithIPHeader uses a specific header for client IP first (e.g., "X-Real-IP").
func WithIPHeader(header string) Option {
	return func(l *IPLimiter) { l.ipHeader = header }
}

// WithNow overrides the time source (primarily for tests).
func WithNow(f func() time.Time) Option {
	return func(l *IPLimiter) { l.now = f }
}

// WithDefaultActionDrop makes defaults enforce 429 instead of Wait().
func WithDefaultActionDrop() Option {
	return func(l *IPLimiter) { l.enforceDefaults = true }
}

// WithAllowLogEvery enables sampled "allow" logging (e.g., 5*time.Second). 0 disables.
func WithAllowLogEvery(d time.Duration) Option {
	return func(l *IPLimiter) { l.allowLogEvery = d }
}

// WithAutoQuarantine enables auto adding IPs that exceed a threshold in a window.
func WithAutoQuarantine(rule AutoRule) Option {
	return func(l *IPLimiter) {
		l.autoRule = &rule
		l.enforceAuto = true
	}
}

// WithLogOnlyImportant filters JSONL to 429/auto-add/auto-expire/wait-canceled.
func WithLogOnlyImportant() Option {
	return func(l *IPLimiter) { l.logImportantOnly = true }
}

// WithMirrorToMainLog mirrors important limiter events to main.log via the global logger.
func WithMirrorToMainLog() Option {
	return func(l *IPLimiter) { l.mirrorMain = true }
}

// New creates an IPLimiter with global defaults and per-IP overrides.
func New(defaults RateSpec, overrides map[string]RateSpec, opts ...Option) *IPLimiter {
	l := &IPLimiter{
		defaults:   defaults,
		trustProxy: false,
		ipHeader:   "",
		now:        time.Now,
	}
	// default logger to stderr; replaced by WithLogPath below
	l.logger = log.New(os.Stderr, "", 0)
	// ensure default log path unless overridden
	WithLogPath(defaultLogPath())(l)

	for ip, spec := range overrides {
		l.overrides.Store(ip, spec)
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func defaultLogPath() string {
	if v := strings.TrimSpace(os.Getenv("VPROX_HOME")); v != "" {
		return filepath.Join(v, "data", "logs", "rate-limit.jsonl")
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Join(h, ".vProx", "data", "logs", "rate-limit.jsonl")
	}
	return "logs/rate-limit.jsonl"
}

// ----- status context plumbing -----
type ctxKey int

const ctxStatusKey ctxKey = iota

// StatusOf returns "ok" if no status was set by the limiter.
func StatusOf(r *http.Request) string {
	if v := r.Context().Value(ctxStatusKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return "ok"
}

// Middleware wraps an http.Handler with IP rate limiting + auto-quarantine.
func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := l.clientIP(r)

		// expire any auto override
		l.autoMaybeExpire(ip, r)
		// count for auto rule
		l.autoMaybeFlag(ip, r)

		lim := l.limiterFor(ip)

		// optional debug line
		if os.Getenv("LIMIT_DEBUG") == "1" {
			dbg := map[string]any{
				"ts":       l.now().UTC().Format(time.RFC3339Nano),
				"debug":    "lim",
				"ip":       ip,
				"override": l.hasOverride(ip),
				"path":     r.URL.Path,
			}
			if b, _ := json.Marshal(dbg); b != nil {
				l.logger.Println(string(b))
			}
		}

		// STRICT MODE for overrides (manual or auto): use Allow() => 429
		if l.hasOverride(ip) {
			if !lim.Allow() {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("X-RateLimit-Policy", l.policyString(ip))
				w.Header().Set("X-RateLimit-Status", "blocked")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				l.logEvent(ip, r, "429")
				return
			}
			w.Header().Set("X-RateLimit-Status", "limited")
			r = r.WithContext(context.WithValue(r.Context(), ctxStatusKey, "limited"))
			next.ServeHTTP(w, r)
			l.maybeLogAllow(ip, r)
			return
		}

		// DEFAULTS: either Allow() (drop) or Wait() (smooth)
		if l.enforceDefaults {
			if !lim.Allow() {
				w.Header().Set("Retry-After", "1")
				w.Header().Set("X-RateLimit-Policy", l.policyString(ip))
				w.Header().Set("X-RateLimit-Status", "blocked")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				l.logEvent(ip, r, "429")
				return
			}
			// Allowed under defaults → status=ok
			w.Header().Set("X-RateLimit-Status", "ok")
			r = r.WithContext(context.WithValue(r.Context(), ctxStatusKey, "ok"))
			next.ServeHTTP(w, r)
			l.maybeLogAllow(ip, r)
			return
		}

		// Smoothing mode (no 429; Wait blocks until token available).
		if err := lim.Wait(r.Context()); err != nil {
			w.Header().Set("X-RateLimit-Status", "blocked")
			http.Error(w, "request canceled", http.StatusTooManyRequests)
			l.logEvent(ip, r, "wait-canceled")
			return
		}
		w.Header().Set("X-RateLimit-Status", "ok")
		r = r.WithContext(context.WithValue(r.Context(), ctxStatusKey, "ok"))
		next.ServeHTTP(w, r)
		l.maybeLogAllow(ip, r)
	})
}

// SetOverride adds/updates a per-IP RateSpec at runtime (resets cached limiter).
func (l *IPLimiter) SetOverride(ip string, spec RateSpec) error {
	if net.ParseIP(ip) == nil {
		return errors.New("invalid ip")
	}
	l.overrides.Store(ip, spec)
	l.pool.Delete(ip)
	return nil
}

// DeleteOverride removes a per-IP override (falls back to defaults).
func (l *IPLimiter) DeleteOverride(ip string) {
	l.overrides.Delete(ip)
	l.pool.Delete(ip)
}

// Close releases resources (e.g., log file).
func (l *IPLimiter) Close() error {
	if l.logFile != nil {
		return l.logFile.Close()
	}
	return nil
}

// --- internals ---

func (l *IPLimiter) hasOverride(ip string) bool {
	_, ok := l.overrides.Load(ip)
	return ok
}

func (l *IPLimiter) limiterFor(ip string) *rate.Limiter {
	if v, ok := l.pool.Load(ip); ok {
		return v.(*rate.Limiter)
	}
	spec := l.defaults
	if o, ok := l.overrides.Load(ip); ok {
		spec = o.(RateSpec)
	}
	// guard: burst must be >= 1 for Allow/Wait to function
	if spec.Burst < 1 {
		spec.Burst = 1
	}
	lim := rate.NewLimiter(rate.Limit(spec.RPS), spec.Burst)
	actual, _ := l.pool.LoadOrStore(ip, lim)
	return actual.(*rate.Limiter)
}

func (l *IPLimiter) clientIP(r *http.Request) string {
	// 0) Cloudflare direct hint
	if l.trustProxy {
		if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
			if p := parseFirstIP(v); p != "" {
				return p
			}
		}
	}
	// 1) explicit header
	if l.ipHeader != "" {
		if ip := strings.TrimSpace(r.Header.Get(l.ipHeader)); ip != "" {
			if p := parseFirstIP(ip); p != "" {
				return p
			}
		}
	}
	if l.trustProxy {
		// 2) Forwarded
		if fwd := r.Header.Get("Forwarded"); fwd != "" {
			if ip := forwardedForIP(fwd); ip != "" {
				return ip
			}
		}
		// 3) X-Forwarded-For
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				if ip := strings.TrimSpace(parts[0]); ip != "" {
					if p := parseFirstIP(ip); p != "" {
						return p
					}
				}
			}
		}
	}
	// 4) RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func forwardedForIP(h string) string {
	semi := strings.Split(h, ";")
	for _, seg := range semi {
		kv := strings.SplitN(strings.TrimSpace(seg), "=", 2)
		if len(kv) != 2 || !strings.EqualFold(kv[0], "for") {
			continue
		}
		val := strings.Trim(kv[1], `"`)
		val = strings.TrimPrefix(val, "[")
		val = strings.TrimSuffix(val, "]")
		if host, _, err := net.SplitHostPort(val); err == nil {
			val = host
		}
		if p := parseFirstIP(val); p != "" {
			return p
		}
	}
	return ""
}

func parseFirstIP(s string) string {
	host := s
	if strings.Contains(s, ":") && !strings.Contains(s, "]") && countRune(s, ':') > 1 {
		if net.ParseIP(s) != nil {
			return s
		}
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		host = h
	}
	if net.ParseIP(host) != nil {
		return host
	}
	return ""
}

func countRune(s string, r rune) int {
	n := 0
	for _, c := range s {
		if c == r {
			n++
		}
	}
	return n
}

func (l *IPLimiter) policyString(ip string) string {
	spec := l.defaults
	if o, ok := l.overrides.Load(ip); ok {
		spec = o.(RateSpec)
	}
	return strings.TrimSpace(formatPolicy(ip, spec))
}

func formatPolicy(ip string, spec RateSpec) string {
	return "ip=" + ip + "; rps=" + formatFloat(spec.RPS) + "; burst=" + itoa(spec.Burst)
}

func formatFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(strconvFormatFloat(f, 'f', 2, 64), "0"), ".")
}

func itoa(i int) string { return strconvItoa(i) }

// Small wrappers to avoid pulling fmt just for numbers.
func strconvItoa(i int) string { return (*itoaTable)(nil).i(i) }

type itoaTable struct{}

func (*itoaTable) i(i int) string { return string(intToBytes(i)) }

func strconvFormatFloat(f float64, fmt byte, prec, bitSize int) string {
	return strconv.FormatFloat(f, fmt, prec, bitSize)
}

func intToBytes(i int) []byte {
	if i == 0 {
		return []byte{'0'}
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return b[n:]
}

// --------- Auto-quarantine ----------

func (l *IPLimiter) autoMaybeFlag(ip string, r *http.Request) {
	if !l.enforceAuto || ip == "" {
		return
	}
	now := l.now()

	v, _ := l.autoState.LoadOrStore(ip, &strikeState{})
	s := v.(*strikeState)

	s.mu.Lock()
	defer s.mu.Unlock()

	// start / roll window
	if now.After(s.windowEnd) || s.windowEnd.IsZero() {
		s.windowEnd = now.Add(l.autoRule.Window)
		s.count = 0
	}
	s.count++

	if s.count >= l.autoRule.Threshold {
		// apply penalty override
		_ = l.SetOverride(ip, l.autoRule.Penalty)
		l.autoExpiry.Store(ip, now.Add(l.autoRule.TTL))
		l.logEvent(ip, r, "auto-override-add")
		// reset strikes for a fresh window after quarantine
		s.count = 0
		s.windowEnd = now.Add(l.autoRule.Window)
	}
}

func (l *IPLimiter) autoMaybeExpire(ip string, r *http.Request) {
	if !l.enforceAuto || ip == "" {
		return
	}
	if exp, ok := l.autoExpiry.Load(ip); ok {
		if t, ok2 := exp.(time.Time); ok2 && l.now().After(t) {
			l.DeleteOverride(ip)
			l.autoExpiry.Delete(ip)
			l.logEvent(ip, r, "auto-override-expire")
		}
	}
}

// --------- Logging ----------

func (l *IPLimiter) maybeLogAllow(ip string, r *http.Request) {
	if l.allowLogEvery <= 0 {
		return
	}
	now := l.now()
	if last, ok := l.lastAllowLog.Load(ip); ok {
		if t, ok2 := last.(time.Time); ok2 && now.Sub(t) < l.allowLogEvery {
			return
		}
	}
	l.lastAllowLog.Store(ip, now)
	l.logEvent(ip, r, "allow-sample")
}

func (l *IPLimiter) shouldLog(reason string) bool {
	if !l.logImportantOnly {
		return true
	}
	switch reason {
	case "429", "auto-override-add", "auto-override-expire", "wait-canceled":
		return true
	default:
		return false
	}
}

func (l *IPLimiter) logEvent(ip string, r *http.Request, reason string) {
	if !l.shouldLog(reason) {
		return
	}
	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" {
		country = geo.Country(ip)
	}
	asn := geo.ASN(ip)

	spec := l.defaults
	if o, ok := l.overrides.Load(ip); ok {
		spec = o.(RateSpec)
	}

	type ev struct {
		TS      string  `json:"ts"`
		IP      string  `json:"ip"`
		Country string  `json:"country,omitempty"`
		ASN     string  `json:"asn,omitempty"`
		Method  string  `json:"method"`
		Path    string  `json:"path"`
		Host    string  `json:"host"`
		UA      string  `json:"ua"`
		Reason  string  `json:"reason"`
		RPS     float64 `json:"rps"`
		Burst   int     `json:"burst"`
	}
	rec := ev{
		TS:      l.now().UTC().Format(time.RFC3339Nano),
		IP:      ip,
		Country: country,
		ASN:     asn,
		Method:  r.Method,
		Path:    r.URL.Path,
		Host:    r.Host,
		UA:      r.Header.Get("User-Agent"),
		Reason:  reason,
		RPS:     spec.RPS,
		Burst:   spec.Burst,
	}
	if b, err := json.Marshal(rec); err == nil {
		l.logger.Println(string(b))
	}

	if l.mirrorMain {
		log.Printf("[rate] reason=%s ip=%s country=%s asn=%s rps=%.2f burst=%d path=%s host=%s ua=%q",
			reason, ip, country, asn, spec.RPS, spec.Burst, r.URL.Path, r.Host, r.Header.Get("User-Agent"))
	}
}
