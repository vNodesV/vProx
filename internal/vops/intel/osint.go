package intel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vNodesV/vProx/internal/vops/db"
)

// ScanPorts is the set of ports probed by CheckOSINT / OSINTStream.
var ScanPorts = []int{22, 80, 443, 1317, 9090, 26656, 26657}

// OSINTResult holds the network-layer intelligence gathered by CheckOSINT.
type OSINTResult struct {
	RDNS      string  // comma-joined PTR records, empty if none
	OpenPorts []int   // TCP-reachable ports from ScanPorts
	LatencyMs float64 // min TCP dial latency across open ports (-1 if none open)
	Protocol  string  // "https", "http", or ""
	Moniker   string  // Cosmos RPC moniker (empty if not a Cosmos node)
	ChainID   string  // Cosmos RPC chain/network ID
	// Geo / org from ip-api.com (no key required)
	Org     string
	Country string
	ASN     string
}

// ipAPIResponse is the JSON shape returned by ip-api.com/json/{ip}.
type ipAPIResponse struct {
	Status      string `json:"status"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	Org         string `json:"org"`
	AS          string `json:"as"` // e.g. "AS13335 Cloudflare, Inc."
	Query       string `json:"query"`
}

// CheckOSINT performs network-layer OSINT for ip concurrently:
//   - Reverse DNS (PTR lookup)
//   - TCP port scan across ScanPorts (all ports in parallel, 1.5s timeout)
//   - IP geo/org via ip-api.com
//   - Protocol detection (HTTPS vs HTTP)
//   - Cosmos RPC probe (port 26657)
//
// All five operations run concurrently; total time ≈ max of their durations.
func CheckOSINT(ctx context.Context, ip string) (*OSINTResult, error) {
	res := &OSINTResult{LatencyMs: -1}

	// Shared HTTP client for all sub-requests (ip-api, protocol probe, Cosmos RPC).
	client := &http.Client{
		Timeout: 4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	// ---- Reverse DNS ----
	wg.Add(1)
	go func() {
		defer wg.Done()
		if ptrs, err := net.LookupAddr(ip); err == nil {
			var cleaned []string
			for _, p := range ptrs {
				cleaned = append(cleaned, strings.TrimSuffix(p, "."))
			}
			mu.Lock()
			res.RDNS = strings.Join(cleaned, ", ")
			mu.Unlock()
		}
	}()

	// ---- Concurrent port scan ----
	type portResult struct {
		port    int
		open    bool
		latency float64
	}
	portResults := make([]portResult, len(ScanPorts))
	wg.Add(1)
	go func() {
		defer wg.Done()
		var portWg sync.WaitGroup
		for i, port := range ScanPorts {
			portWg.Add(1)
			go func(idx, p int) {
				defer portWg.Done()
				addr := fmt.Sprintf("[%s]:%d", ip, p)
				if net.ParseIP(ip) != nil && net.ParseIP(ip).To4() != nil {
					addr = fmt.Sprintf("%s:%d", ip, p)
				}
				t0 := time.Now()
				conn, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
				elapsed := float64(time.Since(t0).Microseconds()) / 1000.0
				if err == nil {
					conn.Close()
					portResults[idx] = portResult{port: p, open: true, latency: elapsed}
				} else {
					portResults[idx] = portResult{port: p, open: false, latency: elapsed}
				}
			}(i, port)
		}
		portWg.Wait()
		minLatency := -1.0
		mu.Lock()
		for _, r := range portResults {
			if r.open {
				res.OpenPorts = append(res.OpenPorts, r.port)
				if minLatency < 0 || r.latency < minLatency {
					minLatency = r.latency
				}
			}
		}
		res.LatencyMs = minLatency
		mu.Unlock()
	}()

	// ---- IP geo / org (ip-api.com, no key required) ----
	wg.Add(1)
	go func() {
		defer wg.Done()
		if info, err := checkIPInfo(ctx, client, ip); err == nil {
			mu.Lock()
			res.Org = info.Org
			res.Country = info.Country
			// Normalise ASN: "AS13335 Cloudflare, Inc." → "AS13335"
			if parts := strings.SplitN(info.AS, " ", 2); len(parts) > 0 {
				res.ASN = parts[0]
			}
			mu.Unlock()
		}
	}()

	// ---- Protocol detection ----
	wg.Add(1)
	go func() {
		defer wg.Done()
		var proto string
		if probeHTTP(ctx, client, "https://"+ip) {
			proto = "https"
		} else if probeHTTP(ctx, client, "http://"+ip) {
			proto = "http"
		}
		if proto != "" {
			mu.Lock()
			res.Protocol = proto
			mu.Unlock()
		}
	}()

	// ---- Cosmos RPC probe ----
	wg.Add(1)
	go func() {
		defer wg.Done()
		for _, base := range []string{"http://" + ip + ":26657", "https://" + ip + ":26657"} {
			m, c, err := checkCosmosRPC(ctx, client, base)
			if err == nil && (m != "" || c != "") {
				mu.Lock()
				res.Moniker = m
				res.ChainID = c
				mu.Unlock()
				break
			}
		}
	}()

	wg.Wait()
	return res, nil
}

// OSINTStream runs OSINT for ip, streaming progress via emit, and saves
// results into the IPAccount record. Always gathers fresh data (no cache).
func (e *Enricher) OSINTStream(ctx context.Context, ip string, emit func(EnrichProgress)) (*db.IPAccount, error) {
	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)

	emit(EnrichProgress{Step: "rdns", Msg: "Resolving reverse DNS\u2026", Pct: 8})

	// Run the full OSINT check (concurrent internally).
	// Emit "scanning" while it runs, then report results.
	type osintDone struct {
		res *OSINTResult
		err error
	}
	ch := make(chan osintDone, 1)
	go func() {
		r, err := CheckOSINT(ctx, ip)
		ch <- osintDone{r, err}
	}()

	// Show intermediate progress while waiting.
	emit(EnrichProgress{Step: "portscan", Msg: "Scanning ports (22, 80, 443, 1317, 9090, 26656, 26657)\u2026", Pct: 20})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case done := <-ch:
		if done.err != nil {
			emit(EnrichProgress{Step: "osint_err", Msg: "OSINT error: " + done.err.Error(), Pct: 80, IsErr: true})
		}
		osint := done.res

		// Emit per-result events.
		if osint.Org != "" {
			msg := osint.Org
			if osint.Country != "" {
				msg += " \u2022 " + osint.Country
			}
			if osint.ASN != "" {
				msg += " (" + osint.ASN + ")"
			}
			emit(EnrichProgress{Step: "org_done", Msg: "Organization: " + msg, Pct: 35})
		} else {
			emit(EnrichProgress{Step: "org_none", Msg: "Organization: lookup failed", Pct: 35})
		}

		if osint.RDNS != "" {
			emit(EnrichProgress{Step: "rdns_done", Msg: "Reverse DNS: " + osint.RDNS, Pct: 45})
		} else {
			emit(EnrichProgress{Step: "rdns_none", Msg: "Reverse DNS: (none)", Pct: 45})
		}

		if len(osint.OpenPorts) > 0 {
			portStrs := make([]string, len(osint.OpenPorts))
			for i, p := range osint.OpenPorts {
				portStrs[i] = fmt.Sprintf("%d", p)
			}
			msg := "Open ports: " + strings.Join(portStrs, ", ")
			if osint.LatencyMs >= 0 {
				msg += fmt.Sprintf("  (%.1f ms)", osint.LatencyMs)
			}
			emit(EnrichProgress{Step: "ports_done", Msg: msg, Pct: 55})
		} else {
			emit(EnrichProgress{Step: "ports_none", Msg: "No ports open in scan range", Pct: 55})
		}

		if osint.Protocol != "" {
			emit(EnrichProgress{Step: "proto_done", Msg: "Protocol: " + osint.Protocol, Pct: 65})
		} else {
			emit(EnrichProgress{Step: "proto_none", Msg: "Protocol: no HTTP/HTTPS response", Pct: 65})
		}

		if osint.Moniker != "" || osint.ChainID != "" {
			emit(EnrichProgress{Step: "cosmos_done", Msg: fmt.Sprintf("Cosmos node: %s / %s", osint.Moniker, osint.ChainID), Pct: 78})
		} else {
			emit(EnrichProgress{Step: "cosmos_none", Msg: "Cosmos RPC: not a node", Pct: 78})
		}

		// ---- Update IPAccount ----
		emit(EnrichProgress{Step: "save", Msg: "Saving results\u2026", Pct: 90})

		acc, err := e.db.GetIPAccount(ip)
		if err != nil {
			acc = &db.IPAccount{
				IP:          ip,
				FirstSeen:   nowISO,
				LastSeen:    nowISO,
				Status:      string(ThreatUnknown),
				PingMs:      -1,
				ThreatScore: -1,
				VTMalicious: -1,
				AbuseScore:  -1,
			}
		}

		acc.RDNS = osint.RDNS
		acc.Protocol = osint.Protocol
		acc.Moniker = osint.Moniker
		acc.ChainID = osint.ChainID
		acc.PingMs = osint.LatencyMs
		acc.OSINTUpdatedAt = nowISO
		// Only overwrite Org/Country/ASN if ip-api.com returned data
		// (preserves Shodan-populated values if ip-api fails).
		if osint.Org != "" {
			acc.Org = osint.Org
		}
		if osint.Country != "" {
			acc.Country = osint.Country
		}
		if osint.ASN != "" {
			acc.ASN = osint.ASN
		}
		if len(osint.OpenPorts) > 0 {
			acc.OpenPorts = portsToJSON(osint.OpenPorts)
		}

		if err := e.db.UpsertIPAccount(acc); err != nil {
			emit(EnrichProgress{Step: "error", Msg: "Save failed: " + err.Error(), Pct: 100, IsErr: true})
			return acc, err
		}

		summary := fmt.Sprintf("%d port(s) open", len(osint.OpenPorts))
		if osint.Moniker != "" {
			summary += " \u2022 Cosmos: " + osint.Moniker
		}
		emit(EnrichProgress{Step: "done", Msg: "Complete \u2022 " + summary, Pct: 100})
		return acc, nil
	}
}

func probeHTTP(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode > 0
}

func checkCosmosRPC(ctx context.Context, client *http.Client, baseURL string) (moniker, chainID string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", "", err
	}
	var parsed struct {
		Result struct {
			NodeInfo struct {
				Moniker string `json:"moniker"`
				Network string `json:"network"`
			} `json:"node_info"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", "", err
	}
	return parsed.Result.NodeInfo.Moniker, parsed.Result.NodeInfo.Network, nil
}

// checkIPInfo queries ip-api.com (free, no key, 45 req/min) for geo/org data.
func checkIPInfo(ctx context.Context, client *http.Client, ip string) (*ipAPIResponse, error) {
	url := "http://ip-api.com/json/" + ip + "?fields=status,country,countryCode,org,as,query"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	if err != nil {
		return nil, err
	}
	var result ipAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("ip-api: status %s", result.Status)
	}
	return &result, nil
}
