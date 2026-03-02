package logging

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ColorLevel
// ---------------------------------------------------------------------------

func TestColorLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level string
		want  string
	}{
		{"DBG", AnsiBlue},
		{"dbg", AnsiBlue},
		{"WRN", AnsiYellow},
		{"ERR", AnsiRed},
		{"INF", AnsiGreen},
		{"INFO", AnsiGreen},
		{"unknown", AnsiGreen},
		{"", AnsiGreen},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			t.Parallel()
			if got := ColorLevel(tt.level); got != tt.want {
				t.Errorf("ColorLevel(%q) = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ColorValueForKey
// ---------------------------------------------------------------------------

func TestColorValueForKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key   string
		value string
		want  string // expected ANSI color prefix
	}{
		{"module", "backup", AnsiMagenta},
		{"height", "12345", AnsiYellow},
		{"latency_ms", "42", AnsiYellow},
		{"src_count", "5", AnsiYellow},
		{"status", `"ok"`, AnsiGreen},
		{"status", `"error"`, AnsiRed},
		{"error", "timeout", AnsiRed},
		{"request_id", "req-abc", AnsiCyan},
		{"ip", "1.2.3.4", AnsiCyan},
		{"host", "example.com", AnsiCyan},
		{"route", "rpc", AnsiCyan},
		{"method", "GET", AnsiCyan},
		{"plain", "value", AnsiGreen}, // default
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			t.Parallel()
			got := ColorValueForKey(tt.key, tt.value)
			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("ColorValueForKey(%q, %q) starts with %q, want prefix %q",
					tt.key, tt.value, got[:min(len(got), 10)], tt.want)
			}
			if !strings.HasSuffix(got, AnsiReset) {
				t.Errorf("ColorValueForKey result should end with AnsiReset")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ColorizeLogLine
// ---------------------------------------------------------------------------

func TestColorizeLogLine(t *testing.T) {
	t.Parallel()

	t.Run("structured line", func(t *testing.T) {
		t.Parallel()
		line := "10:23AM INF finalizing commit height=23116134 module=consensus"
		got := ColorizeLogLine(line)
		if !strings.Contains(got, AnsiGreen) {
			t.Error("expected green color for INF level")
		}
		if !strings.Contains(got, "height") {
			t.Error("expected height key in output")
		}
	})

	t.Run("error level", func(t *testing.T) {
		t.Parallel()
		line := "10:23AM ERR failed error=timeout module=proxy"
		got := ColorizeLogLine(line)
		if !strings.Contains(got, AnsiRed) {
			t.Error("expected red color for ERR level")
		}
	})

	t.Run("empty line", func(t *testing.T) {
		t.Parallel()
		got := ColorizeLogLine("")
		if got != "" {
			t.Errorf("empty line should return empty, got %q", got)
		}
	})

	t.Run("whitespace only", func(t *testing.T) {
		t.Parallel()
		got := ColorizeLogLine("   \n")
		// Should be returned as-is since TrimSpace is empty
		if got != "   \n" {
			t.Errorf("whitespace line unexpected: %q", got)
		}
	})

	t.Run("short line", func(t *testing.T) {
		t.Parallel()
		got := ColorizeLogLine("hello world")
		// Less than 3 parts → returned as-is
		if got != "hello world" {
			t.Errorf("short line unexpected: %q", got)
		}
	})

	t.Run("newline preserved", func(t *testing.T) {
		t.Parallel()
		line := "10:23AM INF message key=value\n"
		got := ColorizeLogLine(line)
		if !strings.HasSuffix(got, "\n") {
			t.Error("trailing newline should be preserved")
		}
	})

	t.Run("no kv pairs", func(t *testing.T) {
		t.Parallel()
		line := "10:23AM INF just a plain message here"
		got := ColorizeLogLine(line)
		if !strings.Contains(got, AnsiCyan) {
			t.Error("message without KV pairs should be cyan")
		}
	})
}

// ---------------------------------------------------------------------------
// SplitLogWriter
// ---------------------------------------------------------------------------

func TestSplitLogWriter(t *testing.T) {
	t.Parallel()

	t.Run("writes to both", func(t *testing.T) {
		t.Parallel()
		var stdout, file bytes.Buffer
		w := &SplitLogWriter{Stdout: &stdout, File: &file}
		msg := []byte("test log line\n")
		n, err := w.Write(msg)
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		if n != len(msg) {
			t.Errorf("n = %d, want %d", n, len(msg))
		}
		if file.String() != "test log line\n" {
			t.Errorf("file = %q", file.String())
		}
		if stdout.String() != "test log line\n" {
			t.Errorf("stdout = %q", stdout.String())
		}
	})

	t.Run("colorize stdout", func(t *testing.T) {
		t.Parallel()
		var stdout, file bytes.Buffer
		w := &SplitLogWriter{Stdout: &stdout, File: &file, Colorize: true}
		msg := []byte("10:23AM INF msg key=value\n")
		_, err := w.Write(msg)
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		// File should be raw
		if strings.Contains(file.String(), AnsiGreen) {
			t.Error("file should not have ANSI codes")
		}
		// Stdout should have colors
		if !strings.Contains(stdout.String(), "\x1b[") {
			t.Error("stdout should have ANSI codes when Colorize=true")
		}
	})

	t.Run("nil file", func(t *testing.T) {
		t.Parallel()
		var stdout bytes.Buffer
		w := &SplitLogWriter{Stdout: &stdout, File: nil}
		n, err := w.Write([]byte("msg"))
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		if n != 3 {
			t.Errorf("n = %d, want 3", n)
		}
	})

	t.Run("nil stdout", func(t *testing.T) {
		t.Parallel()
		var file bytes.Buffer
		w := &SplitLogWriter{Stdout: nil, File: &file}
		n, err := w.Write([]byte("msg"))
		if err != nil {
			t.Fatalf("Write error: %v", err)
		}
		if n != 3 {
			t.Errorf("n = %d, want 3", n)
		}
	})
}

// ---------------------------------------------------------------------------
// Line / LineLifecycle
// ---------------------------------------------------------------------------

func TestLine(t *testing.T) {
	t.Parallel()

	t.Run("basic structured line", func(t *testing.T) {
		t.Parallel()
		got := Line("INFO", "proxy", "request received", F("ip", "1.2.3.4"), F("method", "GET"))
		if !strings.Contains(got, "INF") {
			t.Error("expected INF in output")
		}
		if !strings.Contains(got, "request received") {
			t.Error("expected event message in output")
		}
		if !strings.Contains(got, "ip=1.2.3.4") {
			t.Error("expected ip field in output")
		}
		if !strings.Contains(got, "module=proxy") {
			t.Error("expected module=proxy in output")
		}
	})

	t.Run("empty level defaults", func(t *testing.T) {
		t.Parallel()
		got := Line("", "", "")
		if !strings.Contains(got, "INF") {
			t.Error("expected default level INF")
		}
		if !strings.Contains(got, "module=app") {
			t.Error("expected default module=app")
		}
	})

	t.Run("DEBUG level", func(t *testing.T) {
		t.Parallel()
		got := Line("DEBUG", "test", "debug msg")
		if !strings.Contains(got, "DBG") {
			t.Error("expected DBG for DEBUG level")
		}
	})

	t.Run("WARN level", func(t *testing.T) {
		t.Parallel()
		got := Line("WARN", "test", "warn msg")
		if !strings.Contains(got, "WRN") {
			t.Error("expected WRN for WARN level")
		}
	})

	t.Run("ERROR level", func(t *testing.T) {
		t.Parallel()
		got := Line("ERROR", "test", "err msg")
		if !strings.Contains(got, "ERR") {
			t.Error("expected ERR for ERROR level")
		}
	})

	t.Run("module field not duplicated", func(t *testing.T) {
		t.Parallel()
		got := Line("INFO", "proxy", "test", F("module", "override"))
		// Should have module=override but not module=proxy
		count := strings.Count(got, "module=")
		if count != 1 {
			t.Errorf("expected exactly 1 module field, got %d in %q", count, got)
		}
	})
}

func TestLineLifecycle(t *testing.T) {
	t.Parallel()

	got := LineLifecycle("NEW", "backup", F("ID", "BUP123"), F("status", "STARTED"))
	if !strings.Contains(got, "NEW") {
		t.Error("expected lifecycle NEW in output")
	}
	if !strings.Contains(got, "ID=BUP123") {
		t.Error("expected ID field")
	}
	if !strings.Contains(got, "module=backup") {
		t.Error("expected module=backup")
	}
}

// ---------------------------------------------------------------------------
// RequestID functions
// ---------------------------------------------------------------------------

func TestRequestIDFrom(t *testing.T) {
	t.Parallel()

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()
		if got := RequestIDFrom(nil); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("valid ID", func(t *testing.T) {
		t.Parallel()
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set(RequestIDHeader, "req-abc123")
		if got := RequestIDFrom(r); got != "req-abc123" {
			t.Errorf("got %q, want req-abc123", got)
		}
	})

	t.Run("missing header", func(t *testing.T) {
		t.Parallel()
		r, _ := http.NewRequest("GET", "/", nil)
		if got := RequestIDFrom(r); got != "" {
			t.Errorf("expected empty for missing header, got %q", got)
		}
	})

	t.Run("unsafe chars rejected", func(t *testing.T) {
		t.Parallel()
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set(RequestIDHeader, "req<script>")
		if got := RequestIDFrom(r); got != "" {
			t.Errorf("expected empty for unsafe ID, got %q", got)
		}
	})
}

func TestEnsureRequestID(t *testing.T) {
	t.Parallel()

	t.Run("generates when missing", func(t *testing.T) {
		t.Parallel()
		r, _ := http.NewRequest("GET", "/", nil)
		id := EnsureRequestID(r)
		if id == "" {
			t.Error("expected non-empty ID")
		}
		if !strings.HasPrefix(id, "req-") {
			t.Errorf("expected req- prefix, got %q", id)
		}
		// Should be stored in header
		if r.Header.Get(RequestIDHeader) != id {
			t.Error("ID not stored in request header")
		}
	})

	t.Run("preserves existing", func(t *testing.T) {
		t.Parallel()
		r, _ := http.NewRequest("GET", "/", nil)
		r.Header.Set(RequestIDHeader, "existing-id")
		id := EnsureRequestID(r)
		if id != "existing-id" {
			t.Errorf("expected existing-id, got %q", id)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		t.Parallel()
		if got := EnsureRequestID(nil); got != "" {
			t.Errorf("expected empty for nil, got %q", got)
		}
	})
}

func TestSetResponseRequestID(t *testing.T) {
	t.Parallel()

	t.Run("sets header", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		SetResponseRequestID(w, "test-id")
		if got := w.Header().Get(RequestIDHeader); got != "test-id" {
			t.Errorf("got %q, want test-id", got)
		}
	})

	t.Run("empty id skips", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		SetResponseRequestID(w, "")
		if got := w.Header().Get(RequestIDHeader); got != "" {
			t.Errorf("expected empty header, got %q", got)
		}
	})

	t.Run("nil writer", func(t *testing.T) {
		t.Parallel()
		SetResponseRequestID(nil, "test") // should not panic
	})
}

func TestNewRequestID(t *testing.T) {
	t.Parallel()
	id := NewRequestID()
	if !strings.HasPrefix(id, "req-") {
		t.Errorf("expected req- prefix, got %q", id)
	}
	if len(id) < 10 {
		t.Errorf("ID too short: %q", id)
	}
}

func TestNewTypedID(t *testing.T) {
	t.Parallel()
	id := NewTypedID("WSS")
	if !strings.HasPrefix(id, "WSS") {
		t.Errorf("expected WSS prefix, got %q", id)
	}
	if len(id) != 3+24 { // prefix + 24 hex chars
		t.Errorf("ID length = %d, want 27", len(id))
	}
}

func TestF(t *testing.T) {
	t.Parallel()
	f := F("  key  ", "value")
	if f.Key != "key" {
		t.Errorf("Key = %q, want key", f.Key)
	}
	if f.Value != "value" {
		t.Errorf("Value = %v, want value", f.Value)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
