// Package web provides an embedded HTTP server for the vOps dashboard.
//
// It serves an html/template + htmx UI for browsing IP accounts,
// viewing threat intelligence, querying log events, and triggering
// archive ingestion. All assets are embedded via go:embed for
// single-binary deployment.
package web

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/vNodesV/vProx/internal/fleet"
	"github.com/vNodesV/vProx/internal/fleet/api"
	"github.com/vNodesV/vProx/internal/vops/config"
	"github.com/vNodesV/vProx/internal/vops/db"
	"github.com/vNodesV/vProx/internal/vops/ingest"
	"github.com/vNodesV/vProx/internal/vops/intel"
)

//go:embed templates static
var webFS embed.FS

// templateFuncs provides arithmetic helpers for pagination in templates.
var templateFuncs = template.FuncMap{
	"add":      func(a, b int) int { return a + b },
	"subtract": func(a, b int) int { return a - b },
	"multiply": func(a, b int) int { return a * b },
	"intSlice": func(vals ...int) []int { return vals },
	// threatClass returns a CSS class name for a threat score (int64).
	"threatClass": func(score int64) string {
		switch {
		case score <= 30:
			return "threat-fill-low"
		case score <= 60:
			return "threat-fill-medium"
		default:
			return "threat-fill-high"
		}
	},
}

// Server is the vOps HTTP server. It owns the ServeMux, parsed
// templates, and references to the database and enrichment subsystems.
type Server struct {
	db       *db.DB
	enricher *intel.Enricher
	ingester *ingest.Ingester
	cfg      config.Config
	home     string
	cfgPath  string // resolved path to vops.toml (may be legacy or new layout)
	version  string // binary version string, set at startup
	httpSrv  *http.Server
	pages    map[string]*template.Template
	fleet    *api.Handlers // nil when fleet module is not configured
	fleetSvc *fleet.Service

	// Session state for dashboard login.
	sessions   map[string]time.Time // token → expiry
	sessionMu  sync.RWMutex
	sessionKey []byte // 32-byte HMAC key, generated at startup

	// Brute-force protection: per-IP failed login tracking.
	loginMu      sync.Mutex
	loginAttempts map[string]*loginAttempt
}

// loginAttempt tracks failed login attempts for a single source IP.
type loginAttempt struct {
	count       int
	lockedUntil time.Time
}

// New creates a Server, parses embedded templates, registers all routes,
// and returns a server ready to Start().
// fleetSvc is optional — pass nil to disable the fleet module routes.
func New(d *db.DB, enricher *intel.Enricher, ingester *ingest.Ingester, cfg config.Config, fleetSvc *fleet.Service, cfgPath, appVersion string) (*Server, error) {
	// Each page template is parsed together with the base layout so
	// that block overrides (title, content) are scoped per page.
	pageFiles := []string{"dashboard.html", "accounts.html", "account.html", "settings.html"}
	pages := make(map[string]*template.Template, len(pageFiles))
	for _, pf := range pageFiles {
		t, err := template.New("").Funcs(templateFuncs).ParseFS(
			webFS, "templates/base.html", "templates/"+pf,
		)
		if err != nil {
			return nil, fmt.Errorf("web: parse template %s: %w", pf, err)
		}
		pages[pf] = t
	}

	// Parse standalone login template (not based on base.html).
	loginTmpl, err := template.New("login.html").Funcs(templateFuncs).ParseFS(webFS, "templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("web: parse template login.html: %w", err)
	}
	pages["login.html"] = loginTmpl

	// Generate session HMAC key.
	sessionKey := make([]byte, 32)
	if _, err := rand.Read(sessionKey); err != nil {
		return nil, fmt.Errorf("web: generate session key: %w", err)
	}

	s := &Server{
		db:            d,
		enricher:      enricher,
		ingester:      ingester,
		cfg:           cfg,
		home:          config.FindHome(),
		cfgPath:       cfgPath,
		version:       appVersion,
		pages:         pages,
		sessions:      make(map[string]time.Time),
		sessionKey:    sessionKey,
		loginAttempts: make(map[string]*loginAttempt),
	}
	if fleetSvc != nil {
		s.fleet = api.New(fleetSvc)
		s.fleetSvc = fleetSvc
	}

	mux := http.NewServeMux()

	// Login/logout routes — exempt from session check.
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLoginSubmit)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// Static assets — exempt from session check.
	// Serve only the "static/" subtree to prevent path traversal to templates/.
	staticSub, err := fs.Sub(webFS, "static")
	if err != nil {
		return nil, fmt.Errorf("web: embed static sub: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Page routes — session-protected.
	mux.Handle("GET /", s.requireSession(http.HandlerFunc(s.handleDashboard)))
	mux.Handle("GET /accounts", s.requireSession(http.HandlerFunc(s.handleAccountList)))
	mux.Handle("GET /accounts/{ip}", s.requireSession(http.HandlerFunc(s.handleAccountDetail)))
	mux.Handle("GET /settings", s.requireSession(http.HandlerFunc(s.handleSettingsPage)))
	mux.Handle("GET /settings/wizard", s.requireSession(http.HandlerFunc(s.handleWizardPage)))
	mux.Handle("GET /settings/api/config/current", s.requireSession(http.HandlerFunc(s.handleAPISettingsCurrent)))
	mux.Handle("POST /settings/api/config/import", s.requireSession(http.HandlerFunc(s.handleAPISettingsImport)))
	mux.Handle("POST /settings/api/config/remove", s.requireSession(http.HandlerFunc(s.handleAPISettingsRemove)))
	mux.Handle("POST /settings/api/config/apply", s.requireSession(http.HandlerFunc(s.handleAPISettingsApply)))
	mux.Handle("POST /settings/api/config/ports", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("ports"))))
	mux.Handle("POST /settings/api/config/settings", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("settings"))))
	mux.Handle("POST /settings/api/config/chain", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("chain"))))
	mux.Handle("POST /settings/api/config/vops", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("vops"))))
	mux.Handle("POST /settings/api/config/fleet", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("fleet"))))
	mux.Handle("POST /settings/api/config/infra", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("infra"))))
	mux.Handle("POST /settings/api/config/backup", s.requireSession(http.HandlerFunc(s.handleAPISettingsSave("backup"))))
	mux.Handle("POST /settings/api/config/done", s.requireSession(http.HandlerFunc(s.handleAPISettingsDone)))
	mux.Handle("POST /settings/api/config/preferences", s.requireSession(http.HandlerFunc(s.handleAPISettingsPreferences)))

	// API routes — session-protected.
	mux.Handle("POST /api/v1/ingest", s.requireSession(http.HandlerFunc(s.handleAPIIngest)))
	mux.Handle("GET /api/v1/ingest/stats", s.requireSession(http.HandlerFunc(s.handleAPIArchiveStats)))
	mux.Handle("POST /api/v1/ingest/backup", s.requireSession(http.HandlerFunc(s.handleAPIBackupAndIngest)))
	mux.Handle("GET /api/v1/accounts", s.requireSession(http.HandlerFunc(s.handleAPIAccountList)))
	mux.Handle("GET /api/v1/accounts/{ip}", s.requireSession(http.HandlerFunc(s.handleAPIAccountDetail)))
	mux.Handle("POST /api/v1/enrich/{ip}", s.requireSession(http.HandlerFunc(s.handleAPIEnrich)))
	mux.Handle("POST /api/v1/osint/{ip}", s.requireSession(http.HandlerFunc(s.handleAPIosint)))
	mux.Handle("POST /api/v1/investigate/{ip}", s.requireSession(http.HandlerFunc(s.handleAPIInvestigate)))
	mux.Handle("POST /api/v1/block/{ip}", s.requireSession(http.HandlerFunc(s.handleAPIBlock)))
	mux.Handle("POST /api/v1/unblock/{ip}", s.requireSession(http.HandlerFunc(s.handleAPIUnblock)))
	mux.Handle("POST /api/v1/ufw/sync", s.requireSession(http.HandlerFunc(s.handleAPIUFWSync)))
	mux.Handle("GET /api/v1/stats", s.requireSession(http.HandlerFunc(s.handleAPIStats)))
	mux.Handle("GET /api/v1/chart", s.requireSession(http.HandlerFunc(s.handleAPIChart)))
	mux.Handle("GET /api/v1/probe", s.requireSession(http.HandlerFunc(s.handleAPIProbe)))
	mux.Handle("GET /api/v1/fleet/chains/traffic", s.requireSession(http.HandlerFunc(s.handleAPIChainTraffic)))

	// Fleet module routes — only registered when fleet is configured.
	if s.fleet != nil {
		mux.Handle("GET /api/v1/fleet/vms",
			s.requireSession(http.HandlerFunc(s.fleet.HandleVMs)))
		mux.Handle("GET /api/v1/fleet/vms/status",
			s.requireSession(http.HandlerFunc(s.fleet.HandleVMStatus)))
		mux.Handle("GET /api/v1/fleet/chains",
			s.requireSession(http.HandlerFunc(s.fleet.HandleChains)))
		mux.Handle("GET /api/v1/fleet/chains/{chain}",
			s.requireSession(http.HandlerFunc(s.fleet.HandleChainStatus)))
		mux.Handle("GET /api/v1/fleet/deployments",
			s.requireSession(http.HandlerFunc(s.fleet.HandleDeployments)))
		mux.Handle("POST /api/v1/fleet/deploy",
			s.requireSession(http.HandlerFunc(s.fleet.HandleDeploy)))
		mux.Handle("GET /api/v1/fleet/chains/registered",
			s.requireSession(http.HandlerFunc(s.fleet.HandleRegisteredChains)))
		mux.Handle("POST /api/v1/fleet/chains/registered",
			s.requireSession(http.HandlerFunc(s.fleet.HandleRegisteredChains)))
		mux.Handle("DELETE /api/v1/fleet/chains/registered/{chain}",
			s.requireSession(http.HandlerFunc(s.fleet.HandleRegisteredChainDelete)))
		// POST alias for Apache environments that block DELETE method pass-through.
		mux.Handle("POST /api/v1/fleet/chains/registered/{chain}",
			s.requireSession(http.HandlerFunc(s.fleet.HandleRegisteredChainDelete)))
		mux.Handle("POST /api/v1/fleet/poll",
			s.requireSession(http.HandlerFunc(s.fleet.HandlePoll)))
	}

	readTimeout := time.Duration(cfg.VOps.Server.ReadTimeoutSec) * time.Second
	writeTimeout := time.Duration(cfg.VOps.Server.WriteTimeoutSec) * time.Second

	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.VOps.BindAddress, cfg.VOps.Port),
		Handler:      securityHeaders(mux),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	return s, nil
}

// Start begins listening on the configured port. It blocks until the
// server is shut down or encounters a fatal error.
func (s *Server) Start() error {
	return s.httpSrv.ListenAndServe()
}

// requireSession redirects to /login if no valid session cookie is present.
// If auth is not configured (PasswordHash empty), this is a no-op pass-through.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.VOps.Auth.PasswordHash == "" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie("vops_session")
		if err != nil || !s.validSession(cookie.Value) {
			http.Redirect(w, r, s.cfg.VOps.BasePath+"/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// newSession creates a new HMAC-signed session token with 24h TTL.
func (s *Server) newSession() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("web: newSession rand: %w", err)
	}
	mac := hmac.New(sha256.New, s.sessionKey)
	mac.Write(raw)
	token := hex.EncodeToString(raw) + "." + hex.EncodeToString(mac.Sum(nil))
	s.sessionMu.Lock()
	s.sessions[token] = time.Now().Add(24 * time.Hour)
	s.sessionMu.Unlock()
	return token, nil
}

// validSession reports whether token exists and has not expired.
// Expired tokens are removed from the map to prevent unbounded growth.
func (s *Server) validSession(token string) bool {
	s.sessionMu.RLock()
	expiry, ok := s.sessions[token]
	s.sessionMu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		s.sessionMu.Lock()
		delete(s.sessions, token)
		s.sessionMu.Unlock()
		return false
	}
	return true
}

// deleteSession removes a session token.
func (s *Server) deleteSession(token string) {
	s.sessionMu.Lock()
	delete(s.sessions, token)
	s.sessionMu.Unlock()
}

// authEnabled reports whether dashboard login is configured.
func (s *Server) authEnabled() bool {
	return s.cfg.VOps.Auth.PasswordHash != ""
}

// checkCredentials validates username + password against the stored config.
func (s *Server) checkCredentials(username, password string) bool {
	if username != s.cfg.VOps.Auth.Username {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(s.cfg.VOps.Auth.PasswordHash), []byte(password)) == nil
}

// checkLoginLock returns (true, retryAfterSeconds) when clientIP is locked out,
// (false, 0) otherwise. Stale entries older than 30 min are pruned on each call.
func (s *Server) checkLoginLock(clientIP string) (locked bool, retryAfter int) {
	const lockDuration = 5 * time.Minute
	const staleCutoff = 30 * time.Minute

	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	now := time.Now()
	// Prune stale entries.
	for ip, att := range s.loginAttempts {
		if now.Sub(att.lockedUntil) > staleCutoff && att.lockedUntil.IsZero() {
			delete(s.loginAttempts, ip)
		} else if !att.lockedUntil.IsZero() && now.Sub(att.lockedUntil) > staleCutoff {
			delete(s.loginAttempts, ip)
		}
	}

	att, ok := s.loginAttempts[clientIP]
	if !ok || att.lockedUntil.IsZero() {
		return false, 0
	}
	if now.Before(att.lockedUntil) {
		remaining := int(att.lockedUntil.Sub(now).Seconds()) + 1
		return true, remaining
	}
	// Lock expired — reset.
	att.count = 0
	att.lockedUntil = time.Time{}
	return false, 0
}

// recordLoginFailure increments the failed attempt counter for clientIP.
// After 5 failures, the IP is locked out for 5 minutes.
func (s *Server) recordLoginFailure(clientIP string) {
	const maxAttempts = 5
	const lockDuration = 5 * time.Minute

	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	att, ok := s.loginAttempts[clientIP]
	if !ok {
		att = &loginAttempt{}
		s.loginAttempts[clientIP] = att
	}
	att.count++
	if att.count >= maxAttempts {
		att.lockedUntil = time.Now().Add(lockDuration)
		att.count = 0
	}
}

// clearLoginAttempts resets the failure counter for clientIP on successful login.
func (s *Server) clearLoginAttempts(clientIP string) {
	s.loginMu.Lock()
	delete(s.loginAttempts, clientIP)
	s.loginMu.Unlock()
}

// requireAPIKey is middleware that enforces API key authentication.
// The key must be provided via the X-API-Key request header.
// If the server's configured APIKey is empty, all requests are rejected (key not configured).
//
//nolint:unused // middleware registered dynamically in future auth expansion
func (s *Server) requireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.VOps.APIKey == "" {
			http.Error(w, "endpoint disabled: api_key not configured in vops.toml", http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("X-API-Key") != s.cfg.VOps.APIKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// securityHeaders adds standard HTTP security headers to every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self';"+
				" script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://unpkg.com;"+
				" style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net https://fonts.googleapis.com;"+
				" font-src 'self' https://fonts.gstatic.com;"+
				" img-src 'self' data:;"+
				" connect-src 'self' https://cdn.jsdelivr.net")
		next.ServeHTTP(w, r)
	})
}

// Shutdown performs a graceful shutdown, waiting for in-flight requests
// to complete or the context to expire.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}
