package configwizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	chainconfig "github.com/vNodesV/vProx/internal/config"
	vlogcfg "github.com/vNodesV/vProx/internal/vlog/config"
)

// listConfigs prints all TOML files under $VPROX_HOME/config/ with status markers.
func listConfigs(home string) error {
	cfgDir := filepath.Join(home, "config")
	fmt.Printf("\nConfig files in %s\n", cfgDir)
	fmt.Println(strings.Repeat("─", 64))

	return filepath.WalkDir(cfgDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".toml") {
			return nil
		}
		rel, _ := filepath.Rel(cfgDir, path)
		info, _ := d.Info()
		size := ""
		if info != nil {
			size = fmt.Sprintf("%d bytes", info.Size())
		}
		fmt.Printf("  %-50s  %s\n", rel, size)
		return nil
	})
}

// validateConfigs parses and validates all known config files.
func validateConfigs(home string) error {
	cfgDir := filepath.Join(home, "config")
	var errCount int

	checkFile := func(label, path string, validate func([]byte) error) {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			fmt.Printf("  ⚪ %-50s  (absent)\n", label)
			return
		}
		if err != nil {
			fmt.Printf("  ✗ %-50s  read error: %v\n", label, err)
			errCount++
			return
		}
		if err := validate(data); err != nil {
			fmt.Printf("  ✗ %-50s  %v\n", label, err)
			errCount++
			return
		}
		fmt.Printf("  ✓ %-50s\n", label)
	}

	fmt.Printf("\nValidating configs in %s\n", cfgDir)
	fmt.Println(strings.Repeat("─", 64))

	// ports.toml
	checkFile("chains/ports.toml", filepath.Join(cfgDir, "chains", "ports.toml"), func(b []byte) error {
		var p chainconfig.Ports
		return toml.Unmarshal(b, &p)
	})

	// vprox/settings.toml
	checkFile("vprox/settings.toml", filepath.Join(cfgDir, "vprox", "settings.toml"), func(b []byte) error {
		var s proxySettingsFile
		return toml.Unmarshal(b, &s)
	})

	// vlog/vlog.toml
	checkFile("vlog/vlog.toml", filepath.Join(cfgDir, "vlog", "vlog.toml"), func(b []byte) error {
		_, err := vlogcfg.Load(filepath.Join(cfgDir, "vlog", "vlog.toml"))
		return err
	})

	// chain configs
	chainDir := filepath.Join(cfgDir, "chains")
	entries, _ := os.ReadDir(chainDir)
	for _, e := range entries {
		if e.IsDir() || !chainconfig.IsChainTOML(e.Name()) {
			continue
		}
		fpath := filepath.Join(chainDir, e.Name())
		label := "chains/" + e.Name()
		checkFile(label, fpath, func(b []byte) error {
			f, err := os.Open(fpath)
			if err != nil {
				return err
			}
			defer f.Close()
			var c chainconfig.ChainConfig
			if err := toml.NewDecoder(f).Decode(&c); err != nil {
				return err
			}
			return chainconfig.ValidateConfig(&c)
		})
	}

	// backup/backup.toml
	checkFile("backup/backup.toml", filepath.Join(cfgDir, "backup", "backup.toml"), func(b []byte) error {
		var s struct {
			Backup interface{} `toml:"backup"`
		}
		return toml.Unmarshal(b, &s)
	})

	if errCount > 0 {
		return fmt.Errorf("%d validation error(s) found", errCount)
	}
	fmt.Println("\n✓ All present configs are valid.")
	return nil
}
