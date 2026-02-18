package main

import (
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	backup "github.com/vNodesV/vApp/modules/vProx/internal/backup"
	"github.com/vNodesV/vApp/modules/vProx/internal/geo"
	"github.com/vNodesV/vApp/modules/vProx/internal/limit"
	ws "github.com/vNodesV/vApp/modules/vProx/internal/ws"
)

// --------------------- TYPES ---------------------

type Ports struct {
	RPC     int `toml:"rpc"`
	REST    int `toml:"rest"`
	GRPC    int `toml:"grpc"`
	GRPCWeb int `toml:"grpc_web"`
	API     int `toml:"api"`
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
	dataDir    string
	logsDir    string
	archiveDir string

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
		if entry.IsDir() || !strings.HasSuffix(name, ".toml") {
			continue
		}
		if strings.EqualFold(name, "ports.toml") {
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

func rewriteLinks(html, routePrefix, internalIP, baseHost, absoluteHost string, rpcVHost bool) string {
	switch routePrefix {
	case rpcPrefix:
		// Tendermint RPC runs on 26657
		repl := "/rpc/"
		if rpcVHost {
			// On rpc.<base>, links should be root (e.g. /status)
			repl = "/"
		}
		ipPattern := regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(internalIP) + `:26657/?`)
		hostPattern := regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(baseHost) + `:26657/?`)
		html = ipPattern.ReplaceAllString(html, repl)
		html = hostPattern.ReplaceAllString(html, repl)

		// If we’re on an RPC vhost, make sure any stray /rpc/ prefixes are collapsed to /
		if rpcVHost {
			html = strings.ReplaceAll(html, `href="/rpc/`, `href="/`)
			html = strings.ReplaceAll(html, `src="/rpc/`, `src="/`)
		}

	case restPrefix, apiPrefix:
		// Cosmos REST typically on 1317
		ipPattern := regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(internalIP) + `:1317/?`)
		hostPattern := regexp.MustCompile(`(?i)(https?:)?//` + regexp.QuoteMeta(baseHost) + `:1317/?`)
		html = ipPattern.ReplaceAllString(html, "/")
		html = hostPattern.ReplaceAllString(html, "/")
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
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		return strings.Split(v, ",")[0]
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

func logRequestSummary(r *http.Request, proxied bool, route string, host string, start time.Time) {
	src := clientIP(r)
	hostNorm := normalizeHost(host)

	// counter
	counterMutex.Lock()
	srcCounter[src]++
	srcQty := srcCounter[src]
	counterMutex.Unlock()

	// timing + fields
	durMS := time.Since(start).Seconds() * 1000
	dst := r.URL.RequestURI()
	ua := r.Header.Get("User-Agent")
	ts := start.Local().Format("2006/01/02 15:04:05")

	// Country (CF hint then local db)
	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" {
		country = geo.Country(clientIP(r))
	}

	// Status from limiter (defaults ok, overrides limited, 429 blocked doesn't reach here)
	status := limit.StatusOf(r)

	header := fmt.Sprintf("[ :::: LOGGING SUMMARY :::: %s ]", ts)
	line2 := fmt.Sprintf("  HOST: %s  ROUTE: %s PROXIED: %t", hostNorm, route, proxied)
	line3 := fmt.Sprintf("  REQUEST: %s", dst)
	line4 := fmt.Sprintf("  IP: %s (%d) %.2fms UA: %s", src, srcQty, durMS, ua)
	if country == "" {
		country = "--"
	}
	line4 += fmt.Sprintf("  COUNTRY: %s  STATUS: %s", country, status)

	width := len(line4)
	if len(line3) > width {
		width = len(line3)
	}
	footer := fmt.Sprintf("[ %s ]", strings.Repeat("-", width))
	logLines(log.Default(), "", header, line2, line3, line4, footer)
	if ch, ok := chains[hostNorm]; ok {
		if cl := getChainLogger(ch); cl != nil {
			logLines(cl, "", header, line2, line3, line4, footer)
		}
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

func hasChainConfigs(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".toml") && !strings.EqualFold(name, "ports.toml") {
			return true
		}
	}
	return false
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
	w.WriteHeader(resp.StatusCode)

	// If not modifying, stream raw (keep original encoding)
	if !willModify {
		_, _ = io.Copy(w, resp.Body)
		logRequestSummary(r, true, route, host, start)
		return
	}

	// If modifying HTML, transparently handle gzip
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

	// Read, rewrite, inject, respond
	rawHTML, _ := io.ReadAll(reader)
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
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		os.Args[1] = "--backup"
	}
	backupFlag := flag.Bool("backup", false, "run backup and exit")
	homeFlag := flag.String("home", "", "override VPROX_HOME")
	configFlag := flag.String("config", "", "override config directory")
	addrFlag := flag.String("addr", "", "listen address (default :3000)")
	flag.Parse()

	vproxHome = resolveVProxHome()
	if *homeFlag != "" {
		vproxHome = *homeFlag
	}
	if vproxHome != "" {
		_ = os.Setenv("VPROX_HOME", vproxHome)
	}

	configDir = filepath.Join(vproxHome, "config")
	if *configFlag != "" {
		if filepath.IsAbs(*configFlag) {
			configDir = *configFlag
		} else {
			configDir = filepath.Join(vproxHome, *configFlag)
		}
	}
	dataDir = filepath.Join(vproxHome, "data")
	logsDir = filepath.Join(vproxHome, "logs")
	archiveDir = filepath.Join(logsDir, "archived")

	for _, dir := range []string{configDir, dataDir, logsDir, archiveDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("Could not create directory %s: %v", dir, err)
		}
	}

	mainLogPath := filepath.Join(logsDir, "main.log")
	f, err := os.OpenFile(mainLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Could not open %s: %v", mainLogPath, err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.SetFlags(0) // no default date/time; our logger prints its own header

	if *backupFlag {
		if err := backup.RunOnce(backup.Options{
			LogPath:    mainLogPath,
			ArchiveDir: archiveDir,
			StatePath:  filepath.Join(dataDir, "backup.last"),
		}); err != nil {
			log.Fatalf("Backup failed: %v", err)
		}
		log.Println("Backup completed")
		return
	}

	// Geo status line
	log.Println(geo.Info())

	// Load configs (TOML only)
	var loadErr error
	portsPath := filepath.Join(configDir, "ports.toml")
	if _, err := os.Stat(portsPath); err != nil {
		portsPath = "chains/ports/ports.toml"
	}
	defaultPorts, loadErr = loadPorts(portsPath)
	if loadErr != nil {
		log.Fatalf("Could not load default ports: %v", loadErr)
	}
	chainsDir := configDir
	if !hasChainConfigs(chainsDir) {
		chainsDir = "chains"
	}
	if err := loadChains(chainsDir); err != nil {
		log.Fatalf("Could not load chain configs: %v", err)
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

	var stopBackup func()
	if envBool("VPROX_BACKUP_ENABLED") {
		intervalDays := envInt("VPROX_BACKUP_INTERVAL_DAYS", 0)
		maxBytes := envBytes("VPROX_BACKUP_MAX_BYTES")
		checkMin := envInt("VPROX_BACKUP_CHECK_MINUTES", 10)
		var err error
		stopBackup, err = backup.StartAuto(backup.Options{
			LogPath:       mainLogPath,
			ArchiveDir:    archiveDir,
			StatePath:     filepath.Join(dataDir, "backup.last"),
			IntervalDays:  intervalDays,
			MaxBytes:      maxBytes,
			CheckInterval: time.Duration(checkMin) * time.Minute,
		})
		if err != nil {
			log.Printf("[backup] auto start failed: %v", err)
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
	log.Println("")
	log.Println("LOG RESTARTED #############################")
	log.Printf("[INFO] vProx listening on %s", addr)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
		cleanup()
	case <-ctx.Done():
		log.Println("[INFO] shutdown requested")
		ctxTimeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctxTimeout); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
		cleanup()
	}
}
