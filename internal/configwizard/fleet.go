package configwizard

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// fleetSettingsFile mirrors the shape of config/fleet/settings.toml.
type fleetSettingsFile struct {
	SSH struct {
		User       string `toml:"user"`
		KeyPath    string `toml:"key_path"`
		Port       int    `toml:"port"`
		TimeoutSec int    `toml:"timeout_sec"`
	} `toml:"ssh"`
	Poll struct {
		IntervalSec int `toml:"interval_sec"`
	} `toml:"poll"`
	Defaults struct {
		Datacenter string `toml:"datacenter"`
	} `toml:"defaults"`
}

// loadFleetSettings reads config/fleet/settings.toml if it exists.
func loadFleetSettings(home string) fleetSettingsFile {
	var s fleetSettingsFile
	f, err := os.Open(configPath(home, "fleet", "settings.toml"))
	if err != nil {
		return s
	}
	defer f.Close()
	_ = toml.NewDecoder(f).Decode(&s)
	return s
}

// runFleet runs the interactive wizard for config/fleet/settings.toml (Step 5).
func runFleet(home string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 5 — Fleet Settings  (config/fleet/settings.toml)      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("  SSH defaults used when chain.toml [management] fields are empty.")

	ex := loadFleetSettings(home)

	var s fleetSettingsFile

	section("SSH Defaults")
	s.SSH.User = readString("ssh.user", stringDefault(ex.SSH.User, "ubuntu"), true)
	s.SSH.KeyPath = readString("ssh.key_path (e.g. ~/.ssh/vprox_fleet)", ex.SSH.KeyPath, true)
	s.SSH.Port = readPort("ssh.port", portDefault(ex.SSH.Port, 22))

	section("Poll")
	defInterval := 60
	if ex.Poll.IntervalSec > 0 {
		defInterval = ex.Poll.IntervalSec
	}
	s.Poll.IntervalSec = readInt("poll.interval_sec (chain status refresh)", defInterval, 10, 3600)

	section("Defaults")
	s.Defaults.Datacenter = readString("defaults.datacenter (e.g. QC, RBX)", ex.Defaults.Datacenter, false)

	return writeConfig(configPath(home, "fleet", "settings.toml"), s)
}
