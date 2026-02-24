package backup

import (
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// BackupConfig is the top-level structure for backup.toml.
type BackupConfig struct {
	Backup BackupSection `toml:"backup"`
}

// BackupSection holds all backup settings.
type BackupSection struct {
	// Automation enables scheduled (automatic) backup runs.
	// true  → vProx starts the background scheduler on startup (equivalent to old method="auto").
	// false → backups only run when `vprox --backup` is invoked manually.
	// The --backup CLI flag always triggers a backup regardless of this setting.
	Automation bool `toml:"automation"`

	// Compression archive format. Only "tar.gz" is currently supported.
	Compression string `toml:"compression"`

	// Destination overrides the archive output directory.
	// Default: $VPROX_HOME/data/logs/archives
	Destination string `toml:"destination"`

	// IntervalDays triggers a backup every N days. 0 = disabled.
	IntervalDays int `toml:"interval_days"`

	// MaxSizeMB triggers a backup when main.log exceeds N MB. 0 = disabled.
	MaxSizeMB int64 `toml:"max_size_mb"`

	// CheckIntervalMin is how often (minutes) the auto loop evaluates triggers.
	CheckIntervalMin int `toml:"check_interval_min"`

	// Files lists which files to include in the archive.
	Files FilesSection `toml:"files"`
}

// FilesSection lists files to include by directory scope.
type FilesSection struct {
	// Logs: filenames relative to $VPROX_HOME/data/logs/
	Logs []string `toml:"logs"`

	// Data: filenames relative to $VPROX_HOME/data/
	Data []string `toml:"data"`

	// Config: filenames relative to $VPROX_HOME/config/
	Config []string `toml:"config"`
}

// DefaultConfig returns the built-in defaults:
// automation=true, compression=tar.gz, main.log + access-counts.json included.
func DefaultConfig() BackupConfig {
	return BackupConfig{
		Backup: BackupSection{
			Automation:       true,
			Compression:      "tar.gz",
			CheckIntervalMin: 10,
			Files: FilesSection{
				Logs: []string{"main.log"},
				Data: []string{"access-counts.json"},
			},
		},
	}
}

// LoadConfig loads backup.toml from path.
// Returns (defaults, false, nil) when the file is absent — backward compatible.
// Returns (cfg, true, nil) when loaded successfully.
// Returns (defaults, false, err) on parse errors.
func LoadConfig(path string) (BackupConfig, bool, error) {
	cfg := DefaultConfig()

	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, false, nil
		}
		return cfg, false, err
	}

	if err := toml.Unmarshal(b, &cfg); err != nil {
		return DefaultConfig(), false, err
	}

	// Apply defaults for any zero-value fields after decode.
	if strings.TrimSpace(cfg.Backup.Compression) == "" {
		cfg.Backup.Compression = "tar.gz"
	}
	if cfg.Backup.CheckIntervalMin <= 0 {
		cfg.Backup.CheckIntervalMin = 10
	}
	// If the operator left the file list completely empty, apply defaults.
	if len(cfg.Backup.Files.Logs) == 0 && len(cfg.Backup.Files.Data) == 0 && len(cfg.Backup.Files.Config) == 0 {
		cfg.Backup.Files.Logs = []string{"main.log"}
		cfg.Backup.Files.Data = []string{"access-counts.json"}
	}

	return cfg, true, nil
}
