package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// ChainSample is a read-only chain descriptor loaded from config/chains/*.sample files.
//
// Sample files contain identity-only fields (tree_name, chain_id, dashboard_name,
// network_type, recommended_version, explorers) and are committed to the repository
// as operator templates. Runtime proxy and management config lives in the separate
// config/services/nodes/*.toml files (see ServiceNode).
//
// v1.4.0 P1 loader. Join key: ChainSample.TreeName == ServiceNode.Tree.
type ChainSample struct {
	TreeName           string   `toml:"tree_name"`
	ChainID            string   `toml:"chain_id"`
	DashboardName      string   `toml:"dashboard_name"`
	NetworkType        string   `toml:"network_type"`
	RecommendedVersion string   `toml:"recommended_version"`
	Explorers          []string `toml:"explorers"`

	// SourceFile is populated by LoadChainIdentities from the file path; not read from TOML.
	SourceFile string `toml:"-"`
}

// LoadChainIdentities scans $home/config/chains/*.sample and returns parsed ChainSample entries.
//
// Only files with the .sample extension are loaded; .toml files (legacy chain configs) and
// all other extensions are intentionally ignored. Returns a nil slice (no error) when the
// directory does not exist yet — this is normal during initial setup.
//
// Individual file parse errors are printed to stderr and skipped rather than failing the
// entire load, so a corrupt sample does not prevent valid identities from loading.
func LoadChainIdentities(home string) ([]ChainSample, error) {
	dir := filepath.Join(home, "config", "chains")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty is fine; directory may not exist yet
		}
		return nil, fmt.Errorf("LoadChainIdentities: read dir %s: %w", dir, err)
	}

	var out []ChainSample
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sample" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vprox: LoadChainIdentities: skip %s: %v\n", path, err)
			continue
		}
		var s ChainSample
		if err := toml.Unmarshal(raw, &s); err != nil {
			fmt.Fprintf(os.Stderr, "vprox: LoadChainIdentities: skip %s: %v\n", path, err)
			continue
		}
		s.SourceFile = path
		out = append(out, s)
	}
	return out, nil
}

// JoinChainTree returns all ServiceNodes whose Tree field matches identity.TreeName.
// The result slice is nil (not empty) when no nodes match — callers may compare to nil
// to detect "no nodes configured yet" vs "nodes loaded but empty".
func JoinChainTree(identity ChainSample, nodes []ServiceNode) []ServiceNode {
	var result []ServiceNode
	for _, n := range nodes {
		if n.Tree == identity.TreeName {
			result = append(result, n)
		}
	}
	return result
}
