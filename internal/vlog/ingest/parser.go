// Package ingest parses vProx log archives and loads events into the vLog database.
package ingest

import (
	"encoding/json"
	"strings"

	"github.com/vNodesV/vProx/internal/vlog/db"
)

// ---------------------------------------------------------------------------
// main.log parser
// ---------------------------------------------------------------------------

// ParseLogLine parses one structured key=value line from main.log.
// Returns nil if the line does not represent an access event (module=access
// or module=proxy for older format compatibility).
// archiveTS is used as the event timestamp (archive-level granularity).
func ParseLogLine(line, archiveName, archiveTS string) *db.RequestEvent {
	line = strings.TrimSpace(line)
	if len(line) == 0 || line[0] == '#' {
		return nil
	}

	// Tokenise: time level <rest>
	// Token 1 = time (ignored), Token 2 = level, remainder = message + kv pairs.
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil
	}

	// Skip token 0 (time) and token 1 (level); parse key=value from the rest.
	kv := parseKV(fields[2:])

	// Only process access / proxy module lines.
	mod := kv["module"]
	if mod != "access" && mod != "proxy" {
		return nil
	}

	ev := &db.RequestEvent{
		Archive:   archiveName,
		Ts:        archiveTS,
		RequestID: kv["request_id"],
		IP:        kv["ip"],
		Method:    kv["method"],
		Host:      kv["host"],
		Route:     kv["route"],
		Status:    kv["status"],
		Country:   kv["country"],
		ASN:       kv["asn"],
	}

	// Path: prefer explicit "path", fall back to "request" (older format).
	if p := kv["path"]; p != "" {
		ev.Path = p
	} else {
		ev.Path = kv["request"]
	}

	// UserAgent: both "user_agent" and "ua" are valid aliases.
	if ua := kv["user_agent"]; ua != "" {
		ev.UserAgent = ua
	} else {
		ev.UserAgent = kv["ua"]
	}

	return ev
}

// parseKV extracts key=value pairs from a slice of tokens.
// Values may be bare words or "quoted strings" that span multiple tokens.
func parseKV(tokens []string) map[string]string {
	m := make(map[string]string, len(tokens))
	i := 0
	for i < len(tokens) {
		t := tokens[i]
		eqIdx := strings.IndexByte(t, '=')
		if eqIdx < 1 {
			// Not a key=value token (part of freeform message); skip.
			i++
			continue
		}
		key := t[:eqIdx]
		val := t[eqIdx+1:]

		if strings.HasPrefix(val, `"`) {
			// Quoted value — may span multiple tokens.
			val = val[1:] // strip leading quote
			for !strings.HasSuffix(val, `"`) {
				i++
				if i >= len(tokens) {
					break
				}
				val += " " + tokens[i]
			}
			val = strings.TrimSuffix(val, `"`)
		}

		m[key] = val
		i++
	}
	return m
}

// ---------------------------------------------------------------------------
// rate-limit.jsonl parser
// ---------------------------------------------------------------------------

// rateLimitJSON is the on-disk JSON shape for rate-limit events.
type rateLimitJSON struct {
	Ts        string  `json:"ts"`
	Level     string  `json:"level"`
	Event     string  `json:"event"`
	Reason    string  `json:"reason"`
	RequestID string  `json:"request_id"`
	IP        string  `json:"ip"`
	Country   string  `json:"country"`
	ASN       string  `json:"asn"`
	Method    string  `json:"method"`
	Path      string  `json:"path"`
	Host      string  `json:"host"`
	UserAgent string  `json:"user_agent"`
	UA        string  `json:"ua"`
	RPS       float64 `json:"rps"`
	Burst     int     `json:"burst"`
}

// ParseRateLimitLine parses one JSONL line from rate-limit.jsonl.
// Returns nil on parse error or empty line.
func ParseRateLimitLine(line, archiveName string) *db.RateLimitEvent {
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}

	var r rateLimitJSON
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return nil
	}

	ua := r.UserAgent
	if ua == "" {
		ua = r.UA
	}

	return &db.RateLimitEvent{
		Archive:   archiveName,
		Ts:        r.Ts,
		RequestID: r.RequestID,
		IP:        r.IP,
		Event:     r.Event,
		Reason:    r.Reason,
		Method:    r.Method,
		Path:      r.Path,
		Host:      r.Host,
		Country:   r.Country,
		ASN:       r.ASN,
		UserAgent: ua,
		RPS:       r.RPS,
		Burst:     int64(r.Burst),
	}
}
