package intel

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/vNodesV/vProx/internal/vlog/db"
)

// ThreatLevel classifies a composite threat score.
type ThreatLevel string

const (
	ThreatUnknown    ThreatLevel = "unknown"
	ThreatClean      ThreatLevel = "clean"
	ThreatSuspicious ThreatLevel = "suspicious"
	ThreatMalicious  ThreatLevel = "malicious"
)

// ComputeScore calculates a composite threat score (0-100) from:
//   - abuseScore: AbuseIPDB confidence score (0-100), -1 if not fetched
//   - vtMalicious: VirusTotal malicious detections count, -1 if not fetched
//   - shodanRiskFlags: number of risky Shodan signals (open C2-indicative ports etc.)
//
// Weights: AbuseIPDB 40%, VirusTotal 40%, Shodan 20%.
// If a source is -1 (not fetched), distribute its weight to available sources.
// Returns -1 if no sources have data.
func ComputeScore(abuseScore, vtMalicious, shodanRiskFlags int64) int64 {
	type source struct {
		value  float64
		weight float64
	}

	var sources []source

	if abuseScore >= 0 {
		// AbuseIPDB score is already 0-100.
		v := float64(abuseScore)
		if v > 100 {
			v = 100
		}
		sources = append(sources, source{value: v, weight: 0.40})
	}

	if vtMalicious >= 0 {
		// Normalize VT malicious count: cap at 20 detections → 100.
		v := float64(vtMalicious) * 5.0
		if v > 100 {
			v = 100
		}
		sources = append(sources, source{value: v, weight: 0.40})
	}

	if shodanRiskFlags >= 0 {
		// Normalize Shodan risk flags: cap at 5 flags → 100.
		v := float64(shodanRiskFlags) * 20.0
		if v > 100 {
			v = 100
		}
		sources = append(sources, source{value: v, weight: 0.20})
	}

	if len(sources) == 0 {
		return -1
	}

	// Redistribute total weight across available sources proportionally.
	var totalWeight float64
	for _, s := range sources {
		totalWeight += s.weight
	}

	var score float64
	for _, s := range sources {
		score += s.value * (s.weight / totalWeight)
	}

	result := int64(score + 0.5) // round
	if result > 100 {
		result = 100
	}
	if result < 0 {
		result = 0
	}
	return result
}

// Level classifies a score into a ThreatLevel.
//
//	<0  → unknown
//	0-19  → clean
//	20-49 → suspicious
//	50-100 → malicious
func Level(score int64) ThreatLevel {
	switch {
	case score < 0:
		return ThreatUnknown
	case score <= 19:
		return ThreatClean
	case score <= 49:
		return ThreatSuspicious
	default:
		return ThreatMalicious
	}
}

// riskyPorts is the set of ports considered C2-indicative or risky.
var riskyPorts = map[int]bool{
	23:   true, // telnet
	2323: true, // alt telnet
	4444: true, // Meterpreter default
	6666: true, // IRC backdoor
	1080: true, // SOCKS proxy
	9050: true, // Tor SOCKS
	8080: true, // HTTP proxy (evaluated with label check)
}

// ExtractShodanRiskFlags counts risky signals from raw Shodan JSON data.
// Risky ports: 4444, 6666, 1080, 9050, 8080 (if labeled proxy), 23, 2323 (telnet).
// Returns 0 on parse error or empty data.
func ExtractShodanRiskFlags(shodanData string) int64 {
	if shodanData == "" {
		return 0
	}

	var raw struct {
		Ports []int `json:"ports"`
		Data  []struct {
			Port    int      `json:"port"`
			Product string   `json:"product"`
			Tags    []string `json:"tags"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(shodanData), &raw); err != nil {
		return 0
	}

	// Build a product label map from service banners for the proxy check.
	portLabels := make(map[int]string, len(raw.Data))
	for _, d := range raw.Data {
		portLabels[d.Port] = strings.ToLower(d.Product)
	}

	var flags int64
	seen := make(map[int]bool, len(raw.Ports))
	for _, p := range raw.Ports {
		if seen[p] {
			continue
		}
		seen[p] = true

		if !riskyPorts[p] {
			continue
		}
		// Port 8080 only counts if labeled as proxy.
		if p == 8080 {
			label := portLabels[p]
			if !strings.Contains(label, "proxy") {
				continue
			}
		}
		flags++
	}
	return flags
}

// BuildThreatFlags constructs a JSON array of human-readable threat flag
// strings from intel data on an IPAccount.
//
// Possible flags:
//
//	ABUSEIPDB_CONFIRMED    — AbuseScore >= 50
//	VT_MALICIOUS           — VTMalicious >= 3
//	SHODAN_OPEN_RISKY_PORT — ExtractShodanRiskFlags > 0
//	HIGH_RATELIMIT_EVENTS  — RatelimitEvents > 10
//	DATACENTER_ASN         — Org contains "hosting", "cloud", or "datacenter" (case-insensitive)
func BuildThreatFlags(acc *db.IPAccount) string {
	if acc == nil {
		return "[]"
	}

	var flags []string

	if acc.AbuseScore >= 50 {
		flags = append(flags, "ABUSEIPDB_CONFIRMED")
	}
	if acc.VTMalicious >= 3 {
		flags = append(flags, "VT_MALICIOUS")
	}
	if ExtractShodanRiskFlags(acc.ShodanData) > 0 {
		flags = append(flags, "SHODAN_OPEN_RISKY_PORT")
	}
	if acc.RatelimitEvents > 10 {
		flags = append(flags, "HIGH_RATELIMIT_EVENTS")
	}

	orgLower := strings.ToLower(acc.Org)
	for _, kw := range []string{"hosting", "cloud", "datacenter"} {
		if strings.Contains(orgLower, kw) {
			flags = append(flags, "DATACENTER_ASN")
			break
		}
	}

	if len(flags) == 0 {
		return "[]"
	}

	b, err := json.Marshal(flags)
	if err != nil {
		// Fallback: manual construction (should never happen with []string).
		var sb strings.Builder
		sb.WriteByte('[')
		for i, f := range flags {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteByte('"')
			sb.WriteString(f)
			sb.WriteByte('"')
		}
		sb.WriteByte(']')
		return sb.String()
	}
	return string(b)
}

// portsToJSON converts a slice of ints to a JSON array string.
func portsToJSON(ports []int) string {
	if len(ports) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, p := range ports {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.Itoa(p))
	}
	sb.WriteByte(']')
	return sb.String()
}

// hostnamesToJSON converts a slice of strings to a JSON array string.
func hostnamesToJSON(hosts []string) string {
	if len(hosts) == 0 {
		return "[]"
	}
	b, err := json.Marshal(hosts)
	if err != nil {
		return "[]"
	}
	return string(b)
}
