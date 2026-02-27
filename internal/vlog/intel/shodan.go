package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	goshodan "github.com/ns3777k/go-shodan/v4/shodan"
)

// ShodanResult represents the enriched host data returned from Shodan.
type ShodanResult struct {
	IP          string          // IP address as string
	Org         string          // Organisation name
	ISP         string          // Internet service provider
	Country     string          // Full country name
	CountryCode string          // ISO country code
	ASN         string          // Autonomous system number
	Hostnames   []string        // Reverse-DNS hostnames
	Ports       []int           // Open ports reported by Shodan
	OS          string          // Detected OS (may be empty)
	Tags        []string        // Shodan host tags (e.g. "self-signed", "vpn")
	Vulns       []string        // Vulnerability IDs (CVEs) from Shodan
	Services    []ShodanService // Per-service detail
}

// ShodanService holds typed metadata for a single open service.
type ShodanService struct {
	Port      int
	Transport string // "tcp" or "udp"
	Product   string // e.g. "Apache httpd"
	Version   string // e.g. "2.4.51"
	CPE       []string
}

// CheckShodan queries Shodan /shodan/host/{ip} via the ns3777k/go-shodan client.
// Signature is identical to the previous hand-rolled implementation so callers
// in intel.go need zero changes.
//
// Returns (parsed result, raw JSON response body, error).
// Returns (nil, "", nil) if apiKey is empty — no-op, not an error.
func CheckShodan(apiKey, ip string, httpClient *http.Client) (result *ShodanResult, rawJSON string, err error) {
	if apiKey == "" {
		return nil, "", nil
	}

	client := goshodan.NewClient(httpClient, apiKey)

	host, err := client.GetServicesForHost(context.Background(), ip, nil)
	if err != nil {
		// Shodan returns HTTP 404 with {"error":"No information available for that IP."}
		// for unindexed IPs. Treat this as "no data" rather than an error so the
		// enrichment pipeline continues and the caller can emit shodan_none.
		if strings.Contains(err.Error(), "No information available") {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("shodan: GetServicesForHost %s: %w", ip, err)
	}

	// Re-marshal the typed response so callers that store rawJSON (e.g. intel
	// cache) and re-parse it via ExtractShodanRiskFlags still work correctly —
	// the JSON tags on goshodan.Host match the field names ExtractShodanRiskFlags
	// expects ("ports", "data[].port", "data[].product").
	raw, merr := json.Marshal(host)
	if merr != nil {
		rawJSON = "{}"
	} else {
		rawJSON = string(raw)
	}

	return hostToResult(host), rawJSON, nil
}

// CheckShodanSearch queries Shodan's /shodan/host/search endpoint.
// Returns matched hosts mapped to []*ShodanResult, the total hit count, and any error.
// Returns (nil, 0, nil) if apiKey is empty.
func CheckShodanSearch(apiKey, query string, httpClient *http.Client) (matches []*ShodanResult, total int, err error) {
	if apiKey == "" {
		return nil, 0, nil
	}

	client := goshodan.NewClient(httpClient, apiKey)
	opts := &goshodan.HostQueryOptions{Query: query}

	found, err := client.GetHostsForQuery(context.Background(), opts)
	if err != nil {
		return nil, 0, fmt.Errorf("shodan: GetHostsForQuery %q: %w", query, err)
	}

	matches = make([]*ShodanResult, 0, len(found.Matches))
	for _, m := range found.Matches {
		if r := hostDataToResult(m); r != nil {
			matches = append(matches, r)
		}
	}
	return matches, found.Total, nil
}

// ParseShodanJSON re-hydrates a *ShodanResult from a raw JSON blob that was
// produced by json.Marshal(*goshodan.Host) and stored in the intel cache / DB.
// Returns nil if raw is empty or unparseable.
func ParseShodanJSON(raw string) *ShodanResult {
	if raw == "" {
		return nil
	}
	var host goshodan.Host
	if err := json.Unmarshal([]byte(raw), &host); err != nil {
		return nil
	}
	return hostToResult(&host)
}

// ── internal mappers ──────────────────────────────────────────────────────────

// hostToResult maps a *goshodan.Host (single-host lookup) to *ShodanResult.
func hostToResult(h *goshodan.Host) *ShodanResult {
	if h == nil {
		return nil
	}

	services := make([]ShodanService, 0, len(h.Data))
	for _, d := range h.Data {
		if d == nil {
			continue
		}
		services = append(services, ShodanService{
			Port:      d.Port,
			Transport: d.Transport,
			Product:   d.Product,
			Version:   string(d.Version), // IntString is a named string type
			CPE:       d.CPE,
		})
	}

	ipStr := ""
	if h.IP != nil {
		ipStr = h.IP.String()
	}

	return &ShodanResult{
		IP:          ipStr,
		Org:         h.Organization, // field is Organization, not Org
		ISP:         h.ISP,
		Country:     h.Country,     // embedded HostLocation.Country
		CountryCode: h.CountryCode, // embedded HostLocation.CountryCode
		ASN:         h.ASN,
		Hostnames:   h.Hostnames,
		Ports:       h.Ports,
		OS:          h.OS,
		Vulns:       h.Vulnerabilities, // field is Vulnerabilities, not Vulns
		Services:    services,
	}
}

// hostDataToResult maps a *goshodan.HostData (search-result match) to *ShodanResult.
// Each HostData represents one service banner; Ports is synthesised as [d.Port].
func hostDataToResult(d *goshodan.HostData) *ShodanResult {
	if d == nil {
		return nil
	}

	var country, countryCode string
	if d.Location != nil {
		country = d.Location.Country
		countryCode = d.Location.CountryCode
	}

	ipStr := ""
	if d.IP != nil {
		ipStr = d.IP.String()
	}

	svc := ShodanService{
		Port:      d.Port,
		Transport: d.Transport,
		Product:   d.Product,
		Version:   string(d.Version),
		CPE:       d.CPE,
	}

	return &ShodanResult{
		IP:          ipStr,
		Org:         d.Organization,
		ISP:         d.ISP,
		Country:     country,
		CountryCode: countryCode,
		ASN:         d.ASN,
		Hostnames:   d.Hostnames,
		Ports:       []int{d.Port},
		OS:          d.OS,
		Services:    []ShodanService{svc},
	}
}
