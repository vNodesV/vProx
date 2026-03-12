package configwizard

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/vNodesV/vProx/internal/backup"
	chainconfig "github.com/vNodesV/vProx/internal/config"
	fleetcfg "github.com/vNodesV/vProx/internal/fleet/config"
	vlogcfg "github.com/vNodesV/vProx/internal/vlog/config"
)

// ApplyFields applies one web wizard step payload to disk.
// Exported for reuse by the dashboard Settings page.
func ApplyFields(home, step string, f map[string]any) error {
	return applyWebFields(home, step, f)
}

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
	c.Management.PublicIP = fieldStr(f, "management_public_ip", "")
	c.Management.User = fieldStr(f, "management_user", "")
	c.Management.KeyPath = fieldStr(f, "management_key_path", "")
	c.Management.Port = fieldInt(f, "management_port", 0)
	c.Management.Datacenter = fieldStr(f, "management_datacenter", "")
	c.Management.Type = splitList(fieldStr(f, "management_type", ""))
	c.Management.ExposedServices = fieldBool(f, "exposed_services", true)
	c.Management.Valoper = fieldStr(f, "valoper", "")
	c.Management.Ping.Country = strings.ToUpper(fieldStrAny(f, "", "management_ping_country", "ping_country"))
	c.Management.Ping.Provider = fieldStrAny(f, "", "management_ping_provider", "ping_provider")
	c.ChainPing.Country = strings.ToUpper(fieldStr(f, "chain_ping_country", ""))
	c.ChainPing.Provider = fieldStr(f, "chain_ping_provider", "")
	c.ChainServices.Validator.Mainnet.Address = fieldStr(f, "validator_mainnet_address", "")
	c.ChainServices.Validator.Testnet.Address = fieldStr(f, "validator_testnet_address", "")
	c.ChainServices.SP.Mainnet.Hostname = fieldStr(f, "sp_mainnet_hostname", "")
	c.ChainServices.SP.Mainnet.LanIP = fieldStr(f, "sp_mainnet_lan_ip", "")
	c.ChainServices.SP.Testnet.Hostname = fieldStr(f, "sp_testnet_hostname", "")
	c.ChainServices.SP.Testnet.LanIP = fieldStr(f, "sp_testnet_lan_ip", "")
	if !c.DefaultPorts {
		c.Ports.RPC = fieldInt(f, "port_rpc", 26657)
		c.Ports.REST = fieldInt(f, "port_rest", 1317)
		c.Ports.GRPC = fieldInt(f, "port_grpc", 9090)
		c.Ports.GRPCWeb = fieldInt(f, "port_grpc_web", 9091)
		c.Ports.API = fieldInt(f, "port_api", 1317)
	}

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
	dc := strings.ToLower(strings.TrimSpace(fieldStr(f, "datacenter", "")))
	if dc == "" {
		return fmt.Errorf("datacenter name is required")
	}

	// Backward compatibility: accept raw TOML payload from older wizard pages.
	if tomlRaw := strings.TrimSpace(fieldStr(f, "toml_raw", "")); tomlRaw != "" {
		var inf infraFile
		if err := toml.Unmarshal([]byte(tomlRaw), &inf); err != nil {
			return fmt.Errorf("parse infra TOML: %w", err)
		}
		return writeConfigNoPrompt(configPath(home, "infra", dc+".toml"), inf)
	}

	var inf infraFile
	inf.Host.Name = fieldStr(f, "host_name", "")
	inf.Host.LanIP = fieldStr(f, "host_lan_ip", "")
	inf.Host.PublicIP = fieldStr(f, "host_public_ip", "")
	inf.Host.Datacenter = fieldStr(f, "host_datacenter", "")
	inf.Host.User = fieldStr(f, "host_user", "")
	inf.Host.SSHKeyPath = fieldStr(f, "host_ssh_key_path", "")

	if inf.Host.Name == "" {
		if inf.Host.LanIP != "" || inf.Host.PublicIP != "" || inf.Host.Datacenter != "" || inf.Host.User != "" || inf.Host.SSHKeyPath != "" {
			return fmt.Errorf("host_name is required when host fields are set")
		}
	} else {
		if inf.Host.Datacenter == "" {
			inf.Host.Datacenter = strings.ToUpper(dc)
		}
		if inf.Host.LanIP != "" && net.ParseIP(inf.Host.LanIP) == nil {
			return fmt.Errorf("host_lan_ip must be a valid IP address")
		}
		if inf.Host.PublicIP != "" && net.ParseIP(inf.Host.PublicIP) == nil {
			return fmt.Errorf("host_public_ip must be a valid IP address")
		}
	}

	inf.VProx.Name = fieldStr(f, "vprox_name", "vProx")
	inf.VProx.LanIP = fieldStr(f, "vprox_lan_ip", "")
	inf.VProx.Key = fieldStr(f, "vprox_key", "")
	inf.VProx.SSHKeyPath = fieldStr(f, "vprox_ssh_key_path", "")
	if inf.VProx.LanIP != "" && net.ParseIP(inf.VProx.LanIP) == nil {
		return fmt.Errorf("vprox_lan_ip must be a valid IP address")
	}

	vms, err := parseInfraVMs(fieldStr(f, "vms_json", ""))
	if err != nil {
		return err
	}
	seen := loadExistingInfraVMKeys(home, dc)
	inf.VMs = make([]fleetcfg.VM, 0, len(vms))
	for _, vm := range vms {
		key := vmIdentityKey(vm)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		inf.VMs = append(inf.VMs, vm)
	}

	return writeConfigNoPrompt(configPath(home, "infra", dc+".toml"), inf)
}

func parseInfraVMs(raw string) ([]fleetcfg.VM, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var rows []map[string]any
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, fmt.Errorf("invalid vms_json payload: %w", err)
	}

	vms := make([]fleetcfg.VM, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i, row := range rows {
		port := fieldInt(row, "port", 0)
		vm := fleetcfg.VM{
			Name:       fieldStr(row, "name", ""),
			Host:       fieldStr(row, "host", ""),
			LanIP:      fieldStr(row, "lan_ip", ""),
			PublicIP:   fieldStr(row, "public_ip", ""),
			Port:       port,
			User:       fieldStr(row, "user", ""),
			KeyPath:    fieldStr(row, "key_path", ""),
			Datacenter: fieldStr(row, "datacenter", ""),
			Type:       fieldStr(row, "type", ""),
			ChainName:  fieldStr(row, "chain_name", ""),
		}
		vm.Ping.Country = strings.ToUpper(fieldStr(row, "ping_country", ""))
		vm.Ping.Provider = fieldStr(row, "ping_provider", "")

		hasAny := vm.Name != "" || vm.Host != "" || vm.LanIP != "" || vm.PublicIP != "" ||
			port != 0 || vm.User != "" || vm.KeyPath != "" || vm.Datacenter != "" ||
			vm.Type != "" || vm.ChainName != "" || vm.Ping.Country != "" || vm.Ping.Provider != ""
		if !hasAny {
			continue
		}
		if vm.Name == "" {
			return nil, fmt.Errorf("vm[%d].name is required", i+1)
		}
		if vm.Host == "" {
			return nil, fmt.Errorf("vm[%d].host is required", i+1)
		}
		if vm.Port == 0 {
			vm.Port = 22
		}
		if vm.Port < 1 || vm.Port > 65535 {
			return nil, fmt.Errorf("vm[%d].port must be 1-65535", i+1)
		}
		if vm.LanIP != "" && net.ParseIP(vm.LanIP) == nil {
			return nil, fmt.Errorf("vm[%d].lan_ip must be a valid IP address", i+1)
		}
		if vm.PublicIP != "" && net.ParseIP(vm.PublicIP) == nil {
			return nil, fmt.Errorf("vm[%d].public_ip must be a valid IP address", i+1)
		}
		if vm.Ping.Country != "" && !validCountries[vm.Ping.Country] {
			return nil, fmt.Errorf("vm[%d].ping_country is unsupported: %q", i+1, vm.Ping.Country)
		}

		key := vmIdentityKey(vm)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		vms = append(vms, vm)
	}

	return vms, nil
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

func fieldStrAny(f map[string]any, def string, keys ...string) string {
	for _, key := range keys {
		if v := fieldStr(f, key, ""); strings.TrimSpace(v) != "" {
			return v
		}
	}
	return def
}

func vmIdentityKey(vm fleetcfg.VM) string {
	return strings.ToLower(strings.TrimSpace(vm.Name)) + "|" +
		strings.ToLower(strings.TrimSpace(vm.Host)) + "|" +
		strings.ToLower(strings.TrimSpace(vm.ChainName)) + "|" +
		strings.ToLower(strings.TrimSpace(vm.Datacenter))
}

func loadExistingInfraVMKeys(home, targetDC string) map[string]struct{} {
	keys := make(map[string]struct{})
	dir := configPath(home, "infra")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return keys
	}
	target := strings.ToLower(strings.TrimSpace(targetDC)) + ".toml"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".toml") {
			continue
		}
		if strings.EqualFold(e.Name(), target) {
			continue
		}
		data, err := os.ReadFile(configPath(home, "infra", e.Name()))
		if err != nil {
			continue
		}
		var inf infraFile
		if err := toml.Unmarshal(data, &inf); err != nil {
			continue
		}
		for _, vm := range inf.VMs {
			keys[vmIdentityKey(vm)] = struct{}{}
		}
	}
	return keys
}

// runCmd executes an external command. Used for browser launch.
func runCmd(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}
