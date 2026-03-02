package ws

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	applog "github.com/vNodesV/vProx/internal/logging"
)

// Deps abstracts what we need from main without importing it.
// You supply these funcs when wiring the handler.
type Deps struct {
	// ClientIP extracts the real client IP (after proxy headers etc.)
	ClientIP func(*http.Request) string

	// LogRequestSummary is your 3-line summary emitter
	// (the one that prints HOST/ROUTE/REQUEST + IP + latency + country, etc.)
	LogRequestSummary func(r *http.Request, proxied bool, route string, host string, start time.Time, statusCode int)

	// BackendWSParams returns backend WS URL + timeouts, or ok=false if WS isn't enabled for this host.
	// Example return: ("ws://10.0.0.13:26657/websocket", 300s, 0s, true)
	BackendWSParams func(host string) (backendURL string, idle time.Duration, hard time.Duration, ok bool)

	// AllowedOrigins controls WebSocket CORS (SEC-M4).
	// Empty  = same-origin only (Origin must match Host).
	// ["*"]  = allow all origins (explicit opt-in).
	// Otherwise only the listed origins are accepted.
	AllowedOrigins []string
}

var defaultUpgraderConfig = struct {
	ReadBufferSize  int
	WriteBufferSize int
}{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
}

// makeOriginChecker builds a CheckOrigin function from the AllowedOrigins list.
func makeOriginChecker(allowed []string) func(*http.Request) bool {
	// Wildcard: allow all (explicit opt-in).
	for _, o := range allowed {
		if o == "*" {
			return func(r *http.Request) bool { return true }
		}
	}
	// Non-empty list: match against allowed origins.
	if len(allowed) > 0 {
		set := make(map[string]struct{}, len(allowed))
		for _, o := range allowed {
			set[strings.ToLower(strings.TrimSpace(o))] = struct{}{}
		}
		return func(r *http.Request) bool {
			origin := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin")))
			if origin == "" {
				return true // no Origin header (non-browser client)
			}
			_, ok := set[origin]
			return ok
		}
	}
	// Empty list: same-origin only — Origin host must match request Host.
	return func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true // no Origin header (non-browser client)
		}
		host := strings.ToLower(r.Host)
		// Strip port from origin URL: "https://example.com:443" → "example.com"
		originHost := strings.ToLower(origin)
		if idx := strings.Index(originHost, "://"); idx != -1 {
			originHost = originHost[idx+3:]
		}
		originHost = strings.TrimSuffix(originHost, "/")
		if h, _, err := net.SplitHostPort(originHost); err == nil {
			originHost = h
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		return originHost == host
	}
}

// Fix SEC-M2: per-frame read limit to prevent OOM from oversized frames.
// Fix SEC-M3: global connection cap to prevent FD exhaustion.
const (
	wsMaxMessageBytes = 512 * 1024 // 512 KB per frame
	wsMaxConnections  = 1000
)

var wsActiveConns int64

// HandleWS returns an http.HandlerFunc you can register at /websocket.
func HandleWS(d Deps) http.HandlerFunc {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  defaultUpgraderConfig.ReadBufferSize,
		WriteBufferSize: defaultUpgraderConfig.WriteBufferSize,
		CheckOrigin:     makeOriginChecker(d.AllowedOrigins),
	}

	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		host := strings.ToLower(r.Host)

		// Generate WSS-prefixed log ID now so both NEW (connect) and UPD (close)
		// share the same correlation ID via the request header.
		wssID := applog.NewTypedID("WSS")
		r.Header.Set(applog.RequestIDHeader, wssID)
		requestID := wssID
		applog.SetResponseRequestID(w, requestID)

		backendURL, idle, hard, ok := d.BackendWSParams(host)
		if !ok {
			http.Error(w, "WebSocket not enabled", http.StatusNotFound)
			d.LogRequestSummary(r, false, "ws-deny", host, start, http.StatusNotFound)
			return
		}

		// Fix SEC-M3: reject new connections when at capacity.
		if atomic.AddInt64(&wsActiveConns, 1) > wsMaxConnections {
			atomic.AddInt64(&wsActiveConns, -1)
			http.Error(w, "too many WebSocket connections", http.StatusServiceUnavailable)
			return
		}
		defer atomic.AddInt64(&wsActiveConns, -1)

		// Upgrade client side (echo request id in handshake response headers)
		respHdr := http.Header{}
		if requestID != "" {
			respHdr.Set(applog.RequestIDHeader, requestID)
		}
		cConn, err := upgrader.Upgrade(w, r, respHdr)
		if err != nil {
			d.LogRequestSummary(r, false, "ws-upgrade-fail", host, start, http.StatusBadRequest)
			return
		}
		defer cConn.Close()
		cConn.SetReadLimit(wsMaxMessageBytes) // Fix SEC-M2

		// Dial backend WS (CometBFT/Tendermint speaks WS at /websocket)
		hdr := http.Header{}
		hdr.Set("X-Forwarded-For", d.ClientIP(r))
		hdr.Set("X-Forwarded-Host", host)
		if requestID != "" {
			hdr.Set(applog.RequestIDHeader, requestID)
		}

		bConn, _, err := websocket.DefaultDialer.Dial(backendURL, hdr) //nolint:bodyclose // websocket upgrade response body managed by gorilla/websocket
		if err != nil {
			_ = cConn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "backend unreachable"),
				time.Now().Add(2*time.Second),
			)
			d.LogRequestSummary(r, false, "ws-backend-fail", host, start, http.StatusBadGateway)
			return
		}
		defer bConn.Close()
		bConn.SetReadLimit(wsMaxMessageBytes) // Fix SEC-M2

		// Emit CONNECTED log now that both sides are up.
		d.LogRequestSummary(r, true, "websocket", host, start, http.StatusSwitchingProtocols)

		// Defaults
		if idle <= 0 {
			idle = 3600 * time.Second
		}

		// Deadlines and keepalive behavior:
		// We extend read deadlines on Pong so long-lived WS sessions survive if pongs flow.
		extendDeadline := func(conn *websocket.Conn) {
			_ = conn.SetReadDeadline(time.Now().Add(idle))
		}

		extendDeadline(cConn)
		extendDeadline(bConn)
		_ = cConn.SetWriteDeadline(time.Now().Add(idle))
		_ = bConn.SetWriteDeadline(time.Now().Add(idle))

		cConn.SetPongHandler(func(appData string) error {
			extendDeadline(cConn)
			return nil
		})
		bConn.SetPongHandler(func(appData string) error {
			extendDeadline(bConn)
			return nil
		})

		// Hard lifetime (optional): signal via channel instead of closing
		// connections directly from the timer goroutine (gorilla WS is not
		// concurrent-safe for Close).
		hardDone := make(chan struct{})
		if hard > 0 {
			hardTimer := time.AfterFunc(hard, func() {
				close(hardDone)
			})
			defer hardTimer.Stop()
		}

		// Pumps
		errc := make(chan error, 2)
		var upBytes int64   // client -> backend
		var downBytes int64 // backend -> client
		var wg sync.WaitGroup
		// Fix CR-4: one mutex per connection to serialize concurrent writes
		// (WriteMessage in pump goroutines vs WriteControl in the closer below).
		var cMu, bMu sync.Mutex

		// client -> backend
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mt, p, readErr := cConn.ReadMessage()
				if readErr != nil {
					errc <- readErr
					return
				}
				extendDeadline(cConn)
				_ = bConn.SetWriteDeadline(time.Now().Add(idle))

				bMu.Lock()
				writeErr := bConn.WriteMessage(mt, p)
				bMu.Unlock()
				if writeErr != nil {
					errc <- writeErr
					return
				}
				atomic.AddInt64(&upBytes, int64(len(p)))
			}
		}()

		// backend -> client
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				mt, p, readErr := bConn.ReadMessage()
				if readErr != nil {
					errc <- readErr
					return
				}
				extendDeadline(bConn)
				_ = cConn.SetWriteDeadline(time.Now().Add(idle))

				cMu.Lock()
				writeErr := cConn.WriteMessage(mt, p)
				cMu.Unlock()
				if writeErr != nil {
					errc <- writeErr
					return
				}
				atomic.AddInt64(&downBytes, int64(len(p)))
			}
		}()

		// Wait for close/error
		cause := "ok"
		var finalErr error

		if hard > 0 {
			select {
			case finalErr = <-errc:
				cause = classifyWSCause(finalErr)
			case <-hardDone:
				cause = "hard_timeout"
			}
		} else {
			finalErr = <-errc
			cause = classifyWSCause(finalErr)
		}

		// Send close frames before closing (best-effort, non-blocking).
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, cause)
		cMu.Lock()
		_ = cConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(2*time.Second))
		cMu.Unlock()
		bMu.Lock()
		_ = bConn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(2*time.Second))
		bMu.Unlock()
		_ = cConn.Close()
		_ = bConn.Close()
		wg.Wait()

		// Your usual 3-line summary
		// (removed — CONNECTED was already emitted at connect time above)

		// Emit UPD with session stats.
		dur := time.Since(start)
		up := atomic.LoadInt64(&upBytes)
		down := atomic.LoadInt64(&downBytes)
		total := up + down
		applog.PrintLifecycle("UPD", "ws",
			applog.F("ID", requestID),
			applog.F("status", "CLOSED"),
			applog.F("reason", strings.ToUpper(cause)),
			applog.F("duration", dur.Truncate(time.Second).String()),
			applog.F("upload", humanBytes(up)),
			applog.F("download", humanBytes(down)),
			applog.F("averageRate", humanRate(total, dur)),
		)
	}
}

func classifyWSCause(err error) string {
	if err == nil {
		return "ok"
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return "idle_timeout"
	}
	if websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
		websocket.CloseAbnormalClosure,
	) {
		return "closed"
	}
	return "error"
}

func humanBytes(n int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	abs := n
	if abs < 0 {
		abs = -abs
	}

	switch {
	case abs >= TB:
		return fmt.Sprintf("%.2fTiB", float64(n)/float64(TB))
	case abs >= GB:
		return fmt.Sprintf("%.2fGiB", float64(n)/float64(GB))
	case abs >= MB:
		return fmt.Sprintf("%.2fMiB", float64(n)/float64(MB))
	case abs >= KB:
		return fmt.Sprintf("%.2fKiB", float64(n)/float64(KB))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

func humanRate(bytes int64, d time.Duration) string {
	if d <= 0 {
		return "0B/s"
	}
	bps := float64(bytes) / d.Seconds()

	const (
		KB = 1024.0
		MB = 1024.0 * KB
		GB = 1024.0 * MB
	)

	switch {
	case bps >= GB:
		return fmt.Sprintf("%.2fGiB/s", bps/GB)
	case bps >= MB:
		return fmt.Sprintf("%.2fMiB/s", bps/MB)
	case bps >= KB:
		return fmt.Sprintf("%.2fKiB/s", bps/KB)
	default:
		return fmt.Sprintf("%.2fB/s", bps)
	}
}
