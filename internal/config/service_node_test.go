package config

import (
	"os"
	"path/filepath"
	"testing"
)

// makeNodesDir creates $tmp/config/services/nodes and returns the tmp root.
func makeNodesDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "config", "services", "nodes"), 0755); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func writeNodeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

const validNodeTOML = `
tree        = "cheqd"
role        = "validator"
datacenter  = "ca1"
host        = "cheqd.srvs.example.net"
ip          = "10.0.0.66"

[proxy]
expose_path       = true
expose_vhost      = true
vhost_prefix_rpc  = "rpc"
vhost_prefix_rest = "api"

[services]
rpc       = true
rest      = true
websocket = true
grpc      = false

[management]
managed_host = false
ssh_user     = ""
ssh_key      = ""
ssh_port     = 22
`

// ---------- LoadServiceNodes ----------

func TestLoadServiceNodes_BasicLoad(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")

	writeNodeFile(t, nodesDir, "cheqd-validator-ca1.toml", validNodeTOML)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	n := nodes[0]
	if n.Tree != "cheqd" {
		t.Errorf("Tree: want cheqd, got %q", n.Tree)
	}
	if n.Role != "validator" {
		t.Errorf("Role: want validator, got %q", n.Role)
	}
	if n.Datacenter != "ca1" {
		t.Errorf("Datacenter: want ca1, got %q", n.Datacenter)
	}
	if n.Host != "cheqd.srvs.example.net" {
		t.Errorf("Host: want cheqd.srvs.example.net, got %q", n.Host)
	}
	if n.IP != "10.0.0.66" {
		t.Errorf("IP: want 10.0.0.66, got %q", n.IP)
	}
}

func TestLoadServiceNodes_ProxyFields(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")
	writeNodeFile(t, nodesDir, "cheqd.toml", validNodeTOML)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	if !n.Proxy.ExposePath {
		t.Error("Proxy.ExposePath: want true")
	}
	if !n.Proxy.ExposeVhost {
		t.Error("Proxy.ExposeVhost: want true")
	}
	if n.Proxy.VhostPrefixRPC != "rpc" {
		t.Errorf("VhostPrefixRPC: want rpc, got %q", n.Proxy.VhostPrefixRPC)
	}
	if n.Proxy.VhostPrefixREST != "api" {
		t.Errorf("VhostPrefixREST: want api, got %q", n.Proxy.VhostPrefixREST)
	}
}

func TestLoadServiceNodes_ServicesFields(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")
	writeNodeFile(t, nodesDir, "cheqd.toml", validNodeTOML)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("load failed: err=%v len=%d", err, len(nodes))
	}
	n := nodes[0]

	if !n.Services.RPC {
		t.Error("Services.RPC: want true")
	}
	if !n.Services.REST {
		t.Error("Services.REST: want true")
	}
	if !n.Services.WebSocket {
		t.Error("Services.WebSocket: want true")
	}
	if n.Services.GRPC {
		t.Error("Services.GRPC: want false")
	}
}

func TestLoadServiceNodes_ManagementFields(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")
	writeNodeFile(t, nodesDir, "cheqd.toml", validNodeTOML)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("load failed: err=%v len=%d", err, len(nodes))
	}
	n := nodes[0]

	if n.Management.ManagedHost {
		t.Error("Management.ManagedHost: want false")
	}
	if n.Management.SSHPort != 22 {
		t.Errorf("Management.SSHPort: want 22, got %d", n.Management.SSHPort)
	}
}

func TestLoadServiceNodes_SourceFilePopulated(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")
	writeNodeFile(t, nodesDir, "cheqd.toml", `tree = "cheqd"`)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("load failed: err=%v len=%d", err, len(nodes))
	}
	want := filepath.Join(nodesDir, "cheqd.toml")
	if nodes[0].SourceFile != want {
		t.Errorf("SourceFile: want %q, got %q", want, nodes[0].SourceFile)
	}
}

func TestLoadServiceNodes_MultipleFiles(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")
	writeNodeFile(t, nodesDir, "cheqd-validator.toml", `tree = "cheqd"`)
	writeNodeFile(t, nodesDir, "osmosis-rpc.toml", `tree = "osmosis"`)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
}

func TestLoadServiceNodes_NonExistentDir(t *testing.T) {
	tmp := t.TempDir()
	// No config/services/nodes directory created
	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if nodes != nil {
		t.Errorf("expected nil slice for missing dir, got %v", nodes)
	}
}

func TestLoadServiceNodes_EmptyDir(t *testing.T) {
	tmp := makeNodesDir(t)
	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for empty dir, got %d", len(nodes))
	}
}

func TestLoadServiceNodes_SkipsNonToml(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")

	// These should be skipped
	writeNodeFile(t, nodesDir, "cheqd.sample", `tree = "cheqd"`)
	writeNodeFile(t, nodesDir, "README.md", `# nodes`)
	// This should load
	writeNodeFile(t, nodesDir, "osmosis.toml", `tree = "osmosis"`)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (.sample/.md skipped), got %d", len(nodes))
	}
	if nodes[0].Tree != "osmosis" {
		t.Errorf("expected osmosis, got %q", nodes[0].Tree)
	}
}

func TestLoadServiceNodes_MalformedFileSkipped(t *testing.T) {
	tmp := makeNodesDir(t)
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")

	writeNodeFile(t, nodesDir, "bad.toml", `this is not valid toml @@@@`)
	writeNodeFile(t, nodesDir, "good.toml", `tree = "osmosis"`)

	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 valid node (bad skipped), got %d", len(nodes))
	}
	if nodes[0].Tree != "osmosis" {
		t.Errorf("expected osmosis, got %q", nodes[0].Tree)
	}
}

// ---------- Integration: JoinChainTree with LoadChainIdentities + LoadServiceNodes ----------

func TestJoinChainTree_Integration(t *testing.T) {
	tmp := t.TempDir()

	// Set up chain sample
	chainsDir := filepath.Join(tmp, "config", "chains")
	if err := os.MkdirAll(chainsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chainsDir, "cheqd.sample"), []byte(`tree_name = "cheqd"`), 0644); err != nil {
		t.Fatal(err)
	}

	// Set up service nodes
	nodesDir := filepath.Join(tmp, "config", "services", "nodes")
	if err := os.MkdirAll(nodesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodesDir, "cheqd-val.toml"), []byte(`tree = "cheqd"`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodesDir, "osmosis-rpc.toml"), []byte(`tree = "osmosis"`), 0644); err != nil {
		t.Fatal(err)
	}

	// Load both
	identities, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("LoadChainIdentities: %v", err)
	}
	nodes, err := LoadServiceNodes(tmp)
	if err != nil {
		t.Fatalf("LoadServiceNodes: %v", err)
	}

	// Join
	if len(identities) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(identities))
	}
	result := JoinChainTree(identities[0], nodes)
	if len(result) != 1 {
		t.Fatalf("expected 1 node for cheqd, got %d", len(result))
	}
	if result[0].Tree != "cheqd" {
		t.Errorf("expected cheqd, got %q", result[0].Tree)
	}
}
