package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// InfraHost describes a physical server in config/modules/infra/<dc>.toml.
// Files use [[host]] TOML array-of-tables.
type InfraHost struct {
	Name       string        `toml:"name"`
	PublicIP   string        `toml:"public_ip"`
	LanIP      string        `toml:"lan_ip"`
	Datacenter string        `toml:"datacenter"`
	User       string        `toml:"user"`
	SSHKeyPath string        `toml:"ssh_key_path"`
	Ping       InfraHostPing `toml:"ping"`
}

// InfraHostPing holds probe configuration for external latency checks.
type InfraHostPing struct {
	Country  string `toml:"country"`
	Provider string `toml:"provider"`
}

// InfraModulesFile is the top-level struct for config/modules/infra/<dc>.toml.
// Each file contains one or more [[host]] array-of-tables entries.
type InfraModulesFile struct {
	Hosts []InfraHost `toml:"host"`
}

// LoadInfraHosts scans $home/config/modules/infra/*.toml and returns all hosts.
//
// Each file is decoded as an InfraModulesFile containing [[host]] array-of-tables.
// All hosts across all files are aggregated into a single slice.
// Returns an empty (non-nil) slice when the directory does not exist.
// Individual file parse errors are printed to stderr and skipped.
func LoadInfraHosts(home string) ([]InfraHost, error) {
	dir := filepath.Join(home, "config", "modules", "infra")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []InfraHost{}, nil
		}
		return nil, fmt.Errorf("LoadInfraHosts: read dir %s: %w", dir, err)
	}

	var out []InfraHost
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vprox: LoadInfraHosts: skip %s: %v\n", path, err)
			continue
		}
		var f InfraModulesFile
		if err := toml.Unmarshal(raw, &f); err != nil {
			fmt.Fprintf(os.Stderr, "vprox: LoadInfraHosts: skip %s: %v\n", path, err)
			continue
		}
		out = append(out, f.Hosts...)
	}

	if out == nil {
		return []InfraHost{}, nil
	}
	return out, nil
}
