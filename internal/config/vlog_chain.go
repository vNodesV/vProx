package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ChainIdentity is the per-chain configuration for config/vlog/chains/<chain>.toml.
//
// It holds chain identity, validator tracking, and dashboard probe settings.
// Proxy settings (host, ports, services, management) live in a separate
// config/vprox/nodes/<node>.toml, linked via the Node field (or TreeName fallback).
//
// v1.4.0 layout: replaces the identity sections of the old merged config/chains/<chain>.toml.
// vProx never reads this file — it is vLog-only.
type ChainIdentity struct {
	SchemaVersion int `toml:"schema_version"`

	// Node is the basename (no extension) of config/vprox/nodes/<node>.toml that
	// proxies this chain. Primary join key. When empty, TreeName is used as fallback.
	Node string `toml:"node"`

	// TreeName is the chain slug (e.g. "cheqd"). Used as fallback join key when Node is empty.
	// Also used in log filenames and dashboard routes.
	TreeName string `toml:"tree_name"`

	// Chain identity — cosmos.directory fields.
	ChainID            string   `toml:"chain_id"`            // official chain-id, e.g. "cheqd-mainnet-1"
	ChainName          string   `toml:"chain_name"`          // short slug, e.g. "cheqd"
	DashboardName      string   `toml:"dashboard_name"`      // display name; empty = cosmos.directory pretty_name
	NetworkType        string   `toml:"network_type"`        // "mainnet" or "testnet"
	RecommendedVersion string   `toml:"recommended_version"` // e.g. "v4.2.1"
	ExplorerBase       string   `toml:"explorer_base"`       // block explorer base URL
	Explorers          []string `toml:"explorers"`           // override cosmos.directory explorer list

	// Chain services — validator + SP + relayer (drives per-chain dashboard rows).
	ChainServices ChainServicesConfig `toml:"chain_services"`

	// ChainPing — datacenter probe settings for the dashboard DC column.
	// Falls back to [management.ping] in the linked NodeConfig when empty.
	ChainPing ChainPingConfig `toml:"chain_ping"`

	// BaseName is populated by LoadChains from the file name; not read from TOML.
	BaseName string `toml:"-"`
}

// EffectiveNode returns the node basename used to join to a NodeConfig.
// Prefers the explicit Node field; falls back to TreeName; then BaseName.
func (c *ChainIdentity) EffectiveNode() string {
	if c.Node != "" {
		return c.Node
	}
	if c.TreeName != "" {
		return c.TreeName
	}
	return c.BaseName
}

// LoadChains scans dir for *.toml files and returns all ChainIdentity entries.
// The BaseName field is populated from the filename (without extension).
// Files matching the IsChainTOML exclusion list and *.sample files are skipped.
// Returns nil slice (not error) when dir does not exist.
func LoadChains(dir string) ([]ChainIdentity, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config/vlog_chain: read dir %s: %w", dir, err)
	}

	var chains []ChainIdentity
	for _, e := range entries {
		if e.IsDir() || !IsChainTOML(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("[config] warn: skip chain identity file %s: %v", e.Name(), err)
			continue
		}
		var ci ChainIdentity
		if err := toml.Unmarshal(data, &ci); err != nil {
			log.Printf("[config] warn: parse chain identity file %s: %v", e.Name(), err)
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".toml")
		ci.BaseName = base
		if ci.TreeName == "" {
			ci.TreeName = base
		}
		chains = append(chains, ci)
	}
	return chains, nil
}
