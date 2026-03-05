// Package config loads and validates the push VM registry (vms.toml).
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// VMPing holds probe configuration for external latency checks via check-host.net.
type VMPing struct {
	Country  string `toml:"country"`  // ISO 3166-1 alpha-2 datacenter country, e.g. "CA", "FI"
	Provider string `toml:"provider"` // optional: pin to a specific node, e.g. "fi1"
}

// VM describes one validator VM reachable via SSH.
// In the standard 1:1 layout, vm.Name is the chain name.
// Type classifies the chain role: validator | sp | relayer.
// RPCURL and RESTURL are optional; when empty they are derived from Host
// using standard Cosmos SDK ports (26657 / 1317).
type VM struct {
	Name       string `toml:"name"`
	Host       string `toml:"host"`
	Port       int    `toml:"port"`
	User       string `toml:"user"`
	KeyPath    string `toml:"key_path"`
	Datacenter string `toml:"datacenter"`
	Type       string `toml:"type"`     // validator | sp | relayer
	RPCURL     string `toml:"rpc_url"`  // optional override
	RESTURL    string `toml:"rest_url"` // optional override

	// Block explorer config — used by vLog dashboard for governance links.
	ExplorerBase string `toml:"explorer"`  // e.g. "ping.pub"
	ChainID      string `toml:"chain_id"`  // official chain-id, e.g. "cheqd-mainnet-1"

	// Ping config — selects check-host.net probe node for datacenter latency column.
	Ping VMPing `toml:"ping"`
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

// ExplorerChainURL returns the base URL for this chain on the block explorer,
// e.g. "https://ping.pub/cheqd". The chain slug is derived by trimming the
// chain_id from the first "-" onwards (cheqd-mainnet-1 → cheqd).
// Returns "" if chain_id is not configured.
func (v VM) ExplorerChainURL() string {
	if v.ChainID == "" {
		return ""
	}
	base := v.ExplorerBase
	if base == "" {
		base = "ping.pub"
	}
	slug := strings.SplitN(v.ChainID, "-", 2)[0]
	return "https://" + base + "/" + slug
}

// Config is the top-level push configuration parsed from vms.toml.
type Config struct {
	VMs []VM `toml:"vm"`
}

// Load reads and parses path, applying defaults.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("push/config: read %s: %w", path, err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("push/config: parse %s: %w", path, err)
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
