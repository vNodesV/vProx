package configwizard

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
	chainconfig "github.com/vNodesV/vProx/internal/config"
	fleetcfg "github.com/vNodesV/vProx/internal/fleet/config"
)

//go:embed wizard.html
var wizardHTML embed.FS

// Web is the embedded HTTP wizard server.
type Web struct {
	home   string
	server *http.Server
	done   chan struct{}
}

// NewWeb creates a new Web wizard for the given $VPROX_HOME.
func NewWeb(home string) *Web {
	return &Web{home: home, done: make(chan struct{})}
}

// Run starts the web wizard on a random loopback port, opens the browser, and
// blocks until the operator clicks "Finish" or the process is interrupted.
func (w *Web) Run() error {
	// Bind to a random free loopback port (security: localhost only).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("wizard listen: %w", err)
	}
	addr := fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)

	mux := http.NewServeMux()
	w.registerRoutes(mux)

	w.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "wizard server error: %v\n", err)
		}
	}()

	fmt.Printf("\n  ✦ Config Wizard running at %s\n", addr)
	fmt.Printf("  Opening browser… (close tab and press Ctrl+C or click Finish to exit)\n\n")
	openBrowser(addr)

	<-w.done
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = w.server.Shutdown(ctx)
	fmt.Println("\n  ✓ Wizard finished.")
	return nil
}

// registerRoutes wires all HTTP handlers.
func (w *Web) registerRoutes(mux *http.ServeMux) {
	// Serve the embedded wizard.html at root.
	mux.HandleFunc("/", w.handleIndex)

	// Config read (pre-fill form).
	mux.HandleFunc("/api/config/current", w.enforceLocalhost(w.handleGetCurrent))

	// Per-step save endpoints.
	mux.HandleFunc("/api/config/ports", w.enforceLocalhost(w.handlePOST(w.saveStep("ports"))))
	mux.HandleFunc("/api/config/settings", w.enforceLocalhost(w.handlePOST(w.saveStep("settings"))))
	mux.HandleFunc("/api/config/chain", w.enforceLocalhost(w.handlePOST(w.saveStep("chain"))))
	mux.HandleFunc("/api/config/vlog", w.enforceLocalhost(w.handlePOST(w.saveStep("vlog"))))
	mux.HandleFunc("/api/config/fleet", w.enforceLocalhost(w.handlePOST(w.saveStep("fleet"))))
	mux.HandleFunc("/api/config/infra", w.enforceLocalhost(w.handlePOST(w.saveStep("infra"))))
	mux.HandleFunc("/api/config/backup", w.enforceLocalhost(w.handlePOST(w.saveStep("backup"))))

	// Shutdown signal.
	mux.HandleFunc("/api/config/done", w.enforceLocalhost(w.handleDone))
}

// enforceLocalhost is a security guard that rejects requests not from 127.0.0.1 or ::1.
func (w *Web) enforceLocalhost(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsLoopback()) {
			http.Error(rw, "forbidden — wizard only accepts connections from localhost", http.StatusForbidden)
			return
		}
		// Reject unexpected Origin headers (basic CSRF protection).
		origin := r.Header.Get("Origin")
		if origin != "" && !strings.HasPrefix(origin, "http://127.0.0.1:") && !strings.HasPrefix(origin, "http://localhost:") {
			http.Error(rw, "forbidden — cross-origin requests rejected", http.StatusForbidden)
			return
		}
		next(rw, r)
	}
}

// handlePOST wraps a handler to enforce HTTP POST only.
func (w *Web) handlePOST(next http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(rw, r)
	}
}

// handleIndex serves the embedded wizard.html.
func (w *Web) handleIndex(rw http.ResponseWriter, r *http.Request) {
	data, err := WizardHTML()
	if err != nil {
		http.Error(rw, "wizard.html not embedded", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	rw.Header().Set("X-Frame-Options", "DENY")
	rw.Header().Set("Cache-Control", "no-store")
	_, _ = rw.Write(data)
}

// handleGetCurrent reads all present TOML files and returns their values as JSON.
func (w *Web) handleGetCurrent(rw http.ResponseWriter, r *http.Request) {
	writeJSON(rw, CurrentSnapshot(w.home, r.URL.Query().Get("mode")))
}

// WizardHTML returns the embedded wizard HTML page.
func WizardHTML() ([]byte, error) {
	return wizardHTML.ReadFile("wizard.html")
}

// CurrentSnapshot returns the current config payload used to prefill the wizard.
func CurrentSnapshot(home, mode string) map[string]any {
	mode = normalizeDeployMode(mode)
	files := map[string]string{
		"ports":    configPath(home, "chains", "ports.toml"),
		"settings": configPath(home, "vprox", "settings.toml"),
		"vlog":     configPath(home, "vlog", "vlog.toml"),
		"fleet":    configPath(home, "fleet", "settings.toml"),
		"backup":   configPath(home, "backup", "backup.toml"),
	}
	out := make(map[string]any, len(files)+2)
	out["mode"] = mode
	if mode == "new" {
		return out
	}
	for k, path := range files {
		data, err := os.ReadFile(path)
		if err == nil {
			out[k] = string(data)
		}
	}

	if chain := loadFirstChain(home); chain != nil {
		out["chain"] = chain
	}

	infra := loadFirstInfra(home)
	if mode == "migration" {
		legacy := loadLegacyInfra(home)
		switch {
		case infra == nil:
			infra = legacy
		case legacy != nil:
			infra["vms"] = mergeInfraVMs(infra["vms"], legacy["vms"])
		}
	}
	if infra != nil {
		out["infra"] = infra
	}
	return out
}

func normalizeDeployMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "new":
		return "new"
	case "migration":
		return "migration"
	default:
		return "upgrade"
	}
}

func loadFirstChain(home string) map[string]any {
	dir := configPath(home, "chains")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !chainconfig.IsChainTOML(e.Name()) {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := configPath(home, "chains", name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var c chainconfig.ChainConfig
		if err := toml.Unmarshal(data, &c); err != nil {
			continue
		}
		return map[string]any{
			"file": name,
			"raw":  string(data),
		}
	}
	return nil
}

func loadFirstInfra(home string) map[string]any {
	dir := configPath(home, "infra")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".toml") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		path := configPath(home, "infra", name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var inf infraFile
		if err := toml.Unmarshal(data, &inf); err != nil {
			continue
		}
		inf.VMs = dedupeFleetVMs(inf.VMs)
		return map[string]any{
			"datacenter": strings.TrimSuffix(name, ".toml"),
			"host":       inf.Host,
			"vprox":      inf.VProx,
			"vms":        inf.VMs,
		}
	}
	return nil
}

func loadLegacyInfra(home string) map[string]any {
	path := configPath(home, "push", "vms.toml")
	cfg, err := fleetcfg.Load(path)
	if err != nil || cfg == nil {
		return nil
	}
	var host fleetcfg.Host
	if len(cfg.Hosts) > 0 {
		host = cfg.Hosts[0]
	}
	vms := dedupeFleetVMs(cfg.VMs)
	if len(vms) == 0 && host.Name == "" {
		return nil
	}
	dc := strings.ToLower(strings.TrimSpace(host.Datacenter))
	if dc == "" {
		for _, vm := range vms {
			if strings.TrimSpace(vm.Datacenter) != "" {
				dc = strings.ToLower(strings.TrimSpace(vm.Datacenter))
				break
			}
		}
	}
	if dc == "" {
		dc = strings.TrimSuffix(filepath.Base(path), ".toml")
	}
	return map[string]any{
		"datacenter": dc,
		"host":       host,
		"vprox":      vproxEntry{Name: "vProx"},
		"vms":        vms,
	}
}

func dedupeFleetVMs(vms []fleetcfg.VM) []fleetcfg.VM {
	if len(vms) <= 1 {
		return vms
	}
	seen := make(map[string]struct{}, len(vms))
	out := make([]fleetcfg.VM, 0, len(vms))
	for _, vm := range vms {
		key := vmIdentityKey(vm)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, vm)
	}
	return out
}

func mergeInfraVMs(primary, fallback any) []fleetcfg.VM {
	primaryVMs := toFleetVMs(primary)
	fallbackVMs := toFleetVMs(fallback)
	if len(primaryVMs) == 0 {
		return dedupeFleetVMs(fallbackVMs)
	}
	if len(fallbackVMs) == 0 {
		return dedupeFleetVMs(primaryVMs)
	}
	merged := append(primaryVMs, fallbackVMs...)
	return dedupeFleetVMs(merged)
}

func toFleetVMs(raw any) []fleetcfg.VM {
	switch v := raw.(type) {
	case []fleetcfg.VM:
		return v
	case []any:
		out := make([]fleetcfg.VM, 0, len(v))
		for _, row := range v {
			m, ok := row.(map[string]any)
			if !ok {
				continue
			}
			vm := fleetcfg.VM{
				Name:       fieldStr(m, "name", ""),
				Host:       fieldStr(m, "host", ""),
				LanIP:      fieldStr(m, "lan_ip", ""),
				PublicIP:   fieldStr(m, "public_ip", ""),
				Port:       fieldInt(m, "port", 0),
				User:       fieldStr(m, "user", ""),
				KeyPath:    fieldStr(m, "key_path", ""),
				Datacenter: fieldStr(m, "datacenter", ""),
				Type:       fieldStr(m, "type", ""),
				ChainName:  fieldStr(m, "chain_name", ""),
			}
			vm.Ping.Country = strings.ToUpper(fieldStr(m, "ping_country", ""))
			vm.Ping.Provider = fieldStr(m, "ping_provider", "")
			out = append(out, vm)
		}
		return out
	default:
		return nil
	}
}

// saveStep returns a handler that writes the POSTed form fields to the appropriate TOML file.
// For web mode, we delegate to the same terminal step functions by temporarily capturing
// the stdin-based prompts — instead we parse a JSON body with the field values directly.
func (w *Web) saveStep(step string) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(rw, r.Body, 512*1024) // 512 KB max
		var fields map[string]any
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			http.Error(rw, "invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := ApplyFields(w.home, step, fields); err != nil {
			http.Error(rw, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		writeJSON(rw, map[string]string{"status": "ok"})
	}
}

// handleDone signals the server to shut down.
func (w *Web) handleDone(rw http.ResponseWriter, r *http.Request) {
	writeJSON(rw, map[string]string{"status": "done"})
	close(w.done)
}

// writeJSON encodes v as JSON and writes it with content-type header.
func writeJSON(rw http.ResponseWriter, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.Header().Set("X-Content-Type-Options", "nosniff")
	_ = json.NewEncoder(rw).Encode(v)
}

// openBrowser opens url in the default system browser.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	// Non-blocking; ignore errors (user can always navigate manually).
	go func() {
		_ = runCmd(cmd, args...)
	}()
}
