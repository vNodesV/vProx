// Package config loads and validates the push VM registry (vms.toml).
package config

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// VMChain describes one chain deployed on a VM.
type VMChain struct {
	Name       string   `toml:"name"`
	RPCURL     string   `toml:"rpc_url"`
	RESTURL    string   `toml:"rest_url"`
	Components []string `toml:"components"` // node|validator|provider|relayer
}

// VM describes one validator VM reachable via SSH.
type VM struct {
	Name       string    `toml:"name"`
	Host       string    `toml:"host"`
	Port       int       `toml:"port"`
	User       string    `toml:"user"`
	KeyPath    string    `toml:"key_path"`
	Datacenter string    `toml:"datacenter"`
	Chains     []VMChain `toml:"chain"`
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

// AllChains returns a deduplicated list of all chain names across all VMs.
func (c *Config) AllChains() []string {
	seen := make(map[string]struct{})
	var chains []string
	for _, vm := range c.VMs {
		for _, ch := range vm.Chains {
			if _, ok := seen[ch.Name]; !ok {
				seen[ch.Name] = struct{}{}
				chains = append(chains, ch.Name)
			}
		}
	}
	return chains
}
