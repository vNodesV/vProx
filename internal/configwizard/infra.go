package configwizard

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	fleetcfg "github.com/vNodesV/vProx/internal/fleet/config"
)

// infraFile is the top-level structure for config/infra/<dc>.toml.
type infraFile struct {
	Host  fleetcfg.Host `toml:"host"`
	VProx vproxEntry    `toml:"vprox"`
	VMs   []fleetcfg.VM `toml:"vm"`
}

// vproxEntry holds the [vprox] section in infra TOML files.
type vproxEntry struct {
	Name       string `toml:"name"`
	LanIP      string `toml:"lan_ip"`
	Key        string `toml:"key"`
	SSHKeyPath string `toml:"ssh_key_path"`
}

// loadInfraFile reads an existing datacenter TOML file.
func loadInfraFile(path string) infraFile {
	var f infraFile
	fh, err := os.Open(path)
	if err != nil {
		return f
	}
	defer fh.Close()
	_ = toml.NewDecoder(fh).Decode(&f)
	return f
}

// runInfra runs the interactive wizard for config/infra/<dc>.toml (Step 6).
// dc is optional; if empty, the wizard prompts for the datacenter name.
func runInfra(home, dc string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 6 — Infra Config  (config/infra/<dc>.toml)            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("  Defines one physical host and its child VMs (or standalone VPS entries).")

	if dc == "" {
		dc = readString("datacenter name (slug, e.g. qc, rbx, ovh1)", "", true)
	}
	dc = strings.ToLower(strings.TrimSpace(dc))
	path := configPath(home, "infra", dc+".toml")
	ex := loadInfraFile(path)

	var inf infraFile

	section("[host] — Physical Server (optional)")
	fmt.Println("  Leave name empty to skip the [host] section (for standalone VPS operators).")
	inf.Host.Name = readString("host.name", ex.Host.Name, false)
	if inf.Host.Name != "" {
		inf.Host.LanIP = readOptionalIP("host.lan_ip", ex.Host.LanIP)
		inf.Host.PublicIP = readOptionalIP("host.public_ip", ex.Host.PublicIP)
		inf.Host.Datacenter = readString("host.datacenter", stringDefault(ex.Host.Datacenter, strings.ToUpper(dc)), false)
		inf.Host.User = readString("host.user (SSH default for VMs in this file)", ex.Host.User, false)
		inf.Host.SSHKeyPath = readString("host.ssh_key_path", ex.Host.SSHKeyPath, false)
	}

	section("[vprox] — vProx Instance")
	inf.VProx.Name = readString("vprox.name", stringDefault(ex.VProx.Name, "vProx"), false)
	inf.VProx.LanIP = readOptionalIP("vprox.lan_ip", ex.VProx.LanIP)
	inf.VProx.Key = readString("vprox.key (path to vProx private key)", ex.VProx.Key, false)
	inf.VProx.SSHKeyPath = readString("vprox.ssh_key_path", ex.VProx.SSHKeyPath, false)

	section("[[vm]] — Virtual Machines")
	// Seed with existing VMs as defaults.
	inf.VMs = append(inf.VMs, ex.VMs...)
	for {
		fmt.Printf("\n  %d VM(s) configured so far. Add another? ", len(inf.VMs))
		if !confirm("add VM", false) {
			break
		}
		vm := promptVM()
		inf.VMs = append(inf.VMs, vm)
	}

	return writeConfig(path, inf)
}

// promptVM interactively collects one [[vm]] entry.
func promptVM() fleetcfg.VM {
	var vm fleetcfg.VM
	vm.Name = readString("vm.name", "", true)
	vm.Host = readString("vm.host (SSH target IP or hostname)", "", true)
	vm.LanIP = readOptionalIP("vm.lan_ip", "")
	vm.PublicIP = readOptionalIP("vm.public_ip", "")
	vm.Port = readOptionalPort("vm.port (SSH)", 22)
	vm.User = readString("vm.user (SSH user; empty = host default)", "", false)
	vm.KeyPath = readString("vm.key_path (SSH key; empty = host default)", "", false)
	vm.Datacenter = readString("vm.datacenter", "", false)
	vm.Type = readString("vm.type (validator|sp|rpc|relayer|node, comma-separated)", "", false)
	vm.ChainName = readString("vm.chain_name (matches chains/<name>.toml)", "", false)
	vm.Ping.Country = readCountry("vm.ping.country", "CA")
	vm.Ping.Provider = readString("vm.ping.provider (optional)", "", false)
	return vm
}
