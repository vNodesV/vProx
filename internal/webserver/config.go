// Package webserver provides an embedded HTTP/HTTPS server for vProx,
// eliminating the need for an external reverse proxy (Apache/nginx) to
// handle TLS termination, static file serving, and CORS/header management.
package webserver

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ----- Top-level config -----

// Config is the in-memory representation of the webserver module.
// It is assembled from two sources:
//   - webservice.toml  → Enable + Server settings
//   - config/vhosts/*.toml → one VHostConfig per file (loaded via LoadVHostsDir)
type Config struct {
	// Enable controls whether the webserver module starts.
	// Default (absent key): true. Set to false to disable without removing the file.
	Enable *bool `toml:"enable"`

	Server ServerConfig `toml:"server"`

	// VHosts is populated by LoadVHostsDir; it is not decoded from webservice.toml.
	VHosts []VHostConfig `toml:"-"`
}

// Enabled returns true unless the config explicitly sets enable = false.
func (c Config) Enabled() bool {
	if c.Enable == nil {
		return true
	}
	return *c.Enable
}

// ServerConfig holds global listener addresses.
type ServerConfig struct {
	// HTTPAddr is the plain-HTTP listener used only for HTTP→HTTPS redirect.
	// Default: ":80"
	HTTPAddr string `toml:"http_addr"`
	// HTTPSAddr is the TLS listener.
	// Default: ":443"
	HTTPSAddr string `toml:"https_addr"`
}

// ----- Per-vhost config -----

// VHostConfig describes a single virtual host.
// Each vhost lives in its own file under config/vhosts/*.toml.
// Fields are at the top level of the file (no [vhost] section header).
type VHostConfig struct {
	// Name is a human-readable label used in logs.
	Name string `toml:"name"`

	// Host is the primary ServerName matched against the HTTP Host header.
	Host string `toml:"host"`

	// Aliases are additional hostnames that resolve to this vhost.
	Aliases []string `toml:"aliases"`

	// Backend is the upstream URL for reverse-proxy mode (e.g. "http://10.0.0.20:3000").
	// Leave empty for static-only vhosts.
	Backend string `toml:"backend"`

	// Root is the filesystem directory served as static files.
	// Leave empty for proxy-only vhosts.
	Root string `toml:"root"`

	// Index is the directory index filename (default: "index.html").
	Index string `toml:"index"`

	// Compress enables outbound gzip compression for text/* and application/json.
	Compress bool `toml:"compress"`

	// ProxyTimeoutSec overrides the default upstream proxy timeout (default: 30).
	ProxyTimeoutSec int `toml:"proxy_timeout_sec"`

	// HTTPRedirect forces HTTP→HTTPS redirect for this vhost.
	// nil = default (true when TLS configured), false = disable, true = enable.
	HTTPRedirect *bool `toml:"http_redirect"`

	// TLS holds the certificate and key paths for this vhost.
	TLS TLSConfig `toml:"tls"`

	// CORS configures Cross-Origin Resource Sharing headers.
	CORS CORSConfig `toml:"cors"`

	// Headers configures response header manipulation.
	Headers HeaderConfig `toml:"headers"`

	// Security configures security-related response headers.
	Security SecurityConfig `toml:"security"`
}

// TLSConfig holds TLS certificate material paths.
type TLSConfig struct {
	// Cert is the path to the PEM-encoded certificate chain (e.g. Let's Encrypt fullchain.pem).
	Cert string `toml:"cert"`
	// Key is the path to the PEM-encoded private key (e.g. Let's Encrypt privkey.pem).
	Key string `toml:"key"`
}

// CORSConfig configures Access-Control-* response headers.
type CORSConfig struct {
	Enabled   bool     `toml:"enabled"`
	Origins   []string `toml:"origins"`     // e.g. ["*"]
	Methods   []string `toml:"methods"`     // e.g. ["GET", "POST", "HEAD"]
	Headers   []string `toml:"headers"`     // allowed request headers
	MaxAgeSec int      `toml:"max_age_sec"` // Access-Control-Max-Age
}

// HeaderConfig controls response header manipulation.
type HeaderConfig struct {
	// Strip is a list of response header names to remove (case-insensitive).
	Strip []string `toml:"strip"`
	// Inject is a map of header name → value to add to every response.
	Inject map[string]string `toml:"inject"`
}

// SecurityConfig enables common security response headers.
type SecurityConfig struct {
	// HSTS emits "Strict-Transport-Security: max-age=31536000; includeSubDomains; preload"
	HSTS bool `toml:"hsts"`
	// XFrameOptions sets the X-Frame-Options header (e.g. "SAMEORIGIN", "DENY").
	XFrameOptions string `toml:"x_frame_options"`
	// XContentType sets X-Content-Type-Options: nosniff when true.
	XContentType bool `toml:"x_content_type"`
	// CSP sets the Content-Security-Policy header value.
	CSP string `toml:"csp"`
}

// ----- Loaders -----

// LoadWebServiceConfig reads webservice.toml for service-level settings (enable, [server]).
// If path does not exist, sensible defaults are returned without error.
// VHosts are NOT loaded here — use LoadVHostsDir for that.
func LoadWebServiceConfig(path string) (Config, error) {
	cfg := defaultConfig()

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("webserver: open %s: %w", path, err)
	}
	defer f.Close()

	if err := toml.NewDecoder(f).Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("webserver: decode %s: %w", path, err)
	}
	applyServerDefaults(&cfg)
	return cfg, nil
}

// LoadVHostsDir scans dir for *.toml files and decodes each as a VHostConfig.
// Files named *.sample.toml are silently skipped.
// Returns (nil, nil) when dir does not exist (module simply has no vhosts).
func LoadVHostsDir(dir string) ([]VHostConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("webserver: read vhosts dir %s: %w", dir, err)
	}

	seen := map[string]string{} // host → filename for duplicate detection
	var vhosts []VHostConfig

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".toml") || strings.HasSuffix(name, ".sample.toml") {
			continue
		}

		fpath := filepath.Join(dir, name)
		f, err := os.Open(fpath)
		if err != nil {
			return nil, fmt.Errorf("webserver: open vhost %s: %w", name, err)
		}
		var v VHostConfig
		decErr := toml.NewDecoder(f).Decode(&v)
		f.Close()
		if decErr != nil {
			return nil, fmt.Errorf("webserver: decode %s: %w", name, decErr)
		}

		if err := validateVHost(v, seen, name); err != nil {
			return nil, err
		}
		applyVHostDefaults(&v)
		vhosts = append(vhosts, v)
	}
	return vhosts, nil
}

// LoadWebServer is the primary entry point for main.go.
// It reads webservice.toml for service settings, then loads all vhosts from vhostsDir.
// If enable = false in webservice.toml, vhosts are not loaded and an empty Config is returned.
func LoadWebServer(servicePath, vhostsDir string) (Config, error) {
	cfg, err := LoadWebServiceConfig(servicePath)
	if err != nil {
		return cfg, err
	}
	if !cfg.Enabled() {
		return cfg, nil
	}
	vhosts, err := LoadVHostsDir(vhostsDir)
	if err != nil {
		return cfg, err
	}
	cfg.VHosts = vhosts
	return cfg, nil
}

// defaultConfig returns a Config with sensible zero-configuration defaults.
func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			HTTPAddr:  ":80",
			HTTPSAddr: ":443",
		},
	}
}

// applyServerDefaults fills in zero-value server fields.
func applyServerDefaults(cfg *Config) {
	if cfg.Server.HTTPAddr == "" {
		cfg.Server.HTTPAddr = ":80"
	}
	if cfg.Server.HTTPSAddr == "" {
		cfg.Server.HTTPSAddr = ":443"
	}
}

// applyVHostDefaults fills in per-vhost fields that require a non-zero runtime value.
func applyVHostDefaults(v *VHostConfig) {
	if v.Index == "" {
		v.Index = "index.html"
	}
	if v.ProxyTimeoutSec <= 0 {
		v.ProxyTimeoutSec = 30
	}
	v.Host = strings.ToLower(strings.TrimSpace(v.Host))
	for j, a := range v.Aliases {
		v.Aliases[j] = strings.ToLower(strings.TrimSpace(a))
	}
	if v.CORS.Enabled {
		if len(v.CORS.Origins) == 0 {
			v.CORS.Origins = []string{"*"}
		}
		if len(v.CORS.Methods) == 0 {
			v.CORS.Methods = []string{"GET", "POST", "HEAD"}
		}
	}
	if v.TLS.Cert != "" && v.TLS.Key != "" && v.HTTPRedirect == nil {
		t := true
		v.HTTPRedirect = &t
	}
}

// validateVHost checks one VHostConfig for logical errors.
// seen tracks host→filename across the whole directory for duplicate detection.
func validateVHost(v VHostConfig, seen map[string]string, filename string) error {
	if v.Host == "" {
		return fmt.Errorf("webserver: %s: host must not be empty", filename)
	}
	if v.Backend == "" && v.Root == "" {
		return fmt.Errorf("webserver: %s: at least one of backend or root must be set", filename)
	}
	if v.TLS.Cert != "" && v.TLS.Key == "" {
		return fmt.Errorf("webserver: %s: tls.cert set but tls.key is missing", filename)
	}
	if v.TLS.Key != "" && v.TLS.Cert == "" {
		return fmt.Errorf("webserver: %s: tls.key set but tls.cert is missing", filename)
	}
	allHosts := append([]string{v.Host}, v.Aliases...)
	for _, h := range allHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if prior, ok := seen[h]; ok {
			return fmt.Errorf("webserver: %s: host %q already registered by %s", filename, h, prior)
		}
		seen[h] = filename
	}
	return nil
}

// WantsHTTPRedirect returns true if the vhost has HTTP→HTTPS redirect enabled.
func (v VHostConfig) WantsHTTPRedirect() bool {
	if v.HTTPRedirect != nil {
		return *v.HTTPRedirect
	}
	return false
}
