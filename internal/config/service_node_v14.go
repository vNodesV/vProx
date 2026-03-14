package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// ServiceNode is a deployable node instance loaded from config/services/nodes/*.toml.
//
// It contains proxy routing, service exposure, and optional fleet management settings
// for a single upstream node. Chain identity metadata (chain_id, dashboard_name, etc.)
// lives in a separate config/chains/<chain>.sample and is joined via
// ServiceNode.Tree == ChainSample.TreeName.
//
// v1.4.0 P2 loader.
type ServiceNode struct {
	// Tree is the join key linking this node to a ChainSample identity.
	// Must match ChainSample.TreeName exactly (e.g., "cheqd", "osmosis").
	Tree string `toml:"tree"`

	// Role describes the node's function: validator | sp | api | relayer | node.
	Role string `toml:"role"`

	// Datacenter is the short datacenter/provider label (e.g., "ca1", "us2").
	Datacenter string `toml:"datacenter"`

	// Host is the public vProx service hostname for this node.
	Host string `toml:"host"`

	// IP is the internal LAN IP of the upstream node used for proxy routing.
	IP string `toml:"ip"`

	// Proxy controls how this node is exposed via vProx.
	Proxy struct {
		ExposePath      bool   `toml:"expose_path"`
		ExposeVhost     bool   `toml:"expose_vhost"`
		VhostPrefixRPC  string `toml:"vhost_prefix_rpc"`
		VhostPrefixREST string `toml:"vhost_prefix_rest"`
	} `toml:"proxy"`

	// Services lists which Cosmos SDK endpoints are proxied for this node.
	Services struct {
		RPC       bool `toml:"rpc"`
		REST      bool `toml:"rest"`
		WebSocket bool `toml:"websocket"`
		GRPC      bool `toml:"grpc"`
	} `toml:"services"`

	// Management holds optional SSH + fleet module settings.
	// Populated only when managed_host = true.
	Management struct {
		ManagedHost bool   `toml:"managed_host"`
		SSHUser     string `toml:"ssh_user"`
		SSHKey      string `toml:"ssh_key"`
		SSHPort     int    `toml:"ssh_port"`
	} `toml:"management"`

	// SourceFile is populated by LoadServiceNodes from the file path; not read from TOML.
	SourceFile string `toml:"-"`
}

// LoadServiceNodes scans $home/config/services/nodes/*.toml and returns parsed ServiceNode entries.
//
// Only files with the .toml extension are loaded. Returns a nil slice (no error) when the
// directory does not exist yet. Individual file parse errors are printed to stderr and
// skipped so that a single malformed file does not block other nodes from loading.
func LoadServiceNodes(home string) ([]ServiceNode, error) {
	dir := filepath.Join(home, "config", "services", "nodes")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // empty is fine; directory may not exist yet
		}
		return nil, fmt.Errorf("LoadServiceNodes: read dir %s: %w", dir, err)
	}

	var out []ServiceNode
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vprox: LoadServiceNodes: skip %s: %v\n", path, err)
			continue
		}
		var n ServiceNode
		if err := toml.Unmarshal(raw, &n); err != nil {
			fmt.Fprintf(os.Stderr, "vprox: LoadServiceNodes: skip %s: %v\n", path, err)
			continue
		}
		n.SourceFile = path
		out = append(out, n)
	}
	return out, nil
}
