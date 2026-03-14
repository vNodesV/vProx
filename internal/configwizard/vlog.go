package configwizard

import (
	"fmt"

	vopscfg "github.com/vNodesV/vProx/internal/vops/config"
)

// runVOps runs the interactive wizard for config/vops/vops.toml (Step 4).
func runVOps(home string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 4 — vOps Config  (config/vops/vops.toml)             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Load existing as defaults.
	ex, _ := vopscfg.Load(configPath(home, "vops", "vops.toml"))

	var cfg vopscfg.Config
	v := &cfg.VOps

	section("Group A — Server")
	defPort := 8889
	if ex.VOps.Port != 0 {
		defPort = ex.VOps.Port
	}
	v.Port = readPort("port", defPort)

	defBind := "127.0.0.1"
	if ex.VOps.BindAddress != "" {
		defBind = ex.VOps.BindAddress
	}
	v.BindAddress = readIP("bind_address", defBind, true)
	v.BasePath = readString("base_path (URL prefix, e.g. /vops)", ex.VOps.BasePath, false)
	v.APIKey = readString("api_key (shared secret for /block,/unblock; empty=disabled)", ex.VOps.APIKey, false)
	if v.APIKey == "" {
		fmt.Println("  ⚠  api_key is empty — block/unblock endpoints will be disabled")
	}

	section("Group B — Auth")
	v.Auth.Username = readString("auth.username", stringDefault(ex.VOps.Auth.Username, "admin"), true)
	fmt.Println("  ℹ  Leave password empty to disable login (public access — not recommended).")
	v.Auth.PasswordHash = readPassword("auth.password")
	if v.Auth.PasswordHash == "" {
		fmt.Println("  ⚠  No password set — auth bypass enabled (backward compat)")
	}

	section("Group C — Intel API Keys (optional)")
	v.Intel.AutoEnrich = readBool("intel.auto_enrich", boolDefault(ex.VOps.Intel.AutoEnrich, true))
	defTTL := 24
	if ex.VOps.Intel.CacheTTLHours != 0 {
		defTTL = ex.VOps.Intel.CacheTTLHours
	}
	v.Intel.CacheTTLHours = readInt("intel.cache_ttl_hours", defTTL, 1, 8760)
	defRPM := 10
	if ex.VOps.Intel.RateLimitRPM != 0 {
		defRPM = ex.VOps.Intel.RateLimitRPM
	}
	v.Intel.RateLimitRPM = readInt("intel.rate_limit_rpm", defRPM, 1, 600)
	v.Intel.Keys.AbuseIPDB = readString("intel.keys.abuseipdb (optional)", ex.VOps.Intel.Keys.AbuseIPDB, false)
	v.Intel.Keys.VirusTotal = readString("intel.keys.virustotal (optional)", ex.VOps.Intel.Keys.VirusTotal, false)
	v.Intel.Keys.Shodan = readString("intel.keys.shodan (optional)", ex.VOps.Intel.Keys.Shodan, false)

	section("Group D — Fleet Push Defaults")
	v.Push.Defaults.User = readString("push.defaults.user (SSH user)", ex.VOps.Push.Defaults.User, false)
	v.Push.Defaults.KeyPath = readString("push.defaults.key_path (SSH key)", ex.VOps.Push.Defaults.KeyPath, false)
	defPoll := 60
	if ex.VOps.Push.PollIntervalSec != 0 {
		defPoll = ex.VOps.Push.PollIntervalSec
	}
	v.Push.PollIntervalSec = readInt("push.poll_interval_sec", defPoll, 10, 3600)

	section("Group E — Paths (optional overrides)")
	v.DBPath = readString("db_path (default: $VPROX_HOME/data/vops.db)", ex.VOps.DBPath, false)
	v.ArchivesDir = readString("archives_dir (default: $VPROX_HOME/data/logs/archives)", ex.VOps.ArchivesDir, false)
	v.VProxBin = readString("vprox_bin (default: vprox)", stringDefault(ex.VOps.VProxBin, "vprox"), false)

	return writeConfig(configPath(home, "vops", "vops.toml"), cfg)
}

func stringDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
