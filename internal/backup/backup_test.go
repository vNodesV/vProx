package backup

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

// Manager interface cannot be satisfied by package-level functions directly,
// but verify the interface is well-formed.
func TestManagerInterfaceCompile(t *testing.T) {
	t.Parallel()
	// This test ensures the Manager interface is importable and usable.
	var _ Manager = managerAdapter{}
}

type managerAdapter struct{}

func (managerAdapter) RunOnce(opts Options) error                         { return RunOnce(opts) }
func (managerAdapter) StartAuto(opts Options) (func(), error)             { return StartAuto(opts) }
func (managerAdapter) LoadConfig(path string) (BackupConfig, bool, error) { return LoadConfig(path) }

// ---------------------------------------------------------------------------
// LoadConfig
// ---------------------------------------------------------------------------

func TestLoadConfigMissingFile(t *testing.T) {
	t.Parallel()
	cfg, loaded, err := LoadConfig("/nonexistent/backup.toml")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if loaded {
		t.Error("expected loaded=false for missing file")
	}
	// Should return defaults
	if !cfg.Backup.Automation {
		t.Error("default Automation should be true")
	}
	if cfg.Backup.Compression != "tar.gz" {
		t.Errorf("default Compression = %q, want tar.gz", cfg.Backup.Compression)
	}
}

func TestLoadConfigValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.toml")
	toml := `[backup]
automation = false
compression = "tar.gz"
destination = "/tmp/backups"
interval_days = 7
max_size_mb = 100
check_interval_min = 15

[backup.files]
logs = ["main.log", "rate-limit.jsonl"]
data = ["access-counts.json"]
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !loaded {
		t.Error("expected loaded=true")
	}
	if cfg.Backup.Automation {
		t.Error("Automation should be false")
	}
	if cfg.Backup.Destination != "/tmp/backups" {
		t.Errorf("Destination = %q", cfg.Backup.Destination)
	}
	if cfg.Backup.IntervalDays != 7 {
		t.Errorf("IntervalDays = %d, want 7", cfg.Backup.IntervalDays)
	}
	if len(cfg.Backup.Files.Logs) != 2 {
		t.Errorf("Files.Logs len = %d, want 2", len(cfg.Backup.Files.Logs))
	}
}

func TestLoadConfigInvalidTOML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.toml")
	if err := os.WriteFile(path, []byte("{{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadConfig(path)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadConfigEmptyFileLists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.toml")
	toml := `[backup]
automation = true
compression = "tar.gz"
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	// Should default to main.log and access-counts.json
	if len(cfg.Backup.Files.Logs) == 0 {
		t.Error("expected default Files.Logs when empty")
	}
}

func TestLoadConfigDefaultCompression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.toml")
	toml := `[backup]
automation = true
compression = ""
[backup.files]
logs = ["main.log"]
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backup.Compression != "tar.gz" {
		t.Errorf("empty compression should default to tar.gz, got %q", cfg.Backup.Compression)
	}
}

func TestLoadConfigDefaultCheckInterval(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.toml")
	toml := `[backup]
automation = true
check_interval_min = 0
[backup.files]
logs = ["main.log"]
`
	if err := os.WriteFile(path, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backup.CheckIntervalMin != 10 {
		t.Errorf("zero CheckIntervalMin should default to 10, got %d", cfg.Backup.CheckIntervalMin)
	}
}

// ---------------------------------------------------------------------------
// DefaultConfig
// ---------------------------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	if !cfg.Backup.Automation {
		t.Error("default Automation should be true")
	}
	if cfg.Backup.Compression != "tar.gz" {
		t.Errorf("default Compression = %q", cfg.Backup.Compression)
	}
	if cfg.Backup.CheckIntervalMin != 10 {
		t.Errorf("default CheckIntervalMin = %d", cfg.Backup.CheckIntervalMin)
	}
	if len(cfg.Backup.Files.Logs) == 0 {
		t.Error("default Files.Logs should not be empty")
	}
}

// ---------------------------------------------------------------------------
// RunOnce
// ---------------------------------------------------------------------------

func TestRunOnceMissingLogPath(t *testing.T) {
	t.Parallel()
	err := RunOnce(Options{})
	if err == nil {
		t.Error("expected error for missing LogPath")
	}
}

func TestRunOnceMissingArchiveDir(t *testing.T) {
	t.Parallel()
	err := RunOnce(Options{LogPath: "/tmp/test.log"})
	if err == nil {
		t.Error("expected error for missing ArchiveDir")
	}
}

func TestRunOnceNothingToArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "empty.log")
	archiveDir := filepath.Join(dir, "archives")

	// Create empty log file (should be skipped)
	if err := os.WriteFile(logPath, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	err := RunOnce(Options{
		LogPath:    logPath,
		ArchiveDir: archiveDir,
		Now:        func() time.Time { return time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("RunOnce empty log: %v", err)
	}
}

func TestRunOnceCreatesArchive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "main.log")
	archiveDir := filepath.Join(dir, "archives")

	// Create log dir and log file with content
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("log line 1\nlog line 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fixedTime := time.Date(2025, 1, 15, 10, 30, 45, 0, time.UTC)
	err := RunOnce(Options{
		LogPath:    logPath,
		ArchiveDir: archiveDir,
		Now:        func() time.Time { return fixedTime },
		Method:     "MANUAL",
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	// Verify archive was created
	expected := filepath.Join(archiveDir, "backup.20250115_103045.tar.gz")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("archive not found: %v", err)
	}

	// Verify archive contents
	entries := readTarGzEntries(t, expected)
	if len(entries) == 0 {
		t.Fatal("archive is empty")
	}
	found := false
	for _, e := range entries {
		if e == "main.log" {
			found = true
		}
	}
	if !found {
		t.Errorf("main.log not found in archive, entries: %v", entries)
	}

	// Verify source log was truncated
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile after truncation: %v", err)
	}
	if len(b) != 0 {
		t.Errorf("log should be truncated after backup, got %d bytes", len(b))
	}
}

func TestRunOnceWithExtraFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "main.log")
	extraPath := filepath.Join(dir, "data", "access-counts.json")
	archiveDir := filepath.Join(dir, "archives")

	os.MkdirAll(filepath.Dir(logPath), 0o755)
	os.MkdirAll(filepath.Dir(extraPath), 0o755)
	os.WriteFile(logPath, []byte("main log data"), 0o644)
	os.WriteFile(extraPath, []byte(`{"1.2.3.4": 5}`), 0o644)

	fixedTime := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	err := RunOnce(Options{
		LogPath:    logPath,
		ArchiveDir: archiveDir,
		ExtraFiles: []string{extraPath},
		Now:        func() time.Time { return fixedTime },
	})
	if err != nil {
		t.Fatalf("RunOnce with extras: %v", err)
	}

	expected := filepath.Join(archiveDir, "backup.20250601_120000.tar.gz")
	entries := readTarGzEntries(t, expected)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(entries), entries)
	}

	// ExtraFiles should NOT be truncated
	b, err := os.ReadFile(extraPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"1.2.3.4": 5}` {
		t.Error("ExtraFiles should not be truncated")
	}
}

func TestRunOnceStatePath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "main.log")
	archiveDir := filepath.Join(dir, "archives")
	statePath := filepath.Join(dir, "state", "last_run")

	os.MkdirAll(filepath.Dir(logPath), 0o755)
	os.WriteFile(logPath, []byte("data"), 0o644)

	fixedTime := time.Date(2025, 3, 15, 8, 0, 0, 0, time.UTC)
	err := RunOnce(Options{
		LogPath:    logPath,
		ArchiveDir: archiveDir,
		StatePath:  statePath,
		Now:        func() time.Time { return fixedTime },
	})
	if err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("state file not created: %v", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		t.Error("state file should contain timestamp")
	}
}

// ---------------------------------------------------------------------------
// StartAuto
// ---------------------------------------------------------------------------

func TestStartAutoNoTriggers(t *testing.T) {
	t.Parallel()
	stop, err := StartAuto(Options{
		IntervalDays: 0,
		MaxBytes:     0,
	})
	if err != nil {
		t.Fatalf("StartAuto: %v", err)
	}
	stop() // should not panic
}

// ---------------------------------------------------------------------------
// humanSize
// ---------------------------------------------------------------------------

func TestHumanSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.00KB"},
		{1048576, "1.00MB"},
		{1073741824, "1.00GB"},
		{-1, "0B"},
	}
	for _, tt := range tests {
		got := humanSize(tt.bytes)
		if got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// containsPath
// ---------------------------------------------------------------------------

func TestContainsPath(t *testing.T) {
	t.Parallel()
	paths := []string{"/tmp/a.log", "/tmp/b.log"}
	if !containsPath(paths, "/tmp/a.log") {
		t.Error("should contain /tmp/a.log")
	}
	if containsPath(paths, "/tmp/c.log") {
		t.Error("should not contain /tmp/c.log")
	}
}

// ---------------------------------------------------------------------------
// shouldBackup
// ---------------------------------------------------------------------------

func TestShouldBackup(t *testing.T) {
	t.Parallel()

	t.Run("empty LogPath", func(t *testing.T) {
		t.Parallel()
		ok, _, err := shouldBackup(Options{})
		if err == nil {
			t.Error("expected error for empty LogPath")
		}
		if ok {
			t.Error("expected false")
		}
	})

	t.Run("interval triggered", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logPath := filepath.Join(dir, "main.log")
		os.WriteFile(logPath, []byte("data"), 0o644)

		ok, reason, err := shouldBackup(Options{
			LogPath:      logPath,
			IntervalDays: 1,
			StatePath:    filepath.Join(dir, "state"), // doesn't exist = zero last_run
			Now:          func() time.Time { return time.Now() },
		})
		if err != nil {
			t.Fatal(err)
		}
		if !ok || reason != "interval" {
			t.Errorf("got ok=%v reason=%q, want true/interval", ok, reason)
		}
	})

	t.Run("size triggered", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logPath := filepath.Join(dir, "main.log")
		os.WriteFile(logPath, []byte("lots of data"), 0o644)

		ok, reason, err := shouldBackup(Options{
			LogPath:  logPath,
			MaxBytes: 5, // file is larger than 5 bytes
		})
		if err != nil {
			t.Fatal(err)
		}
		if !ok || reason != "size" {
			t.Errorf("got ok=%v reason=%q, want true/size", ok, reason)
		}
	})

	t.Run("not triggered", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		logPath := filepath.Join(dir, "main.log")
		os.WriteFile(logPath, []byte("data"), 0o644)

		ok, reason, err := shouldBackup(Options{
			LogPath:  logPath,
			MaxBytes: 1024 * 1024, // file is well under this
		})
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Errorf("should not trigger, got reason=%q", reason)
		}
	})
}

// ---------------------------------------------------------------------------
// fileSize
// ---------------------------------------------------------------------------

func TestFileSize(t *testing.T) {
	t.Parallel()

	t.Run("existing file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		p := filepath.Join(dir, "file.txt")
		os.WriteFile(p, []byte("hello"), 0o644)
		size, err := fileSize(p)
		if err != nil {
			t.Fatal(err)
		}
		if size != 5 {
			t.Errorf("fileSize = %d, want 5", size)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		t.Parallel()
		size, err := fileSize("/nonexistent/file.txt")
		if err != nil {
			t.Fatal(err)
		}
		if size != 0 {
			t.Errorf("fileSize = %d, want 0", size)
		}
	})
}

// ---------------------------------------------------------------------------
// copyFile
// ---------------------------------------------------------------------------

func TestCopyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	os.WriteFile(src, []byte("copy me"), 0o644)

	if err := copyFile(src, dst, 0o644); err != nil {
		t.Fatalf("copyFile: %v", err)
	}

	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "copy me" {
		t.Errorf("dst content = %q", string(b))
	}
}

func TestCopyFileMissingSrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := copyFile("/nonexistent/src.txt", filepath.Join(dir, "dst.txt"), 0o644)
	if err == nil {
		t.Error("expected error for missing source")
	}
}

// ---------------------------------------------------------------------------
// nowOrDefault
// ---------------------------------------------------------------------------

func TestNowOrDefault(t *testing.T) {
	t.Parallel()

	t.Run("nil uses time.Now", func(t *testing.T) {
		t.Parallel()
		f := nowOrDefault(nil)
		now := f()
		if now.IsZero() {
			t.Error("should return current time")
		}
	})

	t.Run("custom function", func(t *testing.T) {
		t.Parallel()
		fixed := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		f := nowOrDefault(func() time.Time { return fixed })
		if got := f(); !got.Equal(fixed) {
			t.Errorf("got %v, want %v", got, fixed)
		}
	})
}

// ---------------------------------------------------------------------------
// readLastRun / writeLastRun
// ---------------------------------------------------------------------------

func TestReadWriteLastRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "last_run")

	// Read from nonexistent returns zero
	if got := readLastRun(path); !got.IsZero() {
		t.Errorf("expected zero time, got %v", got)
	}

	// Read from empty path returns zero
	if got := readLastRun(""); !got.IsZero() {
		t.Error("expected zero for empty path")
	}

	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := writeLastRun(path, now); err != nil {
		t.Fatalf("writeLastRun: %v", err)
	}

	got := readLastRun(path)
	if got.Unix() != now.Unix() {
		t.Errorf("readLastRun = %v, want %v", got, now)
	}
}

// ---------------------------------------------------------------------------
// writeTarGz — verify archive integrity
// ---------------------------------------------------------------------------

func TestWriteTarGz(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Create source files
	src1 := filepath.Join(dir, "file1.txt")
	src2 := filepath.Join(dir, "file2.txt")
	os.WriteFile(src1, []byte("hello"), 0o644)
	os.WriteFile(src2, []byte("world"), 0o644)

	tarPath := filepath.Join(dir, "test.tar.gz")
	entries := []archiveEntry{
		{SrcPath: src1, Name: "file1.txt"},
		{SrcPath: src2, Name: "file2.txt"},
	}

	if err := writeTarGz(entries, tarPath); err != nil {
		t.Fatalf("writeTarGz: %v", err)
	}

	names := readTarGzEntries(t, tarPath)
	if len(names) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(names))
	}
}

func TestWriteTarGzMissingSrc(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "test.tar.gz")
	entries := []archiveEntry{
		{SrcPath: "/nonexistent/file.txt", Name: "file.txt"},
	}
	if err := writeTarGz(entries, tarPath); err == nil {
		t.Error("expected error for missing source file")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func readTarGzEntries(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names = append(names, hdr.Name)
	}
	return names
}
