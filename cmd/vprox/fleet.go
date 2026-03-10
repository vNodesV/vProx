package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	chainconfig "github.com/vNodesV/vProx/internal/config"
	"github.com/vNodesV/vProx/internal/fleet/config"
	fleetssh "github.com/vNodesV/vProx/internal/fleet/ssh"
	"github.com/vNodesV/vProx/internal/fleet/state"
	vlogconfig "github.com/vNodesV/vProx/internal/vlog/config"
)

// runFleetCmd handles: vprox fleet <sub> [flags]
//
//	fleet hosts|vms              — list registered VMs (reads config/infra/*.toml)
//	fleet chains                 — list externally-registered chains (DB)
//	fleet unregister <chain>     — remove a registered chain from the DB
//	fleet deploy ...             — run a script on a VM
//	fleet update [--host <name>] — SSH apt-get upgrade on one or all VMs
//
// VM configuration is managed in config/infra/*.toml (one file per datacenter).
// Use chain.toml [management] sections for chain-specific VM metadata.
func runFleetCmd(home string, args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "hosts", "vms":
		fleetHosts(home)
	case "chains":
		fleetChains(home)
	case "unregister":
		fleetUnregister(home, args)
	case "deploy":
		fleetDeploy(home, args)
	case "update":
		fleetUpdate(home, args)
	default:
		fmt.Fprintf(os.Stderr, "vprox fleet: unknown subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: vprox fleet <subcommand> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  hosts|vms                          list registered VMs")
		fmt.Fprintln(os.Stderr, "  chains                             list externally-registered chains")
		fmt.Fprintln(os.Stderr, "  unregister <chain>                 remove a registered chain from monitoring")
		fmt.Fprintln(os.Stderr, "  deploy --vm <n> --chain <c> --component <c> --script <s> [--dry-run]")
		fmt.Fprintln(os.Stderr, "  update [--host <name>]             apt-get upgrade on VM(s)")
		os.Exit(1)
	}
}

// loadFleetVMsCfg loads the merged VM registry from all available sources:
//  1. config/chains/*.toml   — [management] sections where managed_host=true
//  2. config/infra/*.toml    — canonical host+VM registry; one file per datacenter (highest priority)
//
// Infra files are scanned by name (qc.toml, rbx.toml, etc.) so any *.toml added to
// config/infra/ is automatically picked up. Sources are merged with later entries
// overriding earlier ones by name. Infra-sourced VMs are enriched with chain identity
// data (chain_id, valoper, explorer) from the corresponding chains/{chain_name}.toml.
func loadFleetVMsCfg(home string) (*config.Config, error) {
	merged := &config.Config{}
	chainsDir := filepath.Join(home, "config", "chains")

	// 2. Overlay chain.toml [management] sections (medium priority).
	if chainCfg, err := config.LoadFromChainConfigs(chainsDir, config.FleetDefaults{}); err == nil && len(chainCfg.VMs) > 0 {
		merged = config.MergeConfigs(merged, chainCfg)
	}

	// 3. Overlay infra/*.toml (highest priority).
	infraDir := filepath.Join(home, "config", "infra")
	if infraCfg, err := config.LoadFromInfraFiles(infraDir); err == nil && (len(infraCfg.VMs) > 0 || len(infraCfg.Hosts) > 0) {
		merged = config.MergeInfraConfig(merged, infraCfg)
	}

	// Enrich infra-sourced VMs with chain identity from chains/{chain_name}.toml.
	enrichVMsFromChains(merged.VMs, chainsDir)

	if len(merged.VMs) == 0 && len(merged.Hosts) == 0 {
		return nil, fmt.Errorf("no VMs registered — add entries to config/infra/*.toml or set managed_host=true in config/chains/*.toml")
	}
	return merged, nil
}

// enrichVMsFromChains populates missing chain identity fields (chain_id, valoper,
// dashboard_name, network_type, explorer) on VMs that carry a chain_name reference
// but lack those fields. This wires the infra→chain join for infra-sourced VMs.
func enrichVMsFromChains(vms []config.VM, chainsDir string) {
	cache := make(map[string]*chainconfig.ChainConfig)
	for i := range vms {
		if vms[i].ChainName == "" || vms[i].ChainID != "" {
			continue
		}
		cc, seen := cache[vms[i].ChainName]
		if !seen {
			data, err := os.ReadFile(filepath.Join(chainsDir, vms[i].ChainName+".toml"))
			if err == nil {
				var c chainconfig.ChainConfig
				if toml.Unmarshal(data, &c) == nil {
					cc = &c
				}
			}
			cache[vms[i].ChainName] = cc // nil on error — skip silently
		}
		if cc == nil {
			continue
		}
		vms[i].ChainID = cc.ChainID
		if vms[i].DashboardName == "" {
			vms[i].DashboardName = cc.DashboardName
		}
		if vms[i].NetworkType == "" {
			vms[i].NetworkType = cc.NetworkType
		}
		if vms[i].Explorer == "" {
			vms[i].Explorer = cc.ExplorerBase
		}
		if vms[i].Valoper == "" {
			vms[i].Valoper = cc.ChainServices.Validator.Mainnet.Address
		}
	}
}

// fleetHosts prints the VM registry as a text table.
func fleetHosts(home string) {
	cfg, err := loadFleetVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet hosts: %v\n", err)
		os.Exit(1)
	}
	if len(cfg.VMs) == 0 {
		fmt.Println("No VMs registered.")
		return
	}
	fmt.Printf("%-20s %-30s %-6s %-12s %s\n", "NAME", "HOST", "PORT", "DATACENTER", "TYPE")
	fmt.Println(strings.Repeat("─", 88))
	for _, vm := range cfg.VMs {
		fmt.Printf("%-20s %-30s %-6d %-12s %s\n",
			vm.Name, vm.Host, vm.Port, vm.Datacenter, vm.Type)
	}
}

// fleetChains lists all externally-registered chains from the fleet DB.
func fleetChains(home string) {
	db, err := openFleetDB(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet chains: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	chains, err := db.ListRegisteredChains()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet chains: query: %v\n", err)
		os.Exit(1)
	}
	if len(chains) == 0 {
		fmt.Println("No registered chains.")
		return
	}
	fmt.Printf("%-24s %-40s %-40s %s\n", "CHAIN", "RPC URL", "REST URL", "ADDED AT")
	fmt.Println(strings.Repeat("─", 120))
	for _, c := range chains {
		fmt.Printf("%-24s %-40s %-40s %s\n",
			c.Chain, c.RPCURL, c.RESTURL, c.AddedAt.Format("2006-01-02 15:04"))
	}
}

// fleetUnregister removes a registered chain from the fleet DB.
func fleetUnregister(home string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fleet unregister: chain name required")
		fmt.Fprintln(os.Stderr, "Usage: vprox fleet unregister <chain>")
		fmt.Fprintln(os.Stderr, "       vprox fleet chains   (to list registered chains)")
		os.Exit(1)
	}
	chain := args[0]

	db, err := openFleetDB(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet unregister: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.RemoveRegisteredChain(chain); err != nil {
		if errors.Is(err, state.ErrNotFound) {
			fmt.Fprintf(os.Stderr, "fleet unregister: no registered chain named %q\n", chain)
			fmt.Fprintln(os.Stderr, "  Hint: run 'vprox fleet chains' to list registered chains")
		} else {
			fmt.Fprintf(os.Stderr, "fleet unregister: %v\n", err)
		}
		os.Exit(1)
	}
	fmt.Printf("Removed %q from registered chains.\n", chain)
}

// openFleetDB opens the fleet SQLite state DB using the configured path.
// It reads db_path from vlog.toml ([vlog.push] db_path) and falls back
// to $VPROX_HOME/data/push.db when the config is absent or the field is empty.
func openFleetDB(home string) (*state.DB, error) {
	cfgPath := filepath.Join(home, "config", "vlog.toml")
	cfg, err := vlogconfig.Load(cfgPath)
	dbPath := cfg.VLog.Push.DBPath
	if err != nil || strings.TrimSpace(dbPath) == "" {
		dbPath = filepath.Join(home, "data", "push.db")
	}
	return state.Open(dbPath)
}


func fleetDeploy(home string, args []string) {
	fs := flag.NewFlagSet("fleet deploy", flag.ExitOnError)
	vmName := fs.String("vm", "", "VM name (required)")
	chain := fs.String("chain", "", "chain name (required)")
	component := fs.String("component", "", "component: node|validator|provider|relayer")
	script := fs.String("script", "", "script name: install|configure|service|...")
	dryRun := fs.Bool("dry-run", false, "pass --dry-run to script")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *vmName == "" || *chain == "" || *component == "" || *script == "" {
		fmt.Fprintln(os.Stderr, "fleet deploy: --vm, --chain, --component, and --script are required")
		os.Exit(1)
	}

	cfg, err := loadFleetVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet deploy: %v\n", err)
		os.Exit(1)
	}
	vm := cfg.FindVM(*vmName)
	if vm == nil {
		fmt.Fprintf(os.Stderr, "fleet deploy: VM %q not found\n", *vmName)
		os.Exit(1)
	}

	scriptPath := fmt.Sprintf("~/vProx/scripts/chains/%s/%s/%s.sh", *chain, *component, *script)
	dryFlag := ""
	if *dryRun {
		dryFlag = " --dry-run"
	}
	cmd := fmt.Sprintf("bash %s%s", scriptPath, dryFlag)

	fmt.Printf("→ %s@%s:%d  %s\n", vm.User, vm.Host, vm.Port, cmd)

	conn, err := fleetssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet deploy: ssh: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	out, err := conn.Run(cmd)
	fmt.Print(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet deploy: script error: %v\n", err)
		os.Exit(1)
	}
}

// fleetUpdate runs apt-get upgrade on one or all VMs.
func fleetUpdate(home string, args []string) {
	fs := flag.NewFlagSet("fleet update", flag.ExitOnError)
	name := fs.String("host", "", "target VM name (all VMs if omitted)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := loadFleetVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fleet update: %v\n", err)
		os.Exit(1)
	}

	vms := cfg.VMs
	if *name != "" {
		vm := cfg.FindVM(*name)
		if vm == nil {
			fmt.Fprintf(os.Stderr, "fleet update: VM %q not found\n", *name)
			os.Exit(1)
		}
		vms = []config.VM{*vm}
	}

	const upgradeCmd = "sudo apt-get update -qq && sudo apt-get upgrade -y"
	for _, vm := range vms {
		fmt.Printf("→ %s (%s) ... ", vm.Name, vm.Host)
		conn, err := fleetssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
		if err != nil {
			fmt.Printf("FAIL: %v\n", err)
			continue
		}
		out, err := conn.Run(upgradeCmd)
		conn.Close()
		if err != nil {
			fmt.Printf("FAIL: %v\n%s\n", err, out)
		} else {
			fmt.Println("OK")
		}
	}
}

// writeVMsCfg serializes cfg to path, creating parent dirs as needed.
// Used internally when persisting infra config edits.
func writeVMsCfg(path string, cfg *config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
