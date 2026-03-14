// Package ingest parses vProx log archives and loads events into the vOps database.
package ingest

import (
	"encoding/json"
	"strings"

	"github.com/vNodesV/vProx/internal/vops/db"
)

// ---------------------------------------------------------------------------
// main.log parser
// ---------------------------------------------------------------------------

// ParseLogLine parses one structured key=value line from main.log.
// Returns nil if the line does not represent a request event.
// archiveTS is used as the event timestamp (archive-level granularity).
//
// Current vProx log format (LineLifecycle):
//
//	10:23AM NEW ID=API1A2B status=COMPLETED method=GET from=1.2.3.4 \
//	  count=5 to=HOST endpoint=/rpc latency=12ms userAgent="..." \
//	  country=US module=vProx
func ParseLogLine(line, archiveName, archiveTS string) *db.RequestEvent {
	line = strings.TrimSpace(line)
	if len(line) == 0 || line[0] == '#' {
		return nil
	}

	// Tokenise: time lifecycle key=value...
	// Token 0 = time (ignored), Token 1 = lifecycle, remainder = kv pairs.
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return nil
	}

	// Skip token 0 (time) and token 1 (lifecycle); parse key=value from the rest.
	kv := parseKV(fields[2:])

	// Accept current module name (vProx / vProxWeb) and legacy aliases.
	mod := strings.ToLower(kv["module"])
	if mod != "vprox" && mod != "vproxweb" && mod != "access" && mod != "proxy" {
		return nil
	}

	// IP: "from" is the current field; "ip" is the legacy alias.
	ip := kv["from"]
	if ip == "" {
		ip = kv["ip"]
	}

	// Path: "endpoint" is the current field; "path" / "request" are legacy aliases.
	path := kv["endpoint"]
	if path == "" {
		path = kv["path"]
	}
	if path == "" {
		path = kv["request"]
	}

	// UserAgent: "userAgent" is the current field; "user_agent" / "ua" are legacy aliases.
	ua := kv["userAgent"]
	if ua == "" {
		ua = kv["user_agent"]
	}
	if ua == "" {
		ua = kv["ua"]
	}

	// Host: "to" is the current field; "host" is the legacy alias.
	host := kv["to"]
	if host == "" {
		host = kv["host"]
	}

	// RequestID: "ID" is the current field; "request_id" is the legacy alias.
	requestID := kv["ID"]
	if requestID == "" {
		requestID = kv["request_id"]
	}

	// Route: infer from the ID prefix (API, RPC, WSS, REQ) or fall back to "route" field.
	route := kv["route"]
	if route == "" && len(requestID) >= 3 {
		route = requestID[:3]
	}

	return &db.RequestEvent{
		Archive:   archiveName,
		Ts:        archiveTS,
		RequestID: requestID,
		IP:        ip,
		Method:    kv["method"],
		Host:      host,
		Route:     route,
		Status:    kv["status"],
		Country:   kv["country"],
		ASN:       kv["asn"],
		Path:      path,
		UserAgent: ua,
	}
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
