package configwizard

import (
	"fmt"

	vlogcfg "github.com/vNodesV/vProx/internal/vlog/config"
)

// runVLog runs the interactive wizard for config/vlog/vlog.toml (Step 4).
func runVLog(home string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 4 — vLog Config  (config/vlog/vlog.toml)             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// Load existing as defaults.
	ex, _ := vlogcfg.Load(configPath(home, "vlog", "vlog.toml"))

	var cfg vlogcfg.Config
	v := &cfg.VLog

	section("Group A — Server")
	defPort := 8889
	if ex.VLog.Port != 0 {
		defPort = ex.VLog.Port
	}
	v.Port = readPort("port", defPort)

	defBind := "127.0.0.1"
	if ex.VLog.BindAddress != "" {
		defBind = ex.VLog.BindAddress
	}
	v.BindAddress = readIP("bind_address", defBind, true)
	v.BasePath = readString("base_path (URL prefix, e.g. /vlog)", ex.VLog.BasePath, false)
	v.APIKey = readString("api_key (shared secret for /block,/unblock; empty=disabled)", ex.VLog.APIKey, false)
	if v.APIKey == "" {
		fmt.Println("  ⚠  api_key is empty — block/unblock endpoints will be disabled")
	}

	section("Group B — Auth")
	v.Auth.Username = readString("auth.username", stringDefault(ex.VLog.Auth.Username, "admin"), true)
	fmt.Println("  ℹ  Leave password empty to disable login (public access — not recommended).")
	v.Auth.PasswordHash = readPassword("auth.password")
	if v.Auth.PasswordHash == "" {
		fmt.Println("  ⚠  No password set — auth bypass enabled (backward compat)")
	}

	section("Group C — Intel API Keys (optional)")
	v.Intel.AutoEnrich = readBool("intel.auto_enrich", boolDefault(ex.VLog.Intel.AutoEnrich, true))
	defTTL := 24
	if ex.VLog.Intel.CacheTTLHours != 0 {
		defTTL = ex.VLog.Intel.CacheTTLHours
	}
	v.Intel.CacheTTLHours = readInt("intel.cache_ttl_hours", defTTL, 1, 8760)
	defRPM := 10
	if ex.VLog.Intel.RateLimitRPM != 0 {
		defRPM = ex.VLog.Intel.RateLimitRPM
	}
	v.Intel.RateLimitRPM = readInt("intel.rate_limit_rpm", defRPM, 1, 600)
	v.Intel.Keys.AbuseIPDB = readString("intel.keys.abuseipdb (optional)", ex.VLog.Intel.Keys.AbuseIPDB, false)
	v.Intel.Keys.VirusTotal = readString("intel.keys.virustotal (optional)", ex.VLog.Intel.Keys.VirusTotal, false)
	v.Intel.Keys.Shodan = readString("intel.keys.shodan (optional)", ex.VLog.Intel.Keys.Shodan, false)

	section("Group D — Fleet Push Defaults")
	v.Push.Defaults.User = readString("push.defaults.user (SSH user)", ex.VLog.Push.Defaults.User, false)
	v.Push.Defaults.KeyPath = readString("push.defaults.key_path (SSH key)", ex.VLog.Push.Defaults.KeyPath, false)
	defPoll := 60
	if ex.VLog.Push.PollIntervalSec != 0 {
		defPoll = ex.VLog.Push.PollIntervalSec
	}
	v.Push.PollIntervalSec = readInt("push.poll_interval_sec", defPoll, 10, 3600)

	section("Group E — Paths (optional overrides)")
	v.DBPath = readString("db_path (default: $VPROX_HOME/data/vlog.db)", ex.VLog.DBPath, false)
	v.ArchivesDir = readString("archives_dir (default: $VPROX_HOME/data/logs/archives)", ex.VLog.ArchivesDir, false)
	v.VProxBin = readString("vprox_bin (default: vprox)", stringDefault(ex.VLog.VProxBin, "vprox"), false)

	return writeConfig(configPath(home, "vlog", "vlog.toml"), cfg)
}

func stringDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
