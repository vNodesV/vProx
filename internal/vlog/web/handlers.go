package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
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
	BasePath string
}

type dashboardData struct {
	pageBase
	Stats           map[string]int64
	BlockedAccounts []*db.IPAccount
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

	data := dashboardData{
		pageBase:        pageBase{BasePath: s.cfg.VLog.BasePath},
		Stats:           stats,
		BlockedAccounts: blocked,
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
		pageBase: pageBase{BasePath: s.cfg.VLog.BasePath},
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
		pageBase:       pageBase{BasePath: s.cfg.VLog.BasePath},
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"processed": processed})
}

func (s *Server) handleAPIAccountList(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)

	accounts, err := s.db.ListIPAccounts(limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
	if ip == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ip"})
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
				fmt.Fprintf(w, ": ping\n\n")
				flush()
			}
		}
	}()
	defer close(kaDone)

	emit := func(p intel.EnrichProgress) {
		data, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flush()
	}

	// Use context.Background() so provider saves complete even if the Apache
	// proxy closes the HTTP connection mid-stream.
	if _, err := s.enricher.EnrichStream(context.Background(), ip, true, emit); err != nil {
		log.Printf("[web] enrich %s: %v", ip, err)
	}
}

func (s *Server) handleAPIosint(w http.ResponseWriter, r *http.Request) {
	ip := r.PathValue("ip")
	if ip == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing ip"})
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
				fmt.Fprintf(w, ": ping\n\n")
				flush()
			}
		}
	}()
	defer close(kaDone)

	emit := func(p intel.EnrichProgress) {
		data, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flush()
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
				fmt.Fprintf(w, ": ping\n\n")
				flush()
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
			fmt.Fprintf(w, "data: %s\n\n", data)
			flush()
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

	var (
		points []db.ChartPoint
		err    error
	)
	switch chartType {
	case "ips_over_time":
		points, err = s.db.IPsOverTime(days)
	case "requests_over_time":
		points, err = s.db.RequestsOverTime(days)
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if points == nil {
		points = []db.ChartPoint{}
	}
	writeJSON(w, http.StatusOK, points)
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
