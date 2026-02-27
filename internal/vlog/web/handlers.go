package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/vNodesV/vProx/internal/vlog/db"
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
	Stats       map[string]int64
	RecentFlags []*db.IPAccount
}

type accountListData struct {
	pageBase
	Accounts []*db.IPAccount
	Total    int64
	Page     int
	PageSize int
}

type accountDetailData struct {
	pageBase
	Account        *db.IPAccount
	RecentRequests []*db.RequestEvent
	RecentLimits   []*db.RateLimitEvent
	ThreatFlagsArr []string
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

	flagged, err := s.db.ListTopThreatAccounts(10)
	if err != nil {
		log.Printf("[web] dashboard flagged: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := dashboardData{
		pageBase:    pageBase{BasePath: s.cfg.VLog.BasePath},
		Stats:       stats,
		RecentFlags: flagged,
	}
	if err := s.pages["dashboard.html"].ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("[web] dashboard render: %v", err)
	}
}

func (s *Server) handleAccountList(w http.ResponseWriter, r *http.Request) {
	page, pageSize := parsePagination(r, 1, 50)
	offset := (page - 1) * pageSize

	accounts, err := s.db.ListIPAccounts(pageSize, offset)
	if err != nil {
		log.Printf("[web] account list: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	total, err := s.db.CountIPAccounts()
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

	account, err := s.enricher.EnrichNow(ip)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (s *Server) handleAPIStats(w http.ResponseWriter, _ *http.Request) {
	stats, err := s.db.Stats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
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
	if pageSize < 1 || pageSize > 100 {
		pageSize = defaultSize
	}
	return page, pageSize
}
