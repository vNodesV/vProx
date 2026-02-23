package webserver

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// WebServer builds and owns the HTTP handlers derived from a Config.
type WebServer struct {
	cfg Config
}

// New creates a WebServer from cfg.
func New(cfg Config) *WebServer {
	return &WebServer{cfg: cfg}
}

// Mount describes a registered handler and the host(s) it matches.
type Mount struct {
	// Hosts contains all hostnames (primary + aliases) this mount handles.
	// An empty Hosts slice means the handler should not be mounted (disabled vhost).
	Hosts   []string
	Handler http.Handler
	// HasTLS is true when this vhost has a TLS certificate configured.
	HasTLS bool
	// VHost is the source config.
	VHost VHostConfig
}

// Mounts returns one Mount per configured vhost.
func (ws *WebServer) Mounts() []Mount {
	mounts := make([]Mount, 0, len(ws.cfg.VHosts))
	for _, v := range ws.cfg.VHosts {
		h := ws.buildHandler(v)
		hosts := append([]string{v.Host}, v.Aliases...)
		mounts = append(mounts, Mount{
			Hosts:   hosts,
			Handler: h,
			HasTLS:  v.TLS.Cert != "" && v.TLS.Key != "",
			VHost:   v,
		})
	}
	return mounts
}

// buildHandler composes the middleware stack for a single vhost.
// Stack (outermost → innermost):
//
//	securityHeaders → cors → headerManip → compress → (proxy | static)
func (ws *WebServer) buildHandler(v VHostConfig) http.Handler {
	var core http.Handler

	switch {
	case v.Backend != "" && v.Root != "":
		// Proxy with static fallback: proxy takes priority; 404s fall through to static.
		core = ws.proxyWithStaticFallback(v)
	case v.Backend != "":
		core = ws.proxyHandler(v)
	default:
		core = ws.staticHandler(v)
	}

	// Apply middleware chain (innermost applied last = outermost in execution).
	h := core
	if v.Compress {
		h = gzipMiddleware(h)
	}
	h = headerMiddleware(v, h)
	if v.CORS.Enabled {
		h = corsMiddleware(v.CORS, h)
	}
	h = securityMiddleware(v.Security, h)
	return h
}

// ----- Core handlers -----

func (ws *WebServer) proxyHandler(v VHostConfig) http.Handler {
	target, err := url.Parse(v.Backend)
	if err != nil {
		// Config validation should have caught this; return error handler.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, fmt.Sprintf("bad backend URL: %v", err), http.StatusBadGateway)
		})
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1 // streaming-friendly

	timeout := time.Duration(v.ProxyTimeoutSec) * time.Second
	rp.Transport = &http.Transport{
		ResponseHeaderTimeout: timeout,
		ForceAttemptHTTP2:     false, // h2c handled separately in Phase 2
	}

	// Rewrite director: strip path prefix if backend has a path, forward Host.
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		orig(req)
		req.Host = target.Host
		// Ensure X-Forwarded-Proto is set; X-Forwarded-For is handled by ReverseProxy.
		if req.Header.Get("X-Forwarded-Proto") == "" {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
	}

	return rp
}

func (ws *WebServer) staticHandler(v VHostConfig) http.Handler {
	fs := http.FileServer(http.Dir(v.Root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve index file for directory root.
		if r.URL.Path == "/" || r.URL.Path == "" {
			r.URL.Path = "/" + v.Index
		}
		fs.ServeHTTP(w, r)
	})
}

func (ws *WebServer) proxyWithStaticFallback(v VHostConfig) http.Handler {
	proxy := ws.proxyHandler(v)
	static := ws.staticHandler(v)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use a response recorder to detect 404 from proxy.
		rec := &statusRecorder{ResponseWriter: w, code: 0}
		proxy.ServeHTTP(rec, r)
		if rec.code == http.StatusNotFound {
			// Clear any headers written by the proxy 404 (e.g. Set-Cookie, upstream IDs)
			// before the static handler writes its own response headers.
			for k := range w.Header() {
				delete(w.Header(), k)
			}
			static.ServeHTTP(w, r)
		}
	})
}

// ----- Middleware -----

// gzipMiddleware compresses responses for common text/data content types.
func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		// Only compress content types that benefit from it.
		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

// corsMiddleware sets Access-Control-* headers.
// Per spec, Access-Control-Allow-Origin must be exactly "*" or a single origin.
// When multiple origins are configured, the request Origin is reflected if it matches.
func corsMiddleware(cfg CORSConfig, next http.Handler) http.Handler {
	allowedOrigins := make(map[string]struct{}, len(cfg.Origins))
	wildcard := false
	for _, o := range cfg.Origins {
		if o == "*" {
			wildcard = true
		}
		allowedOrigins[o] = struct{}{}
	}
	methods := strings.Join(cfg.Methods, ", ")
	headers := strings.Join(cfg.Headers, ", ")
	maxAge := fmt.Sprintf("%d", cfg.MaxAgeSec)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wildcard {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			reqOrigin := r.Header.Get("Origin")
			if _, ok := allowedOrigins[reqOrigin]; ok && reqOrigin != "" {
				w.Header().Set("Access-Control-Allow-Origin", reqOrigin)
				// Vary: Origin tells caches this response is origin-dependent.
				w.Header().Add("Vary", "Origin")
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", methods)
		if headers != "" {
			w.Header().Set("Access-Control-Allow-Headers", headers)
		}
		if cfg.MaxAgeSec > 0 {
			w.Header().Set("Access-Control-Max-Age", maxAge)
		}
		// Handle preflight.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// headerMiddleware strips and injects response headers per vhost config.
func headerMiddleware(v VHostConfig, next http.Handler) http.Handler {
	// Normalise strip list to lowercase for case-insensitive comparison.
	strip := make(map[string]struct{}, len(v.Headers.Strip))
	for _, h := range v.Headers.Strip {
		strip[strings.ToLower(h)] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&headerManipWriter{ResponseWriter: w, strip: strip, inject: v.Headers.Inject}, r)
	})
}

// securityMiddleware adds security-related headers (HSTS, CSP, X-Frame-Options, etc.).
func securityMiddleware(cfg SecurityConfig, next http.Handler) http.Handler {
	if !cfg.HSTS && cfg.XFrameOptions == "" && !cfg.XContentType && cfg.CSP == "" {
		return next // nothing to add
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.HSTS {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		if cfg.XFrameOptions != "" {
			w.Header().Set("X-Frame-Options", cfg.XFrameOptions)
		}
		if cfg.XContentType {
			w.Header().Set("X-Content-Type-Options", "nosniff")
		}
		if cfg.CSP != "" {
			w.Header().Set("Content-Security-Policy", cfg.CSP)
		}
		next.ServeHTTP(w, r)
	})
}

// ----- Helper types -----

// statusRecorder captures the status code from a ResponseWriter without
// buffering the body (used for proxy→static fallback).
type statusRecorder struct {
	http.ResponseWriter
	code    int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	if code != http.StatusNotFound {
		r.ResponseWriter.WriteHeader(code)
		r.written = true
	}
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.code == http.StatusNotFound {
		return len(b), nil // discard 404 body; static will respond
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter so http.ResponseController
// can find Flusher/Hijacker interfaces for streaming and WebSocket upgrades.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// headerManipWriter wraps ResponseWriter to strip/inject headers before
// the first Write or WriteHeader call.
type headerManipWriter struct {
	http.ResponseWriter
	strip  map[string]struct{}
	inject map[string]string
	done   bool
}

func (h *headerManipWriter) applyOnce() {
	if h.done {
		return
	}
	h.done = true
	hdr := h.ResponseWriter.Header()
	for k := range h.strip {
		hdr.Del(k)
	}
	for k, v := range h.inject {
		hdr.Set(k, v)
	}
}

func (h *headerManipWriter) WriteHeader(code int) {
	h.applyOnce()
	h.ResponseWriter.WriteHeader(code)
}

func (h *headerManipWriter) Write(b []byte) (int, error) {
	h.applyOnce()
	return h.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter so http.ResponseController
// can find Flusher/Hijacker interfaces for streaming and WebSocket upgrades.
func (h *headerManipWriter) Unwrap() http.ResponseWriter { return h.ResponseWriter }

// gzipResponseWriter lazily starts gzip compression once the content-type
// is known (on first Write). Text and JSON types are compressed; others pass through.
// The status code is buffered so that Content-Encoding/Content-Length headers can be
// set before the response is committed to the wire.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	bypass  bool
	started bool
	code    int // buffered status code; 0 = not yet set
}

var compressibleTypes = []string{
	"text/", "application/json", "application/javascript",
	"application/xml", "image/svg+xml",
}

func shouldCompress(ct string) bool {
	ct = strings.ToLower(ct)
	for _, prefix := range compressibleTypes {
		if strings.Contains(ct, prefix) {
			return true
		}
	}
	return false
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	// Buffer the status code; forwarded after Content-Encoding is set in Write.
	// This prevents committing headers before gzip headers are applied.
	g.code = code
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.started {
		g.started = true
		ct := g.ResponseWriter.Header().Get("Content-Type")
		if shouldCompress(ct) {
			g.ResponseWriter.Header().Set("Content-Encoding", "gzip")
			g.ResponseWriter.Header().Del("Content-Length")
			g.gz = gzip.NewWriter(g.ResponseWriter)
		} else {
			g.bypass = true
		}
		// Forward buffered status code now that headers are finalized.
		if g.code != 0 {
			g.ResponseWriter.WriteHeader(g.code)
		}
	}
	if g.bypass || g.gz == nil {
		return g.ResponseWriter.Write(b)
	}
	return g.gz.Write(b)
}

func (g *gzipResponseWriter) close() {
	if g.gz != nil {
		_ = g.gz.Close()
	}
	// If Write was never called (e.g. 204 No Content, HEAD), forward the buffered code now.
	if !g.started && g.code != 0 {
		g.ResponseWriter.WriteHeader(g.code)
	}
}

// Flush implements http.Flusher for streaming responses.
func (g *gzipResponseWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter so http.ResponseController
// can find Hijacker for WebSocket upgrades. Flush is handled by the explicit
// Flush method above (ResponseController checks interfaces before unwrapping).
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }
