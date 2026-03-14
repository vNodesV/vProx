package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// NodeConfig is the per-node configuration for config/vprox/nodes/<node>.toml.
//
// It contains proxy routing, service exposure, and optional fleet management settings.
// Chain identity (chain_id, dashboard_name, validator info, etc.) lives in a separate
// config/vops/chains/<chain>.toml and is joined via ChainIdentity.Node (basename match)
// or ChainIdentity.TreeName fallback.
//
// v1.4.0 layout: replaces the proxy sections of the old merged config/chains/<chain>.toml.
type NodeConfig struct {
	SchemaVersion int `toml:"schema_version"`

	// TreeName is the join key linking this node to a ChainIdentity entry.
	// When empty, the TOML file basename (without extension) is used as the tree name.
	TreeName string `toml:"tree_name"`

	// Proxy routing target.
	Host string `toml:"host"` // public service hostname vProx serves
	IP   string `toml:"ip"`   // LAN IP of the upstream node (internal routing)

	DefaultPorts bool `toml:"default_ports"`
	MsgRPC       bool `toml:"msg_rpc"` // inject rpc_msg banner on RPC index
	MsgAPI       bool `toml:"msg_api"` // inject api_msg banner on REST/swagger

	RPCAliases  []string `toml:"rpc_aliases"`  // extra RPC hostnames (expose.vhost must be true)
	RESTAliases []string `toml:"rest_aliases"` // extra REST/API hostnames
	APIAliases  []string `toml:"api_aliases"`  // extra /api hostnames

	Expose   Expose     `toml:"expose"`
	Services Services   `toml:"services"`
	Ports    Ports      `toml:"ports"`
	WS       WSConfig   `toml:"ws"`
	Features Features   `toml:"features"`
	Logging  LoggingCfg `toml:"logging"`
	Message  Message    `toml:"message"`

	// Management holds SSH + fleet module settings for this node.
	// Populated only when managed_host = true.
	Management Management `toml:"management"`

	// BaseName is populated by LoadNodes from the file name; not read from TOML.
	BaseName string `toml:"-"`
}

// EffectiveTreeName returns the tree name used for joining to a ChainIdentity.
// Prefers the explicit TreeName field; falls back to BaseName.
func (n *NodeConfig) EffectiveTreeName() string {
	if n.TreeName != "" {
		return n.TreeName
	}
	return n.BaseName
}

// LoadNodes scans dir for *.toml files and returns all NodeConfig entries.
// The BaseName field is populated from the filename (without extension).
// Files matching IsChainTOML exclusion list (settings.toml, services.toml, etc.)
// and *.sample files are automatically skipped.
// Returns nil slice (not error) when dir does not exist.
func LoadNodes(dir string) ([]NodeConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config/vprox_node: read dir %s: %w", dir, err)
	}

	var nodes []NodeConfig
	for _, e := range entries {
		if e.IsDir() || !IsChainTOML(e.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Printf("[config] warn: skip node file %s: %v", e.Name(), err)
			continue
		}
		var nc NodeConfig
		if err := toml.Unmarshal(data, &nc); err != nil {
			log.Printf("[config] warn: parse node file %s: %v", e.Name(), err)
			continue
		}
		base := strings.TrimSuffix(e.Name(), ".toml")
		nc.BaseName = base
		if nc.TreeName == "" {
			nc.TreeName = base
		}
		nodes = append(nodes, nc)
	}
	return nodes, nil
}
