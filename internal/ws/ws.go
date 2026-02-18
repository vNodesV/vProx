package ws

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Deps abstracts what we need from main without importing it.
// You supply these funcs when wiring the handler.
type Deps struct {
	// ClientIP extracts the real client IP (after proxy headers etc.)
	ClientIP func(*http.Request) string

	// LogRequestSummary is your 3-line summary emitter
	// (the one that prints HOST/ROUTE/REQUEST + IP + latency + country, etc.)
	LogRequestSummary func(r *http.Request, proxied bool, route string, host string, start time.Time)

	// BackendWSParams returns backend WS URL + timeouts, or ok=false if WS isn't enabled for this host.
	// Example return: ("ws://10.0.0.13:26657/websocket", 300s, 0s, true)
	BackendWSParams func(host string) (backendURL string, idle time.Duration, hard time.Duration, ok bool)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  32 << 10,
	WriteBufferSize: 32 << 10,
	CheckOrigin:     func(r *http.Request) bool { return true }, // edge terminates, trust local policy
}

// HandleWS returns an http.HandlerFunc you can register at /websocket.
func HandleWS(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		host := strings.ToLower(r.Host)

		backendURL, idle, hard, ok := d.BackendWSParams(host)
		if !ok {
			http.Error(w, "WebSocket not enabled", http.StatusNotFound)
			d.LogRequestSummary(r, false, "ws-deny", host, start)
			return
		}

		// Upgrade client side
		cConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			d.LogRequestSummary(r, false, "ws-upgrade-fail", host, start)
			return
		}
		defer cConn.Close()

		// Dial backend WS (CometBFT/Tendermint speaks WS at /websocket)
		hdr := http.Header{}
		hdr.Set("X-Forwarded-For", d.ClientIP(r))
		hdr.Set("X-Forwarded-Host", host)

		bConn, _, err := websocket.DefaultDialer.Dial(backendURL, hdr)
		if err != nil {
			_ = cConn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "backend unreachable"),
				time.Now().Add(2*time.Second),
			)
			d.LogRequestSummary(r, false, "ws-backend-fail", host, start)
			return
		}
		defer bConn.Close()

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

		// Hard lifetime (optional)
		var hardTimer *time.Timer
		if hard > 0 {
			hardTimer = time.AfterFunc(hard, func() {
				_ = cConn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "max lifetime reached"),
					time.Now().Add(2*time.Second),
				)
				_ = bConn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "max lifetime reached"),
					time.Now().Add(2*time.Second),
				)
				_ = cConn.Close()
				_ = bConn.Close()
			})
			defer hardTimer.Stop()
		}

		// Pumps
		errc := make(chan error, 2)
		var upBytes int64   // client -> backend
		var downBytes int64 // backend -> client
		var wg sync.WaitGroup

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

				if writeErr := bConn.WriteMessage(mt, p); writeErr != nil {
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

				if writeErr := cConn.WriteMessage(mt, p); writeErr != nil {
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
			case <-time.After(hard):
				cause = "hard_timeout"
			}
		} else {
			finalErr = <-errc
			cause = classifyWSCause(finalErr)
		}

		_ = cConn.Close()
		_ = bConn.Close()
		wg.Wait()

		// Your usual 3-line summary
		d.LogRequestSummary(r, true, "ws", host, start)

		// Data transfer quantities + avg throughput
		dur := time.Since(start)
		up := atomic.LoadInt64(&upBytes)
		down := atomic.LoadInt64(&downBytes)
		total := up + down

		log.Printf("[WS] backend=%s idle=%0.fs max=%0.fs dur=%s up=%s down=%s total=%s avg=%s cause=%s",
			backendURL,
			idle.Seconds(),
			hard.Seconds(),
			dur.Truncate(time.Millisecond),
			humanBytes(up),
			humanBytes(down),
			humanBytes(total),
			humanRate(total, dur),
			cause,
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
