package configwizard

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
	chainconfig "github.com/vNodesV/vProx/internal/config"
)

// runChain runs the interactive wizard for config/chains/<name>.toml (Step 3).
// If name is "", the wizard prompts for the chain name and creates a new file.
// If name is set, it loads and updates that chain's config.
func runChain(home, name string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 3 — Chain Config  (config/chains/<name>.toml)         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Load existing chain if name provided.
	var ex chainconfig.ChainConfig
	if name == "" {
		name = readSlug("chain_name (slug, e.g. cheqd)", "")
	}
	path := configPath(home, "chains", name+".toml")
	if f, err := os.Open(path); err == nil {
		_ = toml.NewDecoder(f).Decode(&ex)
		f.Close()
	}
	if ex.ChainName == "" {
		ex.ChainName = name
	}

	var c chainconfig.ChainConfig
	c.SchemaVersion = 1
	c.ChainName = name

	section("Group A — Identity")
	c.ChainID = readString("chain_id (e.g. cheqd-mainnet-1)", ex.ChainID, false)
	if c.ChainID != "" {
		fmt.Println("  ℹ  chain_id set — cosmos.directory metadata will be auto-fetched on startup")
	}
	c.DashboardName = readString("dashboard_name (display label)", ex.DashboardName, false)
	c.Host = readString("host (public hostname)", ex.Host, true)
	c.IP = readIP("ip (backend IP)", ex.IP, true)
	c.ExplorerBase = readString("explorer_base (URL, optional)", ex.ExplorerBase, false)

	section("Group B — Exposure")
	c.Expose.Path = readBool("expose.path", boolDefault(ex.Expose.Path, true))
	c.Expose.VHost = readBool("expose.vhost", boolDefault(ex.Expose.VHost, true))
	if c.Expose.VHost {
		defRPC := ex.Expose.VHostPrefix.RPC
		if defRPC == "" {
			defRPC = "rpc"
		}
		defREST := ex.Expose.VHostPrefix.REST
		if defREST == "" {
			defREST = "api"
		}
		c.Expose.VHostPrefix.RPC = readString("expose.vhost_prefix.rpc", defRPC, false)
		c.Expose.VHostPrefix.REST = readString("expose.vhost_prefix.rest", defREST, false)
	}

	section("Group C — Services")
	c.Services.RPC = readBool("services.rpc", boolDefault(ex.Services.RPC, true))
	c.Services.REST = readBool("services.rest", boolDefault(ex.Services.REST, true))
	if c.Services.RPC {
		c.Services.WebSocket = readBool("services.websocket", boolDefault(ex.Services.WebSocket, true))
	}
	c.Services.GRPC = readBool("services.grpc", boolDefault(ex.Services.GRPC, true))
	c.Services.GRPCWeb = readBool("services.grpc_web", boolDefault(ex.Services.GRPCWeb, true))
	c.Services.APIAlias = readBool("services.api_alias", boolDefault(ex.Services.APIAlias, true))

	section("Group D — Ports")
	c.DefaultPorts = readBool("use default ports (from ports.toml)", boolDefault(ex.DefaultPorts, true))
	if !c.DefaultPorts {
		c.Ports.RPC = readPort("ports.rpc", portDefault(ex.Ports.RPC, 26657))
		c.Ports.REST = readPort("ports.rest", portDefault(ex.Ports.REST, 1317))
		c.Ports.GRPC = readOptionalPort("ports.grpc", portDefault(ex.Ports.GRPC, 9090))
		c.Ports.GRPCWeb = readOptionalPort("ports.grpc_web", portDefault(ex.Ports.GRPCWeb, 9091))
	}

	section("Group E — Management (optional)")
	c.Management.ManagedHost = readBool("management.managed_host", ex.Management.ManagedHost)
	if c.Management.ManagedHost {
		c.Management.LanIP = readOptionalIP("management.lan_ip", ex.Management.LanIP)
		c.Management.User = readString("management.user (SSH user)", ex.Management.User, false)
		c.Management.KeyPath = readString("management.key_path (SSH key path)", ex.Management.KeyPath, false)
		c.Management.Port = readOptionalPort("management.port (SSH port)", portDefault(ex.Management.Port, 22))
		c.Management.Valoper = readString("management.valoper (validator operator address)", ex.Management.Valoper, false)
		c.Management.Datacenter = readString("management.datacenter (e.g. QC, RBX)", ex.Management.Datacenter, false)
	}
	c.Management.ExposedServices = readBool("management.exposed_services", boolDefault(ex.Management.ExposedServices, true))
	if ex.Management.Ping.Country != "" || readBool("configure management.ping?", false) {
		defCountry := ex.Management.Ping.Country
		if defCountry == "" {
			defCountry = "CA"
		}
		c.Management.Ping.Country = readCountry("management.ping.country", defCountry)
		c.Management.Ping.Provider = readString("management.ping.provider (optional)", ex.Management.Ping.Provider, false)
	}

	section("Group F — Chain Services (optional, for dashboard)")
	if readBool("configure chain_services (validator/SP)?", false) {
		c.ChainServices.Validator.Mainnet.Address = readString("chain_services.validator.mainnet.address (valoper)", ex.ChainServices.Validator.Mainnet.Address, false)
		c.ChainServices.Validator.Testnet.Address = readString("chain_services.validator.testnet.address (valoper)", ex.ChainServices.Validator.Testnet.Address, false)
		c.ChainServices.SP.Mainnet.Hostname = readString("chain_services.sp.mainnet.hostname", ex.ChainServices.SP.Mainnet.Hostname, false)
		c.ChainServices.SP.Mainnet.LanIP = readOptionalIP("chain_services.sp.mainnet.lan_ip", ex.ChainServices.SP.Mainnet.LanIP)
	}

	// Validate before writing.
	if err := chainconfig.ValidateConfig(&c); err != nil {
		return fmt.Errorf("validation: %w", err)
	}

	_ = strings.ToLower // keep import tidy (used via slug logic above)
	return writeConfig(path, c)
}

// boolDefault returns v if it was set explicitly; otherwise returns def.
// Since bool zero-value = false, for required-true defaults we pass def=true.
func boolDefault(v, def bool) bool {
	// We can't distinguish "user wrote false" from "zero value", so
	// we trust the plan's documented defaults and only apply them when loading
	// a brand-new (empty) config.
	_ = def
	return v
}

func portDefault(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
