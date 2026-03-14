package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// ChainDetails contains immutable chain identity metadata used by the dashboard.
type ChainDetails struct {
	Name          string   `toml:"name"`           // chain slug (e.g. "cheqd")
	DashboardName string   `toml:"dashboard_name"` // display label
	ChainID       string   `toml:"chain_id"`       // official chain-id
	ExplorerBase  string   `toml:"explorer_base"`  // primary explorer URL
	Explorers     []string `toml:"explorers"`      // optional explorer list
}

// ChainService describes one managed service running on the chain.
type ChainService struct {
	Name       string `toml:"name"`         // service label shown in dashboard
	Moniker    string `toml:"moniker"`      // optional node moniker override
	ServiceType string `toml:"service_type"` // node | validator | relayer | sp
	Valoper    string `toml:"valoper"`      // required for validator service rows
	InternalIP string `toml:"internal_ip"`  // LAN IP used for VM linking
	Host       string `toml:"host"`         // optional hostname/domain
	LinkToVM   bool   `toml:"link_to_vm"`   // true = match InternalIP against VM registry
}

// ChainIdentity is the per-chain configuration for config/vops/chains/<chain>.toml.
//
// This file is vOps-owned and stores chain identity + managed service rows.
// Proxy routing and management settings live in config/vprox/nodes/<node>.toml.
type ChainIdentity struct {
	SchemaVersion int `toml:"schema_version"`

	// Node is the basename (no extension) of config/vprox/nodes/<node>.toml that
	// proxies this chain. Primary join key. When empty, TreeName is used as fallback.
	Node string `toml:"node"`

	// TreeName is the chain slug (e.g. "cheqd"). Used as fallback join key when Node is empty.
	// Also used in log filenames and dashboard routes.
	TreeName string `toml:"tree_name"`

	// Legacy top-level identity fields kept for backward compatibility with
	// older readers and migration imports.
	ChainID            string   `toml:"chain_id"`
	ChainName          string   `toml:"chain_name"`
	DashboardName      string   `toml:"dashboard_name"`
	NetworkType        string   `toml:"network_type"`
	RecommendedVersion string   `toml:"recommended_version"`
	ExplorerBase       string   `toml:"explorer_base"`
	Explorers          []string `toml:"explorers"`

	// Canonical v1.4+ shape.
	ChainDetails  ChainDetails   `toml:"chain_details"`
	ChainServices []ChainService `toml:"chain_services"`

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
	if c.EffectiveChainName() != "" {
		return c.EffectiveChainName()
	}
	return c.BaseName
}

// EffectiveChainName returns the canonical chain slug.
func (c *ChainIdentity) EffectiveChainName() string {
	if strings.TrimSpace(c.ChainDetails.Name) != "" {
		return strings.TrimSpace(c.ChainDetails.Name)
	}
	if strings.TrimSpace(c.ChainName) != "" {
		return strings.TrimSpace(c.ChainName)
	}
	if strings.TrimSpace(c.TreeName) != "" {
		return strings.TrimSpace(c.TreeName)
	}
	return strings.TrimSpace(c.BaseName)
}

// EffectiveChainID returns the canonical chain-id.
func (c *ChainIdentity) EffectiveChainID() string {
	if strings.TrimSpace(c.ChainDetails.ChainID) != "" {
		return strings.TrimSpace(c.ChainDetails.ChainID)
	}
	return strings.TrimSpace(c.ChainID)
}

// EffectiveDashboardName returns the canonical display label.
func (c *ChainIdentity) EffectiveDashboardName() string {
	if strings.TrimSpace(c.ChainDetails.DashboardName) != "" {
		return strings.TrimSpace(c.ChainDetails.DashboardName)
	}
	return strings.TrimSpace(c.DashboardName)
}

// EffectiveExplorerBase returns the canonical primary explorer URL.
func (c *ChainIdentity) EffectiveExplorerBase() string {
	if strings.TrimSpace(c.ChainDetails.ExplorerBase) != "" {
		return strings.TrimSpace(c.ChainDetails.ExplorerBase)
	}
	return strings.TrimSpace(c.ExplorerBase)
}

// EffectiveExplorers returns a deduplicated explorer list.
func (c *ChainIdentity) EffectiveExplorers() []string {
	if len(c.ChainDetails.Explorers) > 0 {
		return uniqueStrings(c.ChainDetails.Explorers)
	}
	return uniqueStrings(c.Explorers)
}

// PrimaryValoper returns the first validator valoper from chain services.
func (c *ChainIdentity) PrimaryValoper() string {
	for _, svc := range c.ChainServices {
		if strings.EqualFold(strings.TrimSpace(svc.ServiceType), "validator") && strings.TrimSpace(svc.Valoper) != "" {
			return strings.TrimSpace(svc.Valoper)
		}
	}
	return ""
}

func (c *ChainIdentity) normalize(base string) {
	base = strings.TrimSpace(strings.TrimSuffix(base, ".toml"))
	if c.BaseName == "" {
		c.BaseName = base
	}
	if c.TreeName == "" {
		c.TreeName = firstNonEmpty(c.TreeName, c.ChainDetails.Name, c.ChainName, base)
	}

	// Keep canonical + legacy identity fields in sync.
	if c.ChainDetails.Name == "" {
		c.ChainDetails.Name = firstNonEmpty(c.ChainName, c.TreeName, base)
	}
	if c.ChainName == "" {
		c.ChainName = c.ChainDetails.Name
	}
	if c.ChainDetails.ChainID == "" {
		c.ChainDetails.ChainID = c.ChainID
	}
	if c.ChainID == "" {
		c.ChainID = c.ChainDetails.ChainID
	}
	if c.ChainDetails.DashboardName == "" {
		c.ChainDetails.DashboardName = c.DashboardName
	}
	if c.DashboardName == "" {
		c.DashboardName = c.ChainDetails.DashboardName
	}
	if c.ChainDetails.ExplorerBase == "" {
		c.ChainDetails.ExplorerBase = c.ExplorerBase
	}
	if c.ExplorerBase == "" {
		c.ExplorerBase = c.ChainDetails.ExplorerBase
	}
	if len(c.ChainDetails.Explorers) == 0 && len(c.Explorers) > 0 {
		c.ChainDetails.Explorers = append([]string(nil), c.Explorers...)
	}
	if len(c.Explorers) == 0 && len(c.ChainDetails.Explorers) > 0 {
		c.Explorers = append([]string(nil), c.ChainDetails.Explorers...)
	}
	c.ChainDetails.Explorers = uniqueStrings(c.ChainDetails.Explorers)
	c.Explorers = uniqueStrings(c.Explorers)

	// Normalize chain service rows.
	if len(c.ChainServices) == 0 {
		c.ChainServices = make([]ChainService, 0)
		return
	}
	seen := make(map[string]struct{}, len(c.ChainServices))
	out := make([]ChainService, 0, len(c.ChainServices))
	for i, svc := range c.ChainServices {
		svc.Name = strings.TrimSpace(svc.Name)
		svc.Moniker = strings.TrimSpace(svc.Moniker)
		svc.ServiceType = strings.ToLower(strings.TrimSpace(svc.ServiceType))
		svc.Valoper = strings.TrimSpace(svc.Valoper)
		svc.InternalIP = strings.TrimSpace(svc.InternalIP)
		svc.Host = strings.TrimSpace(svc.Host)
		if svc.Name == "" {
			baseName := svc.ServiceType
			if baseName == "" {
				baseName = "service"
			}
			svc.Name = fmt.Sprintf("%s-%d", baseName, i+1)
		}
		key := strings.ToLower(svc.Name) + "|" + strings.ToLower(svc.ServiceType) + "|" + strings.ToLower(svc.InternalIP)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, svc)
	}
	c.ChainServices = out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		t := strings.TrimSpace(v)
		if t == "" {
			continue
		}
		key := strings.ToLower(t)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

type legacyChainIdentity struct {
	SchemaVersion int `toml:"schema_version"`
	Node          string `toml:"node"`
	TreeName      string `toml:"tree_name"`

	ChainID            string   `toml:"chain_id"`
	ChainName          string   `toml:"chain_name"`
	DashboardName      string   `toml:"dashboard_name"`
	NetworkType        string   `toml:"network_type"`
	RecommendedVersion string   `toml:"recommended_version"`
	ExplorerBase       string   `toml:"explorer_base"`
	Explorers          []string `toml:"explorers"`

	ChainServices ChainServicesConfig `toml:"chain_services"`
	ChainPing     ChainPingConfig     `toml:"chain_ping"`
	Management    Management          `toml:"management"`
}

func (l legacyChainIdentity) toChainIdentity(base string) ChainIdentity {
	ci := ChainIdentity{
		SchemaVersion:       l.SchemaVersion,
		Node:                l.Node,
		TreeName:            l.TreeName,
		ChainID:             l.ChainID,
		ChainName:           l.ChainName,
		DashboardName:       l.DashboardName,
		NetworkType:         l.NetworkType,
		RecommendedVersion:  l.RecommendedVersion,
		ExplorerBase:        l.ExplorerBase,
		Explorers:           append([]string(nil), l.Explorers...),
		ChainPing:           l.ChainPing,
		ChainDetails: ChainDetails{
			Name:          l.ChainName,
			DashboardName: l.DashboardName,
			ChainID:       l.ChainID,
			ExplorerBase:  l.ExplorerBase,
			Explorers:     append([]string(nil), l.Explorers...),
		},
	}

	if ci.ChainPing.Country == "" && strings.TrimSpace(l.Management.Ping.Country) != "" {
		ci.ChainPing.Country = strings.TrimSpace(l.Management.Ping.Country)
		ci.ChainPing.Provider = strings.TrimSpace(l.Management.Ping.Provider)
	}

	addService := func(svc ChainService) {
		ci.ChainServices = append(ci.ChainServices, svc)
	}
	if addr := strings.TrimSpace(l.ChainServices.Validator.Mainnet.Address); addr != "" {
		addService(ChainService{
			Name:        "validator-mainnet",
			ServiceType: "validator",
			Valoper:     addr,
			LinkToVM:    true,
		})
	}
	if addr := strings.TrimSpace(l.ChainServices.Validator.Testnet.Address); addr != "" {
		addService(ChainService{
			Name:        "validator-testnet",
			ServiceType: "validator",
			Valoper:     addr,
			LinkToVM:    true,
		})
	}
	if host := strings.TrimSpace(l.ChainServices.SP.Mainnet.Hostname); host != "" ||
		strings.TrimSpace(l.ChainServices.SP.Mainnet.LanIP) != "" {
		addService(ChainService{
			Name:       "service-mainnet",
			ServiceType: "sp",
			Host:       strings.TrimSpace(l.ChainServices.SP.Mainnet.Hostname),
			InternalIP: strings.TrimSpace(l.ChainServices.SP.Mainnet.LanIP),
			LinkToVM:   true,
		})
	}
	if host := strings.TrimSpace(l.ChainServices.SP.Testnet.Hostname); host != "" ||
		strings.TrimSpace(l.ChainServices.SP.Testnet.LanIP) != "" {
		addService(ChainService{
			Name:       "service-testnet",
			ServiceType: "sp",
			Host:       strings.TrimSpace(l.ChainServices.SP.Testnet.Hostname),
			InternalIP: strings.TrimSpace(l.ChainServices.SP.Testnet.LanIP),
			LinkToVM:   true,
		})
	}
	if strings.TrimSpace(l.Management.Valoper) != "" && ci.PrimaryValoper() == "" {
		addService(ChainService{
			Name:        "validator",
			ServiceType: "validator",
			Valoper:     strings.TrimSpace(l.Management.Valoper),
			InternalIP:  strings.TrimSpace(l.Management.LanIP),
			LinkToVM:    true,
		})
	}

	ci.normalize(base)
	return ci
}

// ParseChainIdentity decodes one vops chain profile, supporting both the new
// array-based chain_services format and the legacy nested chain_services table.
func ParseChainIdentity(data []byte, fileName string) (ChainIdentity, error) {
	base := strings.TrimSuffix(fileName, ".toml")

	var ci ChainIdentity
	if err := toml.Unmarshal(data, &ci); err == nil {
		ci.normalize(base)
		return ci, nil
	}

	var legacy legacyChainIdentity
	if err := toml.Unmarshal(data, &legacy); err != nil {
		return ChainIdentity{}, err
	}
	return legacy.toChainIdentity(base), nil
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
		ci, err := ParseChainIdentity(data, e.Name())
		if err != nil {
			log.Printf("[config] warn: parse chain identity file %s: %v", e.Name(), err)
			continue
		}
		ci.BaseName = strings.TrimSuffix(e.Name(), ".toml")
		ci.normalize(ci.BaseName)
		chains = append(chains, ci)
	}
	return chains, nil
}
