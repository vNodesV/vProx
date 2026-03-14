package config

import (
	"fmt"
	"path/filepath"
	"strings"

	chainconfig "github.com/vNodesV/vProx/internal/config"
)

// LoadRuntimeConfig builds the fleet runtime config from the current layout:
//   - config/vprox/nodes/*.toml (managed nodes)
//   - config/infra/*.toml (host + VM registry)
//
// If no VM is discovered in the new layout, it falls back to the legacy
// config/chains/*.toml management model for backward compatibility.
//
// Chain identity is enriched from config/vops/chains/*.toml.
func LoadRuntimeConfig(home string, defaults FleetDefaults, legacyChainsDir, infraDir string) (*Config, error) {
	if strings.TrimSpace(home) == "" {
		return nil, fmt.Errorf("home is required")
	}
	if strings.TrimSpace(legacyChainsDir) == "" {
		legacyChainsDir = filepath.Join(home, "config", "chains")
	}
	if strings.TrimSpace(infraDir) == "" {
		infraDir = filepath.Join(home, "config", "infra")
	}

	nodesDir := filepath.Join(home, "config", "vprox", "nodes")
	vopsChainsDir := filepath.Join(home, "config", "vops", "chains")

	merged := &Config{}

	nodeCfg, err := LoadFromNodeConfigs(nodesDir, defaults)
	if err != nil {
		return nil, fmt.Errorf("load node configs: %w", err)
	}
	if len(nodeCfg.VMs) > 0 {
		merged = MergeConfigs(merged, nodeCfg)
	}

	infraCfg, err := LoadFromInfraFiles(infraDir)
	if err != nil {
		return nil, fmt.Errorf("load infra configs: %w", err)
	}
	if len(infraCfg.Hosts) > 0 || len(infraCfg.VMs) > 0 {
		merged = MergeInfraConfig(merged, infraCfg)
	}

	// Backward compatibility: use legacy chain [management] only when new-layout
	// sources did not produce any VM entry.
	if len(merged.VMs) == 0 {
		legacyCfg, err := LoadFromChainConfigs(legacyChainsDir, defaults)
		if err != nil {
			return nil, fmt.Errorf("load legacy chain configs: %w", err)
		}
		if len(legacyCfg.VMs) > 0 {
			merged = MergeConfigs(merged, legacyCfg)
		}
	}

	EnrichVMsFromVOpsChains(merged.VMs, vopsChainsDir)
	return merged, nil
}

// EnrichVMsFromVOpsChains fills missing chain identity metadata on VM entries
// using config/vops/chains/*.toml profiles.
func EnrichVMsFromVOpsChains(vms []VM, vopsChainsDir string) {
	chains, err := chainconfig.LoadChains(vopsChainsDir)
	if err != nil || len(chains) == 0 {
		return
	}

	byBase := make(map[string]*chainconfig.ChainIdentity, len(chains))
	byTree := make(map[string]*chainconfig.ChainIdentity, len(chains))
	for i := range chains {
		ci := &chains[i]
		byBase[strings.ToLower(strings.TrimSpace(ci.BaseName))] = ci
		if t := strings.ToLower(strings.TrimSpace(ci.TreeName)); t != "" {
			byTree[t] = ci
		}
		if t := strings.ToLower(strings.TrimSpace(ci.EffectiveChainName())); t != "" {
			byTree[t] = ci
		}
	}

	for i := range vms {
		vmName := strings.ToLower(strings.TrimSpace(vms[i].Name))
		vmChain := strings.ToLower(strings.TrimSpace(vms[i].ChainName))

		var ci *chainconfig.ChainIdentity
		if v, ok := byBase[vmName]; ok {
			ci = v
		} else if v, ok := byTree[vmChain]; ok {
			ci = v
		} else if v, ok := byTree[vmName]; ok {
			ci = v
		}
		if ci == nil {
			continue
		}

		if vms[i].ChainID == "" {
			vms[i].ChainID = ci.EffectiveChainID()
		}
		if vms[i].ChainName == "" {
			vms[i].ChainName = ci.EffectiveChainName()
		}
		if vms[i].DashboardName == "" {
			vms[i].DashboardName = ci.EffectiveDashboardName()
		}
		if vms[i].Explorer == "" {
			vms[i].Explorer = ci.EffectiveExplorerBase()
		}
		if vms[i].Valoper == "" {
			vms[i].Valoper = ci.PrimaryValoper()
		}
		if vms[i].Ping.Country == "" {
			vms[i].Ping.Country = strings.ToUpper(strings.TrimSpace(ci.ChainPing.Country))
			vms[i].Ping.Provider = strings.TrimSpace(ci.ChainPing.Provider)
		}

		if strings.TrimSpace(vms[i].Type) == "" {
			vmIP := strings.TrimSpace(vms[i].DisplayLanIP())
			for _, svc := range ci.ChainServices {
				if strings.TrimSpace(svc.InternalIP) == "" || vmIP == "" {
					continue
				}
				if strings.EqualFold(strings.TrimSpace(svc.InternalIP), vmIP) {
					vms[i].Type = strings.TrimSpace(svc.ServiceType)
					break
				}
			}
		}
	}
}
