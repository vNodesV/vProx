package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	applog "github.com/vNodesV/vProx/internal/logging"
)

const defaultCompression = "tar.gz"

// Options controls backup behavior.
type Options struct {
	// LogPath is the primary log file (main.log). It is snapshotted and truncated.
	LogPath string

	// ArchiveDir is the output directory for archives.
	ArchiveDir string

	// StatePath stores the last-run timestamp for interval-based triggering.
	StatePath string

	IntervalDays  int
	MaxBytes      int64
	CheckInterval time.Duration
	Now           func() time.Time

	// Method is "AUTO" or "MANUAL" — written to the log line.
	Method string

	// RotateExtra is a list of absolute paths that are snapshotted AND
	// truncated after snapshot (copy-truncate), just like LogPath.
	// Use for chain-specific *.log files discovered in data/logs/.
	RotateExtra []string

	// ExtraFiles is a list of absolute paths to include in the archive
	// (snapshotted but NOT truncated — e.g. access-counts.json).
	ExtraFiles []string

	// ListSource is "loaded" (from backup.toml) or "default" (built-in).
	ListSource string
}

// newBupID generates a BUP-prefixed correlation ID matching the log format.
// Format: BUP + 24 uppercase hex chars (e.g. BUP70B489B8891A01531C223422).
func newBupID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("BUP%d", time.Now().UTC().UnixNano())
	}
	return "BUP" + strings.ToUpper(hex.EncodeToString(buf))
}

// archiveEntry pairs a source path with its name inside the archive.
type archiveEntry struct {
	SrcPath string
	Name    string
}

// RunOnce performs a single backup run.
//
// Behavior:
//  1. Collect all source files (LogPath + ExtraFiles that exist)
//  2. Compute total pre-compressed size
//  3. Snapshot all files to temp copies
//  4. Emit NEW STARTED log line
//  5. Compress all snapshots into a single tar.gz archive
//  6. Truncate source logs (only after archive write succeeds; best-effort)
//  7. Remove temp copies
//  8. Emit UPD COMPLETED (or UPD FAILED) log line
func RunOnce(opts Options) error {
	if strings.TrimSpace(opts.LogPath) == "" {
		return errors.New("backup: LogPath is required")
	}
	if strings.TrimSpace(opts.ArchiveDir) == "" {
		return errors.New("backup: ArchiveDir is required")
	}

	nowFn := nowOrDefault(opts.Now)
	now := nowFn()
	id := newBupID()
	compression := defaultCompression
	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = "MANUAL"
	}
	listSource := strings.TrimSpace(opts.ListSource)
	if listSource == "" {
		listSource = "default"
	}

	logPath := filepath.Clean(opts.LogPath)
	logDir := filepath.Dir(logPath)
	archiveDir := filepath.Clean(opts.ArchiveDir)
	sourceDir := logDir // primary source dir shown in the log

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("backup: create log dir: %w", err)
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("backup: create archive dir: %w", err)
	}

	// Gather source files that exist.
	allSources := append([]string{logPath}, opts.RotateExtra...)
	allSources = append(allSources, opts.ExtraFiles...)
	var presentSources []string
	var totalSize int64
	for _, p := range allSources {
		p = filepath.Clean(p)
		info, err := os.Stat(p)
		if err != nil {
			continue // absent or inaccessible — skip silently
		}
		if info.Size() == 0 {
			continue // skip empty files
		}
		presentSources = append(presentSources, p)
		totalSize += info.Size()
	}
	if len(presentSources) == 0 {
		return nil // nothing to archive
	}

	stamp := now.Format("20060102_150405")
	tarName := fmt.Sprintf("backup.%s.%s", stamp, compression)
	finalPath := filepath.Join(archiveDir, tarName)
	timestamp := now.UTC().Format("2006-01-02T15:04:05Z")

	// Snapshot all source files to temp copies.
	var entries []archiveEntry
	var tmpPaths []string
	for _, src := range presentSources {
		info, _ := os.Stat(src)
		copyName := fmt.Sprintf("%s.%s.copy", filepath.Base(src), stamp)
		copyPath := filepath.Join(logDir, copyName)
		if err := copyFile(src, copyPath, info.Mode()); err != nil {
			_ = cleanupTemps(tmpPaths)
			return fmt.Errorf("backup: snapshot %s: %w", filepath.Base(src), err)
		}
		entries = append(entries, archiveEntry{SrcPath: copyPath, Name: filepath.Base(src)})
		tmpPaths = append(tmpPaths, copyPath)
	}

	// Emit NEW STARTED line.
	applog.PrintLifecycle("NEW", "backup",
		applog.F("ID", id),
		applog.F("status", "STARTED"),
		applog.F("method", method),
		applog.F("timestamp", timestamp),
		applog.F("compression", strings.ToUpper(compression)),
		applog.F("source", sourceDir),
		applog.F("list", listSource),
		applog.F("to", finalPath),
		applog.F("size", humanSize(totalSize)),
	)

	if err := writeTarGz(entries, finalPath); err != nil {
		_ = cleanupTemps(tmpPaths)
		emitFailed(id, method, err.Error())
		return err
	}

	// Truncate source logs AFTER archive write succeeds.
	// If truncation fails the archive is already safe; log and continue.
	for _, p := range append([]string{logPath}, opts.RotateExtra...) {
		if containsPath(presentSources, filepath.Clean(p)) {
			if err := os.Truncate(filepath.Clean(p), 0); err != nil {
				applog.Print("WARN", "backup", "truncate failed after archive write",
					applog.F("file", filepath.Base(p)),
					applog.F("error", err.Error()),
				)
			}
		}
	}
	_ = cleanupTemps(tmpPaths)

	archiveInfo, err := os.Stat(finalPath)
	if err != nil {
		emitFailed(id, method, err.Error())
		return fmt.Errorf("backup: stat archive: %w", err)
	}

	// Emit UPD COMPLETED line.
	applog.PrintLifecycle("UPD", "backup",
		applog.F("ID", id),
		applog.F("status", "COMPLETED"),
		applog.F("location", finalPath),
		applog.F("compressedSize", humanSize(archiveInfo.Size())),
	)

	if opts.StatePath != "" {
		_ = writeLastRun(opts.StatePath, now)
	}
	return nil
}

// emitFailed emits a UPD FAILED log line.
func emitFailed(id, method, reason string) {
	applog.PrintLifecycle("UPD", "backup",
		applog.F("ID", id),
		applog.F("status", "FAILED"),
		applog.F("method", method),
		applog.F("reason", reason),
	)
}

// cleanupTemps removes a list of temporary file paths, ignoring errors.
func cleanupTemps(paths []string) error {
	for _, p := range paths {
		_ = os.Remove(p)
	}
	return nil
}

func containsPath(paths []string, target string) bool {
	target = filepath.Clean(target)
	for _, p := range paths {
		if filepath.Clean(p) == target {
			return true
		}
	}
	return false
}

// StartAuto starts an automated backup loop based on interval and/or size.
// Returns a stop function.
func StartAuto(opts Options) (func(), error) {
	if opts.IntervalDays <= 0 && opts.MaxBytes <= 0 {
		return func() {}, nil
	}
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = 10 * time.Minute
	}
	opts.Method = "AUTO" // always AUTO for scheduled runs
	stop := make(chan struct{})

	go func() {
		checkAndRun := func() {
			should, reason, err := shouldBackup(opts)
			if err != nil || !should {
				return
			}
			applog.Print("INFO", "backup", "triggered", applog.F("reason", reason))
			if err := RunOnce(opts); err != nil {
				applog.Print("ERROR", "backup", "failed", applog.F("error", err.Error()))
			}
		}

		checkAndRun()
		ticker := time.NewTicker(opts.CheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				checkAndRun()
			case <-stop:
				return
			}
		}
	}()

	return func() { close(stop) }, nil
}

func nowOrDefault(f func() time.Time) func() time.Time {
	if f != nil {
		return f
	}
	return time.Now
}

func shouldBackup(opts Options) (bool, string, error) {
	if strings.TrimSpace(opts.LogPath) == "" {
		return false, "", errors.New("backup: LogPath is required")
	}

	if opts.IntervalDays > 0 {
		last := readLastRun(opts.StatePath)
		now := nowOrDefault(opts.Now)()
		if last.IsZero() || now.Sub(last) >= time.Duration(opts.IntervalDays)*24*time.Hour {
			return true, "interval", nil
		}
	}

	if opts.MaxBytes > 0 {
		size, err := fileSize(opts.LogPath)
		if err != nil {
			return false, "", err
		}
		if size >= opts.MaxBytes {
			return true, "size", nil
		}
	}

	return false, "", nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return info.Size(), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	s, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("backup: open source: %w", err)
	}
	defer s.Close()

	d, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("backup: create copy: %w", err)
	}
	defer d.Close()

	if _, err := io.Copy(d, s); err != nil {
		return fmt.Errorf("backup: copy: %w", err)
	}
	return nil
}

func humanSize(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	sz := float64(bytes)
	unit := units[0]
	for i := 1; i < len(units) && sz >= 1024; i++ {
		sz /= 1024
		unit = units[i]
	}
	if unit == "B" {
		return fmt.Sprintf("%d%s", bytes, unit)
	}
	return fmt.Sprintf("%.2f%s", sz, unit)
}

// writeTarGz archives all entries into a single tar.gz at tarPath.
func writeTarGz(entries []archiveEntry, tarPath string) error {
	f, err := os.Create(tarPath)
	if err != nil {
		return fmt.Errorf("backup: create tar: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, e := range entries {
		info, err := os.Stat(e.SrcPath)
		if err != nil {
			return fmt.Errorf("backup: stat %s: %w", e.Name, err)
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return fmt.Errorf("backup: tar header %s: %w", e.Name, err)
		}
		header.Name = e.Name
		if err := tw.WriteHeader(header); err != nil {
			return fmt.Errorf("backup: write header %s: %w", e.Name, err)
		}
		file, err := os.Open(e.SrcPath)
		if err != nil {
			return fmt.Errorf("backup: open %s: %w", e.Name, err)
		}
		_, copyErr := io.Copy(tw, file)
		_ = file.Close()
		if copyErr != nil {
			return fmt.Errorf("backup: tar write %s: %w", e.Name, copyErr)
		}
	}
	return nil
}

func readLastRun(statePath string) time.Time {
	if strings.TrimSpace(statePath) == "" {
		return time.Time{}
	}
	b, err := os.ReadFile(statePath)
	if err != nil {
		return time.Time{}
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func writeLastRun(statePath string, t time.Time) error {
	if strings.TrimSpace(statePath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(statePath, []byte(strconv.FormatInt(t.Unix(), 10)), 0o644)
}
