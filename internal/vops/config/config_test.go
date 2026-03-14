package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vNodesV/vProx/internal/vops/config"
)

func TestDefaultConfig(t *testing.T) {
	home := "/tmp/vprox-test"
	cfg := config.DefaultConfig(home)

	if cfg.VOps.Port != 8889 {
		t.Errorf("default port: got %d, want 8889", cfg.VOps.Port)
	}
	if cfg.VOps.DBPath != filepath.Join(home, "data", "vops.db") {
		t.Errorf("default db_path: got %q", cfg.VOps.DBPath)
	}
	if cfg.VOps.ArchivesDir != filepath.Join(home, "data", "logs", "archives") {
		t.Errorf("default archives_dir: got %q", cfg.VOps.ArchivesDir)
	}
	if cfg.VOps.WatchIntervalSec != 60 {
		t.Errorf("default watch_interval_sec: got %d, want 60", cfg.VOps.WatchIntervalSec)
	}
	if !cfg.VOps.Intel.AutoEnrich {
		t.Error("default auto_enrich should be true")
	}
	if cfg.VOps.Intel.CacheTTLHours != 24 {
		t.Errorf("default cache_ttl_hours: got %d, want 24", cfg.VOps.Intel.CacheTTLHours)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := config.Load("/nonexistent/path/vops.toml")
	if err != nil {
		t.Fatalf("missing file should not error, got: %v", err)
	}
	// Should return defaults
	if cfg.VOps.Port != 8889 {
		t.Errorf("missing file should return default port 8889, got %d", cfg.VOps.Port)
	}
}

func TestLoadToml(t *testing.T) {
	toml := `[vops]
port = 9999
watch_interval_sec = 30

[vops.intel]
auto_enrich = false
cache_ttl_hours = 48
`
	f, err := os.CreateTemp(t.TempDir(), "vops*.toml")
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
	if cfg.VOps.Port != 9999 {
		t.Errorf("port: got %d, want 9999", cfg.VOps.Port)
	}
	if cfg.VOps.WatchIntervalSec != 30 {
		t.Errorf("watch_interval_sec: got %d, want 30", cfg.VOps.WatchIntervalSec)
	}
	if cfg.VOps.Intel.AutoEnrich {
		t.Error("auto_enrich should be false after explicit TOML override")
	}
	if cfg.VOps.Intel.CacheTTLHours != 48 {
		t.Errorf("cache_ttl_hours: got %d, want 48", cfg.VOps.Intel.CacheTTLHours)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{"valid default", func(c *config.Config) {}, false},
		{"port 0", func(c *config.Config) { c.VOps.Port = 0 }, true},
		{"port 65536", func(c *config.Config) { c.VOps.Port = 65536 }, true},
		{"watch 0", func(c *config.Config) { c.VOps.WatchIntervalSec = 0 }, true},
		{"cache_ttl 0", func(c *config.Config) { c.VOps.Intel.CacheTTLHours = 0 }, true},
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

// ---------------------------------------------------------------------------
// Additional coverage tests
// ---------------------------------------------------------------------------

func TestLoadInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("{{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadBackfillDefaults(t *testing.T) {
	// TOML with only partial settings — verify backfill
	toml := `[vops]
port = 0
db_path = ""
archives_dir = ""
watch_interval_sec = 0
bind_address = ""

[vops.intel]
cache_ttl_hours = 0
rate_limit_rpm = 0

[vops.server]
read_timeout_sec = 0
write_timeout_sec = 0

[vops.auth]
username = ""
`
	dir := t.TempDir()
	f := filepath.Join(dir, "vops.toml")
	os.WriteFile(f, []byte(toml), 0o644)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.VOps.Port != 8889 {
		t.Errorf("Port should be backfilled to 8889, got %d", cfg.VOps.Port)
	}
	if cfg.VOps.WatchIntervalSec != 60 {
		t.Errorf("WatchIntervalSec should be 60, got %d", cfg.VOps.WatchIntervalSec)
	}
	if cfg.VOps.Intel.CacheTTLHours != 24 {
		t.Errorf("CacheTTLHours should be 24, got %d", cfg.VOps.Intel.CacheTTLHours)
	}
	if cfg.VOps.Intel.RateLimitRPM != 10 {
		t.Errorf("RateLimitRPM should be 10, got %d", cfg.VOps.Intel.RateLimitRPM)
	}
	if cfg.VOps.Server.ReadTimeoutSec != 30 {
		t.Errorf("ReadTimeoutSec should be 30, got %d", cfg.VOps.Server.ReadTimeoutSec)
	}
	if cfg.VOps.Server.WriteTimeoutSec != 30 {
		t.Errorf("WriteTimeoutSec should be 30, got %d", cfg.VOps.Server.WriteTimeoutSec)
	}
	if cfg.VOps.Auth.Username != "admin" {
		t.Errorf("Username should be admin, got %q", cfg.VOps.Auth.Username)
	}
	if cfg.VOps.BindAddress != "127.0.0.1" {
		t.Errorf("BindAddress should be 127.0.0.1, got %q", cfg.VOps.BindAddress)
	}
}

func TestLoadBasePathNormalize(t *testing.T) {
	toml := `[vops]
base_path = "  /vops/  "
`
	dir := t.TempDir()
	f := filepath.Join(dir, "vops.toml")
	os.WriteFile(f, []byte(toml), 0o644)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VOps.BasePath != "/vops" {
		t.Errorf("BasePath = %q, want /vops (trailing slash trimmed)", cfg.VOps.BasePath)
	}
}

func TestLoadAPIKeys(t *testing.T) {
	toml := `[vops]
api_key = "test-api-key"

[vops.intel.keys]
virustotal = "vt-key"
abuseipdb = "abuse-key"
shodan = "shodan-key"
`
	dir := t.TempDir()
	f := filepath.Join(dir, "vops.toml")
	os.WriteFile(f, []byte(toml), 0o644)

	cfg, err := config.Load(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VOps.APIKey != "test-api-key" {
		t.Errorf("APIKey = %q", cfg.VOps.APIKey)
	}
	if cfg.VOps.Intel.Keys.VirusTotal != "vt-key" {
		t.Errorf("VirusTotal key = %q", cfg.VOps.Intel.Keys.VirusTotal)
	}
	if cfg.VOps.Intel.Keys.AbuseIPDB != "abuse-key" {
		t.Errorf("AbuseIPDB key = %q", cfg.VOps.Intel.Keys.AbuseIPDB)
	}
	if cfg.VOps.Intel.Keys.Shodan != "shodan-key" {
		t.Errorf("Shodan key = %q", cfg.VOps.Intel.Keys.Shodan)
	}
}

func TestValidateAdditional(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.Config)
		wantErr bool
	}{
		{"negative port", func(c *config.Config) { c.VOps.Port = -1 }, true},
		{"max valid port", func(c *config.Config) { c.VOps.Port = 65535 }, false},
		{"negative watch", func(c *config.Config) { c.VOps.WatchIntervalSec = -5 }, true},
		{"negative cache_ttl", func(c *config.Config) { c.VOps.Intel.CacheTTLHours = -1 }, true},
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
