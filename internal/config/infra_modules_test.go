package config

import (
	"os"
	"path/filepath"
	"testing"
)

// makeInfraDir creates $tmp/config/modules/infra and returns the tmp root.
func makeInfraDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "config", "modules", "infra"), 0755); err != nil {
		t.Fatal(err)
	}
	return tmp
}

func writeInfraFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, "config", "modules", "infra", name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------- LoadInfraHosts ----------

func TestLoadInfraHosts_EmptyDir(t *testing.T) {
	// Directory doesn't exist → returns empty slice, nil error.
	tmp := t.TempDir()
	hosts, err := LoadInfraHosts(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hosts == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(hosts) != 0 {
		t.Fatalf("expected 0 hosts, got %d", len(hosts))
	}
}

func TestLoadInfraHosts_MultipleHosts(t *testing.T) {
	// Two [[host]] blocks in one file → both loaded.
	tmp := makeInfraDir(t)
	writeInfraFile(t, tmp, "dc-toronto.toml", `
[[host]]
name       = "hv-tor-01"
public_ip  = "1.2.3.4"
lan_ip     = "10.0.1.1"
datacenter = "toronto"
user       = "ubuntu"

[[host]]
name       = "hv-tor-02"
public_ip  = "1.2.3.5"
lan_ip     = "10.0.1.2"
datacenter = "toronto"
`)

	hosts, err := LoadInfraHosts(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	if hosts[0].Name != "hv-tor-01" {
		t.Errorf("host[0].Name = %q, want %q", hosts[0].Name, "hv-tor-01")
	}
	if hosts[0].PublicIP != "1.2.3.4" {
		t.Errorf("host[0].PublicIP = %q, want %q", hosts[0].PublicIP, "1.2.3.4")
	}
	if hosts[0].Datacenter != "toronto" {
		t.Errorf("host[0].Datacenter = %q, want %q", hosts[0].Datacenter, "toronto")
	}

	if hosts[1].Name != "hv-tor-02" {
		t.Errorf("host[1].Name = %q, want %q", hosts[1].Name, "hv-tor-02")
	}
	if hosts[1].LanIP != "10.0.1.2" {
		t.Errorf("host[1].LanIP = %q, want %q", hosts[1].LanIP, "10.0.1.2")
	}
}

func TestLoadInfraHosts_MultipleFiles(t *testing.T) {
	// Two .toml files → all hosts aggregated.
	tmp := makeInfraDir(t)

	writeInfraFile(t, tmp, "dc-toronto.toml", `
[[host]]
name       = "hv-tor-01"
datacenter = "toronto"
`)

	writeInfraFile(t, tmp, "dc-helsinki.toml", `
[[host]]
name       = "hv-hel-01"
datacenter = "helsinki"

[[host]]
name       = "hv-hel-02"
datacenter = "helsinki"
`)

	hosts, err := LoadInfraHosts(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(hosts))
	}

	// Verify all datacenters are present (order depends on ReadDir).
	dcCount := map[string]int{}
	for _, h := range hosts {
		dcCount[h.Datacenter]++
	}
	if dcCount["toronto"] != 1 {
		t.Errorf("expected 1 toronto host, got %d", dcCount["toronto"])
	}
	if dcCount["helsinki"] != 2 {
		t.Errorf("expected 2 helsinki hosts, got %d", dcCount["helsinki"])
	}
}

func TestLoadInfraHosts_WithPing(t *testing.T) {
	// Ping subtable decoded correctly.
	tmp := makeInfraDir(t)
	writeInfraFile(t, tmp, "dc-toronto.toml", `
[[host]]
name         = "hv-tor-01"
public_ip    = "1.2.3.4"
lan_ip       = "10.0.1.1"
datacenter   = "toronto"
user         = "ubuntu"
ssh_key_path = "/home/deploy/.ssh/id_fleet"

[host.ping]
country  = "CA"
provider = "ca1"
`)

	hosts, err := LoadInfraHosts(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}

	h := hosts[0]
	if h.Name != "hv-tor-01" {
		t.Errorf("Name = %q, want %q", h.Name, "hv-tor-01")
	}
	if h.SSHKeyPath != "/home/deploy/.ssh/id_fleet" {
		t.Errorf("SSHKeyPath = %q, want %q", h.SSHKeyPath, "/home/deploy/.ssh/id_fleet")
	}
	if h.Ping.Country != "CA" {
		t.Errorf("Ping.Country = %q, want %q", h.Ping.Country, "CA")
	}
	if h.Ping.Provider != "ca1" {
		t.Errorf("Ping.Provider = %q, want %q", h.Ping.Provider, "ca1")
	}
}

func TestLoadInfraHosts_SkipsNonTOML(t *testing.T) {
	// Non-.toml files and directories are ignored.
	tmp := makeInfraDir(t)
	infraDir := filepath.Join(tmp, "config", "modules", "infra")

	writeInfraFile(t, tmp, "dc-toronto.toml", `
[[host]]
name = "hv-tor-01"
`)
	// Write a non-toml file that should be skipped.
	if err := os.WriteFile(filepath.Join(infraDir, "README.md"), []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}
	// Write a .toml.example file that should be skipped.
	if err := os.WriteFile(filepath.Join(infraDir, "sample.toml.example"), []byte("ignore"), 0644); err != nil {
		t.Fatal(err)
	}

	hosts, err := LoadInfraHosts(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if hosts[0].Name != "hv-tor-01" {
		t.Errorf("Name = %q, want %q", hosts[0].Name, "hv-tor-01")
	}
}

func TestLoadInfraHosts_EmptyDirExists(t *testing.T) {
	// Directory exists but is empty → returns empty slice, nil error.
	tmp := makeInfraDir(t)
	hosts, err := LoadInfraHosts(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hosts == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(hosts) != 0 {
		t.Fatalf("expected 0 hosts, got %d", len(hosts))
	}
}
