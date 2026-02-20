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
	chainsDir  string
	dataDir    string
	logsDir    string
	archiveDir string

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
	requestID := applog.EnsureRequestID(r)

	// counter
	counterMutex.Lock()
	srcCounter[src]++
	srcQty := srcCounter[src]
	_ = saveAccessCountsLocked(accessCountsPath)
	counterMutex.Unlock()

	// timing + fields
	durMS := time.Since(start).Seconds() * 1000
	dst := r.URL.RequestURI()
	ua := r.Header.Get("User-Agent")

	// Country (CF hint then local db)
	country := strings.TrimSpace(r.Header.Get("CF-IPCountry"))
	if country == "" {
		country = geo.Country(clientIP(r))
	}

	// Status from limiter (defaults ok, overrides limited, 429 blocked doesn't reach here)
	status := limit.StatusOf(r)

	if country == "" {
		country = "--"
	}
	line := applog.Line("INFO", "access", "request",
		applog.F("request_id", requestID),
		applog.F("host", hostNorm),
		applog.F("route", route),
		applog.F("proxied", proxied),
		applog.F("request", dst),
		applog.F("method", r.Method),
		applog.F("ip", src),
		applog.F("src_count", srcQty),
		applog.F("latency_ms", durMS),
		applog.F("ua", ua),
		applog.F("country", country),
		applog.F("status", status),
	)
	log.Println(line)
	if ch, ok := chains[hostNorm]; ok {
		if cl := getChainLogger(ch); cl != nil {
			cl.Println(line)
		}
	}
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
	requestID := applog.EnsureRequestID(r)
	applog.SetResponseRequestID(w, requestID)
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
	// Ensure correlation id is forwarded to upstream.
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
	rawArgs := os.Args[1:]
	startMode := false
	if len(rawArgs) > 0 {
		switch rawArgs[0] {
		case "start":
			startMode = true
			rawArgs = rawArgs[1:]
		case "backup":
			rawArgs[0] = "--backup"
		}
	}
	os.Args = append([]string{os.Args[0]}, rawArgs...)

	// Parse flags
	backupFlag := flag.Bool("backup", false, "run backup and exit")
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

	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "Usage: vProx [start] [--flags]")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Commands:")
		fmt.Fprintln(out, "  start                   run in foreground and emit logs to stdout (journalctl friendly)")
		fmt.Fprintln(out, "  backup                  shorthand for --backup")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Flags:")
		fmt.Fprintln(out, "  --addr string           listen address (default :3000)")
		fmt.Fprintln(out, "  --auto-burst int        override auto-quarantine burst (env: VPROX_AUTO_BURST)")
		fmt.Fprintln(out, "  --auto-rps float        override auto-quarantine RPS (env: VPROX_AUTO_RPS)")
		fmt.Fprintln(out, "  --backup                run backup and exit")
		fmt.Fprintln(out, "  --burst int             override default burst (env: VPROX_BURST)")
		fmt.Fprintln(out, "  --chains string         override chains directory")
		fmt.Fprintln(out, "  --config string         override config directory")
		fmt.Fprintln(out, "  --disable-auto          disable auto-quarantine")
		fmt.Fprintln(out, "  --disable-backup        disable automatic backup loop")
		fmt.Fprintln(out, "  --dry-run               load everything but don't start server")
		fmt.Fprintln(out, "  --help                  show this help")
		fmt.Fprintln(out, "  --home string           override VPROX_HOME")
		fmt.Fprintln(out, "  --info                  show loaded config summary and exit")
		fmt.Fprintln(out, "  --log-file string       override main log file path")
		fmt.Fprintln(out, "  --quiet                 suppress non-error output")
		fmt.Fprintln(out, "  --rps float             override default RPS (env: VPROX_RPS)")
		fmt.Fprintln(out, "  --reset_count           reset persisted access counters (backup mode)")
		fmt.Fprintln(out, "  --reset-count           alias for --reset_count")
		fmt.Fprintln(out, "  --validate              validate configs and exit")
		fmt.Fprintln(out, "  --verbose               verbose logging output")
		fmt.Fprintln(out, "  --version               show version and exit")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Note: Go's flag parser accepts both --flag and -flag; use --flag as the documented style.")
	}

	flag.Parse()

	// Handle version flag first
	if *versionFlag {
		fmt.Println("vProx - Reverse proxy with rate limiting and geolocation")
		fmt.Println("Version: 1.0.0")
		// TODO: Build with ldflags for version: go build -ldflags \"-X main.BuildVersion=...\"
		os.Exit(0)
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

	// Create directories
	for _, dir := range []string{configDir, chainsDir, dataDir, logsDir, archiveDir} {
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

	if startMode && !*quietFlag {
		log.SetOutput(&splitLogWriter{stdout: os.Stdout, file: f, colorize: true})
	} else {
		// In non-start mode we keep file-only behavior.
		// With --quiet in start mode, also keep file-only output.
		log.SetOutput(f)
	}
	log.SetFlags(0) // no default date/time; our logger prints its own header

	if *backupFlag {
		if resetCount {
			if err := resetAccessCounts(accessCountsPath); err != nil {
				log.Fatalf("Failed to reset access counters: %v", err)
			}
			applog.Print("INFO", "access", "counter_reset", applog.F("path", accessCountsPath))
		}
		if err := backup.RunOnce(backup.Options{
			LogPath:    mainLogPath,
			ArchiveDir: archiveDir,
			StatePath:  filepath.Join(dataDir, "backup.last"),
		}); err != nil {
			log.Fatalf("Backup failed: %v", err)
		}
		return
	}

	// Geo status line
	applog.Print("INFO", "geo", "status", applog.F("message", geo.Info()))
	loadAccessCounts(accessCountsPath)

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

	// Load chain configs from both chains/ (preferred) and config/ (backward compatibility)
	foundChains := false
	if hasChainConfigs(chainsDir) {
		if err := loadChains(chainsDir); err != nil {
			if *validateFlag {
				log.Printf("[VALIDATE] Error loading chains from %s: %v", chainsDir, err)
				os.Exit(1)
			}
			log.Fatalf("Could not load chain configs from %s: %v", chainsDir, err)
		}
		foundChains = true
		applog.Print("INFO", "config", "chains_loaded", applog.F("dir", chainsDir))
	}
	if hasChainConfigs(configDir) {
		if err := loadChains(configDir); err != nil {
			if *validateFlag {
				log.Printf("[VALIDATE] Error loading chains from %s: %v", configDir, err)
				os.Exit(1)
			}
			log.Fatalf("Could not load chain configs from %s: %v", configDir, err)
		}
		foundChains = true
		applog.Print("INFO", "config", "chains_loaded", applog.F("dir", configDir))
	}
	if !foundChains {
		log.Fatalf("no chain configs found in %s or %s", chainsDir, configDir)
	}

	// Handle --validate flag: print config summary and exit
	if *validateFlag {
		log.Println("")
		log.Println("CONFIG VALIDATION SUCCESSFUL #############################")
		log.Printf("[VALIDATE] Loaded %d chains from %s and %s", len(chains), chainsDir, configDir)
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

	// Determine backup enabled status (before limiter setup for dry-run flag)
	backupEnabled := envBool("VPROX_BACKUP_ENABLED")
	if *disableBackupFlag {
		backupEnabled = false
		if *verboseFlag {
			log.Println("[CLI] Automatic backup disabled")
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
		if err := saveAccessCounts(accessCountsPath); err != nil {
			applog.Print("WARN", "access", "counter_save_failed", applog.F("error", err.Error()))
		}
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
