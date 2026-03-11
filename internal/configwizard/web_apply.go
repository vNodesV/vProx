package configwizard

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/vNodesV/vProx/internal/backup"
	chainconfig "github.com/vNodesV/vProx/internal/config"
	vlogcfg "github.com/vNodesV/vProx/internal/vlog/config"
)

// applyWebFields processes a JSON field map from the browser and writes the resulting TOML file.
// It reuses the same struct types as the terminal wizard.
func applyWebFields(home, step string, f map[string]any) error {
	switch step {
	case "ports":
		return applyPorts(home, f)
	case "settings":
		return applySettings(home, f)
	case "chain":
		return applyChain(home, f)
	case "vlog":
		return applyVLog(home, f)
	case "fleet":
		return applyFleet(home, f)
	case "infra":
		return applyInfra(home, f)
	case "backup":
		return applyBackup(home, f)
	default:
		return fmt.Errorf("unknown step: %q", step)
	}
}

// ---- per-step appliers ----

func applyPorts(home string, f map[string]any) error {
	p := chainconfig.Ports{
		RPC:     fieldInt(f, "rpc", 26657),
		REST:    fieldInt(f, "rest", 1317),
		GRPC:    fieldInt(f, "grpc", 0),
		GRPCWeb: fieldInt(f, "grpc_web", 0),
		API:     fieldInt(f, "api", 0),
		VLogURL: fieldStr(f, "vlog_url", ""),
	}
	if raw := fieldStr(f, "trusted_proxies", ""); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			c = strings.TrimSpace(c)
			if c != "" {
				if _, _, err := net.ParseCIDR(c); err != nil {
					return fmt.Errorf("invalid CIDR %q: %w", c, err)
				}
				p.TrustedProxies = append(p.TrustedProxies, c)
			}
		}
	}
	return writeConfigNoPrompt(configPath(home, "chains", "ports.toml"), p)
}

func applySettings(home string, f map[string]any) error {
	var s proxySettingsFile
	s.RateLimit.RPS = fieldFloat(f, "rps", 25)
	s.RateLimit.Burst = fieldInt(f, "burst", 100)
	s.AutoQuarantine.Enabled = fieldBool(f, "aq_enabled", true)
	s.AutoQuarantine.Threshold = fieldInt(f, "aq_threshold", 120)
	s.AutoQuarantine.WindowSec = fieldInt(f, "aq_window_sec", 10)
	s.AutoQuarantine.PenaltyRPS = fieldFloat(f, "aq_penalty_rps", 1.0)
	s.AutoQuarantine.PenaltyBurst = fieldInt(f, "aq_penalty_burst", 1)
	s.AutoQuarantine.TTLSec = fieldInt(f, "aq_ttl_sec", 900)
	s.Debug.Enabled = fieldBool(f, "debug_enabled", false)
	s.Debug.Port = fieldInt(f, "debug_port", 6060)
	return writeConfigNoPrompt(configPath(home, "vprox", "settings.toml"), s)
}

func applyChain(home string, f map[string]any) error {
	name := fieldStr(f, "chain_name", "")
	if name == "" {
		return fmt.Errorf("chain_name is required")
	}
	var c chainconfig.ChainConfig
	c.SchemaVersion = 1
	c.ChainName = name
	c.ChainID = fieldStr(f, "chain_id", "")
	c.DashboardName = fieldStr(f, "dashboard_name", "")
	c.Host = fieldStr(f, "host", "")
	c.IP = fieldStr(f, "ip", "")
	c.ExplorerBase = fieldStr(f, "explorer_base", "")
	c.Expose.Path = fieldBool(f, "expose_path", true)
	c.Expose.VHost = fieldBool(f, "expose_vhost", true)
	c.Expose.VHostPrefix.RPC = fieldStr(f, "vhost_prefix_rpc", "rpc")
	c.Expose.VHostPrefix.REST = fieldStr(f, "vhost_prefix_rest", "api")
	c.Services.RPC = fieldBool(f, "svc_rpc", true)
	c.Services.REST = fieldBool(f, "svc_rest", true)
	c.Services.WebSocket = fieldBool(f, "svc_ws", true)
	c.Services.GRPC = fieldBool(f, "svc_grpc", true)
	c.Services.GRPCWeb = fieldBool(f, "svc_grpcweb", true)
	c.Services.APIAlias = fieldBool(f, "svc_apialias", true)
	c.DefaultPorts = fieldBool(f, "default_ports", true)
	c.Management.ManagedHost = fieldBool(f, "managed_host", false)
	c.Management.LanIP = fieldStr(f, "management_lan_ip", "")
	c.Management.User = fieldStr(f, "management_user", "")
	c.Management.KeyPath = fieldStr(f, "management_key_path", "")
	c.Management.ExposedServices = fieldBool(f, "exposed_services", true)
	c.Management.Valoper = fieldStr(f, "valoper", "")
	c.Management.Ping.Country = fieldStr(f, "ping_country", "")
	c.Management.Ping.Provider = fieldStr(f, "ping_provider", "")

	if err := chainconfig.ValidateConfig(&c); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	return writeConfigNoPrompt(configPath(home, "chains", name+".toml"), c)
}

func applyVLog(home string, f map[string]any) error {
	var cfg vlogcfg.Config
	v := &cfg.VLog
	v.Port = fieldInt(f, "port", 8889)
	v.BindAddress = fieldStr(f, "bind_address", "127.0.0.1")
	v.BasePath = fieldStr(f, "base_path", "")
	v.APIKey = fieldStr(f, "api_key", "")
	v.Auth.Username = fieldStr(f, "username", "admin")
	v.Auth.PasswordHash = fieldStr(f, "password_hash", "")
	v.Intel.AutoEnrich = fieldBool(f, "auto_enrich", true)
	v.Intel.CacheTTLHours = fieldInt(f, "cache_ttl_hours", 24)
	v.Intel.RateLimitRPM = fieldInt(f, "rate_limit_rpm", 10)
	v.Intel.Keys.AbuseIPDB = fieldStr(f, "abuseipdb", "")
	v.Intel.Keys.VirusTotal = fieldStr(f, "virustotal", "")
	v.Intel.Keys.Shodan = fieldStr(f, "shodan", "")
	v.Push.Defaults.User = fieldStr(f, "push_user", "")
	v.Push.Defaults.KeyPath = fieldStr(f, "push_key_path", "")
	v.Push.PollIntervalSec = fieldInt(f, "poll_interval_sec", 60)
	v.DBPath = fieldStr(f, "db_path", "")
	v.ArchivesDir = fieldStr(f, "archives_dir", "")
	v.VProxBin = fieldStr(f, "vprox_bin", "vprox")
	return writeConfigNoPrompt(configPath(home, "vlog", "vlog.toml"), cfg)
}

func applyFleet(home string, f map[string]any) error {
	var s fleetSettingsFile
	s.SSH.User = fieldStr(f, "ssh_user", "ubuntu")
	s.SSH.KeyPath = fieldStr(f, "ssh_key_path", "")
	s.SSH.Port = fieldInt(f, "ssh_port", 22)
	s.Poll.IntervalSec = fieldInt(f, "poll_interval_sec", 60)
	s.Defaults.Datacenter = fieldStr(f, "datacenter", "")
	return writeConfigNoPrompt(configPath(home, "fleet", "settings.toml"), s)
}

func applyInfra(home string, f map[string]any) error {
	dc := fieldStr(f, "datacenter", "")
	if dc == "" {
		return fmt.Errorf("datacenter name is required")
	}
	tomlRaw := fieldStr(f, "toml_raw", "")
	if tomlRaw == "" {
		return fmt.Errorf("toml_raw field required for infra step")
	}
	var inf infraFile
	if err := toml.Unmarshal([]byte(tomlRaw), &inf); err != nil {
		return fmt.Errorf("parse infra TOML: %w", err)
	}
	return writeConfigNoPrompt(configPath(home, "infra", dc+".toml"), inf)
}

func applyBackup(home string, f map[string]any) error {
	cfg := backup.BackupConfig{}
	b := &cfg.Backup
	b.Automation = fieldBool(f, "automation", false)
	b.IntervalDays = fieldInt(f, "interval_days", 7)
	b.MaxSizeMB = int64(fieldInt(f, "max_size_mb", 100))
	b.CheckIntervalMin = fieldInt(f, "check_interval_min", 10)
	b.Destination = fieldStr(f, "destination", "")
	b.Compression = "tar.gz"
	b.Files.Logs = splitList(fieldStr(f, "files_logs", "main.log"))
	b.Files.Data = splitList(fieldStr(f, "files_data", "access-counts.json"))
	b.Files.Config = splitList(fieldStr(f, "files_config", "chains/ports.toml"))
	return writeConfigNoPrompt(configPath(home, "backup", "backup.toml"), cfg)
}

// ---- field helpers ----

func fieldStr(f map[string]any, key, def string) string {
	if v, ok := f[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return def
}

func fieldInt(f map[string]any, key string, def int) int {
	if v, ok := f[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case string:
			if i, err := strconv.Atoi(n); err == nil {
				return i
			}
		}
	}
	return def
}

func fieldFloat(f map[string]any, key string, def float64) float64 {
	if v, ok := f[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case string:
			if fl, err := strconv.ParseFloat(n, 64); err == nil {
				return fl
			}
		}
	}
	return def
}

func fieldBool(f map[string]any, key string, def bool) bool {
	if v, ok := f[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			switch strings.ToLower(b) {
			case "true", "yes", "1":
				return true
			case "false", "no", "0":
				return false
			}
		}
	}
	return def
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runCmd executes an external command. Used for browser launch.
func runCmd(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}
