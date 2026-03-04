package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// --------------------- TYPES ---------------------

// Ports holds the default port assignments for chain services.
type Ports struct {
	RPC     int    `toml:"rpc"`
	REST    int    `toml:"rest"`
	GRPC    int    `toml:"grpc"`
	GRPCWeb int    `toml:"grpc_web"`
	API     int    `toml:"api"`
	VLogURL        string   `toml:"vlog_url"`        // optional: notify vLog after --new-backup
	TrustedProxies []string `toml:"trusted_proxies"` // CIDRs trusted to set X-Forwarded-For (e.g. ["127.0.0.1/32"])
}

// VHostPrefix holds custom subdomain prefixes for RPC and REST vhosts.
type VHostPrefix struct {
	RPC  string `toml:"rpc"`
	REST string `toml:"rest"`
}

// Expose controls how chain endpoints are exposed (path-based, vhost-based, or both).
type Expose struct {
	Path        bool        `toml:"path"`
	VHost       bool        `toml:"vhost"`
	VHostPrefix VHostPrefix `toml:"vhost_prefix"`
}

// Services toggles which chain services are enabled.
type Services struct {
	RPC       bool `toml:"rpc"`
	REST      bool `toml:"rest"`
	WebSocket bool `toml:"websocket"`
	GRPC      bool `toml:"grpc"`
	GRPCWeb   bool `toml:"grpc_web"`
	APIAlias  bool `toml:"api_alias"`
}

// Features holds per-chain feature flags.
type Features struct {
	RPCAddressMasking bool   `toml:"rpc_address_masking"` // Mask local IP links on RPC index HTML
	MaskRPC           string `toml:"mask_rpc"`            // Replacement label for masked IP (empty = remove)
	SwaggerMasking    bool   `toml:"swagger_masking"`     // Rewrite Swagger Try-It URLs to public host
	AbsoluteLinks     string `toml:"absolute_links"`      // auto | always | never
}

// LoggingCfg holds per-chain logging configuration.
type LoggingCfg struct {
	File   string `toml:"file"`
	Format string `toml:"format"`
}

// Message holds optional banner messages for chain endpoints.
type Message struct {
	APIMsg string `toml:"api_msg"`
	RPCMsg string `toml:"rpc_msg"`
}

// WSConfig holds WebSocket-specific configuration per chain.
type WSConfig struct {
	IdleTimeoutSec int `toml:"idle_timeout_sec"` // default 300
	MaxLifetimeSec int `toml:"max_lifetime_sec"` // 0 = no hard cap
}

// ChainConfig is the top-level per-chain TOML configuration.
type ChainConfig struct {
	SchemaVersion int    `toml:"schema_version"`
	ChainName     string `toml:"chain_name"`
	Host          string `toml:"host"`
	IP            string `toml:"ip"`

	RPCAliases  []string `toml:"rpc_aliases"`  // extra RPC hostnames; active only when expose.vhost = true
	RESTAliases []string `toml:"rest_aliases"` // extra REST/API hostnames; active only when expose.vhost = true
	APIAliases  []string `toml:"api_aliases"`  // extra /api hostnames; active only when expose.vhost = true

	Expose   Expose     `toml:"expose"`
	Services Services   `toml:"services"`
	Ports    Ports      `toml:"ports"`
	WS       WSConfig   `toml:"ws"`
	Features Features   `toml:"features"`
	Logging  LoggingCfg `toml:"logging"`
	Message  Message    `toml:"message"`

	DefaultPorts bool `toml:"default_ports"`
	MsgRPC       bool `toml:"msg_rpc"` // enable rpc_msg banner injection
	MsgAPI       bool `toml:"msg_api"` // enable api_msg banner injection
}

// --------------------- VALIDATION ---------------------

var reHostname = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)+$`)

// IsValidHostname returns true if h is a syntactically valid hostname.
func IsValidHostname(h string) bool {
	h = strings.ToLower(strings.TrimSpace(h))
	if len(h) == 0 || len(h) > 253 {
		return false
	}
	return reHostname.MatchString(h)
}

// ValidatePortsLabel checks that a port number is in the valid range (1–65535).
func ValidatePortsLabel(label string, v int) error {
	if v <= 0 || v > 65535 {
		return fmt.Errorf("%s port out of range: %d", label, v)
	}
	return nil
}

// ValidateAbsoluteLinksMode returns true if m is a recognized absolute_links mode.
func ValidateAbsoluteLinksMode(m string) bool {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "", "auto", "always", "never":
		return true
	default:
		return false
	}
}

// NormalizeVHostPrefixes fills in default values for empty VHost prefixes.
func NormalizeVHostPrefixes(e *Expose) {
	if e.VHostPrefix.RPC == "" {
		e.VHostPrefix.RPC = "rpc"
	}
	if e.VHostPrefix.REST == "" {
		// common defaults: "api" or "rest"
		e.VHostPrefix.REST = "api"
	}
}

// ValidateConfig validates and normalizes a ChainConfig in place.
func ValidateConfig(c *ChainConfig) error {
	if c.SchemaVersion == 0 {
		c.SchemaVersion = 1
	}

	// Host/IP
	c.Host = strings.ToLower(strings.TrimSpace(c.Host))
	if !IsValidHostname(c.Host) {
		return fmt.Errorf("invalid host: %q", c.Host)
	}
	if net.ParseIP(strings.TrimSpace(c.IP)) == nil {
		return fmt.Errorf("invalid ip: %q", c.IP)
	}

	// Expose / prefixes
	NormalizeVHostPrefixes(&c.Expose)

	// Absolute links
	if !ValidateAbsoluteLinksMode(c.Features.AbsoluteLinks) {
		return fmt.Errorf("features.absolute_links must be auto|always|never, got %q", c.Features.AbsoluteLinks)
	}

	// Ports
	if c.DefaultPorts {
		// use global defaults later
	} else {
		if err := ValidatePortsLabel("rpc", c.Ports.RPC); err != nil {
			return err
		}
		if err := ValidatePortsLabel("rest", c.Ports.REST); err != nil {
			return err
		}
		if c.Services.GRPC {
			if err := ValidatePortsLabel("grpc", c.Ports.GRPC); err != nil {
				return err
			}
		}
		if c.Services.GRPCWeb {
			if err := ValidatePortsLabel("grpc_web", c.Ports.GRPCWeb); err != nil {
				return err
			}
		}
		if c.Services.APIAlias {
			if err := ValidatePortsLabel("api", c.Ports.API); err != nil {
				return err
			}
		}
	}

	// Aliases (active only when expose.vhost = true)
	for _, a := range c.RPCAliases {
		if !IsValidHostname(a) {
			return fmt.Errorf("rpc_aliases contains invalid hostname: %q", a)
		}
	}
	for _, a := range c.RESTAliases {
		if !IsValidHostname(a) {
			return fmt.Errorf("rest_aliases contains invalid hostname: %q", a)
		}
	}
	for _, a := range c.APIAliases {
		if !IsValidHostname(a) {
			return fmt.Errorf("api_aliases contains invalid hostname: %q", a)
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

// --------------------- CONFIG LOADERS ---------------------

// LoadPorts reads and validates a ports.toml file.
func LoadPorts(path string) (Ports, error) {
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
	if err := ValidatePortsLabel("rpc", p.RPC); err != nil {
		return p, fmt.Errorf("ports.toml: %w", err)
	}
	if err := ValidatePortsLabel("rest", p.REST); err != nil {
		return p, fmt.Errorf("ports.toml: %w", err)
	}
	if p.GRPC != 0 {
		if err := ValidatePortsLabel("grpc", p.GRPC); err != nil {
			return p, fmt.Errorf("ports.toml: %w", err)
		}
	}
	if p.GRPCWeb != 0 {
		if err := ValidatePortsLabel("grpc_web", p.GRPCWeb); err != nil {
			return p, fmt.Errorf("ports.toml: %w", err)
		}
	}
	if p.API != 0 {
		if err := ValidatePortsLabel("api", p.API); err != nil {
			return p, fmt.Errorf("ports.toml: %w", err)
		}
	}
	for _, cidr := range p.TrustedProxies {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return p, fmt.Errorf("ports.toml: trusted_proxies: invalid CIDR %q: %w", cidr, err)
		}
	}
	return p, nil
}

// --------------------- UTILS ---------------------

// ContainsString reports whether s is in the slice.
func ContainsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// HasChainConfigs returns true if dir contains at least one chain TOML file.
func HasChainConfigs(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if IsChainTOML(entry.Name()) {
			return true
		}
	}
	return false
}

// IsChainTOML returns true only for files that are chain config TOMLs.
// Excludes known non-chain system files and all *.sample.toml files.
func IsChainTOML(name string) bool {
	if !strings.HasSuffix(name, ".toml") {
		return false
	}
	if strings.HasSuffix(name, ".sample.toml") {
		return false
	}
	skip := []string{"ports.toml", "backup.toml", "modules.toml", "vlog.toml", "webservice.toml"}
	for _, s := range skip {
		if strings.EqualFold(name, s) {
			return false
		}
	}
	return true
}

// InList checks if needle (case-insensitive) is in the list.
func InList(list []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), needle) {
			return true
		}
	}
	return false
}

// PathPrefix returns a 3-letter log ID prefix based on the request path.
func PathPrefix(dst string) string {
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

// RouteIDPrefix maps the resolved route to a 3-letter typed ID prefix.
// WSS is assigned by ws.go before this handler; this covers RPC, API, and fallback.
func RouteIDPrefix(prefix, route string, isRPCvhost, isRESTvhost bool) string {
	if isRPCvhost || prefix == "/rpc" || route == "rpc" {
		return "RPC"
	}
	if isRESTvhost || prefix == "/rest" || prefix == "/api" ||
		prefix == "/grpc" || prefix == "/grpc-web" || route == "rest" {
		return "API"
	}
	return "REQ"
}
