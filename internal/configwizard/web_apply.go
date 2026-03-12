package configwizard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
	"github.com/vNodesV/vProx/internal/backup"
	chainconfig "github.com/vNodesV/vProx/internal/config"
	fleetcfg "github.com/vNodesV/vProx/internal/fleet/config"
	fleetstate "github.com/vNodesV/vProx/internal/fleet/state"
	vlogcfg "github.com/vNodesV/vProx/internal/vlog/config"
)

const maxImportFileBytes int64 = 512 * 1024

// ApplyFields applies one web wizard step payload to disk.
// Exported for reuse by the dashboard Settings page.
func ApplyFields(home, step string, f map[string]any) error {
	return applyWebFields(home, step, f)
}

// RemoveStepEntry removes a variable-size config file managed by web editors.
// Supported steps: chain, infra.
func RemoveStepEntry(home, step, target string) error {
	step = strings.ToLower(strings.TrimSpace(step))
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target is required")
	}

	switch step {
	case "chain":
		name, err := normalizeConfigFilename(target)
		if err != nil {
			return err
		}
		if !chainconfig.IsChainTOML(name) {
			return fmt.Errorf("invalid chain target: %q", target)
		}
		if strings.EqualFold(name, "ports.toml") {
			return fmt.Errorf("ports.toml cannot be removed from this endpoint")
		}
		primary := configPath(home, "vlog", "chains", name)
		legacy := configPath(home, "chains", name)
		path := primary
		if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read chain file: %w", err)
		} else if os.IsNotExist(err) {
			path = legacy
		}

		aliases := collectChainAliases(path, name)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove chain file: %w", err)
		}
		if err := unregisterRegisteredChains(home, aliases); err != nil {
			return err
		}
		return nil
	case "infra":
		name, err := normalizeConfigFilename(target)
		if err != nil {
			return err
		}
		path := configPath(home, "infra", name)
		aliases := collectInfraAliases(path)
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove infra file: %w", err)
		}
		if err := unregisterRegisteredChains(home, aliases); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("remove is only supported for chain and infra")
	}
}

func normalizeConfigFilename(target string) (string, error) {
	name := strings.TrimSpace(target)
	if name == "" {
		return "", fmt.Errorf("target is required")
	}
	name = filepath.Clean(name)
	if filepath.Base(name) != name {
		return "", fmt.Errorf("target must be a file name, not a path")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".toml") {
		name += ".toml"
	}
	if len(name) <= len(".toml") {
		return "", fmt.Errorf("invalid target file name")
	}
	return name, nil
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
	name := strings.ToLower(strings.TrimSpace(fieldStr(f, "chain_name", "")))
	if name == "" {
		return fmt.Errorf("chain_name is required")
	}

	if !reSlug.MatchString(name) {
		return fmt.Errorf("chain_name must use lowercase letters, digits, and hyphens")
	}

	chainID := strings.TrimSpace(fieldStr(f, "chain_id", ""))
	if chainID == "" {
		return fmt.Errorf("chain_id is required")
	}

	services, err := parseChainServicesPayload(f)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return fmt.Errorf("at least one chain service is required")
	}

	c := chainconfig.ChainIdentity{
		SchemaVersion: 2,
		TreeName:      name,
		ChainDetails: chainconfig.ChainDetails{
			Name:          name,
			DashboardName: strings.TrimSpace(fieldStr(f, "dashboard_name", "")),
			ChainID:       chainID,
			ExplorerBase:  strings.TrimSpace(fieldStr(f, "explorer_base", "")),
			Explorers:     splitList(fieldStr(f, "explorers", "")),
		},
		ChainServices: services,
		ChainPing: chainconfig.ChainPingConfig{
			Country:  strings.ToUpper(strings.TrimSpace(fieldStr(f, "chain_ping_country", ""))),
			Provider: strings.TrimSpace(fieldStr(f, "chain_ping_provider", "")),
		},
	}

	// Legacy top-level mirrors retained for compatibility with older readers.
	c.ChainName = c.ChainDetails.Name
	c.ChainID = c.ChainDetails.ChainID
	c.DashboardName = c.ChainDetails.DashboardName
	c.ExplorerBase = c.ChainDetails.ExplorerBase
	c.Explorers = append([]string(nil), c.ChainDetails.Explorers...)

	if c.ChainPing.Country != "" && !validCountries[c.ChainPing.Country] {
		return fmt.Errorf("chain_ping_country is unsupported: %q", c.ChainPing.Country)
	}

	return writeConfigNoPrompt(configPath(home, "vlog", "chains", name+".toml"), c)
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

var allowedChainServiceTypes = map[string]struct{}{
	"node":      {},
	"validator": {},
	"relayer":   {},
	"sp":        {},
}

func parseChainServicesPayload(f map[string]any) ([]chainconfig.ChainService, error) {
	raw := strings.TrimSpace(fieldStr(f, "chain_services_json", ""))
	rows := make([]map[string]any, 0)
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &rows); err != nil {
			return nil, fmt.Errorf("invalid chain_services_json payload: %w", err)
		}
	} else {
		rows = legacyChainServiceRowsFromFields(f)
	}

	services := make([]chainconfig.ChainService, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i, row := range rows {
		name := strings.TrimSpace(fieldStr(row, "name", ""))
		moniker := strings.TrimSpace(fieldStr(row, "moniker", ""))
		serviceType := strings.ToLower(strings.TrimSpace(fieldStr(row, "service_type", "")))
		valoper := strings.TrimSpace(fieldStr(row, "valoper", ""))
		internalIP := strings.TrimSpace(fieldStr(row, "internal_ip", ""))
		host := strings.TrimSpace(fieldStr(row, "host", ""))
		linkToVM := fieldBool(row, "link_to_vm", true)

		hasAny := name != "" || moniker != "" || serviceType != "" || valoper != "" || internalIP != "" || host != ""
		if !hasAny {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("chain_services[%d].name is required", i+1)
		}
		if serviceType == "" {
			return nil, fmt.Errorf("chain_services[%d].service_type is required", i+1)
		}
		if _, ok := allowedChainServiceTypes[serviceType]; !ok {
			return nil, fmt.Errorf("chain_services[%d].service_type is unsupported: %q", i+1, serviceType)
		}
		if internalIP != "" && net.ParseIP(internalIP) == nil {
			return nil, fmt.Errorf("chain_services[%d].internal_ip must be a valid IP address", i+1)
		}
		if linkToVM && internalIP == "" {
			return nil, fmt.Errorf("chain_services[%d].internal_ip is required when link_to_vm is enabled", i+1)
		}
		if serviceType == "validator" && valoper == "" {
			return nil, fmt.Errorf("chain_services[%d].valoper is required for validator service rows", i+1)
		}

		key := strings.ToLower(name) + "|" + serviceType + "|" + strings.ToLower(internalIP)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		services = append(services, chainconfig.ChainService{
			Name:        name,
			Moniker:     moniker,
			ServiceType: serviceType,
			Valoper:     valoper,
			InternalIP:  internalIP,
			Host:        host,
			LinkToVM:    linkToVM,
		})
	}

	return services, nil
}

func legacyChainServiceRowsFromFields(f map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, 3)

	valoper := strings.TrimSpace(fieldStrAny(f, "", "valoper", "validator_mainnet_address", "validator_testnet_address"))
	if valoper != "" {
		rows = append(rows, map[string]any{
			"name":         "validator",
			"service_type": "validator",
			"valoper":      valoper,
			"internal_ip":  strings.TrimSpace(fieldStrAny(f, "", "management_lan_ip", "sp_mainnet_lan_ip")),
			"link_to_vm":   true,
		})
	}

	if host := strings.TrimSpace(fieldStrAny(f, "", "sp_mainnet_hostname", "sp_testnet_hostname")); host != "" ||
		strings.TrimSpace(fieldStrAny(f, "", "sp_mainnet_lan_ip", "sp_testnet_lan_ip")) != "" {
		rows = append(rows, map[string]any{
			"name":         "service",
			"service_type": "sp",
			"host":         host,
			"internal_ip":  strings.TrimSpace(fieldStrAny(f, "", "sp_mainnet_lan_ip", "sp_testnet_lan_ip")),
			"link_to_vm":   true,
		})
	}

	return rows
}

func chainServicesToMaps(in []chainconfig.ChainService) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, svc := range in {
		name := strings.TrimSpace(svc.Name)
		serviceType := strings.ToLower(strings.TrimSpace(svc.ServiceType))
		if name == "" && serviceType == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":         name,
			"moniker":      strings.TrimSpace(svc.Moniker),
			"service_type": serviceType,
			"valoper":      strings.TrimSpace(svc.Valoper),
			"internal_ip":  strings.TrimSpace(svc.InternalIP),
			"host":         strings.TrimSpace(svc.Host),
			"link_to_vm":   svc.LinkToVM,
		})
	}
	if len(out) == 0 {
		return make([]map[string]any, 0)
	}
	return out
}

func collectChainAliases(path, fallbackName string) []string {
	base := strings.TrimSuffix(strings.TrimSpace(fallbackName), ".toml")
	out := []string{base}

	data, err := os.ReadFile(path)
	if err != nil {
		return uniqueNonEmpty(out)
	}
	ci, err := chainconfig.ParseChainIdentity(data, filepath.Base(path))
	if err != nil {
		return uniqueNonEmpty(out)
	}
	out = append(out, ci.EffectiveChainName(), ci.EffectiveChainID())
	return uniqueNonEmpty(out)
}

func collectInfraAliases(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var inf infraFile
	if err := toml.Unmarshal(data, &inf); err != nil {
		return nil
	}
	out := make([]string, 0, len(inf.VMs)*2)
	for _, vm := range inf.VMs {
		out = append(out, vm.Name, vm.ChainName)
	}
	return uniqueNonEmpty(out)
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		t := strings.TrimSpace(v)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

func unregisterRegisteredChains(home string, aliases []string) error {
	aliases = uniqueNonEmpty(aliases)
	if len(aliases) == 0 {
		return nil
	}

	dbPath := filepath.Join(home, "data", "push.db")
	if cfg, err := vlogcfg.Load(configPath(home, "vlog", "vlog.toml")); err == nil {
		if trimmed := strings.TrimSpace(cfg.VLog.Push.DBPath); trimmed != "" {
			dbPath = trimmed
		}
	}

	db, err := fleetstate.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open fleet state DB: %w", err)
	}
	defer db.Close()

	var errs []string
	for _, alias := range aliases {
		if err := db.RemoveRegisteredChain(alias); err != nil {
			if errors.Is(err, fleetstate.ErrNotFound) {
				continue
			}
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("unregister chain entries: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ImportStepFieldsFromPath validates and converts an external TOML file into wizard field payloads.
func ImportStepFieldsFromPath(step, srcPath string) (map[string]any, string, error) {
	step = strings.ToLower(strings.TrimSpace(step))
	if step == "" {
		return nil, "", fmt.Errorf("step is required")
	}
	path, data, err := readImportTOML(srcPath)
	if err != nil {
		return nil, "", err
	}
	if err := validateImportLocation(step, path); err != nil {
		return nil, "", err
	}
	fields, err := importFieldsFromTOML(step, path, data)
	if err != nil {
		return nil, "", err
	}
	return fields, path, nil
}

func readImportTOML(raw string) (string, []byte, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", nil, fmt.Errorf("path is required")
	}
	if strings.ContainsRune(path, '\x00') {
		return "", nil, fmt.Errorf("path contains invalid null bytes")
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", nil, fmt.Errorf("resolve path: %w", err)
		}
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	path = filepath.Clean(path)
	if !strings.EqualFold(filepath.Ext(path), ".toml") {
		return "", nil, fmt.Errorf("import file must use .toml extension")
	}

	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("read file metadata: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil, fmt.Errorf("import path must reference a regular file")
	}
	if info.Size() <= 0 {
		return "", nil, fmt.Errorf("import file is empty")
	}
	if info.Size() > maxImportFileBytes {
		return "", nil, fmt.Errorf("import file exceeds %d KB safety limit", maxImportFileBytes/1024)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read import file: %w", err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", nil, fmt.Errorf("import file contains invalid binary data")
	}
	if !utf8.Valid(data) {
		return "", nil, fmt.Errorf("import file must be valid UTF-8 TOML text")
	}
	return path, data, nil
}

func validateImportLocation(step, path string) error {
	lower := strings.ToLower(filepath.ToSlash(path))
	if !strings.Contains(lower, "/config/") {
		return fmt.Errorf("import path must be inside a vProx config directory (.../config/...)")
	}

	base := strings.ToLower(filepath.Base(path))
	switch step {
	case "ports":
		if !strings.Contains(lower, "/config/chains/") || base != "ports.toml" {
			return fmt.Errorf("ports import expects .../config/chains/ports.toml")
		}
	case "settings":
		if !strings.Contains(lower, "/config/vprox/") || base != "settings.toml" {
			return fmt.Errorf("settings import expects .../config/vprox/settings.toml")
		}
	case "chain":
		if strings.Contains(lower, "/config/vlog/chains/") {
			return nil
		}
		if strings.Contains(lower, "/config/chains/") && base != "ports.toml" {
			return nil // legacy chain layout
		}
		return fmt.Errorf("chain import expects .../config/vlog/chains/*.toml (or legacy .../config/chains/*.toml)")
	case "vlog":
		if !strings.Contains(lower, "/config/vlog/") || base != "vlog.toml" {
			return fmt.Errorf("vlog import expects .../config/vlog/vlog.toml")
		}
	case "fleet":
		if !strings.Contains(lower, "/config/fleet/") || base != "settings.toml" {
			return fmt.Errorf("fleet import expects .../config/fleet/settings.toml")
		}
	case "infra":
		if strings.HasSuffix(lower, "/config/push/vms.toml") {
			return nil // legacy migration source
		}
		if !strings.Contains(lower, "/config/infra/") {
			return fmt.Errorf("infra import expects .../config/infra/<datacenter>.toml or legacy .../config/push/vms.toml")
		}
	case "backup":
		if !strings.Contains(lower, "/config/backup/") || base != "backup.toml" {
			return fmt.Errorf("backup import expects .../config/backup/backup.toml")
		}
	default:
		return fmt.Errorf("unknown step: %q", step)
	}
	return nil
}

func importFieldsFromTOML(step, path string, data []byte) (map[string]any, error) {
	switch step {
	case "ports":
		return importPortsFields(data)
	case "settings":
		return importSettingsFields(data)
	case "chain":
		return importChainFields(data)
	case "vlog":
		return importVLogFields(data)
	case "fleet":
		return importFleetFields(data)
	case "infra":
		return importInfraFields(path, data)
	case "backup":
		return importBackupFields(data)
	default:
		return nil, fmt.Errorf("unknown step: %q", step)
	}
}

func importPortsFields(data []byte) (map[string]any, error) {
	var p chainconfig.Ports
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse ports TOML: %w", err)
	}
	if p.RPC <= 0 || p.REST <= 0 {
		return nil, fmt.Errorf("ports.toml must define positive rpc and rest values")
	}
	return map[string]any{
		"rpc":             p.RPC,
		"rest":            p.REST,
		"grpc":            p.GRPC,
		"grpc_web":        p.GRPCWeb,
		"api":             p.API,
		"vlog_url":        p.VLogURL,
		"trusted_proxies": strings.Join(p.TrustedProxies, ","),
	}, nil
}

func importSettingsFields(data []byte) (map[string]any, error) {
	rawLower := strings.ToLower(string(data))
	if !strings.Contains(rawLower, "[rate_limit]") && !strings.Contains(rawLower, "[auto_quarantine]") {
		return nil, fmt.Errorf("file does not look like vProx settings.toml")
	}
	var s proxySettingsFile
	if err := toml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse settings TOML: %w", err)
	}
	return map[string]any{
		"rps":              s.RateLimit.RPS,
		"burst":            s.RateLimit.Burst,
		"aq_enabled":       s.AutoQuarantine.Enabled,
		"aq_threshold":     s.AutoQuarantine.Threshold,
		"aq_window_sec":    s.AutoQuarantine.WindowSec,
		"aq_penalty_rps":   s.AutoQuarantine.PenaltyRPS,
		"aq_penalty_burst": s.AutoQuarantine.PenaltyBurst,
		"aq_ttl_sec":       s.AutoQuarantine.TTLSec,
		"debug_enabled":    s.Debug.Enabled,
		"debug_port":       s.Debug.Port,
	}, nil
}

func importChainFields(data []byte) (map[string]any, error) {
	ci, err := chainconfig.ParseChainIdentity(data, "")
	if err != nil {
		return nil, fmt.Errorf("parse chain TOML: %w", err)
	}

	chainName := strings.ToLower(strings.TrimSpace(ci.EffectiveChainName()))
	if chainName == "" {
		return nil, fmt.Errorf("chain file must define chain_details.name or chain_name")
	}
	if !reSlug.MatchString(chainName) {
		return nil, fmt.Errorf("chain_name must use lowercase letters, digits, and hyphens")
	}
	chainID := strings.TrimSpace(ci.EffectiveChainID())
	if chainID == "" {
		return nil, fmt.Errorf("chain file must define chain_id")
	}

	services := chainServicesToMaps(ci.ChainServices)
	servicesJSON, err := json.Marshal(services)
	if err != nil {
		return nil, fmt.Errorf("encode chain services: %w", err)
	}

	fields := map[string]any{
		"chain_name":          chainName,
		"chain_id":            chainID,
		"dashboard_name":      strings.TrimSpace(ci.EffectiveDashboardName()),
		"explorer_base":       strings.TrimSpace(ci.EffectiveExplorerBase()),
		"explorers":           strings.Join(ci.EffectiveExplorers(), ","),
		"chain_ping_country":  strings.ToUpper(strings.TrimSpace(ci.ChainPing.Country)),
		"chain_ping_provider": strings.TrimSpace(ci.ChainPing.Provider),
		"chain_services_json": string(servicesJSON),
		"chain_services":      services,
	}
	return fields, nil
}

func importVLogFields(data []byte) (map[string]any, error) {
	rawLower := strings.ToLower(string(data))
	if !strings.Contains(rawLower, "[vlog]") {
		return nil, fmt.Errorf("file does not look like vlog.toml")
	}
	var cfg vlogcfg.Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse vlog TOML: %w", err)
	}
	v := cfg.VLog
	return map[string]any{
		"port":              v.Port,
		"bind_address":      v.BindAddress,
		"base_path":         v.BasePath,
		"api_key":           v.APIKey,
		"username":          v.Auth.Username,
		"password_hash":     v.Auth.PasswordHash,
		"auto_enrich":       v.Intel.AutoEnrich,
		"cache_ttl_hours":   v.Intel.CacheTTLHours,
		"rate_limit_rpm":    v.Intel.RateLimitRPM,
		"abuseipdb":         v.Intel.Keys.AbuseIPDB,
		"virustotal":        v.Intel.Keys.VirusTotal,
		"shodan":            v.Intel.Keys.Shodan,
		"push_user":         v.Push.Defaults.User,
		"push_key_path":     v.Push.Defaults.KeyPath,
		"poll_interval_sec": v.Push.PollIntervalSec,
		"db_path":           v.DBPath,
		"archives_dir":      v.ArchivesDir,
		"vprox_bin":         v.VProxBin,
	}, nil
}

func importFleetFields(data []byte) (map[string]any, error) {
	rawLower := strings.ToLower(string(data))
	if !strings.Contains(rawLower, "[ssh]") {
		return nil, fmt.Errorf("file does not look like fleet settings.toml")
	}
	var s fleetSettingsFile
	if err := toml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse fleet TOML: %w", err)
	}
	return map[string]any{
		"ssh_user":          s.SSH.User,
		"ssh_key_path":      s.SSH.KeyPath,
		"ssh_port":          s.SSH.Port,
		"poll_interval_sec": s.Poll.IntervalSec,
		"datacenter":        s.Defaults.Datacenter,
	}, nil
}

func importInfraFields(path string, data []byte) (map[string]any, error) {
	lowerPath := strings.ToLower(filepath.ToSlash(path))
	if strings.HasSuffix(lowerPath, "/config/push/vms.toml") {
		return importLegacyInfraFields(path)
	}

	var inf infraFile
	if err := toml.Unmarshal(data, &inf); err != nil {
		return nil, fmt.Errorf("parse infra TOML: %w", err)
	}
	vms := dedupeFleetVMs(inf.VMs)
	for i, vm := range vms {
		if strings.TrimSpace(vm.Name) == "" {
			return nil, fmt.Errorf("infra vm[%d].name is required", i+1)
		}
		if strings.TrimSpace(vm.Host) == "" {
			return nil, fmt.Errorf("infra vm[%d].host is required", i+1)
		}
		if vm.Port != 0 && (vm.Port < 1 || vm.Port > 65535) {
			return nil, fmt.Errorf("infra vm[%d].port must be between 1 and 65535", i+1)
		}
		if vm.LanIP != "" && net.ParseIP(vm.LanIP) == nil {
			return nil, fmt.Errorf("infra vm[%d].lan_ip must be a valid IP address", i+1)
		}
		if vm.PublicIP != "" && net.ParseIP(vm.PublicIP) == nil {
			return nil, fmt.Errorf("infra vm[%d].public_ip must be a valid IP address", i+1)
		}
		if vm.Ping.Country != "" && !validCountries[strings.ToUpper(vm.Ping.Country)] {
			return nil, fmt.Errorf("infra vm[%d].ping.country is unsupported: %q", i+1, vm.Ping.Country)
		}
	}

	dc := strings.TrimSpace(inf.Host.Datacenter)
	if dc == "" {
		dc = strings.TrimSuffix(filepath.Base(path), ".toml")
	}
	vmMaps := vmsToMaps(vms)
	vmJSON, err := json.Marshal(vmMaps)
	if err != nil {
		return nil, fmt.Errorf("encode infra VM payload: %w", err)
	}
	return map[string]any{
		"datacenter":         strings.ToLower(strings.TrimSpace(dc)),
		"host_name":          inf.Host.Name,
		"host_lan_ip":        inf.Host.LanIP,
		"host_public_ip":     inf.Host.PublicIP,
		"host_datacenter":    inf.Host.Datacenter,
		"host_user":          inf.Host.User,
		"host_ssh_key_path":  inf.Host.SSHKeyPath,
		"vprox_name":         inf.VProx.Name,
		"vprox_lan_ip":       inf.VProx.LanIP,
		"vprox_key":          inf.VProx.Key,
		"vprox_ssh_key_path": inf.VProx.SSHKeyPath,
		"vms_json":           string(vmJSON),
		"vms":                vmMaps,
	}, nil
}

func importLegacyInfraFields(path string) (map[string]any, error) {
	cfg, err := fleetcfg.Load(path)
	if err != nil || cfg == nil {
		if err == nil {
			err = fmt.Errorf("legacy vms.toml is empty or invalid")
		}
		return nil, fmt.Errorf("parse legacy infra TOML: %w", err)
	}
	var host fleetcfg.Host
	if len(cfg.Hosts) > 0 {
		host = cfg.Hosts[0]
	}
	vms := dedupeFleetVMs(cfg.VMs)
	dc := strings.ToLower(strings.TrimSpace(host.Datacenter))
	if dc == "" {
		for _, vm := range vms {
			if strings.TrimSpace(vm.Datacenter) != "" {
				dc = strings.ToLower(strings.TrimSpace(vm.Datacenter))
				break
			}
		}
	}
	if dc == "" {
		dc = "migration"
	}

	vmMaps := vmsToMaps(vms)
	vmJSON, err := json.Marshal(vmMaps)
	if err != nil {
		return nil, fmt.Errorf("encode legacy VM payload: %w", err)
	}
	return map[string]any{
		"datacenter":         dc,
		"host_name":          host.Name,
		"host_lan_ip":        host.LanIP,
		"host_public_ip":     host.PublicIP,
		"host_datacenter":    host.Datacenter,
		"host_user":          host.User,
		"host_ssh_key_path":  host.SSHKeyPath,
		"vprox_name":         "vProx",
		"vprox_lan_ip":       "",
		"vprox_key":          "",
		"vprox_ssh_key_path": "",
		"vms_json":           string(vmJSON),
		"vms":                vmMaps,
	}, nil
}

func importBackupFields(data []byte) (map[string]any, error) {
	rawLower := strings.ToLower(string(data))
	if !strings.Contains(rawLower, "[backup]") {
		return nil, fmt.Errorf("file does not look like backup.toml")
	}
	var cfg backup.BackupConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse backup TOML: %w", err)
	}
	b := cfg.Backup
	return map[string]any{
		"automation":         b.Automation,
		"interval_days":      b.IntervalDays,
		"max_size_mb":        b.MaxSizeMB,
		"check_interval_min": b.CheckIntervalMin,
		"destination":        b.Destination,
		"files_logs":         strings.Join(b.Files.Logs, ","),
		"files_data":         strings.Join(b.Files.Data, ","),
		"files_config":       strings.Join(b.Files.Config, ","),
	}, nil
}

// runCmd executes an external command. Used for browser launch.
func runCmd(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}
