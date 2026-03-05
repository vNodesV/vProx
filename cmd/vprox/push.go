package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/vNodesV/vProx/internal/push/config"
	pushssh "github.com/vNodesV/vProx/internal/push/ssh"
)

// runPushCmd handles: vprox push <sub> [flags]
//
//	push hosts|vms              — list registered VMs
//	push deploy ...             — run a script on a VM
//	push add    --host ... --chain ...  — add VM to vms.toml
//	push remove --host <name>   — remove VM from vms.toml
//	push update [--host <name>] — SSH apt-get upgrade on one or all VMs
func runPushCmd(home string, args []string) {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "hosts", "vms":
		pushHosts(home)
	case "deploy":
		pushDeploy(home, args)
	case "add":
		pushAdd(home, args)
	case "remove":
		pushRemove(home, args)
	case "update":
		pushUpdate(home, args)
	default:
		fmt.Fprintf(os.Stderr, "vprox push: unknown subcommand %q\n\n", sub)
		fmt.Fprintln(os.Stderr, "Usage: vprox push <subcommand> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  hosts|vms                          list registered VMs")
		fmt.Fprintln(os.Stderr, "  deploy --vm <n> --chain <c> --component <c> --script <s> [--dry-run]")
		fmt.Fprintln(os.Stderr, "  add    --host <h> --user <u> --key <p> --chain <c> [--port 22] [--dc <d>]")
		fmt.Fprintln(os.Stderr, "  remove --host <name>               remove VM from registry")
		fmt.Fprintln(os.Stderr, "  update [--host <name>]             apt-get upgrade on VM(s)")
		os.Exit(1)
	}
}

func vmsCfgPath(home string) string {
	return filepath.Join(home, "config", "push", "vms.toml")
}

func loadVMsCfg(home string) (*config.Config, error) {
	return config.Load(vmsCfgPath(home))
}

// pushHosts prints the VM registry as a text table.
func pushHosts(home string) {
	cfg, err := loadVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "push hosts: %v\n", err)
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

// pushDeploy runs a script on a VM.
func pushDeploy(home string, args []string) {
	fs := flag.NewFlagSet("push deploy", flag.ExitOnError)
	vmName := fs.String("vm", "", "VM name (required)")
	chain := fs.String("chain", "", "chain name (required)")
	component := fs.String("component", "", "component: node|validator|provider|relayer")
	script := fs.String("script", "", "script name: install|configure|service|...")
	dryRun := fs.Bool("dry-run", false, "pass --dry-run to script")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *vmName == "" || *chain == "" || *component == "" || *script == "" {
		fmt.Fprintln(os.Stderr, "push deploy: --vm, --chain, --component, and --script are required")
		os.Exit(1)
	}

	cfg, err := loadVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "push deploy: %v\n", err)
		os.Exit(1)
	}
	vm := cfg.FindVM(*vmName)
	if vm == nil {
		fmt.Fprintf(os.Stderr, "push deploy: VM %q not found\n", *vmName)
		os.Exit(1)
	}

	scriptPath := fmt.Sprintf("~/vProx/scripts/chains/%s/%s/%s.sh", *chain, *component, *script)
	dryFlag := ""
	if *dryRun {
		dryFlag = " --dry-run"
	}
	cmd := fmt.Sprintf("bash %s%s", scriptPath, dryFlag)

	fmt.Printf("→ %s@%s:%d  %s\n", vm.User, vm.Host, vm.Port, cmd)

	conn, err := pushssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "push deploy: ssh: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	out, err := conn.Run(cmd)
	fmt.Print(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "push deploy: script error: %v\n", err)
		os.Exit(1)
	}
}

// pushAdd appends a new VM entry to vms.toml.
func pushAdd(home string, args []string) {
	fs := flag.NewFlagSet("push add", flag.ExitOnError)
	name := fs.String("name", "", "chain/VM name (defaults to host)")
	host := fs.String("host", "", "hostname or IP (required)")
	port := fs.Int("port", 22, "SSH port")
	user := fs.String("user", "", "SSH user (required)")
	key := fs.String("key", "", "path to SSH private key (required)")
	dc := fs.String("dc", "", "datacenter label")
	vmType := fs.String("type", "validator", "chain type: validator | sp | relayer")
	rpc := fs.String("rpc", "", "RPC URL override (default: http://host:26657)")
	rest := fs.String("rest", "", "REST URL override (default: http://host:1317)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *host == "" || *user == "" || *key == "" {
		fmt.Fprintln(os.Stderr, "push add: --host, --user, and --key are required")
		os.Exit(1)
	}
	if *name == "" {
		*name = *host
	}

	cfgPath := vmsCfgPath(home)
	var cfg config.Config

	data, err := os.ReadFile(cfgPath)
	if err == nil {
		_ = toml.Unmarshal(data, &cfg)
	}

	// Guard against duplicates.
	for _, vm := range cfg.VMs {
		if vm.Name == *name {
			fmt.Fprintf(os.Stderr, "push add: VM %q already exists\n", *name)
			os.Exit(1)
		}
	}

	vm := config.VM{
		Name:       *name,
		Host:       *host,
		Port:       *port,
		User:       *user,
		KeyPath:    *key,
		Datacenter: *dc,
		Type:       *vmType,
		RPCURL:     *rpc,
		RESTURL:    *rest,
	}
	cfg.VMs = append(cfg.VMs, vm)

	if err := writeVMsCfg(cfgPath, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "push add: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Added VM %q (%s)\n", *name, *host)
}

// pushRemove removes a VM entry from vms.toml.
func pushRemove(home string, args []string) {
	fs := flag.NewFlagSet("push remove", flag.ExitOnError)
	name := fs.String("host", "", "VM name to remove (required)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if *name == "" {
		fmt.Fprintln(os.Stderr, "push remove: --host is required")
		os.Exit(1)
	}

	cfgPath := vmsCfgPath(home)
	cfg, err := loadVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "push remove: %v\n", err)
		os.Exit(1)
	}

	found := false
	filtered := cfg.VMs[:0]
	for _, vm := range cfg.VMs {
		if vm.Name == *name {
			found = true
			continue
		}
		filtered = append(filtered, vm)
	}
	if !found {
		fmt.Fprintf(os.Stderr, "push remove: VM %q not found\n", *name)
		os.Exit(1)
	}
	cfg.VMs = filtered

	if err := writeVMsCfg(cfgPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "push remove: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Removed VM %q\n", *name)
}

// pushUpdate runs apt-get upgrade on one or all VMs.
func pushUpdate(home string, args []string) {
	fs := flag.NewFlagSet("push update", flag.ExitOnError)
	name := fs.String("host", "", "target VM name (all VMs if omitted)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	cfg, err := loadVMsCfg(home)
	if err != nil {
		fmt.Fprintf(os.Stderr, "push update: %v\n", err)
		os.Exit(1)
	}

	vms := cfg.VMs
	if *name != "" {
		vm := cfg.FindVM(*name)
		if vm == nil {
			fmt.Fprintf(os.Stderr, "push update: VM %q not found\n", *name)
			os.Exit(1)
		}
		vms = []config.VM{*vm}
	}

	const upgradeCmd = "sudo apt-get update -qq && sudo apt-get upgrade -y"
	for _, vm := range vms {
		fmt.Printf("→ %s (%s) ... ", vm.Name, vm.Host)
		conn, err := pushssh.Dial(vm.Host, vm.Port, vm.User, vm.KeyPath)
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

// writeVMsCfg serializes cfg back to path, creating parent dirs as needed.
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
