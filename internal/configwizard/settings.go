package configwizard

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// proxySettingsFile mirrors the shape of config/vprox/settings.toml.
// Kept local to the wizard to avoid a circular import into cmd/vprox.
type proxySettingsFile struct {
	RateLimit struct {
		RPS   float64 `toml:"rps"`
		Burst int     `toml:"burst"`
	} `toml:"rate_limit"`
	AutoQuarantine struct {
		Enabled      bool    `toml:"enabled"`
		Threshold    int     `toml:"threshold"`
		WindowSec    int     `toml:"window_sec"`
		PenaltyRPS   float64 `toml:"penalty_rps"`
		PenaltyBurst int     `toml:"penalty_burst"`
		TTLSec       int     `toml:"ttl_sec"`
	} `toml:"auto_quarantine"`
	Debug struct {
		Enabled bool `toml:"enabled"`
		Port    int  `toml:"port"`
	} `toml:"debug"`
}

// loadProxySettingsWizard loads config/vprox/settings.toml if it exists.
func loadProxySettingsWizard(home string) proxySettingsFile {
	var s proxySettingsFile
	data, err := os.ReadFile(filepath.Join(home, "config", "vprox", "settings.toml"))
	if err != nil {
		return s
	}
	_ = toml.Unmarshal(data, &s)
	return s
}

// runSettings runs the interactive wizard for config/vprox/settings.toml (Step 2).
func runSettings(home string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 2 — Proxy Settings  (config/vprox/settings.toml)      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println("  All fields are optional — zero values use built-in defaults.")

	ex := loadProxySettingsWizard(home)

	var s proxySettingsFile

	section("Rate Limiting")
	defRPS := 25.0
	if ex.RateLimit.RPS > 0 {
		defRPS = ex.RateLimit.RPS
	}
	defBurst := 100
	if ex.RateLimit.Burst > 0 {
		defBurst = ex.RateLimit.Burst
	}
	s.RateLimit.RPS = readFloat("rps (requests/sec per IP)", defRPS)
	s.RateLimit.Burst = readInt("burst (token bucket size)", defBurst, 1, 100000)

	section("Auto-Quarantine")
	fmt.Println("  Temporarily throttle IPs that exceed the threshold within the window.")
	defEnabled := true
	if ex.AutoQuarantine.Enabled {
		defEnabled = ex.AutoQuarantine.Enabled
	}
	s.AutoQuarantine.Enabled = readBool("enabled", defEnabled)
	if s.AutoQuarantine.Enabled {
		defThreshold := 120
		if ex.AutoQuarantine.Threshold > 0 {
			defThreshold = ex.AutoQuarantine.Threshold
		}
		defWindow := 10
		if ex.AutoQuarantine.WindowSec > 0 {
			defWindow = ex.AutoQuarantine.WindowSec
		}
		defPenaltyRPS := 1.0
		if ex.AutoQuarantine.PenaltyRPS > 0 {
			defPenaltyRPS = ex.AutoQuarantine.PenaltyRPS
		}
		defPenaltyBurst := 1
		if ex.AutoQuarantine.PenaltyBurst > 0 {
			defPenaltyBurst = ex.AutoQuarantine.PenaltyBurst
		}
		defTTL := 900
		if ex.AutoQuarantine.TTLSec > 0 {
			defTTL = ex.AutoQuarantine.TTLSec
		}
		s.AutoQuarantine.Threshold = readInt("threshold (requests in window)", defThreshold, 1, 0)
		s.AutoQuarantine.WindowSec = readInt("window_sec", defWindow, 1, 3600)
		s.AutoQuarantine.PenaltyRPS = readFloat("penalty_rps", defPenaltyRPS)
		s.AutoQuarantine.PenaltyBurst = readInt("penalty_burst", defPenaltyBurst, 1, 0)
		s.AutoQuarantine.TTLSec = readInt("ttl_sec (quarantine duration)", defTTL, 1, 86400)
	}

	section("Debug / pprof")
	fmt.Println("  Exposes /debug/pprof on a separate port. Never enable on public servers.")
	s.Debug.Enabled = readBool("debug.enabled", ex.Debug.Enabled)
	if s.Debug.Enabled {
		defDebugPort := 6060
		if ex.Debug.Port > 0 {
			defDebugPort = ex.Debug.Port
		}
		s.Debug.Port = readPort("debug.port", defDebugPort)
	}

	return writeConfig(configPath(home, "vprox", "settings.toml"), s)
}
