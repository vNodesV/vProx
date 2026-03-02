package logging

import (
	"io"
	"regexp"
	"strings"
)

// ANSI escape codes for terminal colorization.
const (
	AnsiReset   = "\x1b[0m"
	AnsiBold    = "\x1b[1m"
	AnsiDim     = "\x1b[2m"
	AnsiBlue    = "\x1b[34m"
	AnsiCyan    = "\x1b[36m"
	AnsiGreen   = "\x1b[32m"
	AnsiYellow  = "\x1b[33m"
	AnsiMagenta = "\x1b[35m"
	AnsiRed     = "\x1b[31m"
)

var (
	logKVRe   = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)=("([^"\\]|\\.)*"|[^ ]+)`)
	longHexRe = regexp.MustCompile(`\b[0-9A-Fa-f]{24,}\b`)
)

// SplitLogWriter writes to both a file and stdout, optionally colorizing
// the stdout output. It implements io.Writer.
type SplitLogWriter struct {
	Stdout   io.Writer
	File     io.Writer
	Colorize bool
}

func (w *SplitLogWriter) Write(p []byte) (int, error) {
	if w.File != nil {
		if _, err := w.File.Write(p); err != nil {
			return 0, err
		}
	}
	if w.Stdout != nil {
		out := p
		if w.Colorize {
			out = []byte(ColorizeLogLine(string(p)))
		}
		if _, err := w.Stdout.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// ColorizeLogLine applies ANSI colors to a structured log line.
// Format expected: "<timestamp> <LEVEL> <message> key=value ..."
func ColorizeLogLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}

	trail := ""
	if strings.HasSuffix(line, "\n") {
		trail = "\n"
		line = strings.TrimSuffix(line, "\n")
	}

	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return line + trail
	}

	base := AnsiDim + parts[0] + AnsiReset + " " + ColorLevel(parts[1]) + parts[1] + AnsiReset + " "
	rest := parts[2]

	firstKV := logKVRe.FindStringIndex(rest)
	if firstKV == nil {
		return base + AnsiCyan + rest + AnsiReset + trail
	}

	msg := strings.TrimSpace(rest[:firstKV[0]])
	kvs := rest[firstKV[0]:]
	kvColored := logKVRe.ReplaceAllStringFunc(kvs, func(m string) string {
		kv := strings.SplitN(m, "=", 2)
		if len(kv) != 2 {
			return m
		}
		k, v := kv[0], kv[1]
		return AnsiBold + AnsiBlue + k + AnsiReset + "=" + ColorValueForKey(k, v)
	})

	if msg == "" {
		return base + kvColored + trail
	}
	return base + AnsiCyan + msg + AnsiReset + " " + kvColored + trail
}

// ColorLevel returns the ANSI color code for a log level token.
func ColorLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DBG":
		return AnsiBlue
	case "WRN":
		return AnsiYellow
	case "ERR":
		return AnsiRed
	default:
		return AnsiGreen
	}
}

// ColorValueForKey returns a colorized value string based on the key name.
func ColorValueForKey(key, value string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "module":
		return AnsiMagenta + value + AnsiReset
	case "height", "latency_ms", "src_count":
		return AnsiYellow + value + AnsiReset
	case "status":
		if strings.EqualFold(strings.Trim(value, `"`), "ok") {
			return AnsiGreen + value + AnsiReset
		}
		return AnsiRed + value + AnsiReset
	case "error":
		return AnsiRed + value + AnsiReset
	case "request_id", "ip", "host", "route", "method":
		return AnsiCyan + value + AnsiReset
	}
	vTrim := strings.Trim(value, `"`)
	if longHexRe.MatchString(vTrim) {
		return AnsiGreen + value + AnsiReset
	}
	return AnsiGreen + value + AnsiReset
}
