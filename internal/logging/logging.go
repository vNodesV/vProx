package logging

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const RequestIDHeader = "X-Request-ID"

// Field is a structured key/value pair for log lines.
type Field struct {
	Key   string
	Value any
}

// F creates a structured key/value log field.
func F(key string, value any) Field {
	return Field{Key: strings.TrimSpace(key), Value: value}
}

var bareValueRe = regexp.MustCompile(`^[A-Za-z0-9._:/@+\-]+$`)

// Line renders a CosmosSDK/journalctl-cat style structured line.
//
// Format:
//
//	<time> <LVL> <message> key=value ... module=<MODULE>
//
// Example:
//
//	10:23AM INF finalizing commit height=23116134 module=consensus
func Line(level, module, event string, fields ...Field) string {
	ts := time.Now().Local().Format("3:04PM")
	level = strings.ToUpper(strings.TrimSpace(level))
	if level == "" {
		level = "INFO"
	}
	module = strings.TrimSpace(module)
	if module == "" {
		module = "app"
	}
	event = strings.TrimSpace(event)
	if event == "" {
		event = "log"
	}

	parts := []string{ts, shortLevel(level), normalizeMessage(event)}
	hasModule := false

	for _, f := range fields {
		k := strings.TrimSpace(f.Key)
		if k == "" {
			continue
		}
		if strings.EqualFold(k, "module") {
			hasModule = true
		}
		parts = append(parts, k+"="+encodeValue(f.Value))
	}

	if !hasModule {
		parts = append(parts, "module="+encodeValue(module))
	}

	return strings.Join(parts, " ")
}

// Print emits a structured line via the global default logger.
func Print(level, module, event string, fields ...Field) {
	log.Println(Line(level, module, event, fields...))
}

// PrintTo emits a structured line via the provided logger.
func PrintTo(l *log.Logger, level, module, event string, fields ...Field) {
	if l == nil {
		return
	}
	l.Println(Line(level, module, event, fields...))
}

// RequestIDFrom returns the normalized request id from headers, if present and valid.
func RequestIDFrom(r *http.Request) string {
	if r == nil {
		return ""
	}
	v := strings.TrimSpace(r.Header.Get(RequestIDHeader))
	if !isSafeRequestID(v) {
		return ""
	}
	return v
}

// EnsureRequestID returns a valid request id and writes it into request headers.
// If the incoming header is missing or invalid, a new ID is generated.
func EnsureRequestID(r *http.Request) string {
	if r == nil {
		return ""
	}
	if id := RequestIDFrom(r); id != "" {
		return id
	}
	id := NewRequestID()
	r.Header.Set(RequestIDHeader, id)
	return id
}

// SetResponseRequestID writes X-Request-ID response header if id is non-empty.
func SetResponseRequestID(w http.ResponseWriter, id string) {
	if w == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	w.Header().Set(RequestIDHeader, id)
}

// NewRequestID generates a compact, URL-safe correlation ID.
func NewRequestID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UTC().UnixNano())
	}
	return "req-" + hex.EncodeToString(buf)
}

func isSafeRequestID(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':', r == '/':
		default:
			return false
		}
	}
	return true
}

func encodeValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return "\"\""
		}
		if bareValueRe.MatchString(x) {
			return x
		}
		return strconv.Quote(x)
	case fmt.Stringer:
		s := strings.TrimSpace(x.String())
		if s == "" {
			return "\"\""
		}
		if bareValueRe.MatchString(s) {
			return s
		}
		return strconv.Quote(s)
	case time.Duration:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", x))
		if s == "" {
			return "\"\""
		}
		if bareValueRe.MatchString(s) {
			return s
		}
		return strconv.Quote(s)
	}
}

func shortLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DEBUG":
		return "DBG"
	case "WARN", "WARNING":
		return "WRN"
	case "ERROR":
		return "ERR"
	default:
		return "INF"
	}
}

func normalizeMessage(event string) string {
	event = strings.TrimSpace(event)
	if event == "" {
		return "log"
	}
	event = strings.ReplaceAll(event, "_", " ")
	event = strings.ReplaceAll(event, "-", " ")
	return event
}
