// Package webserver provides an embedded HTTP/HTTPS server for vProx,
// eliminating the need for an external reverse proxy (Apache/nginx) to
// handle TLS termination, static file serving, and CORS/header management.
package webserver

import (
	"errors"
	"fmt"
	"os"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ----- Top-level config -----

// Config is the root structure parsed from vhost.toml.
type Config struct {
	Server ServerConfig  `toml:"server"`
	VHosts []VHostConfig `toml:"vhost"`
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
// A vhost is either a reverse-proxy entry (Backend set) or a static-file
// server (Root set). Both may be combined (e.g. proxy with static fallback).
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

	// HTTPRedirect forces HTTP→HTTPS redirect for this vhost (default: true when TLS configured).
	HTTPRedirect bool `toml:"http_redirect"`

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
	// Common use: ["ETag", "Last-Modified", "X-Powered-By", "Server"]
	Strip []string `toml:"strip"`
	// Inject is a map of header name → value to add to every response.
	// Common use: {"X-Forwarded-Proto": "https"}
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

// ----- Loader -----

// LoadConfig reads and parses path as a TOML vhost config file.
// If path does not exist, an empty Config with sensible defaults is returned
// without error — the webserver module is optional.
func LoadConfig(path string) (Config, error) {
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

	if err := validate(cfg); err != nil {
		return cfg, fmt.Errorf("webserver: %w", err)
	}

	applyDefaults(&cfg)
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

// applyDefaults fills in per-vhost fields that are optional in TOML but
// require a non-zero runtime value.
func applyDefaults(cfg *Config) {
	for i := range cfg.VHosts {
		v := &cfg.VHosts[i]
		if v.Index == "" {
			v.Index = "index.html"
		}
		if v.ProxyTimeoutSec <= 0 {
			v.ProxyTimeoutSec = 30
		}
		// Normalise host to lowercase
		v.Host = strings.ToLower(strings.TrimSpace(v.Host))
		for j, a := range v.Aliases {
			v.Aliases[j] = strings.ToLower(strings.TrimSpace(a))
		}
		// Default CORS methods/headers when enabled but not specified
		if v.CORS.Enabled {
			if len(v.CORS.Origins) == 0 {
				v.CORS.Origins = []string{"*"}
			}
			if len(v.CORS.Methods) == 0 {
				v.CORS.Methods = []string{"GET", "POST", "HEAD"}
			}
		}
		// HTTPRedirect defaults to true when TLS is configured
		if v.TLS.Cert != "" && v.TLS.Key != "" {
			// Only flip to true if user has not explicitly set it to false via
			// a pointer — TOML booleans default to false, so we cannot
			// distinguish "user wrote false" from "user omitted it". We treat
			// omission as "yes, redirect" whenever TLS is present.
			v.HTTPRedirect = true
		}
	}
	if cfg.Server.HTTPAddr == "" {
		cfg.Server.HTTPAddr = ":80"
	}
	if cfg.Server.HTTPSAddr == "" {
		cfg.Server.HTTPSAddr = ":443"
	}
}

// validate checks the config for logical errors.
func validate(cfg Config) error {
	seen := map[string]string{} // host → vhost name
	for _, v := range cfg.VHosts {
		if v.Host == "" {
			return fmt.Errorf("vhost %q: host must not be empty", v.Name)
		}
		if v.Backend == "" && v.Root == "" {
			return fmt.Errorf("vhost %q: at least one of backend or root must be set", v.Name)
		}
		if v.TLS.Cert != "" && v.TLS.Key == "" {
			return fmt.Errorf("vhost %q: tls.cert set but tls.key is missing", v.Name)
		}
		if v.TLS.Key != "" && v.TLS.Cert == "" {
			return fmt.Errorf("vhost %q: tls.key set but tls.cert is missing", v.Name)
		}
		// Duplicate host detection
		allHosts := append([]string{v.Host}, v.Aliases...)
		for _, h := range allHosts {
			h = strings.ToLower(strings.TrimSpace(h))
			if prior, ok := seen[h]; ok {
				return fmt.Errorf("vhost %q: host %q already registered by vhost %q", v.Name, h, prior)
			}
			seen[h] = v.Name
		}
	}
	return nil
}
