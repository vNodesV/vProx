package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// makeOriginChecker
// ---------------------------------------------------------------------------

func TestMakeOriginChecker_Wildcard(t *testing.T) {
	t.Parallel()
	check := makeOriginChecker([]string{"*"})
	r, _ := http.NewRequest("GET", "/", nil)
	r.Header.Set("Origin", "https://evil.com")
	if !check(r) {
		t.Error("wildcard should allow all origins")
	}
}

func TestMakeOriginChecker_SpecificList(t *testing.T) {
	t.Parallel()
	check := makeOriginChecker([]string{"https://example.com", "https://rpc.example.com"})

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{"allowed", "https://example.com", true},
		{"allowed subdomain", "https://rpc.example.com", true},
		{"case insensitive", "HTTPS://EXAMPLE.COM", true},
		{"not allowed", "https://evil.com", false},
		{"no origin header", "", true}, // non-browser client
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, _ := http.NewRequest("GET", "/", nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if got := check(r); got != tt.want {
				t.Errorf("check(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

func TestMakeOriginChecker_SameOrigin(t *testing.T) {
	t.Parallel()
	check := makeOriginChecker(nil) // empty = same-origin

	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"same host", "example.com", "https://example.com", true},
		{"same host with port", "example.com:443", "https://example.com:443", true},
		{"different host", "example.com", "https://evil.com", false},
		{"no origin", "example.com", "", true},
		{"origin with trailing slash", "example.com", "https://example.com/", true},
		{"origin host no scheme match", "example.com", "http://example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, _ := http.NewRequest("GET", "/websocket", nil)
			r.Host = tt.host
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if got := check(r); got != tt.want {
				t.Errorf("sameOriginCheck(host=%q, origin=%q) = %v, want %v",
					tt.host, tt.origin, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// humanBytes
// ---------------------------------------------------------------------------

func TestHumanBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.00KiB"},
		{1048576, "1.00MiB"},
		{1073741824, "1.00GiB"},
		{1099511627776, "1.00TiB"},
	}
	for _, tt := range tests {
		got := humanBytes(tt.n)
		if got != tt.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// humanRate
// ---------------------------------------------------------------------------

func TestHumanRate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		bytes int64
		secs  float64
		want  string
	}{
		{"zero duration", 100, 0, "0B/s"},
		{"low rate", 100, 1, "100.00B/s"},
		{"KB/s", 2048, 1, "2.00KiB/s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := time.Duration(tt.secs * float64(time.Second))
			got := humanRate(tt.bytes, d)
			if got != tt.want {
				t.Errorf("humanRate(%d, %v) = %q, want %q", tt.bytes, d, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyWSCause
// ---------------------------------------------------------------------------

func TestClassifyWSCause(t *testing.T) {
	t.Parallel()

	t.Run("nil error", func(t *testing.T) {
		t.Parallel()
		if got := classifyWSCause(nil); got != "ok" {
			t.Errorf("got %q, want ok", got)
		}
	})

	t.Run("generic error", func(t *testing.T) {
		t.Parallel()
		err := &genericError{msg: "something broke"}
		if got := classifyWSCause(err); got != "error" {
			t.Errorf("got %q, want error", got)
		}
	})

	t.Run("timeout error", func(t *testing.T) {
		t.Parallel()
		err := &timeoutError{}
		if got := classifyWSCause(err); got != "idle_timeout" {
			t.Errorf("got %q, want idle_timeout", got)
		}
	})
}

// ---------------------------------------------------------------------------
// HandleWS — WS not enabled
// ---------------------------------------------------------------------------

func TestHandleWSNotEnabled(t *testing.T) {
	t.Parallel()
	var summaryRoute string
	deps := Deps{
		ClientIP: func(r *http.Request) string { return "127.0.0.1" },
		LogRequestSummary: func(r *http.Request, proxied bool, route, host string, start time.Time) {
			summaryRoute = route
		},
		BackendWSParams: func(host string) (string, time.Duration, time.Duration, bool) {
			return "", 0, 0, false // WS not enabled
		},
	}

	handler := HandleWS(deps)
	r, _ := http.NewRequest("GET", "/websocket", nil)
	r.Host = "example.com"
	w := &fakeResponseWriter{headers: http.Header{}}
	handler.ServeHTTP(w, r)

	if w.statusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.statusCode)
	}
	if summaryRoute != "ws-deny" {
		t.Errorf("route = %q, want ws-deny", summaryRoute)
	}
}

// ---------------------------------------------------------------------------
// HandleWS integration — upgrade to WS, echo messages
// ---------------------------------------------------------------------------

func TestHandleWSIntegration(t *testing.T) {
	// Create a backend WS echo server
	backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer backendSrv.Close()

	backendWSURL := "ws" + strings.TrimPrefix(backendSrv.URL, "http") + "/websocket"

	deps := Deps{
		ClientIP: func(r *http.Request) string { return "127.0.0.1" },
		LogRequestSummary: func(r *http.Request, proxied bool, route, host string, start time.Time) {
		},
		BackendWSParams: func(host string) (string, time.Duration, time.Duration, bool) {
			return backendWSURL, 30 * time.Second, 0, true
		},
		AllowedOrigins: []string{"*"},
	}

	proxySrv := httptest.NewServer(HandleWS(deps))
	defer proxySrv.Close()

	wsURL := "ws" + strings.TrimPrefix(proxySrv.URL, "http") + "/websocket"

	// Connect via gorilla/websocket dialer
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send a message
	testMsg := []byte(`{"jsonrpc":"2.0","method":"subscribe","id":1}`)
	if err := conn.WriteMessage(websocket.TextMessage, testMsg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read echoed response
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(msg) != string(testMsg) {
		t.Errorf("echo: got %q, want %q", string(msg), string(testMsg))
	}
}

// ---------------------------------------------------------------------------
// HandleWS — origin rejected
// ---------------------------------------------------------------------------

func TestHandleWSOriginRejected(t *testing.T) {
	deps := Deps{
		ClientIP: func(r *http.Request) string { return "127.0.0.1" },
		LogRequestSummary: func(r *http.Request, proxied bool, route, host string, start time.Time) {
		},
		BackendWSParams: func(host string) (string, time.Duration, time.Duration, bool) {
			return "ws://127.0.0.1:26657/websocket", 30 * time.Second, 0, true
		},
		AllowedOrigins: []string{"https://allowed.example.com"},
	}

	srv := httptest.NewServer(HandleWS(deps))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/websocket"
	headers := http.Header{}
	headers.Set("Origin", "https://evil.example.com")

	dialer := websocket.Dialer{}
	_, _, err := dialer.Dial(wsURL, headers)
	if err == nil {
		t.Error("expected connection to be rejected (bad origin)")
	}
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

func TestWSConstants(t *testing.T) {
	t.Parallel()
	if wsMaxMessageBytes != 512*1024 {
		t.Errorf("wsMaxMessageBytes = %d, want %d", wsMaxMessageBytes, 512*1024)
	}
	if wsMaxConnections != 1000 {
		t.Errorf("wsMaxConnections = %d, want 1000", wsMaxConnections)
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type genericError struct{ msg string }

func (e *genericError) Error() string { return e.msg }

type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

type fakeResponseWriter struct {
	headers    http.Header
	statusCode int
	body       []byte
}

func (w *fakeResponseWriter) Header() http.Header {
	return w.headers
}

func (w *fakeResponseWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}

func (w *fakeResponseWriter) WriteHeader(code int) {
	w.statusCode = code
}
