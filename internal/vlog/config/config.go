// Package config handles loading and validating vLog configuration from
// $VPROX_HOME/config/vlog.toml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config is the top-level structure for vlog.toml.
type Config struct {
	VLog VLogSection `toml:"vlog"`
}

// VLogSection holds all vLog settings.
type VLogSection struct {
	// Port is the HTTP listen port for the vLog web UI.
	Port int `toml:"port"`

	// BasePath is the URL prefix when vLog is served behind a reverse proxy
	// at a sub-path (e.g. "/vlog" for https://example.com/vlog).
	// Leave empty (default) when served at the root path.
	BasePath string `toml:"base_path"`

	// APIKey is a shared secret required for mutating API endpoints (block/unblock).
	// If empty, mutating endpoints are disabled. Set via vlog.toml [vlog] api_key.
	APIKey string `toml:"api_key"`

	// DBPath is the path to the SQLite database file.
	// Default: $VPROX_HOME/data/vlog.db
	DBPath string `toml:"db_path"`

	// ArchivesDir is the directory containing vProx log archives.
	// Default: $VPROX_HOME/data/logs/archives
	ArchivesDir string `toml:"archives_dir"`

	// VProxBin is the path to the vprox executable used to trigger on-demand
	// backups via the dashboard "Backup & Import" button.
	// Default: "vprox" (resolved from $PATH at runtime).
	VProxBin string `toml:"vprox_bin"`

	// WatchIntervalSec is the poll interval (seconds) for new archives.
	WatchIntervalSec int `toml:"watch_interval_sec"`

	// Push holds configuration for the integrated push validator deployment module.
	Push PushConfig `toml:"push"`

	// Intel holds IP intelligence enrichment settings.
	Intel IntelConfig `toml:"intel"`

	// Server holds HTTP server tuning parameters.
	Server ServerConfig `toml:"server"`

	// BindAddress is the IP address vLog binds to. Default: "127.0.0.1" (localhost only).
	// If Apache runs on the same machine, leave this as 127.0.0.1 and point
	// your Apache ProxyPass to http://127.0.0.1:<port>/.
	// If Apache is on a different machine, set this to the server's LAN IP
	// (e.g. "10.0.0.65") and restrict access with UFW:
	//   ufw allow from <apache-ip> to any port <port>
	BindAddress string `toml:"bind_address"`

	// Auth holds login credentials for the web dashboard.
	Auth AuthConfig `toml:"auth"`
}

// IntelConfig controls automatic IP intelligence enrichment.
type IntelConfig struct {
	// AutoEnrich enables automatic threat intel lookups for new IPs.
	AutoEnrich bool `toml:"auto_enrich"`

	// CacheTTLHours is how long (hours) cached intel results remain valid.
	CacheTTLHours int `toml:"cache_ttl_hours"`

	// RateLimitRPM is the maximum API calls per minute per intel source.
	RateLimitRPM int `toml:"rate_limit_rpm"`

	// Keys holds API keys for each intelligence source.
	Keys IntelKeys `toml:"keys"`
}

// IntelKeys stores API keys for threat intelligence providers.
type IntelKeys struct {
	AbuseIPDB  string `toml:"abuseipdb"`
	VirusTotal string `toml:"virustotal"`
	Shodan     string `toml:"shodan"`
}

// PushDefaults holds global SSH credential defaults for chain-managed hosts.
// Applied when [management] user or key_path are empty in chain.toml.
type PushDefaults struct {
	// User is the default SSH username for chain-managed hosts.
	User string `toml:"user"`
	// KeyPath is the default SSH private key path for chain-managed hosts.
	KeyPath string `toml:"key_path"`
}

// PushConfig configures the integrated push validator deployment module.
type PushConfig struct {
	// VMsPath is the path to the VM registry TOML file.
	// Default: $VPROX_HOME/config/push/vms.toml
	// DEPRECATED (v1.3.0): use chain.toml [management] sections instead.
	// vms.toml still works; chain [management] entries take precedence for matching chains.
	VMsPath string `toml:"vms_path"`

	// ChainsDir is the directory containing chain TOML files with [management] sections.
	// When set, the push module reads managed_host=true entries in addition to vms.toml.
	// Default: $VPROX_HOME/config/chains
	ChainsDir string `toml:"chains_dir"`

	// Defaults holds global SSH credential fallbacks for chain-managed hosts.
	// Applied when [management] user or key_path are empty in a chain.toml file.
	Defaults PushDefaults `toml:"defaults"`

	// DBPath is the path to the push SQLite state database.
	// Default: $VPROX_HOME/data/push.db
	DBPath string `toml:"db_path"`

	// PollIntervalSec is how often (seconds) chain status is refreshed.
	// Default: 60. Set 0 to disable background polling.
	PollIntervalSec int `toml:"poll_interval_sec"`
}

// AuthConfig holds dashboard login credentials.
// If PasswordHash is empty, the dashboard is accessible without login.
type AuthConfig struct {
	// Username is the login username (default: "admin").
	Username string `toml:"username"`

	// PasswordHash is a bcrypt hash of the password.
	// Generate with: htpasswd -nbBC 12 admin yourpassword | cut -d: -f2
	// Or: vlog setup (wizard, coming in v1.3.0)
	PasswordHash string `toml:"password_hash"`
}

// ServerConfig holds HTTP server timeout parameters.
type ServerConfig struct {
	// ReadTimeoutSec is the maximum duration (seconds) for reading a request.
	ReadTimeoutSec int `toml:"read_timeout_sec"`

	// WriteTimeoutSec is the maximum duration (seconds) for writing a response.
	WriteTimeoutSec int `toml:"write_timeout_sec"`
}

// DefaultConfig returns a Config with all defaults computed from the given
// home directory path (typically $VPROX_HOME).
func DefaultConfig(home string) Config {
	return Config{
		VLog: VLogSection{
			Port:             8889,
			DBPath:           filepath.Join(home, "data", "vlog.db"),
			ArchivesDir:      filepath.Join(home, "data", "logs", "archives"),
			WatchIntervalSec: 60,
			Intel: IntelConfig{
				AutoEnrich:    true,
				CacheTTLHours: 24,
				RateLimitRPM:  10,
			},
			Server: ServerConfig{
				ReadTimeoutSec:  30,
				WriteTimeoutSec: 30,
			},
			Auth: AuthConfig{
				Username: "admin",
			},
			Push: PushConfig{
				PollIntervalSec: 60,
			},
		},
	}
}

// Load reads vlog.toml from path and returns the parsed Config.
// If the file does not exist, it returns DefaultConfig with no error.
// TOML fields are optional; zero-value fields are backfilled with defaults.
func Load(path string) (Config, error) {
	home := FindHome()
	cfg := DefaultConfig(home)

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read vlog.toml: %w", err)
	}

	if err := toml.Unmarshal(b, &cfg); err != nil {
		return DefaultConfig(home), fmt.Errorf("parse vlog.toml: %w", err)
	}

	// Backfill zero-value fields with defaults.
	if cfg.VLog.Port == 0 {
		cfg.VLog.Port = 8889
	}
	// Normalise BasePath: trim whitespace and any trailing slash.
	cfg.VLog.BasePath = strings.TrimRight(strings.TrimSpace(cfg.VLog.BasePath), "/")
	if strings.TrimSpace(cfg.VLog.DBPath) == "" {
		cfg.VLog.DBPath = filepath.Join(home, "data", "vlog.db")
	}
	if strings.TrimSpace(cfg.VLog.ArchivesDir) == "" {
		cfg.VLog.ArchivesDir = filepath.Join(home, "data", "logs", "archives")
	}
	if cfg.VLog.WatchIntervalSec <= 0 {
		cfg.VLog.WatchIntervalSec = 60
	}
	if cfg.VLog.Intel.CacheTTLHours <= 0 {
		cfg.VLog.Intel.CacheTTLHours = 24
	}
	if cfg.VLog.Intel.RateLimitRPM <= 0 {
		cfg.VLog.Intel.RateLimitRPM = 10
	}
	if cfg.VLog.Server.ReadTimeoutSec <= 0 {
		cfg.VLog.Server.ReadTimeoutSec = 30
	}
	if cfg.VLog.Server.WriteTimeoutSec <= 0 {
		cfg.VLog.Server.WriteTimeoutSec = 30
	}
	if cfg.VLog.Auth.Username == "" {
		cfg.VLog.Auth.Username = "admin"
	}
	if strings.TrimSpace(cfg.VLog.BindAddress) == "" {
		cfg.VLog.BindAddress = "127.0.0.1"
	}
	if cfg.VLog.Push.PollIntervalSec <= 0 {
		cfg.VLog.Push.PollIntervalSec = 60
	}
	if strings.TrimSpace(cfg.VLog.Push.VMsPath) == "" {
		cfg.VLog.Push.VMsPath = filepath.Join(home, "config", "push", "vms.toml")
	}
	if strings.TrimSpace(cfg.VLog.Push.ChainsDir) == "" {
		cfg.VLog.Push.ChainsDir = filepath.Join(home, "config", "chains")
	}
	if strings.TrimSpace(cfg.VLog.Push.DBPath) == "" {
		cfg.VLog.Push.DBPath = filepath.Join(home, "data", "push.db")
	}

	return cfg, nil
}

// Validate checks Config invariants and returns the first error found.
func (c *Config) Validate() error {
	if c.VLog.Port < 1 || c.VLog.Port > 65535 {
		return fmt.Errorf("vlog: port %d out of range 1-65535", c.VLog.Port)
	}
	if c.VLog.WatchIntervalSec <= 0 {
		return fmt.Errorf("vlog: watch_interval_sec must be > 0, got %d", c.VLog.WatchIntervalSec)
	}
	if c.VLog.Intel.CacheTTLHours <= 0 {
		return fmt.Errorf("vlog: intel.cache_ttl_hours must be > 0, got %d", c.VLog.Intel.CacheTTLHours)
	}
	return nil
}

// FindHome returns the vProx home directory.
// Priority: $VPROX_HOME → $HOME/.vProx → ".vProx" (cwd fallback).
func FindHome() string {
	if v := strings.TrimSpace(os.Getenv("VPROX_HOME")); v != "" {
		return v
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Join(h, ".vProx")
	}
	return ".vProx"
}
