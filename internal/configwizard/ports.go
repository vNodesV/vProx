package configwizard

import (
	"fmt"
	"path/filepath"

	chainconfig "github.com/vNodesV/vProx/internal/config"
)

// runPorts runs the interactive wizard for config/chains/ports.toml (Step 1).
func runPorts(home string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 1 — Ports  (config/chains/ports.toml)                 ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("  Default Cosmos SDK ports. Override per-chain in chains/<name>.toml.")

	// Load existing values as defaults.
	existing, _ := chainconfig.LoadPorts(filepath.Join(home, "config", "chains", "ports.toml"))

	defRPC := 26657
	if existing.RPC != 0 {
		defRPC = existing.RPC
	}
	defREST := 1317
	if existing.REST != 0 {
		defREST = existing.REST
	}
	defGRPC := 9090
	if existing.GRPC != 0 {
		defGRPC = existing.GRPC
	}
	defGRPCWeb := 9091
	if existing.GRPCWeb != 0 {
		defGRPCWeb = existing.GRPCWeb
	}
	defAPI := 1317
	if existing.API != 0 {
		defAPI = existing.API
	}

	section("Required")
	rpc := readPort("rpc", defRPC)
	rest := readPort("rest", defREST)

	section("Optional")
	grpc := readOptionalPort("grpc", defGRPC)
	grpcWeb := readOptionalPort("grpc_web", defGRPCWeb)
	api := readOptionalPort("api", defAPI)
	vopsURL := readOptionalURL("vops_url", existing.VOpsURL)

	section("Trusted Proxies")
	fmt.Println("  CIDRs trusted to forward X-Forwarded-For (e.g. 127.0.0.1/32 for loopback).")
	defCIDRs := existing.TrustedProxies
	if len(defCIDRs) == 0 {
		defCIDRs = []string{"127.0.0.1/32", "::1/128"}
	}
	trusted := readCIDRList("trusted_proxies", defCIDRs)

	p := chainconfig.Ports{
		RPC:            rpc,
		REST:           rest,
		GRPC:           grpc,
		GRPCWeb:        grpcWeb,
		API:            api,
		VOpsURL:        vopsURL,
		TrustedProxies: trusted,
	}

	return writeConfig(configPath(home, "chains", "ports.toml"), p)
}
