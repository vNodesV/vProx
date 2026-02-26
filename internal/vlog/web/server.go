// Package web provides an embedded HTTP server for the vLog dashboard.
//
// It serves an html/template + htmx UI for browsing IP accounts,
// viewing threat intelligence, querying log events, and triggering
// archive ingestion. All assets are embedded via go:embed for
// single-binary deployment.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/vNodesV/vProx/internal/vlog/config"
	"github.com/vNodesV/vProx/internal/vlog/db"
	"github.com/vNodesV/vProx/internal/vlog/ingest"
	"github.com/vNodesV/vProx/internal/vlog/intel"
)

//go:embed templates static
var webFS embed.FS

// templateFuncs provides arithmetic helpers for pagination in templates.
var templateFuncs = template.FuncMap{
	"add":      func(a, b int) int { return a + b },
	"subtract": func(a, b int) int { return a - b },
}

// Server is the vLog HTTP server. It owns the ServeMux, parsed
// templates, and references to the database and enrichment subsystems.
type Server struct {
	db       *db.DB
	enricher *intel.Enricher
	ingester *ingest.Ingester
	cfg      config.Config
	httpSrv  *http.Server
	pages    map[string]*template.Template
}

// New creates a Server, parses embedded templates, registers all routes,
// and returns a server ready to Start().
func New(d *db.DB, enricher *intel.Enricher, ingester *ingest.Ingester, cfg config.Config) (*Server, error) {
	// Each page template is parsed together with the base layout so
	// that block overrides (title, content) are scoped per page.
	pageFiles := []string{"dashboard.html", "accounts.html", "account.html"}
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

	s := &Server{
		db:       d,
		enricher: enricher,
		ingester: ingester,
		cfg:      cfg,
		pages:    pages,
	}

	mux := http.NewServeMux()

	// Page routes.
	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("GET /accounts", s.handleAccountList)
	mux.HandleFunc("GET /accounts/{ip}", s.handleAccountDetail)

	// API routes.
	mux.HandleFunc("POST /api/v1/ingest", s.handleAPIIngest)
	mux.HandleFunc("GET /api/v1/accounts", s.handleAPIAccountList)
	mux.HandleFunc("GET /api/v1/accounts/{ip}", s.handleAPIAccountDetail)
	mux.HandleFunc("POST /api/v1/enrich/{ip}", s.handleAPIEnrich)
	mux.HandleFunc("GET /api/v1/stats", s.handleAPIStats)

	// Static assets (CSS, JS, etc.) served from embedded FS.
	mux.Handle("GET /static/", http.FileServer(http.FS(webFS)))

	readTimeout := time.Duration(cfg.VLog.Server.ReadTimeoutSec) * time.Second
	writeTimeout := time.Duration(cfg.VLog.Server.WriteTimeoutSec) * time.Second

	s.httpSrv = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.VLog.Port),
		Handler:      mux,
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

// Shutdown performs a graceful shutdown, waiting for in-flight requests
// to complete or the context to expire.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}
