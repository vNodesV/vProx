package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vNodesV/vProx/internal/vlog/db"
	"github.com/vNodesV/vProx/internal/vlog/intel"
	"github.com/vNodesV/vProx/internal/vlog/ufw"
)

// ---------------------------------------------------------------------------
// Template data structs
// ---------------------------------------------------------------------------

// pageBase is embedded in every template data struct to provide common
// values available to all templates, such as the URL base path.
type pageBase struct {
	BasePath    string
	AuthEnabled bool
}

type dashboardData struct {
	pageBase
	Stats             map[string]int64
	BlockedAccounts   []*db.IPAccount
	TopThreatAccounts []*db.IPAccount
}

type accountListData struct {
	pageBase
	Accounts []*db.IPAccount
	Total    int64
	Page     int
	PageSize int
	Search   string
}

type accountDetailData struct {
	pageBase
	Account        *db.IPAccount
	RecentRequests []*db.RequestEvent
	RecentLimits   []*db.RateLimitEvent
	ThreatFlagsArr []string
	Ports          []portInfo // pre-computed port status for display
	ShodanResult   *intel.ShodanResult
}

// portInfo holds display state for a single scanned port.
type portInfo struct {
	Port  int
	Label string // human-readable service name
	Open  bool
}

// standardPorts defines the ports displayed on the account page.
var standardPorts = []portInfo{
	{Port: 80, Label: "HTTP"},
	{Port: 443, Label: "HTTPS"},
	{Port: 22, Label: "SSH"},
	{Port: 26657, Label: "CometRPC"},
	{Port: 26656, Label: "P2P"},
	{Port: 1317, Label: "REST"},
	{Port: 9090, Label: "gRPC"},
}

// buildPortInfo parses the openPortsJSON array and marks ports open/closed.
func buildPortInfo(openPortsJSON string) []portInfo {
	var open []int
	_ = json.Unmarshal([]byte(openPortsJSON), &open)
	openSet := make(map[int]bool, len(open))
	for _, p := range open {
		openSet[p] = true
	}
	out := make([]portInfo, len(standardPorts))
	for i, sp := range standardPorts {
		out[i] = portInfo{Port: sp.Port, Label: sp.Label, Open: openSet[sp.Port]}
	}
	return out
}

// newPageBase returns a pageBase initialised from server config.
func (s *Server) newPageBase() pageBase {
	return pageBase{
		BasePath:    s.cfg.VLog.BasePath,
		AuthEnabled: s.authEnabled(),
	}
}

// ---------------------------------------------------------------------------
// Page handlers
// ---------------------------------------------------------------------------

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Exact-match root; reject other paths that fall through.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	stats, err := s.db.Stats()
	if err != nil {
		log.Printf("[web] dashboard stats: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	blocked, err := s.db.ListBlockedAccounts()
	if err != nil {
		log.Printf("[web] dashboard blocked: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	threats, err := s.db.ListTopThreatAccounts(10)
	if err != nil {
		log.Printf("[web] dashboard top threats: %v", err)
		threats = nil
	}

	data := dashboardData{
		pageBase:          s.newPageBase(),
		Stats:             stats,
		BlockedAccounts:   blocked,
		TopThreatAccounts: threats,
	}
	if err := s.pages["dashboard.html"].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("[web] dashboard render: %v", err)
	}
}

func (s *Server) handleAccountList(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r, 1, 50)
	// pageSize=0 means "all": use SQLite LIMIT -1 (no limit), offset=0.
	limit := pageSize
	if limit == 0 {
		limit = -1
	}
	offset := (page - 1) * pageSize
	if pageSize == 0 {
		offset = 0
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))

	var (
		accounts []*db.IPAccount
		total    int64
		err      error
	)
	if search != "" {
		accounts, err = s.db.SearchIPAccounts(search, limit, offset)
		if err != nil {
			log.Printf("[web] account search: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		total, err = s.db.CountSearchIPAccounts(search)
	} else {
		accounts, err = s.db.ListIPAccounts(limit, offset)
		if err != nil {
			log.Printf("[web] account list: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		total, err = s.db.CountIPAccounts()
	}
	if err != nil {
		log.Printf("[web] account count: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := accountListData{
		pageBase: s.newPageBase(),
		Accounts: accounts,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Search:   search,
	}
	if err := s.pages["accounts.html"].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("[web] account list render: %v", err)
	}
}

func (s *Server) handleAccountDetail(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if ip == "" {
		http.NotFound(w, r)
		return
	}

	account, err := s.db.GetIPAccount(ip)
	if err != nil {
		log.Printf("[web] account detail %s: %v", ip, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if account == nil {
		http.NotFound(w, r)
		return
	}

	reqs, err := s.db.ListRequestEventsByIP(ip, 20)
	if err != nil {
		log.Printf("[web] account requests %s: %v", ip, err)
		reqs = nil // non-fatal; show what we can
	}

	rls, err := s.db.ListRateLimitEventsByIP(ip, 20)
	if err != nil {
		log.Printf("[web] account ratelimits %s: %v", ip, err)
		rls = nil
	}

	var flags []string
	if account.ThreatFlags != "" {
		_ = json.Unmarshal([]byte(account.ThreatFlags), &flags)
	}

	data := accountDetailData{
		pageBase:       s.newPageBase(),
		Account:        account,
		RecentRequests: reqs,
		RecentLimits:   rls,
		ThreatFlagsArr: flags,
		Ports:          buildPortInfo(account.OpenPorts),
		ShodanResult:   intel.ParseShodanJSON(account.ShodanData),
	}
	if err := s.pages["account.html"].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("[web] account detail render: %v", err)
	}
}

// ---------------------------------------------------------------------------
// API handlers
// ---------------------------------------------------------------------------

func (s *Server) handleAPIIngest(w http.ResponseWriter, _ *http.Request) {
	processed, err := s.ingester.IngestAll()
	if err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"processed": processed})
}

func (s *Server) handleAPIAccountList(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	accounts, err := s.db.ListIPAccounts(limit, offset)
	if err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handleAPIAccountDetail(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if ip == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ip"})
		return
	}

	account, err := s.db.GetIPAccount(ip)
	if err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if account == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleAPIEnrich(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if net.ParseIP(ip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}
	if isPrivateIP(ip) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}

	if s.enricher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "enricher not configured"})
		return
	}

	// Stream progress via Server-Sent Events so the client can show real steps.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // tell nginx/apache not to buffer
	w.WriteHeader(http.StatusOK)

	// Remove write deadline — enrichment can take >30s at low rate-limit RPM.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	flusher, canFlush := w.(http.Flusher)

	var wMu sync.Mutex
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// Keepalive: send an SSE comment every 15s so Apache's idle-connection
	// timer never fires during slow provider lookups.
	kaDone := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-kaDone:
				return
			case <-t.C:
				wMu.Lock()
				fmt.Fprintf(w, ": ping\n\n")
				flush()
				wMu.Unlock()
			}
		}
	}()
	defer close(kaDone)

	emit := func(p intel.EnrichProgress) {
		data, _ := json.Marshal(p)
		wMu.Lock()
		fmt.Fprintf(w, "data: %s\n\n", data)
		flush()
		wMu.Unlock()
	}

	// Use context.Background() so provider saves complete even if the Apache
	// proxy closes the HTTP connection mid-stream.
	if _, err := s.enricher.EnrichStream(context.Background(), ip, true, emit); err != nil {
		log.Printf("[web] enrich %s: %v", ip, err)
	}
}

func (s *Server) handleAPIosint(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if net.ParseIP(ip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}
	if isPrivateIP(ip) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}

	if s.enricher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "enricher not configured"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Remove write deadline — OSINT scan can take >30s (port probes, latency).
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	flusher, canFlush := w.(http.Flusher)

	var wMu sync.Mutex
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// Keepalive: send an SSE comment every 15s so Apache's idle-connection
	// timer never fires during the port-probe phase.
	kaDone := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-kaDone:
				return
			case <-t.C:
				wMu.Lock()
				fmt.Fprintf(w, ": ping\n\n")
				flush()
				wMu.Unlock()
			}
		}
	}()
	defer close(kaDone)

	emit := func(p intel.EnrichProgress) {
		data, _ := json.Marshal(p)
		wMu.Lock()
		fmt.Fprintf(w, "data: %s\n\n", data)
		flush()
		wMu.Unlock()
	}

	// Use context.Background() so the OSINT scan completes and saves even if
	// Apache closes the proxy connection mid-stream.
	if _, err := s.enricher.OSINTStream(context.Background(), ip, emit); err != nil {
		log.Printf("[web] osint %s: %v", ip, err)
	}
}

// handleAPIInvestigate runs a full investigation: TI enrichment then OSINT scan,
// streaming progress via SSE. Each event carries a "phase" prefix in Step so
// the client popup can track two distinct stages in one stream.
func (s *Server) handleAPIInvestigate(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if net.ParseIP(ip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}
	if isPrivateIP(ip) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}

	if s.enricher == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "enricher not configured"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	flusher, canFlush := w.(http.Flusher)

	var wMu sync.Mutex
	flush := func() {
		if canFlush {
			flusher.Flush()
		}
	}

	// Keepalive: send an SSE comment every 15s so Apache's idle-connection
	// timer never fires during the silent gap between EnrichStream and
	// OSINTStream (or during slow port-probe phases). SSE comments are
	// ignored by browsers and the ReadableStream client.
	kaDone := make(chan struct{})
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-kaDone:
				return
			case <-t.C:
				wMu.Lock()
				fmt.Fprintf(w, ": ping\n\n")
				flush()
				wMu.Unlock()
			}
		}
	}()
	defer close(kaDone)

	emitPhase := func(phase string) func(intel.EnrichProgress) {
		return func(p intel.EnrichProgress) {
			p.Step = phase + ":" + p.Step
			p.Pct = p.Pct / 2 // scale each phase to 0-50 range
			if phase == "osint" {
				p.Pct += 50 // shift OSINT phase to 50-100
			}
			data, _ := json.Marshal(p)
			wMu.Lock()
			fmt.Fprintf(w, "data: %s\n\n", data)
			flush()
			wMu.Unlock()
		}
	}

	// Phase 1: TI enrichment (0-50%). Use context.Background() so saves complete
	// even if the Apache proxy times out and cancels the HTTP connection context.
	if _, err := s.enricher.EnrichStream(context.Background(), ip, true, emitPhase("ti")); err != nil {
		log.Printf("[web] investigate enrich %s: %v", ip, err)
	}

	// Phase 2: OSINT scan (50-100%).
	if _, err := s.enricher.OSINTStream(context.Background(), ip, emitPhase("osint")); err != nil {
		log.Printf("[web] investigate osint %s: %v", ip, err)
	}
}

func (s *Server) handleAPIStats(w http.ResponseWriter, _ *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAPIChart(w http.ResponseWriter, r *http.Request) {
	chartType := r.URL.Query().Get("type")
	daysStr := r.URL.Query().Get("days")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 365 {
		days = d
	}

	// Multi-series types return ChartSeries instead of []ChartPoint.
	switch chartType {
	case "ips_over_time":
		series, err := s.db.IPsOverTimeMulti(days)
		if err != nil {
			log.Printf("[web] internal error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, series)
		return
	case "requests_over_time":
		series, err := s.db.RequestsOverTimeMulti(days)
		if err != nil {
			log.Printf("[web] internal error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		writeJSON(w, http.StatusOK, series)
		return
	case "endpoint_summary":
		stats, err := s.db.EndpointSummary(30)
		if err != nil {
			log.Printf("[web] internal error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if stats == nil {
			stats = []db.EndpointStat{}
		}
		writeJSON(w, http.StatusOK, stats)
		return
	}

	var (
		points []db.ChartPoint
		err    error
	)
	switch chartType {
	case "ratelimits_over_time":
		points, err = s.db.RateLimitsOverTime(days)
	case "top_countries":
		points, err = s.db.TopCountries(10)
	case "status_breakdown":
		points, err = s.db.StatusBreakdown()
	case "threat_distribution":
		points, err = s.db.ThreatDistribution()
	case "top_ips_by_requests":
		points, err = s.db.TopIPsByRequests(10)
	case "requests_by_country":
		points, err = s.db.RequestsByCountry(10)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown chart type"})
		return
	}
	if err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if points == nil {
		points = []db.ChartPoint{}
	}
	writeJSON(w, http.StatusOK, points)
}

// ---------------------------------------------------------------------------
// Multi-location probe
// ---------------------------------------------------------------------------

// caProbeNodes and wwProbeNodes are check-host.net node IDs used for external
// probing. One is chosen at random each probe invocation.
// Node list sourced from https://check-host.net/nodes/hosts (verified live).
var caProbeNodes = []string{
	"ca1.node.check-host.net", // Vancouver, CA
}

var wwProbeNodes = []string{
	"fr2.node.check-host.net", // Paris, FR
	"de1.node.check-host.net", // Nuremberg, DE
	"de4.node.check-host.net", // Frankfurt, DE
	"nl1.node.check-host.net", // Amsterdam, NL
	"uk1.node.check-host.net", // Coventry, GB
	"fi1.node.check-host.net", // Helsinki, FI
	"jp1.node.check-host.net", // Tokyo, JP
	"sg1.node.check-host.net", // Singapore
	"us1.node.check-host.net", // Los Angeles, US
	"us2.node.check-host.net", // Dallas, US
	"br1.node.check-host.net", // Sao Paulo, BR
	"in1.node.check-host.net", // Mumbai, IN
}

type locResult struct {
	Code      int    `json:"code,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Node      string `json:"node,omitempty"`
}

type multiProbeResult struct {
	Host  string    `json:"host"`
	URL   string    `json:"url"`
	Local locResult `json:"local"`
	CA    locResult `json:"ca"`
	WW    locResult `json:"ww"`
}

// checkHostProbe submits an HTTP probe via check-host.net and polls for the
// result. Blocks for up to ~10 s. ctx cancellation is respected.
func checkHostProbe(ctx context.Context, targetURL, node string) locResult {
	shortNode := strings.TrimSuffix(node, ".node.check-host.net")
	client := &http.Client{Timeout: 5 * time.Second}

	// Submit.
	q := url.Values{"host": {targetURL}, "node": {node}}
	submitURL := "https://check-host.net/check-http?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, submitURL, nil)
	if err != nil {
		return locResult{Error: "bad req", Node: shortNode}
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return locResult{Error: "submit failed", Node: shortNode}
	}
	var submit struct {
		RequestID string `json:"request_id"`
	}
	json.NewDecoder(resp.Body).Decode(&submit) //nolint:errcheck
	resp.Body.Close()
	if submit.RequestID == "" {
		return locResult{Error: "no request id", Node: shortNode}
	}

	// Poll until result or deadline.
	pollURL := "https://check-host.net/check-result/" + submit.RequestID
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return locResult{Error: "cancelled", Node: shortNode}
		case <-time.After(2 * time.Second):
		}
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
		req2.Header.Set("Accept", "application/json")
		resp2, err2 := client.Do(req2)
		if err2 != nil {
			continue
		}
		var result map[string]json.RawMessage
		decErr := json.NewDecoder(resp2.Body).Decode(&result)
		resp2.Body.Close()
		if decErr != nil {
			continue
		}
		raw, ok := result[node]
		if !ok || string(raw) == "null" {
			continue // not ready yet
		}
		// Actual shape: [[status_int, latency_secs, msg_str, code_str|null, ip_str|null]]
		// status==1 → success; status==0 → failure.
		var rows [][]json.RawMessage
		if err4 := json.Unmarshal(raw, &rows); err4 != nil || len(rows) == 0 || len(rows[0]) == 0 {
			return locResult{Error: "parse error", Node: shortNode}
		}
		row := rows[0]
		var status float64
		if json.Unmarshal(row[0], &status) != nil {
			return locResult{Error: "bad status", Node: shortNode}
		}
		// row[1] = latency (float seconds)
		var latMs int64
		if len(row) > 1 {
			var lat float64
			if json.Unmarshal(row[1], &lat) == nil && lat > 0 {
				latMs = int64(lat * 1000)
			}
		}
		if status == 1 {
			// row[3] = HTTP code as string (e.g. "200", "301")
			var code int
			if len(row) > 3 {
				var codeStr string
				if json.Unmarshal(row[3], &codeStr) == nil {
					fmt.Sscanf(codeStr, "%d", &code) //nolint:errcheck
				}
			}
			return locResult{OK: true, Code: code, LatencyMs: latMs, Node: shortNode}
		}
		// row[2] = error message string
		var errMsg string
		if len(row) > 2 {
			json.Unmarshal(row[2], &errMsg) //nolint:errcheck
		}
		if errMsg == "" {
			errMsg = "probe error"
		}
		return locResult{Error: errMsg, Node: shortNode}
	}
	return locResult{Error: "timeout", Node: shortNode}
}

func (s *Server) handleAPIProbe(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if host == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host required"})
		return
	}
	if strings.ContainsAny(host, "/:?#@") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid host"})
		return
	}

	// SSRF guard — only hosts present in ingested data.
	stats, err := s.db.EndpointSummary(500)
	if err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	known := false
	for _, e := range stats {
		if strings.EqualFold(e.Host, host) {
			known = true
			break
		}
	}
	if !known {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "host not in dataset"})
		return
	}

	// Additional SSRF layer: reject if host is a literal private/loopback IP.
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(host) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "private address not allowed"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Second)
	defer cancel()

	// Step 1: local probe — discovers the best reachable URL.
	localR, bestURL := localProbe(host)

	// Step 2: fire external probes concurrently using the discovered URL.
	var caR, wwR locResult
	if bestURL == "" {
		caR = locResult{Error: "no reachable URL"}
		wwR = locResult{Error: "no reachable URL"}
	} else {
		caNode := caProbeNodes[rand.Intn(len(caProbeNodes))]
		wwNode := wwProbeNodes[rand.Intn(len(wwProbeNodes))]
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); caR = checkHostProbe(ctx, bestURL, caNode) }()
		go func() { defer wg.Done(); wwR = checkHostProbe(ctx, bestURL, wwNode) }()
		wg.Wait()
	}

	writeJSON(w, http.StatusOK, multiProbeResult{
		Host:  host,
		URL:   bestURL,
		Local: localR,
		CA:    caR,
		WW:    wwR,
	})
}

// localProbe tries candidate URLs for host in order, returning the first 2xx
// result (or first reachable non-2xx as fallback) plus the URL that was used.
func localProbe(host string) (locResult, string) {
	client := &http.Client{Timeout: 5 * time.Second}
	var fallbackR *locResult
	var fallbackURL string
	for _, target := range []string{
		"https://" + host + "/rpc/status",
		"https://" + host + "/cosmos/base/tendermint/v1beta1/node_info",
		"https://" + host + "/rpc/health",
		"https://" + host + "/",
		"http://" + host + "/rpc/status",
		"http://" + host + "/cosmos/base/tendermint/v1beta1/node_info",
		"http://" + host + "/rpc/health",
		"http://" + host + "/",
	} {
		start := time.Now()
		resp, err := client.Get(target) //nolint:noctx
		lat := time.Since(start).Milliseconds()
		if err != nil {
			continue
		}
		code := resp.StatusCode
		resp.Body.Close()
		if code < 400 {
			return locResult{OK: true, Code: code, LatencyMs: lat}, target
		}
		if fallbackR == nil {
			r := locResult{Code: code, LatencyMs: lat}
			fallbackR = &r
			fallbackURL = target
		}
	}
	if fallbackR != nil {
		return *fallbackR, fallbackURL
	}
	return locResult{Error: "unreachable"}, ""
}

func (s *Server) handleAPIBlock(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if net.ParseIP(ip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}

	// Parse optional reason from query param
	reason := r.URL.Query().Get("reason")
	if reason == "" {
		reason = "manual block"
	}

	if err := s.db.BlockIP(ip, reason); err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	ufwOK := true
	if err := ufw.Block(ip); err != nil {
		log.Printf("[web] ufw block %s: %v", ip, err)
		ufwOK = false
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ip":      ip,
		"blocked": true,
		"ufw":     ufwOK,
		"reason":  reason,
	})
}

func (s *Server) handleAPIUnblock(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if net.ParseIP(ip) == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid IP"})
		return
	}

	if err := s.db.UnblockIP(ip); err != nil {
		log.Printf("[web] internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	ufwOK := true
	if err := ufw.Unblock(ip); err != nil {
		log.Printf("[web] ufw unblock %s: %v", ip, err)
		ufwOK = false
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ip":      ip,
		"blocked": false,
		"ufw":     ufwOK,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[web] json encode: %v", err)
	}
}

// isPrivateIP reports whether the given IP string is a loopback, link-local,
// or private RFC1918/RFC4193 address. Used to prevent SSRF attacks.
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	private := []string{
		"127.0.0.0/8",    // loopback
		"::1/128",        // IPv6 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // link-local
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local (RFC4193)
		"100.64.0.0/10",  // shared address space (RFC6598)
	}
	for _, cidr := range private {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func queryInt(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func parsePagination(r *http.Request, defaultPage, defaultSize int) (page, pageSize int) {
	page = queryInt(r, "page", defaultPage)
	pageSize = queryInt(r, "page_size", defaultSize)
	if page < 1 {
		page = 1
	}
	// pageSize=0 means "all" (passed as LIMIT -1 to SQLite); negative values reset to default.
	if pageSize < 0 || (pageSize > 200 && pageSize != 0) {
		pageSize = defaultSize
	}
	return page, pageSize
}

// ---------------------------------------------------------------------------
// Auth handlers
// ---------------------------------------------------------------------------

// handleLoginPage renders the login form.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If auth not configured, redirect to dashboard.
	if !s.authEnabled() {
		http.Redirect(w, r, s.cfg.VLog.BasePath+"/", http.StatusFound)
		return
	}
	// If already logged in, redirect to dashboard.
	if cookie, err := r.Cookie("vlog_session"); err == nil && s.validSession(cookie.Value) {
		http.Redirect(w, r, s.cfg.VLog.BasePath+"/", http.StatusFound)
		return
	}
	data := struct {
		BasePath string
		Error    string
	}{
		BasePath: s.cfg.VLog.BasePath,
		Error:    r.URL.Query().Get("error"),
	}
	if err := s.pages["login.html"].Execute(w, data); err != nil {
		log.Printf("[web] login render: %v", err)
	}
}

// handleLoginSubmit processes login form submission.
func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	if !s.checkCredentials(username, password) {
		http.Redirect(w, r, s.cfg.VLog.BasePath+"/login?error=invalid", http.StatusFound)
		return
	}

	token, err := s.newSession()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "vlog_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24h
	})
	http.Redirect(w, r, s.cfg.VLog.BasePath+"/", http.StatusFound)
}

// handleLogout invalidates the session and redirects to login.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("vlog_session"); err == nil {
		s.deleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "vlog_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, s.cfg.VLog.BasePath+"/login", http.StatusFound)
}
