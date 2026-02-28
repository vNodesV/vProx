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
	backup "github.com/vNodesV/vProx/internal/backup"
	"github.com/vNodesV/vProx/internal/geo"
	"github.com/vNodesV/vProx/internal/limit"
	applog "github.com/vNodesV/vProx/internal/logging"
	ws "github.com/vNodesV/vProx/internal/ws"
)

// --------------------- TYPES ---------------------

type Ports struct {
	RPC     int    `toml:"rpc"`
	REST    int    `toml:"rest"`
	GRPC    int    `toml:"grpc"`
	GRPCWeb int    `toml:"grpc_web"`
	API     int    `toml:"api"`
	VLogURL string `toml:"vlog_url"` // optional: notify vLog after --new-backup
}

type VHostPrefix struct {
	RPC  string `toml:"rpc"`
	REST string `toml:"rest"`
}

type Expose struct {
	Path        bool        `toml:"path"`
	VHost       bool        `toml:"vhost"`
	VHostPrefix VHostPrefix `toml:"vhost_prefix"`
}

type Services struct {
	RPC       bool `toml:"rpc"`
	REST      bool `toml:"rest"`
	WebSocket bool `toml:"websocket"`
	GRPC      bool `toml:"grpc"`
	GRPCWeb   bool `toml:"grpc_web"`
	APIAlias  bool `toml:"api_alias"`
}

type Features struct {
	InjectRPCIndex    bool   `toml:"inject_rpc_index"`
	InjectRestSwagger bool   `toml:"inject_rest_swagger"`
	AbsoluteLinks     string `toml:"absolute_links"` // auto | always | never
}

type LoggingCfg struct {
	File   string `toml:"file"`
	Format string `toml:"format"`
}

type Message struct {
	APIMsg string `toml:"api_msg"`
	RPCMsg string `toml:"rpc_msg"`
}

type Aliases struct {
	RPC  []string `toml:"rpc"`
	REST []string `toml:"rest"`
	API  []string `toml:"api"`
}

type WSConfig struct {
	IdleTimeoutSec int `toml:"idle_timeout_sec"` // default 300
	MaxLifetimeSec int `toml:"max_lifetime_sec"` // 0 = no hard cap
}

type ChainConfig struct {
	SchemaVersion int    `toml:"schema_version"`
	ChainName     string `toml:"chain_name"`
	Host          string `toml:"host"`
	IP            string `toml:"ip"`

	Aliases  Aliases    `toml:"aliases"`
	Expose   Expose     `toml:"expose"`
	Services Services   `toml:"services"`
	Ports    Ports      `toml:"ports"`
	WS       WSConfig   `toml:"ws"`
	Features Features   `toml:"features"`
	Logging  LoggingCfg `toml:"logging"`
	Message  Message    `toml:"message"`

	DefaultPorts bool `toml:"default_ports"`
	Msg          bool `toml:"msg"`
}

// --------------------- GLOBALS ---------------------

var (
	chains       = make(map[string]*ChainConfig)
	defaultPorts Ports

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

	// logger state
	srcCounter   = make(map[string]int64)
	counterMutex sync.Mutex
	counterDirty bool // true when in-memory counts differ from disk

	chainLoggerMu sync.Mutex
	chainLoggers  = make(map[string]*log.Logger)
	chainLogFiles = make(map[string]*os.File)

	logKVRe   = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)=("([^"\\]|\\.)*"|[^ ]+)`)
	longHexRe = regexp.MustCompile(`\b[0-9A-Fa-f]{24,}\b`)
)

const (
	rpcPrefix     = "/rpc"
	restPrefix    = "/rest"
	grpcPrefix    = "/grpc"
	grpcWebPrefix = "/grpc-web"
	apiPrefix     = "/api"

	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiBlue    = "\x1b[34m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiMagenta = "\x1b[35m"
	ansiRed     = "\x1b[31m"
)

// --------------------- VALIDATION ---------------------

var (
	reHostname = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$`)
)

func isValidHostname(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	return reHostname.MatchString(h)
}

func validatePortsLabel(label string, v int) error {
	if v <= 0 || v > 65535 {
		return fmt.Errorf("%s port out of range: %d", label, v)
	}
	return nil
}

func validateAbsoluteLinksMode(m string) bool {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "", "auto", "always", "never":
		return true
	default:
		return false
	}
}

func normalizeVHostPrefixes(e *Expose) {
	if e.VHostPrefix.RPC == "" {
		e.VHostPrefix.RPC = "rpc"
	}
	if e.VHostPrefix.REST == "" {
		// common defaults: "api" or "rest"
		e.VHostPrefix.REST = "api"
	}
}

func validateConfig(c *ChainConfig) error {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}

	// Host/IP
	c.Host = strings.ToLower(strings.TrimSpace(c.Host))
	if !isValidHostname(c.Host) {
		return fmt.Errorf("invalid host: %q", c.Host)
	}
	if net.ParseIP(strings.TrimSpace(c.IP)) == nil {
		return fmt.Errorf("invalid ip: %q", c.IP)
	}

	// Expose / prefixes
	normalizeVHostPrefixes(&c.Expose)

	// Absolute links
	if !validateAbsoluteLinksMode(c.Features.AbsoluteLinks) {
		return fmt.Errorf("features.absolute_links must be auto|always|never, got %q", c.Features.AbsoluteLinks)
	}

	// Ports
	if c.DefaultPorts {
		// use global defaults later
	} else {
		if err := validatePortsLabel("rpc", c.Ports.RPC); err != nil {
			return err
		}
		if err := validatePortsLabel("rest", c.Ports.REST); err != nil {
			return err
		}
		if c.Services.GRPC {
			if err := validatePortsLabel("grpc", c.Ports.GRPC); err != nil {
				return err
			}
		}
		if c.Services.GRPCWeb {
			if err := validatePortsLabel("grpc_web", c.Ports.GRPCWeb); err != nil {
				return err
			}
		}
		if c.Services.APIAlias {
			if err := validatePortsLabel("api", c.Ports.API); err != nil {
				return err
			}
		}
	}

	// Aliases
	for _, a := range c.Aliases.RPC {
		if !isValidHostname(a) {
			return fmt.Errorf("aliases.rpc contains invalid hostname: %q", a)
		}
	}
	for _, a := range c.Aliases.REST {
		if !isValidHostname(a) {
			return fmt.Errorf("aliases.rest contains invalid hostname: %q", a)
		}
	}
	for _, a := range c.Aliases.API {
		if !isValidHostname(a) {
			return fmt.Errorf("aliases.api contains invalid hostname: %q", a)
		}
	}

	// Services sanity: at least one service enabled
	if !(c.Services.RPC || c.Services.REST || c.Services.GRPC || c.Services.GRPCWeb || c.Services.APIAlias || c.Services.WebSocket) {
		return errors.New("no services enabled; enable at least one in [services]")
	}

	// WS requires RPC (tunnels to RPC /websocket)
	if c.Services.WebSocket && !c.Services.RPC {
		return errors.New("services.websocket requires services.rpc to be enabled")
	}

	// WS defaults
	if c.WS.IdleTimeoutSec <= 0 {
		c.WS.IdleTimeoutSec = 3600
	}
	if c.WS.MaxLifetimeSec < 0 {
		c.WS.MaxLifetimeSec = 0
	}

	return nil
}

// --------------------- CONFIG LOADERS (TOML ONLY) ---------------------

func loadPorts(path string) (Ports, error) {
	var p Ports
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	if err := toml.NewDecoder(f).Decode(&p); err != nil {
		return p, err
	}
	// validate global defaults
	if err := validatePortsLabel("rpc", p.RPC); err != nil {
		return p, fmt.Errorf("ports.toml: %w", err)
	}
	if err := validatePortsLabel("rest", p.REST); err != nil {
		return p, fmt.Errorf("ports.toml: %w", err)
	}
	if p.GRPC != 0 {
		if err := validatePortsLabel("grpc", p.GRPC); err != nil {
			return p, fmt.Errorf("ports.toml: %w", err)
		}
	}
	if p.GRPCWeb != 0 {
		if err := validatePortsLabel("grpc_web", p.GRPCWeb); err != nil {
			return p, fmt.Errorf("ports.toml: %w", err)
		}
	}
	if p.API != 0 {
		if err := validatePortsLabel("api", p.API); err != nil {
			return p, fmt.Errorf("ports.toml: %w", err)
		}
	}
	return p, nil
}

func registerHost(host string, c *ChainConfig) error {
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
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isChainTOML(name) {
			continue
		}
		fpath := filepath.Join(dir, name)
		f, err := os.Open(fpath)
		if err != nil {
			return err
		}
		var c ChainConfig
		if err := toml.NewDecoder(f).Decode(&c); err != nil {
			f.Close()
			return fmt.Errorf("decode %s: %w", entry.Name(), err)
		}
		f.Close()

		if err := validateConfig(&c); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}

		base := c.Host // already normalized
		// normalize alias lists
		for i, a := range c.Aliases.RPC {
			c.Aliases.RPC[i] = strings.ToLower(strings.TrimSpace(a))
		}
		for i, a := range c.Aliases.REST {
			c.Aliases.REST[i] = strings.ToLower(strings.TrimSpace(a))
		}
		for i, a := range c.Aliases.API {
			c.Aliases.API[i] = strings.ToLower(strings.TrimSpace(a))
		}

		// register base host
		if err := registerHost(base, &c); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}

		// register standard vhosts (rpc.<base>, <rest|api>.<base>) when enabled
		if c.Expose.VHost {
			rp := c.Expose.VHostPrefix.RPC
			ap := c.Expose.VHostPrefix.REST
			if err := registerHost(rp+"."+base, &c); err != nil {
				return fmt.Errorf("%s: %w", entry.Name(), err)
			}
			if err := registerHost(ap+"."+base, &c); err != nil {
				return fmt.Errorf("%s: %w", entry.Name(), err)
			}
		}

		// register explicit alias hosts
		for _, h := range c.Aliases.RPC {
			if h != "" {
				if err := registerHost(h, &c); err != nil {
					return fmt.Errorf("%s: %w", entry.Name(), err)
				}
			}
		}
		for _, h := range c.Aliases.REST {
			if h != "" {
				if err := registerHost(h, &c); err != nil {
					return fmt.Errorf("%s: %w", entry.Name(), err)
				}
			}
		}
		for _, h := range c.Aliases.API {
			if h != "" {
				if err := registerHost(h, &c); err != nil {
					return fmt.Errorf("%s: %w", entry.Name(), err)
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

func logRequestSummary(r *http.Request, proxied bool, route string, host string, start time.Time) {
	src := clientIP(r)
	hostNorm := normalizeHost(host)

	// counter
	counterMutex.Lock()
	srcCounter[src]++
	srcQty := srcCounter[src]
	counterDirty = true
	counterMutex.Unlock()

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
			logID = applog.NewTypedID(pathPrefix(dst))
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
		applog.F("from", src),
		applog.F("count", srcQty),
		applog.F("to", strings.ToUpper(hostNorm)),
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

// pathPrefix returns a 3-letter log ID prefix based on the request path.
func pathPrefix(dst string) string {
	p := strings.ToUpper(dst)
	switch {
	case strings.HasPrefix(p, "/RPC"):
		return "RPC"
	case strings.HasPrefix(p, "/REST"), strings.HasPrefix(p, "/API"):
		return "API"
	case strings.HasPrefix(p, "/WEBSOCKET"), strings.HasPrefix(p, "/WS"):
		return "WSS"
	default:
		return "REQ"
	}
}

// routeIDPrefix maps the resolved route to a 3-letter typed ID prefix.
// WSS is assigned by ws.go before this handler; this covers RPC, API, and fallback.
func routeIDPrefix(prefix, route string, isRPCvhost, isRESTvhost bool) string {
	if isRPCvhost || prefix == rpcPrefix || route == "rpc" {
		return "RPC"
	}
	if isRESTvhost || prefix == restPrefix || prefix == apiPrefix ||
		prefix == grpcPrefix || prefix == grpcWebPrefix || route == "rest" {
		return "API"
	}
	return "REQ"
}

func loadAccessCounts(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			applog.Print("WARN", "access", "counter_load_failed", applog.F("error", err.Error()))
		}
		return
	}
	var src map[string]int64
	if err := json.Unmarshal(b, &src); err != nil {
		applog.Print("WARN", "access", "counter_load_failed", applog.F("error", err.Error()))
		return
	}
	clean := make(map[string]int64, len(src))
	for ip, qty := range src {
		if strings.TrimSpace(ip) == "" || qty < 0 {
			continue
		}
		clean[ip] = qty
	}
	counterMutex.Lock()
	srcCounter = clean
	counterMutex.Unlock()
}

func saveAccessCounts(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	counterMutex.Lock()
	defer counterMutex.Unlock()
	return saveAccessCountsLocked(path)
}

func saveAccessCountsLocked(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	buf := make(map[string]int64, len(srcCounter))
	for ip, qty := range srcCounter {
		if strings.TrimSpace(ip) == "" || qty < 0 {
			continue
		}
		buf[ip] = qty
	}
	b, err := json.MarshalIndent(buf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func resetAccessCounts(path string) error {
	counterMutex.Lock()
	srcCounter = make(map[string]int64)
	err := saveAccessCountsLocked(path)
	counterMutex.Unlock()
	return err
}

// accessCountSaveInterval controls how often dirty counters are flushed to disk.
const accessCountSaveInterval = 1 * time.Second

// startAccessCountTicker runs a background goroutine that flushes dirty
// counters to disk every accessCountSaveInterval. Returns a stop function
// that flushes once more and stops the ticker.
func startAccessCountTicker(path string) func() {
	ticker := time.NewTicker(accessCountSaveInterval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				counterMutex.Lock()
				if counterDirty {
					_ = saveAccessCountsLocked(path)
					counterDirty = false
				}
				counterMutex.Unlock()
			case <-done:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		// Final flush
		counterMutex.Lock()
		if counterDirty {
			_ = saveAccessCountsLocked(path)
			counterDirty = false
		}
		counterMutex.Unlock()
	}
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

func logLines(l *log.Logger, lines ...string) {
	for _, line := range lines {
		l.Println(line)
	}
}

type splitLogWriter struct {
	stdout   io.Writer
	file     io.Writer
	colorize bool
}

func (w *splitLogWriter) Write(p []byte) (int, error) {
	if w.file != nil {
		if _, err := w.file.Write(p); err != nil {
			return 0, err
		}
	}
	if w.stdout != nil {
		out := p
		if w.colorize {
			out = []byte(colorizeLogLine(string(p)))
		}
		if _, err := w.stdout.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func colorizeLogLine(line string) string {
	if strings.TrimSpace(line) == "" {
		return line
	}

	trail := ""
	if strings.HasSuffix(line, "\n") {
		trail = "\n"
		line = strings.TrimSuffix(line, "\n")
	}

	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 3 {
		return line + trail
	}

	base := ansiDim + parts[0] + ansiReset + " " + colorLevel(parts[1]) + parts[1] + ansiReset + " "
	rest := parts[2]

	firstKV := logKVRe.FindStringIndex(rest)
	if firstKV == nil {
		return base + ansiCyan + rest + ansiReset + trail
	}

	msg := strings.TrimSpace(rest[:firstKV[0]])
	kvs := rest[firstKV[0]:]
	kvColored := logKVRe.ReplaceAllStringFunc(kvs, func(m string) string {
		kv := strings.SplitN(m, "=", 2)
		if len(kv) != 2 {
			return m
		}
		k, v := kv[0], kv[1]
		return ansiBold + ansiBlue + k + ansiReset + "=" + colorValueForKey(k, v)
	})

	if msg == "" {
		return base + kvColored + trail
	}
	return base + ansiCyan + msg + ansiReset + " " + kvColored + trail
}

func colorLevel(level string) string {
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "DBG":
		return ansiBlue
	case "WRN":
		return ansiYellow
	case "ERR":
		return ansiRed
	default:
		return ansiGreen
	}
}

func colorValueForKey(key, value string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "module":
		return ansiMagenta + value + ansiReset
	case "height", "latency_ms", "src_count":
		return ansiYellow + value + ansiReset
	case "status":
		if strings.EqualFold(strings.Trim(value, `"`), "ok") {
			return ansiGreen + value + ansiReset
		}
		return ansiRed + value + ansiReset
	case "error":
		return ansiRed + value + ansiReset
	case "request_id", "ip", "host", "route", "method":
		return ansiCyan + value + ansiReset
	}
	vTrim := strings.Trim(value, `"`)
	if longHexRe.MatchString(vTrim) {
		return ansiGreen + value + ansiReset
	}
	return ansiGreen + value + ansiReset
}

func getChainLogger(c *ChainConfig) *log.Logger {
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

func envBool(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
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
			return os.WriteFile(path, []byte("[backup]\nautomation = false\n"), 0o644)
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
	return os.WriteFile(path, []byte(content), 0o644)
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
			if !containsString(rotate, p) {
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

// containsString reports whether s is in the slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func hasChainConfigs(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if isChainTOML(entry.Name()) {
			return true
		}
	}
	return false
}

// isChainTOML returns true only for files that are chain config TOMLs.
// Excludes known non-chain system files and all *.sample.toml files.
func isChainTOML(name string) bool {
	if !strings.HasSuffix(name, ".toml") {
		return false
	}
	if strings.HasSuffix(name, ".sample.toml") {
		return false
	}
	skip := []string{"ports.toml", "backup.toml"}
	for _, s := range skip {
		if strings.EqualFold(name, s) {
			return false
		}
	}
	return true
}

func inList(list []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), needle) {
			return true
		}
	}
	return false
}

// --------------------- CORE HANDLER ---------------------

func handler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	// Preserve any correlation ID forwarded by an upstream proxy.
	// Typed ID (RPC/API/REQ) is assigned after routing is determined below.
	forwardedID := applog.RequestIDFrom(r)
	host := normalizeHost(r.Host)

	chain, ok := chains[host]
	if !ok {
		http.Error(w, "Unknown host", http.StatusBadRequest)
		logRequestSummary(r, false, "direct", host, start)
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
		isRPCvhost = strings.HasPrefix(host, rp+".") || inList(chain.Aliases.RPC, host)
		isRESTvhost = strings.HasPrefix(host, ap+".") || inList(chain.Aliases.REST, host) || inList(chain.Aliases.API, host)
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
		if chain.Features.InjectRPCIndex && (r.URL.Path == "/" || r.URL.Path == "") {
			bannerHTML = chain.Message.RPCMsg
			bannerFile = bannerPath(chain.ChainName, rpcPrefix)
			injectHTML = true
		}
	} else if isRESTvhost && chain.Services.REST {
		targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.REST, r.URL.Path)
		route = "direct"
		routePrefix = restPrefix
		if chain.Features.InjectRestSwagger && r.URL.Path == "/swagger/" {
			bannerHTML = chain.Message.APIMsg
			bannerFile = bannerPath(chain.ChainName, restPrefix)
			injectHTML = true
		}

	} else {
		// 2) PATH-based routing on base host (if exposed)
		if chain.Expose.Path {
			switch {
			case strings.HasPrefix(r.URL.Path, rpcPrefix) && chain.Services.RPC:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.RPC, strings.TrimPrefix(r.URL.Path, rpcPrefix))
				route = "rpc"
				routePrefix = rpcPrefix
				if chain.Features.InjectRPCIndex && (r.URL.Path == "/rpc" || r.URL.Path == "/rpc/") {
					bannerHTML = chain.Message.RPCMsg
					bannerFile = bannerPath(chain.ChainName, rpcPrefix)
					injectHTML = true
				}

			case strings.HasPrefix(r.URL.Path, restPrefix) && chain.Services.REST:
				targetURL = fmt.Sprintf("http://%s:%d%s", chain.IP, eff.REST, strings.TrimPrefix(r.URL.Path, restPrefix))
				route = "rest"
				routePrefix = restPrefix
				if chain.Features.InjectRestSwagger && r.URL.Path == "/rest/swagger/" {
					bannerHTML = chain.Message.APIMsg
					bannerFile = bannerPath(chain.ChainName, restPrefix)
					injectHTML = true
				}

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
		logRequestSummary(r, false, "direct", host, start)
		return
	}

	// Assign typed request ID now that the route is known.
	// Preserve a forwarded correlation ID from an upstream proxy (e.g. Apache).
	// Otherwise generate RPC/API/REQ based on route type.
	var requestID string
	if forwardedID != "" {
		requestID = forwardedID
	} else {
		requestID = applog.NewTypedID(routeIDPrefix(routePrefix, route, isRPCvhost, isRESTvhost))
		r.Header.Set(applog.RequestIDHeader, requestID)
	}
	applog.SetResponseRequestID(w, requestID)

	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Build upstream request (preserve method/body/headers)
	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Request build error", http.StatusInternalServerError)
		logRequestSummary(r, false, route, host, start)
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
		logRequestSummary(r, false, route, host, start)
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
		logRequestSummary(r, true, route, host, start)
		return
	}

	// If modifying HTML, transparently handle gzip — set up reader
	// before committing the status code so error paths can still send 500.
	var reader io.Reader = resp.Body
	if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			http.Error(w, "Gzip error", http.StatusInternalServerError)
			logRequestSummary(r, false, route, host, start)
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
	logRequestSummary(r, true, route, host, start)
}

// --------------------- BACKUP -------------------

func main() {
	rawArgs := os.Args[1:]
	startMode := false
	restartSubcmd := false
	stopSubcmd := false

	// printHelp is defined early so it can be used before flag.Parse().
	printHelp := func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "Usage: vProx <command> [--flags]")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Commands:")
		fmt.Fprintln(out, "  start                   run in foreground, emit logs to stdout (journalctl friendly)")
		fmt.Fprintln(out, "  stop                    stop the vProx.service daemon")
		fmt.Fprintln(out, "  restart                 restart the vProx.service daemon")
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
		log.SetOutput(&splitLogWriter{stdout: os.Stdout, file: f, colorize: true})
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
			if err := resetAccessCounts(accessCountsPath); err != nil {
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
		// Notify vLog (non-fatal). Prefer ports.toml vlog_url, fall back to env.
		vlogURL := os.Getenv("VLOG_URL")
		if p, err := loadPorts(filepath.Join(configDir, "ports.toml")); err == nil && p.VLogURL != "" {
			vlogURL = p.VLogURL
		}
		go notifyVLog(vlogURL)
		return
	}

	// Geo status line
	applog.Print("INFO", "geo", "status", applog.F("message", geo.Info()))
	loadAccessCounts(accessCountsPath)
	stopCounterTicker := startAccessCountTicker(accessCountsPath)

	// Load configs (TOML only)
	var loadErr error
	portsPath := filepath.Join(configDir, "ports.toml")
	if _, err := os.Stat(portsPath); err != nil {
		log.Fatalf("ports config missing: %s", portsPath)
	}
	defaultPorts, loadErr = loadPorts(portsPath)
	if loadErr != nil {
		log.Fatalf("Could not load default ports: %v", loadErr)
	}

	// Load chain configs — scan order:
	//   1. $configDir/chains/ (new structured layout, primary)
	//   2. $chainsDir (~/.vProx/chains/, legacy primary)
	//   3. $configDir (flat layout, backward compat — filtered by isChainTOML)
	foundChains := false
	for _, scanDir := range []string{chainsConfigDir, chainsDir, configDir} {
		if !hasChainConfigs(scanDir) {
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
		log.Fatalf("no chain configs found in %s, %s, or %s", chainsConfigDir, chainsDir, configDir)
	}

	// Handle --validate flag: print config summary and exit
	if *validateFlag {
		log.Println("")
		log.Println("CONFIG VALIDATION SUCCESSFUL #############################")
		log.Printf("[VALIDATE] Loaded %d chains from %s, %s, %s", len(chains), chainsConfigDir, chainsDir, configDir)
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
	defaultRPS := envFloat("VPROX_RPS", 25)
	defaultBurst := envInt("VPROX_BURST", 100)
	autoEnabled := envBoolDefault("VPROX_AUTO_ENABLED", true)
	autoThreshold := envInt("VPROX_AUTO_THRESHOLD", 120)
	autoWindowSec := envInt("VPROX_AUTO_WINDOW_SEC", 10)
	autoPenaltyRPS := envFloat("VPROX_AUTO_RPS", 1)
	autoPenaltyBurst := envInt("VPROX_AUTO_BURST", 1)
	autoTTL := envInt("VPROX_AUTO_TTL_SEC", 900)

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

// notifyVLog sends a non-blocking POST to vLog's ingest endpoint after a successful backup.
// Called in a goroutine; errors are silently ignored (vLog may not be running).
func notifyVLog(vlogURL string) {
	if vlogURL == "" {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(vlogURL+"/api/v1/ingest", "application/json", nil)
	if err != nil {
		return
	}
	resp.Body.Close()
}
