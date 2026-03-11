package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // G108: pprof intentionally exposed on debug port (localhost only)
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	backup "github.com/vNodesV/vProx/internal/backup"
	"github.com/vNodesV/vProx/internal/config"
	"github.com/vNodesV/vProx/internal/cosmos"
	"github.com/vNodesV/vProx/internal/counter"
	"github.com/vNodesV/vProx/internal/geo"
	"github.com/vNodesV/vProx/internal/limit"
	applog "github.com/vNodesV/vProx/internal/logging"
	"github.com/vNodesV/vProx/internal/metrics"
	ws "github.com/vNodesV/vProx/internal/ws"
)

// --------------------- GLOBALS ---------------------

// startTime records when the vProx process started, used by /healthz.
var startTime = time.Now()

var (
	chains       = make(map[string]*config.ChainConfig)
	defaultPorts config.Ports

	vproxHome  string
	configDir  string
	chainsDir  string
	dataDir    string
	logsDir    string
	archiveDir string

	// Subdirectories under configDir for the new structured layout.
	chainsConfigDir string // $configDir/chains
	backupConfigDir string // $configDir/backup

	accessCountsPath string

	httpClient = &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:       100,
			IdleConnTimeout:    90 * time.Second,
			DisableCompression: false,
		},
	}

	chainLoggerMu sync.Mutex
	chainLoggers  = make(map[string]*log.Logger)
	chainLogFiles = make(map[string]*os.File)
)

const (
	rpcPrefix     = "/rpc"
	restPrefix    = "/rest"
	grpcPrefix    = "/grpc"
	grpcWebPrefix = "/grpc-web"
	apiPrefix     = "/api"
)

// --------------------- CONFIG LOADERS (TOML ONLY) ---------------------

func registerHost(host string, c *config.ChainConfig) error {
	if host == "" {
		return nil
	}
	if existing, ok := chains[host]; ok {
		if existing.ChainName != c.ChainName {
			return fmt.Errorf("duplicate host %q in chain %q conflicts with %q", host, c.ChainName, existing.ChainName)
		}
	}
	chains[host] = c
	return nil
}

func loadChains(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	type chainEntry struct {
		name string
		slug string // cosmos.directory slug; empty = no enrichment
		cfg  config.ChainConfig
	}

	// Pass 1: decode + validate all chain configs.
	var parsed []chainEntry
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !config.IsChainTOML(name) {
			continue
		}
		fpath := filepath.Join(dir, name)
		f, err := os.Open(fpath)
		if err != nil {
			return err
		}
		var c config.ChainConfig
		decErr := toml.NewDecoder(f).Decode(&c)
		f.Close()
		if decErr != nil {
			return fmt.Errorf("decode %s: %w", name, decErr)
		}
		if err := config.ValidateConfig(&c); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		slug := ""
		if c.ChainID != "" {
			slug = c.ChainName
			if slug == "" {
				slug = strings.SplitN(c.ChainID, "-", 2)[0]
			}
		}
		parsed = append(parsed, chainEntry{name: name, slug: slug, cfg: c})
	}

	// Pass 2: parallel cosmos.directory enrichment (only empty fields filled; non-fatal).
	// Running concurrently avoids blocking startup by N × httpTimeout when chain_id is set.
	if len(parsed) > 0 {
		var wg sync.WaitGroup
		for i := range parsed {
			if parsed[i].slug == "" {
				continue
			}
			wg.Add(1)
			go func(e *chainEntry) {
				defer wg.Done()
				cosmos.Enrich(e.slug, &e.cfg.DashboardName, &e.cfg.NetworkType, &e.cfg.RecommendedVersion, &e.cfg.Explorers)
			}(&parsed[i])
		}
		wg.Wait()
	}

	// Pass 3: register all chains.
	for i := range parsed {
		name := parsed[i].name
		c := &parsed[i].cfg

		// normalize alias lists
		for j, a := range c.RPCAliases {
			c.RPCAliases[j] = strings.ToLower(strings.TrimSpace(a))
		}
		for j, a := range c.RESTAliases {
			c.RESTAliases[j] = strings.ToLower(strings.TrimSpace(a))
		}
		for j, a := range c.APIAliases {
			c.APIAliases[j] = strings.ToLower(strings.TrimSpace(a))
		}

		base := c.Host
		if err := registerHost(base, c); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		if c.Expose.VHost {
			rp := c.Expose.VHostPrefix.RPC
			ap := c.Expose.VHostPrefix.REST
			if err := registerHost(rp+"."+base, c); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if err := registerHost(ap+"."+base, c); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}

		for _, h := range c.RPCAliases {
			if h != "" {
				if err := registerHost(h, c); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
		for _, h := range c.RESTAliases {
			if h != "" {
				if err := registerHost(h, c); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
		for _, h := range c.APIAliases {
			if h != "" {
				if err := registerHost(h, c); err != nil {
					return fmt.Errorf("%s: %w", name, err)
				}
			}
		}
	}

	if len(chains) == 0 {
		return errors.New("no chain configs found in " + dir)
	}
	return nil
}

// --------------------- LINK REWRITE & BANNERS ---------------------

// rewriteRegexes holds pre-compiled patterns for a given (IP, host) pair.
type rewriteRegexes struct {
	rpcIP, rpcHost   *regexp.Regexp
	restIP, restHost *regexp.Regexp
}

var (
	rewriteCacheMu sync.RWMutex
	rewriteCache   = make(map[string]*rewriteRegexes) // key: "ip|host"
)

func getRewriteRegexes(internalIP, baseHost string) *rewriteRegexes {
	key := internalIP + "|" + baseHost
	rewriteCacheMu.RLock()
	if r, ok := rewriteCache[key]; ok {
		rewriteCacheMu.RUnlock()
		return r
	}
	rewriteCacheMu.RUnlock()

	r := &rewriteRegexes{
		rpcIP:    regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(internalIP) + `:26657/?`),
		rpcHost:  regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(baseHost) + `:26657/?`),
		restIP:   regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(internalIP) + `:1317/?`),
		restHost: regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(baseHost) + `:1317/?`),
	}
	rewriteCacheMu.Lock()
	rewriteCache[key] = r
	rewriteCacheMu.Unlock()
	return r
}

func rewriteLinks(html, routePrefix, internalIP, baseHost, absoluteHost string, rpcVHost bool) string {
	re := getRewriteRegexes(internalIP, baseHost)
	switch routePrefix {
	case rpcPrefix:
		repl := "/rpc/"
		if rpcVHost {
			repl = "/"
		}
		html = re.rpcIP.ReplaceAllString(html, repl)
		html = re.rpcHost.ReplaceAllString(html, repl)

		if rpcVHost {
			html = strings.ReplaceAll(html, `href="/rpc/`, `href="/`)
			html = strings.ReplaceAll(html, `src="/rpc/`, `src="/`)
		}

	case restPrefix, apiPrefix:
		html = re.restIP.ReplaceAllString(html, "/")
		html = re.restHost.ReplaceAllString(html, "/")
	}

	// Absolute-link policy
	if absoluteHost != "" {
		switch routePrefix {
		case rpcPrefix:
			if rpcVHost {
				// rpc.<base> => absolute root
				html = strings.ReplaceAll(html, `href="/`, `href="https://`+absoluteHost+`/`)
				html = strings.ReplaceAll(html, `src="/`, `src="https://`+absoluteHost+`/`)
			} else {
				// path-based /rpc
				html = strings.ReplaceAll(html, `href="/rpc`, `href="https://`+absoluteHost+`/rpc`)
				html = strings.ReplaceAll(html, `src="/rpc`, `src="https://`+absoluteHost+`/rpc`)
			}
		case restPrefix:
			html = strings.ReplaceAll(html, `href="/rest`, `href="https://`+absoluteHost+`/rest`)
			html = strings.ReplaceAll(html, `src="/rest`, `src="https://`+absoluteHost+`/rest`)
		case apiPrefix:
			html = strings.ReplaceAll(html, `href="/api`, `href="https://`+absoluteHost+`/api`)
			html = strings.ReplaceAll(html, `src="/api`, `src="https://`+absoluteHost+`/api`)
		}
	}
	return html
}

func injectBannerFromString(html, banner string) string {
	if strings.TrimSpace(banner) == "" {
		return html
	}
	return strings.Replace(html, "<body>", "<body>\n<div class=\"banner\">\n"+banner+"\n</div>\n", 1)
}

func injectBannerFile(html, bannerPath string) (string, error) {
	b, err := os.ReadFile(bannerPath)
	if err != nil {
		return "", err
	}
	return strings.Replace(html, "<body>", "<body>\n<div class=\"banner\">\n"+string(b)+"\n</div>\n", 1), nil
}

func bannerPath(chain, routePrefix string) string {
	chain = strings.ToLower(chain)
	switch routePrefix {
	case rpcPrefix:
		return filepath.Join(configDir, "msg", chain, "rpc.msg")
	case restPrefix, apiPrefix:
		return filepath.Join(configDir, "msg", chain, "rest.msg")
	}
	return ""
}

// --------------------- LOGGING (3-line SUMMARY) ---------------------

func clientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		if ip := sanitizeIP(v); ip != "" {
			return ip
		}
	}
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		if ip := sanitizeIP(strings.TrimSpace(strings.Split(v, ",")[0])); ip != "" {
			return ip
		}
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// sanitizeIP validates that s is a well-formed IP address to prevent log injection.
func sanitizeIP(s string) string {
	if net.ParseIP(s) != nil {
		return s
	}
	return ""
}

func logRequestSummary(r *http.Request, proxied bool, route string, host string, start time.Time, statusCode int) {
	src := clientIP(r)
	hostNorm := normalizeHost(host)

	// counter
	srcQty := counter.Increment(src)

	durMS := time.Since(start).Milliseconds()
	dst := r.URL.RequestURI()
	ua := r.Header.Get("User-Agent")

	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" {
		country = geo.Country(clientIP(r))
	}
	if country == "" {
		country = "--"
	}

	// Prefer the typed request ID already set on the request header.
	// For WS routes, ws.go sets WSS{hex} before handler() runs.
	// For RPC/API vhost routes, handler() sets RPC/API{hex} based on resolved route.
	// For early error paths (pre-routing), fall back to path-prefix heuristic.
	var logID string
	switch {
	case strings.HasPrefix(route, "ws") || strings.HasPrefix(route, "websocket"):
		logID = applog.EnsureRequestID(r) // WSS{hex} set by ws.go
	default:
		if id := applog.RequestIDFrom(r); id != "" {
			logID = id
		} else {
			logID = applog.NewTypedID(config.PathPrefix(dst))
		}
	}

	// status: map limiter status to lifecycle token
	limStatus := strings.ToUpper(limit.StatusOf(r))
	status := "COMPLETED"
	if limStatus != "" && limStatus != "OK" {
		status = limStatus
	}
	// WS failure routes get their own status
	switch route {
	case "websocket":
		status = "CONNECTED"
	case "ws-deny":
		status = "DENIED"
	case "ws-upgrade-fail", "ws-backend-fail":
		status = "FAILED"
	}

	line := applog.LineLifecycle("NEW", "vProx",
		applog.F("ID", logID),
		applog.F("status", status),
		applog.F("method", r.Method),
		applog.F("statusCode", statusCode),
		applog.F("from", src),
		applog.F("count", srcQty),
		applog.F("to", strings.ToUpper(hostNorm)),
		applog.F("chainId", hostNorm),
		applog.F("endpoint", dst),
		applog.F("latency", fmt.Sprintf("%dms", durMS)),
		applog.F("userAgent", ua),
		applog.F("country", country),
	)
	log.Println(line)
	if ch, ok := chains[hostNorm]; ok {
		if cl := getChainLogger(ch); cl != nil {
			cl.Println(line)
		}
	}
}

// --------------------- HEALTH ---------------------

// healthHandler serves GET /healthz with a JSON body reporting service status.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status := "ok"
	httpCode := http.StatusOK

	// Degraded if no geo database is available.
	if !geo.IsReady() {
		status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpCode)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  status,
		"version": "1.0.0",
		"uptime":  time.Since(startTime).Round(time.Second).String(),
	})
}

// --------------------- UTILS ---------------------

func resolveVProxHome() string {
	if v := strings.TrimSpace(os.Getenv("VPROX_HOME")); v != "" {
		return v
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Join(h, ".vProx")
	}
	return ".vProx"
}

func normalizeHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" {
		return h
	}
	if strings.HasPrefix(h, "[") {
		if host, _, err := net.SplitHostPort(h); err == nil {
			return host
		}
		return strings.Trim(h, "[]")
	}
	if strings.Count(h, ":") > 1 {
		return h
	}
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}

func getChainLogger(c *config.ChainConfig) *log.Logger {
	if c == nil {
		return nil
	}
	file := strings.TrimSpace(c.Logging.File)
	if file == "" {
		return nil
	}
	if !filepath.IsAbs(file) {
		if strings.HasPrefix(file, "logs"+string(os.PathSeparator)) || strings.HasPrefix(file, "logs/") {
			file = filepath.Join(vproxHome, file)
		} else {
			file = filepath.Join(logsDir, file)
		}
	}

	chainLoggerMu.Lock()
	defer chainLoggerMu.Unlock()
	if lg, ok := chainLoggers[file]; ok {
		return lg
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return nil
	}
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	lg := log.New(f, "", 0)
	chainLoggers[file] = lg
	chainLogFiles[file] = f
	return lg
}

func closeChainLoggers() {
	chainLoggerMu.Lock()
	defer chainLoggerMu.Unlock()
	for path, f := range chainLogFiles {
		_ = f.Close()
		delete(chainLogFiles, path)
		delete(chainLoggers, path)
	}
}

func envBoolDefault(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func envInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return def
}

func envFloat(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		return n
	}
	return def
}

func envBytes(key string) int64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	return parseBytes(v)
}

func parseBytes(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}
	mult := int64(1)
	for _, suf := range []struct {
		S string
		M int64
	}{{"KB", 1 << 10}, {"MB", 1 << 20}, {"GB", 1 << 30}, {"TB", 1 << 40}, {"B", 1}} {
		if strings.HasSuffix(s, suf.S) {
			mult = suf.M
			s = strings.TrimSpace(strings.TrimSuffix(s, suf.S))
			break
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n * mult
	}
	return 0
}

// runServiceCommand executes "service vProx <action>" and uses sudo when available.
// This supports interactive password prompts and sudoers NOPASSWD setups.
func runServiceCommand(action string) error {
	bin := "sudo"
	args := []string{"service", "vProx", action}
	if _, err := exec.LookPath("sudo"); err != nil {
		bin = "service"
		args = []string{"vProx", action}
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// proxySettings holds values loaded from config/vprox/settings.toml.
// All fields are optional; zero/false means "use built-in default".
type proxySettings struct {
	RateLimit struct {
		RPS   float64 `toml:"rps"`
		Burst int     `toml:"burst"`
	} `toml:"rate_limit"`
	AutoQuarantine struct {
		Enabled      *bool   `toml:"enabled"`
		Threshold    int     `toml:"threshold"`
		WindowSec    int     `toml:"window_sec"`
		PenaltyRPS   float64 `toml:"penalty_rps"`
		PenaltyBurst int     `toml:"penalty_burst"`
		TTLSec       int     `toml:"ttl_sec"`
	} `toml:"auto_quarantine"`
	Debug struct {
		Enabled bool `toml:"enabled"`
		Port    int  `toml:"port"`
	} `toml:"debug"`
}

// loadProxySettings reads config/vprox/settings.toml when present.
// Returns zero-value struct (all defaults apply) if the file is absent or unparseable.
func loadProxySettings(configDir string) proxySettings {
	var s proxySettings
	data, err := os.ReadFile(filepath.Join(configDir, "vprox", "settings.toml"))
	if err != nil {
		return s
	}
	_ = toml.Unmarshal(data, &s)
	return s
}

// resolveBackupConfigPath returns the effective path for backup.toml.
// Checks $configDir/backup/backup.toml (new layout) first, then
// $configDir/backup.toml (backward compat).
func resolveBackupConfigPath(configDir string) string {
	newPath := filepath.Join(configDir, "backup", "backup.toml")
	if _, err := os.Stat(newPath); err == nil {
		return newPath
	}
	return filepath.Join(configDir, "backup.toml")
}

// listBackupArchives prints all .tar.gz archives in archiveDir to stdout.
func listBackupArchives(archiveDir string) error {
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No backup archives found.")
			return nil
		}
		return err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, _ := e.Info()
		size := ""
		if info != nil {
			b := info.Size()
			switch {
			case b >= 1<<20:
				size = fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
			case b >= 1<<10:
				size = fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
			default:
				size = fmt.Sprintf("%dB", b)
			}
		}
		fmt.Printf("  %s  (%s)\n", e.Name(), size)
		count++
	}
	if count == 0 {
		fmt.Println("No backup archives found.")
	} else {
		fmt.Printf("\n  Total: %d archive(s)\n", count)
	}
	return nil
}

// disableBackupInConfig sets automation=false in backup.toml.
// Uses a simple string replacement to preserve the rest of the file.
func disableBackupInConfig(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Write minimal file with automation=false
			return os.WriteFile(path, []byte("[backup]\nautomation = false\n"), 0o600)
		}
		return err
	}
	content := string(data)
	if strings.Contains(content, "automation = true") {
		content = strings.ReplaceAll(content, "automation = true", "automation = false")
	} else if !strings.Contains(content, "automation") {
		// Insert under [backup] header so the key doesn't land in a sub-table.
		if idx := strings.Index(content, "[backup]"); idx >= 0 {
			eol := strings.Index(content[idx:], "\n")
			if eol >= 0 {
				insert := idx + eol + 1
				content = content[:insert] + "automation = false\n" + content[insert:]
			} else {
				content += "\nautomation = false\n"
			}
		} else {
			content += "\n[backup]\nautomation = false\n"
		}
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// printBackupStatus prints backup automation state, trigger conditions, and
// the ETA until the next scheduled backup.
func printBackupStatus(cfgPath, statePath, archiveDir string) {
	cfg, loaded, _ := backup.LoadConfig(cfgPath)
	b := cfg.Backup

	fmt.Println("vProx Backup Status")
	fmt.Println("")

	automationLabel := "disabled"
	if b.Automation {
		automationLabel = "enabled"
	}
	effectivelyActive := b.Automation
	activeLabel := "inactive"
	if effectivelyActive {
		activeLabel = "active"
	}

	cfgSource := "defaults"
	if loaded {
		cfgSource = cfgPath
	}
	fmt.Printf("  Automation:       %s  (source: %s)\n", automationLabel, cfgSource)
	fmt.Printf("  Scheduler:        %s\n", activeLabel)
	fmt.Println("")

	if b.IntervalDays > 0 {
		fmt.Printf("  Trigger interval: every %d day(s)\n", b.IntervalDays)
	} else {
		fmt.Println("  Trigger interval: disabled (interval_days = 0)")
	}
	if b.MaxSizeMB > 0 {
		fmt.Printf("  Trigger max size: %d MB\n", b.MaxSizeMB)
	} else {
		fmt.Println("  Trigger max size: disabled (max_size_mb = 0)")
	}
	fmt.Printf("  Check interval:   every %d min\n", b.CheckIntervalMin)
	fmt.Println("")

	// Read last run time from state file.
	var lastRun time.Time
	if data, err := os.ReadFile(statePath); err == nil {
		if sec, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil {
			lastRun = time.Unix(sec, 0).UTC()
		}
	}

	if lastRun.IsZero() {
		fmt.Println("  Last backup:      never")
	} else {
		ago := time.Since(lastRun).Truncate(time.Minute)
		fmt.Printf("  Last backup:      %s  (%s ago)\n", lastRun.Format("2006-01-02 15:04:05 UTC"), ago)
	}

	if !effectivelyActive {
		fmt.Println("  Next backup ETA:  n/a (scheduler is inactive)")
	} else if b.IntervalDays > 0 && !lastRun.IsZero() {
		nextRun := lastRun.Add(time.Duration(b.IntervalDays) * 24 * time.Hour)
		eta := time.Until(nextRun).Truncate(time.Minute)
		if eta <= 0 {
			fmt.Println("  Next backup ETA:  due now (trigger condition met)")
		} else {
			fmt.Printf("  Next backup ETA:  %s  (in %s)\n", nextRun.Format("2006-01-02 15:04:05 UTC"), eta)
		}
	} else if b.IntervalDays > 0 && lastRun.IsZero() {
		fmt.Println("  Next backup ETA:  due now (no previous backup recorded)")
	} else {
		fmt.Println("  Next backup ETA:  n/a (interval trigger disabled)")
	}
	fmt.Println("")

	// Count archives.
	entries, err := os.ReadDir(archiveDir)
	if err == nil {
		count := 0
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".tar.gz") {
				count++
			}
		}
		fmt.Printf("  Archive dir:      %s\n", archiveDir)
		fmt.Printf("  Archives:         %d file(s)\n", count)
	}

	if !b.Automation {
		fmt.Println("")
		fmt.Println("  Automation is disabled. Use 'vProx --new-backup' to create a backup manually.")
	}
}

// resolveBackupExtraFiles returns two lists of absolute file paths:
//   - rotate: files to snapshot AND truncate (chain *.log files in logsDir).
//     Auto-discovery adds all *.log files in logsDir except mainLogPath.
//   - extra: files to snapshot only, not truncated (data, config, non-log files).
//
// main.log is always handled via Options.LogPath; it is excluded here.
// Each config entry may contain comma-separated filenames as a convenience
// for users who write ["file1,file2"] instead of ["file1","file2"].
func resolveBackupExtraFiles(cfg backup.BackupConfig, dataDir, logsDir, configDir, mainLogPath string) (rotate, extra []string) {
	splitNames := func(entries []string) []string {
		var out []string
		for _, entry := range entries {
			for _, name := range strings.Split(entry, ",") {
				name = strings.TrimSpace(name)
				if name != "" {
					out = append(out, name)
				}
			}
		}
		return out
	}

	mainLogClean := filepath.Clean(mainLogPath)

	// Auto-discover *.log files in logsDir (except main.log).
	if entries, err := os.ReadDir(logsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			p := filepath.Join(logsDir, e.Name())
			if filepath.Clean(p) == mainLogClean {
				continue
			}
			rotate = append(rotate, p)
		}
	}

	// Explicit log files from backup.toml: *.log → rotate, others → extra.
	for _, name := range splitNames(cfg.Backup.Files.Logs) {
		p := filepath.Join(logsDir, name)
		if filepath.Clean(p) == mainLogClean {
			continue
		}
		if strings.HasSuffix(name, ".log") {
			// Avoid duplicates with auto-discovered paths.
			if !config.ContainsString(rotate, p) {
				rotate = append(rotate, p)
			}
		} else {
			extra = append(extra, p)
		}
	}

	for _, name := range splitNames(cfg.Backup.Files.Data) {
		extra = append(extra, filepath.Join(dataDir, name))
	}
	for _, name := range splitNames(cfg.Backup.Files.Config) {
		extra = append(extra, filepath.Join(configDir, name))
	}
	return rotate, extra
}

// --------------------- CORE HANDLER ---------------------

func handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	metrics.IncActiveConnections()
	defer metrics.DecActiveConnections()
	host := normalizeHost(r.Host)

	chain, ok := chains[host]
	if !ok {
		http.Error(w, "Unknown host", http.StatusBadRequest)
		metrics.RecordProxyError("direct", "unknown_host")
		metrics.RecordRequest(r.Method, "direct", http.StatusBadRequest, time.Since(start))
		logRequestSummary(r, false, "direct", host, start, http.StatusBadRequest)
		return
	}

	// Resolve effective ports
	eff := defaultPorts
	if !chain.DefaultPorts {
		if chain.Ports.RPC != 0 {
			eff.RPC = chain.Ports.RPC
		}
		if chain.Ports.REST != 0 {
			eff.REST = chain.Ports.REST
		}
		if chain.Ports.GRPC != 0 {
			eff.GRPC = chain.Ports.GRPC
		}
		if chain.Ports.GRPCWeb != 0 {
			eff.GRPCWeb = chain.Ports.GRPCWeb
		}
		if chain.Ports.API != 0 {
			eff.API = chain.Ports.API
		}
	}

	// Detect vhost (rpc.<host> / api|rest.<host>) and explicit aliases
	isRPCvhost, isRESTvhost := false, false
	if chain.Expose.VHost {
		rp := chain.Expose.VHostPrefix.RPC
		ap := chain.Expose.VHostPrefix.REST
		if rp == "" {
			rp = "rpc"
		}
		if ap == "" {
			ap = "api"
		}
		isRPCvhost = strings.HasPrefix(host, rp+".") || config.InList(chain.RPCAliases, host)
		isRESTvhost = strings.HasPrefix(host, ap+".") || config.InList(chain.RESTAliases, host) || config.InList(chain.APIAliases, host)
	}

	var (
		targetURL   string
		bannerFile  string
		bannerHTML  string
		injectHTML  bool
		routePrefix string
		route       string
	)

	// 1) VHOST routing if enabled and matched
	if isRPCvhost && chain.Services.RPC {
		targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.RPC, r.URL.Path)
		route = "direct"
		routePrefix = rpcPrefix
		if chain.Features.RPCAddressMasking && (r.URL.Path == "/" || r.URL.Path == "") {
			injectHTML = true
			if chain.MsgRPC {
				bannerHTML = chain.Message.RPCMsg
				bannerFile = bannerPath(chain.ChainName, rpcPrefix)
			}
		}
	} else if isRESTvhost && chain.Services.REST {
		targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.REST, r.URL.Path)
		route = "direct"
		routePrefix = restPrefix

	} else {
		// 2) PATH-based routing on base host (if exposed)
		if chain.Expose.Path {
			switch {
			case strings.HasPrefix(r.URL.Path, rpcPrefix) && chain.Services.RPC:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.RPC, strings.TrimPrefix(r.URL.Path, rpcPrefix))
				route = "rpc"
				routePrefix = rpcPrefix
				if chain.Features.RPCAddressMasking && (r.URL.Path == "/rpc" || r.URL.Path == "/rpc/") {
					injectHTML = true
					if chain.MsgRPC {
						bannerHTML = chain.Message.RPCMsg
						bannerFile = bannerPath(chain.ChainName, rpcPrefix)
					}
				}

			case strings.HasPrefix(r.URL.Path, restPrefix) && chain.Services.REST:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.REST, strings.TrimPrefix(r.URL.Path, restPrefix))
				route = "rest"
				routePrefix = restPrefix

			case strings.HasPrefix(r.URL.Path, grpcPrefix) && chain.Services.GRPC:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.GRPC, strings.TrimPrefix(r.URL.Path, grpcPrefix))
				route = "rest"

			case strings.HasPrefix(r.URL.Path, grpcWebPrefix) && chain.Services.GRPCWeb:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.GRPCWeb, strings.TrimPrefix(r.URL.Path, grpcWebPrefix))
				route = "rest"

			case strings.HasPrefix(r.URL.Path, apiPrefix) && chain.Services.APIAlias:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.API, strings.TrimPrefix(r.URL.Path, apiPrefix))
				route = "rest"
				routePrefix = apiPrefix

			case (r.URL.Path == "/" || r.URL.Path == "") && chain.Services.REST:
				targetURL = fmt.Sprintf("http://%s:%d/", chain.IP, eff.REST)
				route = "rest"
			}
		}
	}

	if targetURL == "" {
		http.Error(w, "Not Found or service disabled", http.StatusNotFound)
		metrics.RecordProxyError("direct", "unknown_host")
		metrics.RecordRequest(r.Method, "direct", http.StatusNotFound, time.Since(start))
		logRequestSummary(r, false, "direct", host, start, http.StatusNotFound)
		return
	}

	// Always stamp a typed vProx request ID based on the resolved route.
	// Apache has its own access log for upstream correlation; we own this ID space.
	// If Apache forwarded an X-Request-ID, it is overwritten with the typed ID.
	requestID := applog.NewTypedID(config.RouteIDPrefix(routePrefix, route, isRPCvhost, isRESTvhost))
	r.Header.Set(applog.RequestIDHeader, requestID)
	applog.SetResponseRequestID(w, requestID)

	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Build upstream request (preserve method/body/headers)
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Request build error", http.StatusInternalServerError)
		metrics.RecordProxyError(route, "request_build_error")
		metrics.RecordRequest(r.Method, route, http.StatusInternalServerError, time.Since(start))
		logRequestSummary(r, false, route, host, start, http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	// Forward typed correlation ID to upstream.
	if requestID != "" {
		req.Header.Set(applog.RequestIDHeader, requestID)
	}

	// Propagate forwarding info
	req.Header.Set("X-Forwarded-Host", host)
	if xf := req.Header.Get("X-Forwarded-For"); xf == "" {
		req.Header.Set("X-Forwarded-For", clientIP(r))
	}

	// Proxy
	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "Backend error", http.StatusBadGateway)
		metrics.RecordProxyError(route, "backend_error")
		metrics.RecordRequest(r.Method, route, http.StatusBadGateway, time.Since(start))
		logRequestSummary(r, false, route, host, start, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	ctype := resp.Header.Get("Content-Type")
	willModify := injectHTML && strings.HasPrefix(ctype, "text/html")

	// Forward headers
	for k, v := range resp.Header {
		lk := strings.ToLower(k)
		// Always drop Content-Length; Go will recalc
		if lk == "content-length" {
			continue
		}
		// Only drop Content-Encoding if we will modify (decompress/rewrite)
		if willModify && lk == "content-encoding" {
			continue
		}
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	// If not modifying, stream raw (keep original encoding)
	if !willModify {
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		metrics.RecordRequest(r.Method, route, resp.StatusCode, time.Since(start))
		logRequestSummary(r, true, route, host, start, resp.StatusCode)
		return
	}

	// If modifying HTML, transparently handle gzip — set up reader
	// before committing the status code so error paths can still send 500.
	var reader io.Reader = resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			http.Error(w, "Gzip error", http.StatusInternalServerError)
			metrics.RecordProxyError(route, "backend_error")
			metrics.RecordRequest(r.Method, route, http.StatusInternalServerError, time.Since(start))
			logRequestSummary(r, false, route, host, start, http.StatusInternalServerError)
			return
		}
		defer gzReader.Close()
		reader = gzReader
	}
	w.WriteHeader(resp.StatusCode)

	// Decide absolute link policy
	var absoluteHost string
	switch strings.ToLower(chain.Features.AbsoluteLinks) {
	case "always":
		absoluteHost = host
	case "never":
		absoluteHost = ""
	default: // auto
		// heuristic: turn absolute on for common embed referers
		if strings.Contains(r.Header.Get("X-Forwarded-Host"), ".cosmos.directory") ||
			strings.Contains(r.Header.Get("Referer"), ".cosmos.directory") {
			absoluteHost = host
		}
	}

	// Read, rewrite, inject, respond (limit to 10 MB to prevent OOM)
	rawHTML, _ := io.ReadAll(io.LimitReader(reader, 10<<20))
	html := string(rawHTML)

	html = rewriteLinks(html, routePrefix, chain.IP, chain.Host, absoluteHost, isRPCvhost)

	if injectHTML {
		// prefer config message; fallback to file banner
		if strings.TrimSpace(bannerHTML) != "" {
			html = injectBannerFromString(html, bannerHTML)
		} else if bannerFile != "" {
			if mod, err := injectBannerFile(html, bannerFile); err == nil {
				html = mod
			}
		}
	}

	_, _ = w.Write([]byte(html))
	metrics.RecordRequest(r.Method, route, resp.StatusCode, time.Since(start))
	logRequestSummary(r, true, route, host, start, resp.StatusCode)
}

// --------------------- BACKUP -------------------

func main() {
	rawArgs := os.Args[1:]
	startMode := false
	restartSubcmd := false
	stopSubcmd := false

	// resolveHome scans args for --home <val> or --home=<val>, falling back to
	// VPROX_HOME env and then ~/.vProx.
	resolveHome := func(args []string) string {
		for i, a := range args {
			if a == "--home" && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, "--home=") {
				return strings.TrimPrefix(a, "--home=")
			}
		}
		if h := os.Getenv("VPROX_HOME"); h != "" {
			return h
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".vProx")
	}

	// printHelp is defined early so it can be used before flag.Parse().
	printHelp := func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "Usage: vProx <command> [--flags]")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Commands:")
		fmt.Fprintln(out, "  start                   run in foreground, emit logs to stdout (journalctl friendly)")
		fmt.Fprintln(out, "  stop                    stop the vProx.service daemon")
		fmt.Fprintln(out, "  restart                 restart the vProx.service daemon")
		fmt.Fprintln(out, "  fleet <sub> [flags]     manage remote VMs and deployments")
		fmt.Fprintln(out, "  mod   <sub> [flags]     manage vProx ecosystem modules")
		fmt.Fprintln(out, "  chain <sub> [flags]     chain node status and upgrade tracking")
		fmt.Fprintln(out, "  config [step] [--web]   interactive TOML configuration wizard")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Flags:")
		fmt.Fprintln(out, "  --addr string           listen address (default :3000)")
		fmt.Fprintln(out, "  --auto-burst int        override auto-quarantine burst (env: VPROX_AUTO_BURST)")
		fmt.Fprintln(out, "  --auto-rps float        override auto-quarantine RPS (env: VPROX_AUTO_RPS)")
		fmt.Fprintln(out, "  --burst int             override default burst (env: VPROX_BURST)")
		fmt.Fprintln(out, "  --chains string         override chains directory")
		fmt.Fprintln(out, "  --config string         override config directory")
		fmt.Fprintln(out, "  -d, --daemon            start as background daemon (sudo service vProx start)")
		fmt.Fprintln(out, "  --disable-auto          disable auto-quarantine")
		fmt.Fprintln(out, "  --disable-backup        disable automatic backup loop and persist to backup.toml")
		fmt.Fprintln(out, "  --dry-run               load everything but don't start server")
		fmt.Fprintln(out, "  --help                  show this help")
		fmt.Fprintln(out, "  --home string           override VPROX_HOME")
		fmt.Fprintln(out, "  --info                  show loaded config summary and exit")
		fmt.Fprintln(out, "  --list-backup           list available backup archives and exit")
		fmt.Fprintln(out, "  --log-file string       override main log file path")
		fmt.Fprintln(out, "  --new-backup            create a new backup archive and exit")
		fmt.Fprintln(out, "  --quiet                 suppress non-error output")
		fmt.Fprintln(out, "  --reset-count           reset persisted access counters (backup)")
		fmt.Fprintln(out, "  --rps float             override default RPS (env: VPROX_RPS)")
		fmt.Fprintln(out, "  --backup-status         show backup automation status and next-run ETA")
		fmt.Fprintln(out, "  --validate              validate configs and exit")
		fmt.Fprintln(out, "  --verbose               verbose logging output")
		fmt.Fprintln(out, "  --version               show version and exit")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Backup output goes to terminal + main.log. When run standalone (not via systemd),")
		fmt.Fprintln(out, "use 'journalctl -t vProx' to see backup entries in the journal.")
	}

	// No args → print help
	if len(rawArgs) == 0 {
		printHelp()
		os.Exit(0)
	}

	switch rawArgs[0] {
	case "start":
		startMode = true
		rawArgs = rawArgs[1:]
	case "restart":
		restartSubcmd = true
		rawArgs = rawArgs[1:]
	case "stop":
		stopSubcmd = true
		rawArgs = rawArgs[1:]
	case "fleet":
		runFleetCmd(resolveHome(rawArgs[1:]), rawArgs[1:])
		os.Exit(0)
	case "mod":
		runModCmd(resolveHome(rawArgs[1:]), rawArgs[1:])
		os.Exit(0)
	case "chain":
		runChainCmd(resolveHome(rawArgs[1:]), rawArgs[1:])
		os.Exit(0)
	case "config":
		runConfigCmd(resolveHome(rawArgs[1:]), rawArgs[1:])
		os.Exit(0)
	default:
		// Unknown bare word (not a flag) → error
		if !strings.HasPrefix(rawArgs[0], "-") {
			fmt.Fprintf(os.Stderr, "vProx: unknown command %q\n\n", rawArgs[0])
			printHelp()
			os.Exit(1)
		}
	}
	os.Args = append([]string{os.Args[0]}, rawArgs...)

	// Parse flags
	newBackupFlag := flag.Bool("new-backup", false, "create a new backup archive and exit")
	listBackupFlag := flag.Bool("list-backup", false, "list available backup archives and exit")
	statusFlag := flag.Bool("backup-status", false, "show backup automation status and next-run ETA")
	daemonFlag := flag.Bool("daemon", false, "start as background daemon (sudo service vProx start)")
	daemonShortFlag := flag.Bool("d", false, "alias for --daemon")
	// --backup kept as hidden alias for --new-backup (backward compatibility)
	backupFlagAlias := flag.Bool("backup", false, "")
	var resetCount bool
	flag.BoolVar(&resetCount, "reset_count", false, "reset persisted access counters (for backup mode)")
	flag.BoolVar(&resetCount, "reset-count", false, "reset persisted access counters (for backup mode)")
	homeFlag := flag.String("home", "", "override VPROX_HOME")
	configFlag := flag.String("config", "", "override config directory")
	chainsFlag := flag.String("chains", "", "override chains directory")
	addrFlag := flag.String("addr", "", "listen address (default :3000)")
	logFileFlag := flag.String("log-file", "", "override main log file path")
	validateFlag := flag.Bool("validate", false, "validate configs and exit")
	dryRunFlag := flag.Bool("dry-run", false, "load everything but don't start server")
	verboseFlag := flag.Bool("verbose", false, "verbose logging output")
	quietFlag := flag.Bool("quiet", false, "suppress non-error output")
	versionFlag := flag.Bool("version", false, "show version and exit")
	infoFlag := flag.Bool("info", false, "show loaded config summary and exit")
	rpsFlag := flag.Float64("rps", 0, "override default RPS (env: VPROX_RPS)")
	burstFlag := flag.Int("burst", 0, "override default burst (env: VPROX_BURST)")
	autoRpsFlag := flag.Float64("auto-rps", 0, "override auto-quarantine RPS (env: VPROX_AUTO_RPS)")
	autoBurstFlag := flag.Int("auto-burst", 0, "override auto-quarantine burst (env: VPROX_AUTO_BURST)")
	disableAutoFlag := flag.Bool("disable-auto", false, "disable auto-quarantine")
	disableBackupFlag := flag.Bool("disable-backup", false, "disable automatic backup loop")

	flag.Usage = printHelp

	flag.Parse()

	// Handle version flag first
	if *versionFlag {
		fmt.Println("vProx - Reverse proxy with rate limiting and geolocation")
		fmt.Println("Version: 1.0.0")
		// TODO: Build with ldflags for version: go build -ldflags \"-X main.BuildVersion=...\"
		os.Exit(0)
	}

	// restart command: delegate to service command
	if restartSubcmd {
		if err := runServiceCommand("restart"); err != nil {
			fmt.Fprintf(os.Stderr, "restart failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// stop command: delegate to service command
	if stopSubcmd {
		if err := runServiceCommand("stop"); err != nil {
			fmt.Fprintf(os.Stderr, "stop failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// --daemon / -d: start service via service command
	if *daemonFlag || *daemonShortFlag {
		if err := runServiceCommand("start"); err != nil {
			fmt.Fprintf(os.Stderr, "daemon start failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Resolve home directory
	vproxHome = resolveVProxHome()
	if *homeFlag != "" {
		vproxHome = *homeFlag
	}
	if vproxHome != "" {
		_ = os.Setenv("VPROX_HOME", vproxHome)
	}

	// Resolve directories
	configDir = filepath.Join(vproxHome, "config")
	if *configFlag != "" {
		if filepath.IsAbs(*configFlag) {
			configDir = *configFlag
		} else {
			configDir = filepath.Join(vproxHome, *configFlag)
		}
	}

	chainsDir = filepath.Join(vproxHome, "chains")
	if *chainsFlag != "" {
		if filepath.IsAbs(*chainsFlag) {
			chainsDir = *chainsFlag
		} else {
			chainsDir = filepath.Join(vproxHome, *chainsFlag)
		}
	}

	dataDir = filepath.Join(vproxHome, "data")
	logsDir = filepath.Join(dataDir, "logs")
	archiveDir = filepath.Join(logsDir, "archives")
	accessCountsPath = filepath.Join(dataDir, "access-counts.json")

	chainsConfigDir = filepath.Join(configDir, "chains")
	backupConfigDir = filepath.Join(configDir, "backup")

	// Create directories
	for _, dir := range []string{configDir, chainsConfigDir, backupConfigDir, dataDir, logsDir, archiveDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("Could not create directory %s: %v", dir, err)
		}
	}

	// Resolve log file
	mainLogPath := filepath.Join(logsDir, "main.log")
	if *logFileFlag != "" {
		if filepath.IsAbs(*logFileFlag) {
			mainLogPath = *logFileFlag
		} else {
			mainLogPath = filepath.Join(logsDir, *logFileFlag)
		}
	}

	// Setup logging.
	// - start mode: mirror logs to both stdout (journald) and main.log (tail -f)
	// - default mode: append to main.log only
	if err := os.MkdirAll(filepath.Dir(mainLogPath), 0o755); err != nil {
		log.Fatalf("Could not create log directory for %s: %v", mainLogPath, err)
	}
	f, err := os.OpenFile(mainLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("Could not open %s: %v", mainLogPath, err)
	}
	defer f.Close()

	// Consolidate backup mode: --new-backup or legacy --backup alias
	doBackup := *newBackupFlag || *backupFlagAlias
	doListBackup := *listBackupFlag
	doStatus := *statusFlag

	if (startMode && !*quietFlag) || doBackup {
		log.SetOutput(&applog.SplitLogWriter{Stdout: os.Stdout, File: f, Colorize: true})
	} else {
		// In non-start mode we keep file-only behavior.
		// With --quiet in start mode, also keep file-only output.
		log.SetOutput(f)
	}
	log.SetFlags(0) // no default date/time; our logger prints its own header

	if doListBackup {
		if err := listBackupArchives(archiveDir); err != nil {
			fmt.Fprintf(os.Stderr, "list-backup failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if doStatus {
		printBackupStatus(resolveBackupConfigPath(configDir), filepath.Join(dataDir, "backup.last"), archiveDir)
		return
	}

	if doBackup {
		if resetCount {
			if err := counter.Reset(accessCountsPath); err != nil {
				log.Fatalf("Failed to reset access counters: %v", err)
			}
			applog.Print("INFO", "access", "counter_reset", applog.F("path", accessCountsPath))
		}
		bupCfg, bupLoaded, _ := backup.LoadConfig(resolveBackupConfigPath(configDir))
		listSrc := "default"
		if bupLoaded {
			listSrc = "loaded"
		}
		rotateExtra, extraFiles := resolveBackupExtraFiles(bupCfg, dataDir, logsDir, configDir, mainLogPath)
		if err := backup.RunOnce(backup.Options{
			LogPath:     mainLogPath,
			ArchiveDir:  archiveDir,
			StatePath:   filepath.Join(dataDir, "backup.last"),
			Method:      "MANUAL",
			RotateExtra: rotateExtra,
			ExtraFiles:  extraFiles,
			ListSource:  listSrc,
		}); err != nil {
			log.Fatalf("Backup failed: %v", err)
		}
		// Notify vLog (non-fatal). Prefer services.toml/ports.toml vlog_url, fall back to env.
		vlogURL := os.Getenv("VLOG_URL")
		for _, p := range []string{
			filepath.Join(chainsConfigDir, "services.toml"),
			filepath.Join(chainsConfigDir, "ports.toml"),
			filepath.Join(configDir, "ports.toml"),
		} {
			if pp, err := config.LoadPorts(p); err == nil && pp.VLogURL != "" {
				vlogURL = pp.VLogURL
				break
			}
		}
		notifyVLog(vlogURL)
		return
	}

	// Geo status line
	applog.Print("INFO", "geo", "status", applog.F("message", geo.Info()))
	counter.Load(accessCountsPath)
	stopCounterTicker := counter.StartTicker(accessCountsPath)

	// Load configs (TOML only)
	// Search order: config/chains/services.toml → config/chains/ports.toml → config/ports.toml
	var loadErr error
	portsPath := ""
	for _, candidate := range []string{
		filepath.Join(chainsConfigDir, "services.toml"),
		filepath.Join(chainsConfigDir, "ports.toml"),
		filepath.Join(configDir, "ports.toml"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			portsPath = candidate
			break
		}
	}
	if portsPath == "" {
		log.Fatalf("ports config missing: expected config/chains/services.toml, config/chains/ports.toml, or config/ports.toml")
	}
	defaultPorts, loadErr = config.LoadPorts(portsPath)
	if loadErr != nil {
		log.Fatalf("Could not load default ports: %v", loadErr)
	}

	// Load chain configs — scan order:
	//   1. $configDir/chains/ (canonical)
	//   2. $chainsDir (~/.vProx/chains/, legacy)
	foundChains := false
	for _, scanDir := range []string{chainsConfigDir, chainsDir} {
		if !config.HasChainConfigs(scanDir) {
			continue
		}
		if err := loadChains(scanDir); err != nil {
			if *validateFlag {
				log.Printf("[VALIDATE] Error loading chains from %s: %v", scanDir, err)
				os.Exit(1)
			}
			log.Fatalf("Could not load chain configs from %s: %v", scanDir, err)
		}
		foundChains = true
		applog.Print("INFO", "config", "chains_loaded", applog.F("dir", scanDir))
	}
	if !foundChains {
		log.Fatalf("no chain configs found in %s or %s", chainsConfigDir, chainsDir)
	}

	// Handle --validate flag: print config summary and exit
	if *validateFlag {
		log.Println("")
		log.Println("CONFIG VALIDATION SUCCESSFUL #############################")
		log.Printf("[VALIDATE] Loaded %d chains from %s, %s", len(chains), chainsConfigDir, chainsDir)
		for host := range chains {
			log.Printf("  • %s", host)
		}
		log.Printf("[VALIDATE] Default ports: RPC=%d, REST=%d, gRPC=%d, gRPC-Web=%d, API=%d",
			defaultPorts.RPC, defaultPorts.REST, defaultPorts.GRPC, defaultPorts.GRPCWeb, defaultPorts.API)
		log.Println("[VALIDATE] All configs OK")
		return
	}

	// Handle --info flag: print info and exit
	if *infoFlag {
		log.Println("")
		log.Println("VPROX CONFIGURATION INFO #############################")
		log.Printf("VPROX_HOME:        %s", vproxHome)
		log.Printf("Config directory:  %s", configDir)
		log.Printf("Chains directory:  %s", chainsDir)
		log.Printf("Data directory:    %s", dataDir)
		log.Printf("Logs directory:    %s", logsDir)
		log.Printf("Main log file:     %s", mainLogPath)
		log.Println("")
		log.Printf("Loaded chains: %d", len(chains))
		for host, ch := range chains {
			log.Printf("  • %s (%s) @ %s", host, ch.ChainName, ch.IP)
		}
		log.Println("")
		log.Printf("Default ports: RPC=%d, REST=%d, gRPC=%d, gRPC-Web=%d, API=%d",
			defaultPorts.RPC, defaultPorts.REST, defaultPorts.GRPC, defaultPorts.GRPCWeb, defaultPorts.API)
		if *verboseFlag {
			log.Println("")
			log.Println("[VERBOSE] Per-chain details:")
			for host, ch := range chains {
				log.Printf("  %s:", host)
				log.Printf("    Services: RPC=%v, REST=%v, WebSocket=%v, gRPC=%v, gRPC-Web=%v",
					ch.Services.RPC, ch.Services.REST, ch.Services.WebSocket, ch.Services.GRPC, ch.Services.GRPCWeb)
				if !ch.DefaultPorts {
					log.Printf("    Ports: RPC=%d, REST=%d, gRPC=%d, gRPC-Web=%d",
						ch.Ports.RPC, ch.Ports.REST, ch.Ports.GRPC, ch.Ports.GRPCWeb)
				}
			}
		}
		return
	}

	// --- Limiter: defaults ok, overrides limited, 429 blocked
	// Priority: compiled default → settings.toml → env var → CLI flag
	proxyCfg := loadProxySettings(configDir)

	settingsRPS := 25.0
	if proxyCfg.RateLimit.RPS > 0 {
		settingsRPS = proxyCfg.RateLimit.RPS
	}
	settingsBurst := 100
	if proxyCfg.RateLimit.Burst > 0 {
		settingsBurst = proxyCfg.RateLimit.Burst
	}
	settingsAutoEnabled := true
	if proxyCfg.AutoQuarantine.Enabled != nil {
		settingsAutoEnabled = *proxyCfg.AutoQuarantine.Enabled
	}
	settingsThreshold := 120
	if proxyCfg.AutoQuarantine.Threshold > 0 {
		settingsThreshold = proxyCfg.AutoQuarantine.Threshold
	}
	settingsWindowSec := 10
	if proxyCfg.AutoQuarantine.WindowSec > 0 {
		settingsWindowSec = proxyCfg.AutoQuarantine.WindowSec
	}
	settingsPenaltyRPS := 1.0
	if proxyCfg.AutoQuarantine.PenaltyRPS > 0 {
		settingsPenaltyRPS = proxyCfg.AutoQuarantine.PenaltyRPS
	}
	settingsPenaltyBurst := 1
	if proxyCfg.AutoQuarantine.PenaltyBurst > 0 {
		settingsPenaltyBurst = proxyCfg.AutoQuarantine.PenaltyBurst
	}
	settingsTTL := 900
	if proxyCfg.AutoQuarantine.TTLSec > 0 {
		settingsTTL = proxyCfg.AutoQuarantine.TTLSec
	}

	defaultRPS := envFloat("VPROX_RPS", settingsRPS)
	defaultBurst := envInt("VPROX_BURST", settingsBurst)
	autoEnabled := envBoolDefault("VPROX_AUTO_ENABLED", settingsAutoEnabled)
	autoThreshold := envInt("VPROX_AUTO_THRESHOLD", settingsThreshold)
	autoWindowSec := envInt("VPROX_AUTO_WINDOW_SEC", settingsWindowSec)
	autoPenaltyRPS := envFloat("VPROX_AUTO_RPS", settingsPenaltyRPS)
	autoPenaltyBurst := envInt("VPROX_AUTO_BURST", settingsPenaltyBurst)
	autoTTL := envInt("VPROX_AUTO_TTL_SEC", settingsTTL)

	// Apply CLI overrides for rate limiting
	if *rpsFlag > 0 {
		defaultRPS = *rpsFlag
		if *verboseFlag {
			log.Printf("[CLI] Override default RPS: %.2f", defaultRPS)
		}
	}
	if *burstFlag > 0 {
		defaultBurst = *burstFlag
		if *verboseFlag {
			log.Printf("[CLI] Override default burst: %d", defaultBurst)
		}
	}
	if *disableAutoFlag {
		autoEnabled = false
		if *verboseFlag {
			log.Println("[CLI] Auto-quarantine disabled")
		}
	}
	if *autoRpsFlag > 0 {
		autoPenaltyRPS = *autoRpsFlag
		if *verboseFlag {
			log.Printf("[CLI] Override auto-Q penalty RPS: %.2f", autoPenaltyRPS)
		}
	}
	if *autoBurstFlag > 0 {
		autoPenaltyBurst = *autoBurstFlag
		if *verboseFlag {
			log.Printf("[CLI] Override auto-Q penalty burst: %d", autoPenaltyBurst)
		}
	}

	// Load backup config; automation bool is the sole switch for the scheduler.
	bupCfg, bupLoaded, bupErr := backup.LoadConfig(resolveBackupConfigPath(configDir))
	if bupErr != nil {
		applog.Print("WARN", "backup", "config_load_failed", applog.F("error", bupErr.Error()))
	}
	backupEnabled := bupCfg.Backup.Automation
	if *disableBackupFlag {
		backupEnabled = false
		// Persist automation=false to backup.toml so the change survives restarts
		if err := disableBackupInConfig(resolveBackupConfigPath(configDir)); err != nil {
			applog.Print("WARN", "backup", "config_persist_failed", applog.F("error", err.Error()))
		}
		if *verboseFlag {
			log.Println("[CLI] Automatic backup disabled and persisted to backup.toml")
		}
	}

	limOpts := []limit.Option{
		limit.WithTrustProxy(true),
		limit.WithLogPath(filepath.Join(logsDir, "rate-limit.jsonl")),
		limit.WithLogOnlyImportant(),  // JSONL: only 429/auto-add/auto-expire/wait-canceled
		limit.WithMirrorToMainLog(),   // mirror important events into main.log
		limit.WithDefaultActionDrop(), // use Allow() for defaults (429 on overflow)
	}
	if len(defaultPorts.TrustedProxies) > 0 {
		limOpts = append(limOpts, limit.WithTrustedProxies(defaultPorts.TrustedProxies))
	}
	if autoEnabled {
		limOpts = append(limOpts, limit.WithAutoQuarantine(limit.AutoRule{
			Threshold: autoThreshold,
			Window:    time.Duration(autoWindowSec) * time.Second,
			Penalty:   limit.RateSpec{RPS: autoPenaltyRPS, Burst: autoPenaltyBurst}, // burst >= 1
			TTL:       time.Duration(autoTTL) * time.Second,
		}))
	}
	lim := limit.New(
		limit.RateSpec{RPS: defaultRPS, Burst: defaultBurst},
		nil,
		limOpts...,
	)

	// Build mux and routes
	mux := http.NewServeMux()

	// Observability endpoints (registered before catch-all handler)
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", healthHandler)

	// Wire metric hooks — hook pattern avoids import cycles between internal packages.
	limit.OnRateLimitHit = metrics.RecordRateLimitHit
	geo.OnCacheHit = metrics.RecordGeoCacheHit
	geo.OnCacheMiss = metrics.RecordGeoCacheMiss
	backup.OnBackupEvent = metrics.RecordBackupEvent

	// Handle --dry-run flag: load everything but don't start server
	if *dryRunFlag {
		log.Println("")
		log.Println("DRY-RUN MODE #############################")
		log.Printf("Would listen on: %s", func() string {
			addr := ":3000"
			if v := strings.TrimSpace(os.Getenv("VPROX_ADDR")); v != "" {
				addr = v
			}
			if *addrFlag != "" {
				addr = *addrFlag
			}
			return addr
		}())
		log.Printf("Loaded chains: %d", len(chains))
		log.Printf("Rate limit: %.2f RPS, burst %d", defaultRPS, defaultBurst)
		if autoEnabled {
			log.Printf("Auto-quarantine: enabled (threshold=%d, penalty=%.2f RPS)", autoThreshold, autoPenaltyRPS)
		} else {
			log.Println("Auto-quarantine: disabled")
		}
		if backupEnabled {
			log.Println("Backup: enabled")
		} else {
			log.Println("Backup: disabled")
		}
		log.Println("[DRY-RUN] All systems ready (not starting server)")
		return
	}

	var stopBackup func()
	if backupEnabled {
		listSrc := "default"
		if bupLoaded {
			listSrc = "loaded"
		}
		rotateExtra, extraFiles := resolveBackupExtraFiles(bupCfg, dataDir, logsDir, configDir, mainLogPath)

		// Env vars override backup.toml for interval/size (backward compat).
		intervalDays := envInt("VPROX_BACKUP_INTERVAL_DAYS", bupCfg.Backup.IntervalDays)
		maxBytes := envBytes("VPROX_BACKUP_MAX_BYTES")
		if maxBytes == 0 && bupCfg.Backup.MaxSizeMB > 0 {
			maxBytes = bupCfg.Backup.MaxSizeMB * 1024 * 1024
		}
		checkMin := envInt("VPROX_BACKUP_CHECK_MINUTES", bupCfg.Backup.CheckIntervalMin)

		var err error
		stopBackup, err = backup.StartAuto(backup.Options{
			LogPath:       mainLogPath,
			ArchiveDir:    archiveDir,
			StatePath:     filepath.Join(dataDir, "backup.last"),
			IntervalDays:  intervalDays,
			MaxBytes:      maxBytes,
			CheckInterval: time.Duration(checkMin) * time.Minute,
			RotateExtra:   rotateExtra,
			ExtraFiles:    extraFiles,
			ListSource:    listSrc,
		})
		if err != nil {
			applog.Print("ERROR", "backup", "auto_start_failed", applog.F("error", err.Error()))
		}
	}

	mux.HandleFunc("/websocket", ws.HandleWS(ws.Deps{
		ClientIP:          clientIP,
		LogRequestSummary: logRequestSummary,
		BackendWSParams: func(host string) (string, time.Duration, time.Duration, bool) {
			host = normalizeHost(host)
			ch, ok := chains[host]
			if !ok || !ch.Services.WebSocket || !ch.Services.RPC {
				return "", 0, 0, false
			}
			// resolve effective RPC port
			eff := defaultPorts
			if !ch.DefaultPorts && ch.Ports.RPC != 0 {
				eff.RPC = ch.Ports.RPC
			}
			backendURL := fmt.Sprintf("ws://%s:%d/websocket", ch.IP, eff.RPC)
			idle := time.Duration(ch.WS.IdleTimeoutSec) * time.Second
			if idle <= 0 {
				idle = 3600 * time.Second
			}
			hard := time.Duration(ch.WS.MaxLifetimeSec) * time.Second
			return backendURL, idle, hard, true
		},
	}))

	for _, prefix := range []string{rpcPrefix, restPrefix, grpcPrefix, grpcWebPrefix, apiPrefix} {
		mux.HandleFunc(prefix, handler)
		mux.HandleFunc(prefix+"/", handler)
	}
	mux.HandleFunc("/", handler) // catch-all

	addr := ":3000"
	if v := strings.TrimSpace(os.Getenv("VPROX_ADDR")); v != "" {
		addr = v
	}
	if *addrFlag != "" {
		addr = *addrFlag
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           lim.Middleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Optional pprof debug server — MUST be on a separate port from the proxy.
	// Activated by VPROX_DEBUG=1 or VPROX_DEBUG_PORT=<port>.
	if os.Getenv("VPROX_DEBUG") == "1" || os.Getenv("VPROX_DEBUG_PORT") != "" {
		debugPort := "6060"
		if p := os.Getenv("VPROX_DEBUG_PORT"); p != "" {
			debugPort = p
		}
		// http.DefaultServeMux already has /debug/pprof/ handlers registered
		// via the blank import of net/http/pprof at the top of this file.
		go func() {
			applog.Print("INFO", "debug", "pprof_server_started", applog.F("addr", ":"+debugPort))
			if err := http.ListenAndServe(":"+debugPort, http.DefaultServeMux); err != nil { //nolint:gosec // G114: debug server intentionally uses default timeouts
				applog.Print("ERROR", "debug", "pprof_server_error", applog.F("error", err.Error()))
			}
		}()
	}

	cleanup := func() {
		stopCounterTicker() // final flush of dirty counters
		if stopBackup != nil {
			stopBackup()
		}
		_ = lim.Close()
		geo.Close()
		closeChainLoggers()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	// Start server wrapped by limiter middleware
	applog.Print("INFO", "server", "started", applog.F("addr", addr))

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
		cleanup()
	case <-ctx.Done():
		applog.Print("INFO", "server", "shutdown_requested")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctxTimeout); err != nil {
			applog.Print("ERROR", "server", "shutdown_error", applog.F("error", err.Error()))
		}
		cleanup()
	}
}

// notifyVLog sends a POST to vLog's ingest endpoint after a successful backup.
// Errors are silently ignored (vLog may not be running). The HTTP client has a 5s timeout.
func notifyVLog(vlogURL string) {
	if vlogURL == "" {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, vlogURL+"/api/v1/ingest", nil)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
