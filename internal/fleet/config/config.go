// Package config loads and validates the fleet VM registry.
// As of v1.3.0, the canonical source is config/infra/*.toml (one file per datacenter).
// Legacy vms.toml is still accepted for backward compatibility when present.
package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	chainconfig "github.com/vNodesV/vProx/internal/config"
)

// VMPing holds probe configuration for external latency checks via check-host.net.
type VMPing struct {
	Country  string `toml:"country"`  // ISO 3166-1 alpha-2 datacenter country, e.g. "CA", "FI"
	Provider string `toml:"provider"` // optional: pin to a specific node, e.g. "fi1"
}

// Host describes a physical server or hypervisor.
// VMs reference a host via their host_ref field.
// Operators running standalone VPS (no hypervisor) can omit [[host]] sections entirely
// and leave host_ref empty on their [[vm]] entries.
type Host struct {
	Name       string `toml:"name"         json:"name"`
	PublicIP   string `toml:"public_ip"    json:"public_ip,omitempty"`
	LanIP      string `toml:"lan_ip"       json:"lan_ip,omitempty"`
	Datacenter string `toml:"datacenter"   json:"datacenter,omitempty"`
	// User and SSHKeyPath set per-host SSH defaults.
	// Applied to [[vm]] entries in the same file that leave user / key_path empty.
	// Precedence: VM > [host] > [vprox].ssh_key_path
	User       string `toml:"user"         json:"user,omitempty"`
	SSHKeyPath string `toml:"ssh_key_path" json:"ssh_key_path,omitempty"`
}

// VM describes one validator VM (or VPS) reachable via SSH.
//
// Topology options:
//   - VM on a physical host: set host_ref to the [[host]].name; lan_ip is the VM's LAN address.
//   - Standalone VPS: leave host_ref empty; set lan_ip and public_ip directly on the VM.
//
// Type classifies the chain role(s): validator | sp | rpc | relayer | node (comma-separated for multi-role).
// RPCURL and RESTURL are optional; when empty they are derived from Host
// using standard Cosmos SDK ports (26657 / 1317).
type VM struct {
	Name       string `toml:"name"`
	HostRef    string `toml:"host_ref"`  // optional: links to [[host]].name for grouped display
	Host       string `toml:"host"`      // SSH target IP or hostname (used for dial + RPC/REST defaults)
	LanIP      string `toml:"lan_ip"`    // VM's LAN IP shown in Server Status block (defaults to host)
	PublicIP   string `toml:"public_ip"` // VM's public IP, shown in Server Status expanded view
	Port       int    `toml:"port"`
	User       string `toml:"user"`
	KeyPath    string `toml:"key_path"`
	// KnownHostsPath is the SSH known_hosts file for host-key verification on this VM.
	// Populated from FleetDefaults.KnownHostsPath when not set per-VM.
	KnownHostsPath string `toml:"-"` // runtime-only; sourced from push.defaults.known_hosts_path
	Datacenter string `toml:"datacenter"`
	Type       string `toml:"type"`     // validator | sp | rpc | relayer | node (comma-separated)
	RPCURL     string `toml:"rpc_url"`  // optional override
	RESTURL    string `toml:"rest_url"` // optional override

	// Chain identity and explorer (from chain.toml top-level or vms.toml)
	ChainID       string `toml:"chain_id"`       // official chain-id, e.g. "cheqd-mainnet-1"
	ChainName     string `toml:"chain_name"`     // short slug, e.g. "cheqd"
	NetworkType   string `toml:"network_type"`   // "mainnet" or "testnet"
	DashboardName string `toml:"dashboard_name"` // display name; empty = cosmos.directory pretty_name
	Explorer      string `toml:"explorer"`       // block explorer base URL, e.g. "ping.pub"
	Valoper       string `toml:"valoper"`        // validator operator address for governance participation

	// Ping config — selects check-host.net probe node for datacenter latency column.
	Ping VMPing `toml:"ping"`
}

// Config is the top-level fleet configuration.
// Loaded from config/infra/*.toml (canonical) or legacy vms.toml.
type Config struct {
	Hosts []Host `toml:"host"`
	VMs   []VM   `toml:"vm"`
}

// DisplayLanIP returns the LAN IP for display: falls back to Host when lan_ip is not set.
func (v VM) DisplayLanIP() string {
	if v.LanIP != "" {
		return v.LanIP
	}
	return v.Host
}

// RPC returns the effective RPC URL, deriving it from Host when not set.
func (v VM) RPC() string {
	if v.RPCURL != "" {
		return v.RPCURL
	}
	return "http://" + v.Host + ":26657"
}

// REST returns the effective REST URL, deriving it from Host when not set.
func (v VM) REST() string {
	if v.RESTURL != "" {
		return v.RESTURL
	}
	return "http://" + v.Host + ":1317"
}

// FindHost returns the Host with the given name, or nil.
func (c *Config) FindHost(name string) *Host {
	for i := range c.Hosts {
		if c.Hosts[i].Name == name {
			return &c.Hosts[i]
		}
	}
	return nil
}

// Load reads and parses path, applying defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("fleet/config: read %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("fleet/config: parse %s: %w", path, err)
	}

	for i := range cfg.VMs {
		if cfg.VMs[i].Port == 0 {
			cfg.VMs[i].Port = 22
		}
	}

	return &cfg, nil
}

// FindVM returns the VM with the given name, or nil if not found.
func (c *Config) FindVM(name string) *VM {
	for i := range c.VMs {
		if c.VMs[i].Name == name {
			return &c.VMs[i]
		}
	}
	return nil
}

// FindVMForChain returns the VM that manages the given chain slug.
// Checks exact vm.Name match, exact vm.ChainName match, and base-slug match
// (e.g. "cheqd-testnet" → base "cheqd" matches a VM with ChainName "cheqd").
func (c *Config) FindVMForChain(chainSlug string) *VM {
	base := chainBaseSlug(chainSlug)
	for i := range c.VMs {
		if c.VMs[i].Name == chainSlug || c.VMs[i].ChainName == chainSlug {
			return &c.VMs[i]
		}
		if base != "" && (chainBaseSlug(c.VMs[i].ChainName) == base || chainBaseSlug(c.VMs[i].Name) == base) {
			return &c.VMs[i]
		}
	}
	return nil
}

// chainBaseSlug extracts the leading word from a chain slug (letters + digits
// before the first hyphen or underscore). Used for fuzzy chain matching.
// e.g. "cheqd-testnet-6" → "cheqd", "osmosis-1" → "osmosis"
func chainBaseSlug(s string) string {
	for i, r := range s {
		if i > 0 && (r == '-' || r == '_') {
			return strings.ToLower(s[:i])
		}
	}
	return strings.ToLower(s)
}

// AllChains returns a deduplicated list of all chain names (one per VM).
func (c *Config) AllChains() []string {
	seen := make(map[string]struct{})
	var chains []string
	for _, vm := range c.VMs {
		if _, ok := seen[vm.Name]; !ok {
			seen[vm.Name] = struct{}{}
			chains = append(chains, vm.Name)
		}
	}
	return chains
}

// FleetDefaults holds global SSH credential defaults for chain-managed hosts.
// Applied when [management] user or key_path are empty in chain.toml.
// Sourced from [vops.push.defaults] in vops.toml.
type FleetDefaults struct {
	User           string
	KeyPath        string
	KnownHostsPath string
}

// LoadFromNodeConfigs reads all node TOML files from dir (config/vprox/nodes/) and
// extracts entries with managed_host = true as VM entries.
// This is the v1.4.0 equivalent of LoadFromChainConfigs for the new config layout.
// Returns an empty Config (not error) when dir does not exist.
func LoadFromNodeConfigs(dir string, defaults FleetDefaults) (*Config, error) {
	nodes, err := chainconfig.LoadNodes(dir)
	if err != nil || len(nodes) == 0 {
		return &Config{}, nil
	}

	var vms []VM
	for _, nc := range nodes {
		if !nc.Management.ManagedHost {
			continue
		}
		m := nc.Management

		sshHost := m.LanIP
		if sshHost == "" {
			sshHost = nc.IP
		}
		user := m.User
		if user == "" {
			user = defaults.User
		}
		keyPath := m.KeyPath
		if keyPath == "" {
			keyPath = defaults.KeyPath
		}
		port := m.Port
		if port == 0 {
			port = 22
		}

		rpcURL, restURL := "", ""
		if m.ExposedServices && nc.Host != "" {
			base := "http://" + nc.Host
			switch {
			case nc.Expose.Path:
				rpcURL = base + "/rpc"
				restURL = base + "/rest"
			case nc.Expose.VHost:
				rp := nc.Expose.VHostPrefix.RPC
				ap := nc.Expose.VHostPrefix.REST
				if rp == "" {
					rp = "rpc"
				}
				if ap == "" {
					ap = "api"
				}
				rpcURL = "http://" + rp + "." + nc.Host
				restURL = "http://" + ap + "." + nc.Host
			default:
				rpcURL = base
				restURL = base
			}
		}

		vmName := nc.BaseName
		if vmName == "" {
			vmName = chainVMName("", nc.Host)
		}

		vms = append(vms, VM{
			Name:       vmName,
			Host:       sshHost,
			LanIP:      sshHost,
			PublicIP:   m.PublicIP,
			Port:       port,
			User:       user,
			KeyPath:    keyPath,
			Datacenter: m.Datacenter,
			Type:       strings.Join(m.Type, ","),
			RPCURL:     rpcURL,
			RESTURL:    restURL,
			// Chain identity enriched later via enrichVMsFromVOpsChains or enrichVMsFromChains.
			Valoper: m.Valoper,
			Ping: VMPing{
				Country:  m.Ping.Country,
				Provider: m.Ping.Provider,
			},
		})
	}
	return &Config{VMs: vms}, nil
}

// LoadFromChainConfigs reads all chain TOML files from dir and extracts
// [management] sections with managed_host = true as VM entries.
// Per-chain management config takes precedence over vms.toml when merged.
// Returns a Config with only chain-derived VMs (no Hosts).
func LoadFromChainConfigs(dir string, defaults FleetDefaults) (*Config, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("fleet/config: read chains dir %s: %w", dir, err)
	}

	var vms []VM
	for _, e := range entries {
		if e.IsDir() || !chainconfig.IsChainTOML(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("[fleet] warn: skip chain file %s: %v", e.Name(), err)
			continue
		}
		var cc chainconfig.ChainConfig
		if err := toml.Unmarshal(data, &cc); err != nil {
			log.Printf("[fleet] warn: parse chain file %s: %v", e.Name(), err)
			continue
		}
		if !cc.Management.ManagedHost {
			continue
		}
		m := cc.Management

		// Derive SSH target: prefer [management].lan_ip, fall back to chain.ip
		sshHost := m.LanIP
		if sshHost == "" {
			sshHost = cc.IP
		}

		// Apply global defaults for empty credentials
		user := m.User
		if user == "" {
			user = defaults.User
		}
		keyPath := m.KeyPath
		if keyPath == "" {
			keyPath = defaults.KeyPath
		}

		port := m.Port
		if port == 0 {
			port = 22
		}

		// exposed_services routing: when true, probe URLs use the public chain.host domain
		// (requests route through vProx/Apache). When false, probe directly via LAN IP —
		// preferred when vOps is co-located on the same network as the node.
		// This is independent of managed_host (SSH management); both can be set freely.
		rpcURL := ""
		restURL := ""
		if m.ExposedServices && cc.Host != "" {
			base := "http://" + cc.Host
			switch {
			case cc.Expose.Path:
				// vProx path routing: /rpc → RPC port, /rest → REST port
				rpcURL = base + "/rpc"
				restURL = base + "/rest"
			case cc.Expose.VHost:
				// vProx vhost routing: rpc.<host> and api.<host>
				rp := cc.Expose.VHostPrefix.RPC
				ap := cc.Expose.VHostPrefix.REST
				if rp == "" {
					rp = "rpc"
				}
				if ap == "" {
					ap = "api"
				}
				rpcURL = "http://" + rp + "." + cc.Host
				restURL = "http://" + ap + "." + cc.Host
			default:
				// No sub-routing — probe base host directly (non-standard setup)
				rpcURL = base
				restURL = base
			}
		}

		vms = append(vms, VM{
			Name:          chainVMName(cc.ChainName, cc.Host),
			Host:          sshHost,
			LanIP:         sshHost,
			PublicIP:      m.PublicIP,
			Port:          port,
			User:          user,
			KeyPath:       keyPath,
			Datacenter:    m.Datacenter,
			Type:          strings.Join(m.Type, ","),
			RPCURL:        rpcURL,
			RESTURL:       restURL,
			ChainID:       cc.ChainID,
			ChainName:     cc.ChainName,
			NetworkType:   cc.NetworkType,
			DashboardName: cc.DashboardName,
			Explorer:      cc.ExplorerBase,
			Valoper:       m.Valoper,
			Ping: VMPing{
				Country:  m.Ping.Country,
				Provider: m.Ping.Provider,
			},
		})
	}

	return &Config{VMs: vms}, nil
}

// MergeConfigs merges chain-derived VMs into the base vms.toml config.
// Chain-derived entries with the same name override vms.toml entries.
// Hosts from vms.toml are preserved; chain configs add no host records.
func MergeConfigs(base, chains *Config) *Config {
	if base == nil || len(base.VMs) == 0 {
		if chains == nil {
			return &Config{}
		}
		return chains
	}
	if chains == nil || len(chains.VMs) == 0 {
		return base
	}

	// Index chain VMs by name for O(1) lookup.
	chainByName := make(map[string]VM, len(chains.VMs))
	for _, v := range chains.VMs {
		chainByName[v.Name] = v
	}

	// Build merged list: chain entry wins when names match.
	merged := make([]VM, 0, len(base.VMs)+len(chains.VMs))
	seen := make(map[string]bool, len(base.VMs))
	for _, v := range base.VMs {
		if cv, ok := chainByName[v.Name]; ok {
			merged = append(merged, cv) // chain-derived takes precedence
		} else {
			merged = append(merged, v)
		}
		seen[v.Name] = true
	}
	// Append chain VMs not present in vms.toml.
	for _, v := range chains.VMs {
		if !seen[v.Name] {
			merged = append(merged, v)
		}
	}

	return &Config{
		Hosts: base.Hosts, // preserve physical host records from vms.toml
		VMs:   merged,
	}
}

// chainVMName derives a stable VM name from chain metadata.
// Prefers chain_name (lowercase, spaces→dashes), falls back to host.
func chainVMName(chainName, host string) string {
	n := strings.ToLower(strings.TrimSpace(chainName))
	n = strings.ReplaceAll(n, " ", "-")
	if n != "" {
		return n
	}
	return host
}

// infraVproxSchema holds the [vprox] section from a per-datacenter infra file.
// ssh_key_path is applied as the default key for all [[vm]] entries in the same file
// that do not set their own key_path.
type infraVproxSchema struct {
	SSHKeyPath string `toml:"ssh_key_path"`
}

// infraFileSchema is the TOML schema for per-datacenter host files (config/infra/*.toml).
// Each file defines one physical host ([host]), optional vProx agent credentials ([vprox]),
// and the VMs on that host ([[vm]]).
type infraFileSchema struct {
	Host  Host             `toml:"host"`
	Vprox infraVproxSchema `toml:"vprox"`
	VMs   []VM             `toml:"vm"`
}

// LoadFromInfraFiles reads all *.toml files in dir, each representing one physical
// host ([host] section) with its child VMs ([[vm]] sections).
// VMs without an explicit host_ref inherit it from their file's [host].name.
// VMs without an explicit key_path inherit it from [vprox].ssh_key_path.
// Returns a merged Config containing all hosts and VMs from all infra files.
func LoadFromInfraFiles(dir string) (*Config, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("fleet/config: read infra dir %s: %w", dir, err)
	}

	var hosts []Host
	var vms []VM
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("[fleet] warn: skip infra file %s: %v", e.Name(), err)
			continue
		}
		var f infraFileSchema
		if err := toml.Unmarshal(data, &f); err != nil {
			log.Printf("[fleet] warn: parse infra file %s: %v", e.Name(), err)
			continue
		}
		if f.Host.Name != "" {
			hosts = append(hosts, f.Host)
		}
		for i := range f.VMs {
			// Auto-populate host_ref from the file's [host].name when not set.
			if f.VMs[i].HostRef == "" && f.Host.Name != "" {
				f.VMs[i].HostRef = f.Host.Name
			}
			if f.VMs[i].Port == 0 {
				f.VMs[i].Port = 22
			}
			// Credential precedence: VM > [host] > [vprox].ssh_key_path
			if f.VMs[i].User == "" && f.Host.User != "" {
				f.VMs[i].User = f.Host.User
			}
			if f.VMs[i].KeyPath == "" && f.Host.SSHKeyPath != "" {
				f.VMs[i].KeyPath = f.Host.SSHKeyPath
			}
			// Apply [vprox].ssh_key_path as final fallback when the VM still has no key.
			if f.VMs[i].KeyPath == "" && f.Vprox.SSHKeyPath != "" {
				f.VMs[i].KeyPath = f.Vprox.SSHKeyPath
			}
			vms = append(vms, f.VMs[i])
		}
	}

	return &Config{Hosts: hosts, VMs: vms}, nil
}

// MergeInfraConfig merges infra-derived hosts and VMs into the base config.
// Infra VMs take precedence over matching base entries by name.
// New hosts from infra are appended if not already present by name.
func MergeInfraConfig(base, infra *Config) *Config {
	if base == nil {
		if infra == nil {
			return &Config{}
		}
		return infra
	}
	if infra == nil || (len(infra.Hosts) == 0 && len(infra.VMs) == 0) {
		return base
	}

	// Merge hosts: append infra hosts not already in base.
	hostSeen := make(map[string]struct{}, len(base.Hosts))
	for _, h := range base.Hosts {
		hostSeen[h.Name] = struct{}{}
	}
	hosts := append([]Host{}, base.Hosts...)
	for _, h := range infra.Hosts {
		if _, ok := hostSeen[h.Name]; !ok {
			hosts = append(hosts, h)
		}
	}

	// Merge VMs: infra entry wins when names match.
	infraByName := make(map[string]VM, len(infra.VMs))
	for _, v := range infra.VMs {
		infraByName[v.Name] = v
	}
	vms := make([]VM, 0, len(base.VMs)+len(infra.VMs))
	seen := make(map[string]bool, len(base.VMs))
	for _, v := range base.VMs {
		if iv, ok := infraByName[v.Name]; ok {
			vms = append(vms, iv)
		} else {
			vms = append(vms, v)
		}
		seen[v.Name] = true
	}
	for _, v := range infra.VMs {
		if !seen[v.Name] {
			vms = append(vms, v)
		}
	}

	return &Config{Hosts: hosts, VMs: vms}
}
