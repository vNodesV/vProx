package config

import (
	"os"
	"path/filepath"
	"testing"
)

// makeChainDir creates $tmp/config/chains and returns the tmp root.
func makeChainDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "config", "chains"), 0755); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func writeSampleFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------- LoadChainIdentities ----------

func TestLoadChainIdentities_BasicLoad(t *testing.T) {
	tmp := makeChainDir(t)
	chainsDir := filepath.Join(tmp, "config", "chains")

	writeSampleFile(t, chainsDir, "mychain.sample", `
tree_name           = "mychain"
chain_id            = "mychain-mainnet-1"
dashboard_name      = "MyChain"
network_type        = "mainnet"
recommended_version = "v1.0.0"
explorers           = ["https://explorer.example.com"]
`)

	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}

	s := samples[0]
	if s.TreeName != "mychain" {
		t.Errorf("TreeName: want mychain, got %q", s.TreeName)
	}
	if s.ChainID != "mychain-mainnet-1" {
		t.Errorf("ChainID: want mychain-mainnet-1, got %q", s.ChainID)
	}
	if s.DashboardName != "MyChain" {
		t.Errorf("DashboardName: want MyChain, got %q", s.DashboardName)
	}
	if s.NetworkType != "mainnet" {
		t.Errorf("NetworkType: want mainnet, got %q", s.NetworkType)
	}
	if s.RecommendedVersion != "v1.0.0" {
		t.Errorf("RecommendedVersion: want v1.0.0, got %q", s.RecommendedVersion)
	}
	if len(s.Explorers) != 1 || s.Explorers[0] != "https://explorer.example.com" {
		t.Errorf("Explorers: unexpected value %v", s.Explorers)
	}
	if s.SourceFile == "" {
		t.Error("SourceFile should be populated by loader")
	}
}

func TestLoadChainIdentities_MultipleFiles(t *testing.T) {
	tmp := makeChainDir(t)
	chainsDir := filepath.Join(tmp, "config", "chains")

	writeSampleFile(t, chainsDir, "mychain.sample", `tree_name = "mychain"`)
	writeSampleFile(t, chainsDir, "other-chain.sample", `tree_name = "other-chain"`)

	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}
	names := map[string]bool{}
	for _, s := range samples {
		names[s.TreeName] = true
	}
	if !names["mychain"] || !names["other-chain"] {
		t.Errorf("expected mychain and other-chain in results, got %v", names)
	}
}

func TestLoadChainIdentities_SkipsTomlFiles(t *testing.T) {
	tmp := makeChainDir(t)
	chainsDir := filepath.Join(tmp, "config", "chains")

	// .toml files must NOT be loaded by LoadChainIdentities
	writeSampleFile(t, chainsDir, "mychain.toml", `tree_name = "mychain"`)
	// .sample file should load
	writeSampleFile(t, chainsDir, "other-chain.sample", `tree_name = "other-chain"`)

	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample (toml skipped), got %d", len(samples))
	}
	if samples[0].TreeName != "other-chain" {
		t.Errorf("expected other-chain, got %q", samples[0].TreeName)
	}
}

func TestLoadChainIdentities_SkipsOtherExtensions(t *testing.T) {
	tmp := makeChainDir(t)
	chainsDir := filepath.Join(tmp, "config", "chains")

	writeSampleFile(t, chainsDir, "mychain.yaml", `tree_name: mychain`)
	writeSampleFile(t, chainsDir, "README.md", `# chains`)
	writeSampleFile(t, chainsDir, "mychain.sample", `tree_name = "mychain"`)

	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
}

func TestLoadChainIdentities_NonExistentDir(t *testing.T) {
	tmp := t.TempDir()
	// No config/chains directory created — should return nil, nil
	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("expected nil error for missing dir, got: %v", err)
	}
	if samples != nil {
		t.Errorf("expected nil slice for missing dir, got %v", samples)
	}
}

func TestLoadChainIdentities_EmptyDir(t *testing.T) {
	tmp := makeChainDir(t)
	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 0 {
		t.Errorf("expected 0 samples for empty dir, got %d", len(samples))
	}
}

func TestLoadChainIdentities_MalformedSampleSkipped(t *testing.T) {
	tmp := makeChainDir(t)
	chainsDir := filepath.Join(tmp, "config", "chains")

	// Malformed TOML — should be skipped, not fatal
	writeSampleFile(t, chainsDir, "bad.sample", `this is not valid toml @@@@`)
	// Valid one should still load
	writeSampleFile(t, chainsDir, "good.sample", `tree_name = "other-chain"`)

	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 valid sample (bad skipped), got %d", len(samples))
	}
	if samples[0].TreeName != "other-chain" {
		t.Errorf("expected other-chain, got %q", samples[0].TreeName)
	}
}

func TestLoadChainIdentities_SourceFilePopulated(t *testing.T) {
	tmp := makeChainDir(t)
	chainsDir := filepath.Join(tmp, "config", "chains")
	writeSampleFile(t, chainsDir, "mychain.sample", `tree_name = "mychain"`)

	samples, err := LoadChainIdentities(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(samples))
	}
	want := filepath.Join(chainsDir, "mychain.sample")
	if samples[0].SourceFile != want {
		t.Errorf("SourceFile: want %q, got %q", want, samples[0].SourceFile)
	}
}

// ---------- JoinChainTree ----------

func TestJoinChainTree_ReturnsMatchingNodes(t *testing.T) {
	identity := ChainSample{TreeName: "mychain", ChainID: "mychain-mainnet-1"}
	nodes := []ServiceNode{
		{Tree: "mychain", Host: "mychain-rpc.example.com"},
		{Tree: "other-chain", Host: "other-chain-rpc.example.com"},
		{Tree: "mychain", Host: "mychain-rest.example.com"},
	}

	result := JoinChainTree(identity, nodes)
	if len(result) != 2 {
		t.Fatalf("expected 2 nodes for mychain, got %d", len(result))
	}
	for _, n := range result {
		if n.Tree != "mychain" {
			t.Errorf("unexpected tree in result: %q", n.Tree)
		}
	}
}

func TestJoinChainTree_NoMatch(t *testing.T) {
	identity := ChainSample{TreeName: "cosmoshub"}
	nodes := []ServiceNode{
		{Tree: "mychain"},
		{Tree: "other-chain"},
	}
	result := JoinChainTree(identity, nodes)
	if result != nil {
		t.Errorf("expected nil slice when no match, got %v", result)
	}
}

func TestJoinChainTree_EmptyNodes(t *testing.T) {
	identity := ChainSample{TreeName: "mychain"}
	result := JoinChainTree(identity, nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestJoinChainTree_AllMatch(t *testing.T) {
	identity := ChainSample{TreeName: "mychain"}
	nodes := []ServiceNode{
		{Tree: "mychain", Role: "validator"},
		{Tree: "mychain", Role: "api"},
	}
	result := JoinChainTree(identity, nodes)
	if len(result) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(result))
	}
}

func TestJoinChainTree_ExactMatch(t *testing.T) {
	// "mychain-testnet" must NOT match "mychain"
	identity := ChainSample{TreeName: "mychain"}
	nodes := []ServiceNode{
		{Tree: "mychain-testnet"},
		{Tree: "mychain", Role: "validator"},
	}
	result := JoinChainTree(identity, nodes)
	if len(result) != 1 {
		t.Fatalf("expected 1 exact match, got %d", len(result))
	}
	if result[0].Tree != "mychain" {
		t.Errorf("wrong match: %q", result[0].Tree)
	}
}
