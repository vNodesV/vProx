package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vNodesV/vProx/internal/vlog/config"
)

func TestDefaultConfig(t *testing.T) {
	home := "/tmp/vprox-test"
	cfg := config.DefaultConfig(home)

	if cfg.VLog.Port != 8889 {
		t.Errorf("default port: got %d, want 8889", cfg.VLog.Port)
	}
	if cfg.VLog.DBPath != filepath.Join(home, "data", "vlog.db") {
		t.Errorf("default db_path: got %q", cfg.VLog.DBPath)
	}
	if cfg.VLog.ArchivesDir != filepath.Join(home, "data", "logs", "archives") {
		t.Errorf("default archives_dir: got %q", cfg.VLog.ArchivesDir)
	}
	if cfg.VLog.WatchIntervalSec != 60 {
		t.Errorf("default watch_interval_sec: got %d, want 60", cfg.VLog.WatchIntervalSec)
	}
	if !cfg.VLog.Intel.AutoEnrich {
		t.Error("default auto_enrich should be true")
	}
	if cfg.VLog.Intel.CacheTTLHours != 24 {
		t.Errorf("default cache_ttl_hours: got %d, want 24", cfg.VLog.Intel.CacheTTLHours)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := config.Load("/nonexistent/path/vlog.toml")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	// Should return defaults
	if cfg.VLog.Port != 8889 {
		t.Errorf("missing file should return default port 8889, got %d", cfg.VLog.Port)
	}
}

func TestLoadToml(t *testing.T) {
	toml := `[vlog]
port = 9999
watch_interval_sec = 30

[vlog.intel]
auto_enrich = false
cache_ttl_hours = 48
`
	f, err := os.CreateTemp(t.TempDir(), "vlog*.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(toml); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cfg, err := config.Load(f.Name())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.VLog.Port != 9999 {
		t.Errorf("port: got %d, want 9999", cfg.VLog.Port)
	}
	if cfg.VLog.WatchIntervalSec != 30 {
		t.Errorf("watch_interval_sec: got %d, want 30", cfg.VLog.WatchIntervalSec)
	}
	if cfg.VLog.Intel.AutoEnrich {
		t.Error("auto_enrich should be false after explicit TOML override")
	}
	if cfg.VLog.Intel.CacheTTLHours != 48 {
		t.Errorf("cache_ttl_hours: got %d, want 48", cfg.VLog.Intel.CacheTTLHours)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{"valid default", func(c *config.Config) {}, false},
		{"port 0", func(c *config.Config) { c.VLog.Port = 0 }, true},
		{"port 65536", func(c *config.Config) { c.VLog.Port = 65536 }, true},
		{"watch 0", func(c *config.Config) { c.VLog.WatchIntervalSec = 0 }, true},
		{"cache_ttl 0", func(c *config.Config) { c.VLog.Intel.CacheTTLHours = 0 }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig("/tmp/x")
			tt.mutate(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFindHome(t *testing.T) {
	os.Unsetenv("VPROX_HOME")
	home := config.FindHome()
	if home == "" {
		t.Error("FindHome should never return empty string")
	}

	os.Setenv("VPROX_HOME", "/custom/path")
	defer os.Unsetenv("VPROX_HOME")
	home = config.FindHome()
	if home != "/custom/path" {
		t.Errorf("FindHome with VPROX_HOME set: got %q, want /custom/path", home)
	}
}
