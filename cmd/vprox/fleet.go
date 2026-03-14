package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/vNodesV/vProx/internal/fleet/config"
	fleetssh "github.com/vNodesV/vProx/internal/fleet/ssh"
	"github.com/vNodesV/vProx/internal/fleet/state"
	vopsconfig "github.com/vNodesV/vProx/internal/vops/config"
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

// loadFleetVMsCfg loads the VM registry from the v1.4.0 config layout.
//
// Sources (lowest → highest priority, later wins on name collision):
//  1. config/vprox/nodes/*.toml  — per-node proxy + management
//  2. config/infra/*.toml        — canonical host+VM registry
//
// VMs are enriched with chain identity from config/vops/chains/*.toml.
func loadFleetVMsCfg(home string) (*config.Config, error) {
	merged, err := config.LoadRuntimeConfig(
		home,
		config.FleetDefaults{},
		filepath.Join(home, "config", "chains"),
		filepath.Join(home, "config", "infra"),
	)
	if err != nil {
		return nil, err
	}
	if len(merged.VMs) == 0 && len(merged.Hosts) == 0 {
		return nil, fmt.Errorf("no VMs registered — add entries to config/infra/*.toml or set managed_host=true in config/vprox/nodes/*.toml")
	}
	return merged, nil
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
// It reads db_path from vops.toml ([vops.push] db_path) and falls back
// to $VPROX_HOME/data/push.db when the config is absent or the field is empty.
func openFleetDB(home string) (*state.DB, error) {
	cfgPath := filepath.Join(home, "config", "vops.toml")
	cfg, err := vopsconfig.Load(cfgPath)
	dbPath := cfg.VOps.Push.DBPath
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
