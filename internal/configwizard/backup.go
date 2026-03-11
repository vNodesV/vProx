package configwizard

import (
	"fmt"
	"path/filepath"

	"github.com/vNodesV/vProx/internal/backup"
)

// runBackup runs the interactive wizard for config/backup/backup.toml (Step 7).
func runBackup(home string) error {
	fmt.Println("\n╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Step 7 — Backup Config  (config/backup/backup.toml)        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	path := filepath.Join(home, "config", "backup", "backup.toml")
	ex, _, _ := backup.LoadConfig(path)

	cfg := backup.BackupConfig{}
	b := &cfg.Backup

	section("Automation")
	b.Automation = readBool("automation (enable scheduled backups)", ex.Backup.Automation)

	section("Triggers")
	defDays := 7
	if ex.Backup.IntervalDays > 0 {
		defDays = ex.Backup.IntervalDays
	}
	b.IntervalDays = readInt("interval_days (0 = disabled)", defDays, 0, 365)

	defMaxMB := int64(100)
	if ex.Backup.MaxSizeMB > 0 {
		defMaxMB = ex.Backup.MaxSizeMB
	}
	rawMB := readInt("max_size_mb (0 = disabled)", int(defMaxMB), 0, 10240)
	b.MaxSizeMB = int64(rawMB)

	defCheckMin := 10
	if ex.Backup.CheckIntervalMin > 0 {
		defCheckMin = ex.Backup.CheckIntervalMin
	}
	b.CheckIntervalMin = readInt("check_interval_min (how often to evaluate triggers)", defCheckMin, 1, 1440)

	section("Destination")
	fmt.Println("  Default: $VPROX_HOME/data/logs/archives")
	b.Destination = readString("destination (override archive dir; empty = default)", ex.Backup.Destination, false)
	b.Compression = stringDefault(ex.Backup.Compression, "tar.gz")

	section("Files to Include")
	defLogs := ex.Backup.Files.Logs
	if len(defLogs) == 0 {
		defLogs = []string{"main.log"}
	}
	b.Files.Logs = readStringList("files.logs (relative to $VPROX_HOME/data/logs/)", defLogs)

	defData := ex.Backup.Files.Data
	if len(defData) == 0 {
		defData = []string{"access-counts.json"}
	}
	b.Files.Data = readStringList("files.data (relative to $VPROX_HOME/data/)", defData)

	defConfig := ex.Backup.Files.Config
	if len(defConfig) == 0 {
		defConfig = []string{"chains/ports.toml"}
	}
	b.Files.Config = readStringList("files.config (relative to $VPROX_HOME/config/)", defConfig)

	return writeConfig(configPath(home, "backup", "backup.toml"), cfg)
}
