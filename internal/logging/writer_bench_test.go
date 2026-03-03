package logging

import (
	"io"
	"testing"
)

// sample log lines for benchmarks (representative of production output)
var benchLogLines = []string{
	`2025-07-12T10:30:45.123Z INF proxied request method=GET route=/rpc status=200 latency_ms=12 ip=10.0.0.1 host=rpc.cosmos.example.com module=proxy` + "\n",
	`2025-07-12T10:30:45.456Z ERR backend timeout error="context deadline exceeded" route=/api host=api.osmosis.zone module=proxy` + "\n",
	`2025-07-12T10:30:45.789Z WRN rate limited ip=192.168.1.42 method=POST path=/rpc host=rpc.stargaze.example.com module=limiter` + "\n",
	`2025-07-12T10:30:46.001Z DBG geo lookup country=US asn=AS13335 ip=1.1.1.1 module=geo` + "\n",
}

// BenchmarkColorize measures the ANSI colorize hot path which runs
// on every log line sent to stdout.
func BenchmarkColorize(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		_ = ColorizeLogLine(benchLogLines[i%len(benchLogLines)])
	}
}

// BenchmarkColorLevel measures just the level-to-color lookup.
func BenchmarkColorLevel(b *testing.B) {
	b.ReportAllocs()

	levels := []string{"INF", "ERR", "WRN", "DBG"}

	b.ResetTimer()
	for i := range b.N {
		_ = ColorLevel(levels[i%len(levels)])
	}
}

// BenchmarkColorValueForKey measures context-aware value colorization.
func BenchmarkColorValueForKey(b *testing.B) {
	b.ReportAllocs()

	pairs := [][2]string{
		{"module", "proxy"},
		{"status", `"ok"`},
		{"error", `"timeout"`},
		{"ip", "10.0.0.1"},
		{"height", "12345678"},
		{"request_id", "req-abc123def456"},
	}

	b.ResetTimer()
	for i := range b.N {
		p := pairs[i%len(pairs)]
		_ = ColorValueForKey(p[0], p[1])
	}
}

// BenchmarkSplitLogWriter_Write measures the dual-write path with
// colorization enabled (the production hot path).
func BenchmarkSplitLogWriter_Write(b *testing.B) {
	b.ReportAllocs()

	w := &SplitLogWriter{
		Stdout:   io.Discard,
		File:     io.Discard,
		Colorize: true,
	}
	line := []byte(benchLogLines[0])

	b.ResetTimer()
	for b.Loop() {
		_, _ = w.Write(line)
	}
}

// BenchmarkSplitLogWriter_Write_NoColor measures the dual-write path
// without colorization (file-only equivalent).
func BenchmarkSplitLogWriter_Write_NoColor(b *testing.B) {
	b.ReportAllocs()

	w := &SplitLogWriter{
		Stdout:   io.Discard,
		File:     io.Discard,
		Colorize: false,
	}
	line := []byte(benchLogLines[0])

	b.ResetTimer()
	for b.Loop() {
		_, _ = w.Write(line)
	}
}
