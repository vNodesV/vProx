package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
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
	LogPath       string
	ArchiveDir    string
	StatePath     string
	IntervalDays  int
	MaxBytes      int64
	CheckInterval time.Duration
	Now           func() time.Time
}

func nowOrDefault(f func() time.Time) func() time.Time {
	if f != nil {
		return f
	}
	return time.Now
}

// RunOnce performs a single backup for main.log.
// Expected behavior:
// 1) copy main.log -> temporary copy
// 2) truncate main.log
// 3) compress copy to ArchiveDir as tar.gz
// 4) write first line to main.log with backup status and metadata
func RunOnce(opts Options) error {
	if strings.TrimSpace(opts.LogPath) == "" {
		return errors.New("backup: LogPath is required")
	}
	if strings.TrimSpace(opts.ArchiveDir) == "" {
		return errors.New("backup: ArchiveDir is required")
	}

	nowFn := nowOrDefault(opts.Now)
	now := nowFn()
	requestID := applog.NewRequestID()
	compression := defaultCompression

	logPath := filepath.Clean(opts.LogPath)
	logDir := filepath.Dir(logPath)
	archiveDir := filepath.Clean(opts.ArchiveDir)

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("backup: create log dir: %w", err)
	}
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return fmt.Errorf("backup: create archive dir: %w", err)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			f, createErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY, 0o644)
			if createErr == nil {
				_ = f.Close()
			}
			return nil
		}
		return fmt.Errorf("backup: stat log: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}
	sourceSize := info.Size()

	stamp := now.Format("20060102_150405")
	base := filepath.Base(logPath)
	copyName := fmt.Sprintf("%s.%s.copy", base, stamp)
	copyPath := filepath.Join(logDir, copyName)
	tarName := fmt.Sprintf("%s.%s.%s", base, stamp, compression)
	finalPath := filepath.Join(archiveDir, tarName)

	if err := copyFile(logPath, copyPath, info.Mode()); err != nil {
		emitBackupLine(buildBackupStatusLine(requestID, "BACKUP FAILED", err.Error(), sourceSize, 0, compression, archiveDir, tarName, ""))
		return err
	}
	if err := os.Truncate(logPath, 0); err != nil {
		emitBackupLine(buildBackupStatusLine(requestID, "BACKUP FAILED", err.Error(), sourceSize, 0, compression, archiveDir, tarName, ""))
		return fmt.Errorf("backup: truncate log: %w", err)
	}

	emitBackupLine(buildBackupStatusLine(requestID, "BACKUP STARTED", "started", sourceSize, 0, compression, archiveDir, tarName, ""))

	if err := writeTarGz(copyPath, copyName, finalPath); err != nil {
		emitBackupLine(buildBackupStatusLine(requestID, "BACKUP FAILED", err.Error(), sourceSize, 0, compression, archiveDir, tarName, ""))
		return err
	}
	_ = os.Remove(copyPath)

	archiveInfo, err := os.Stat(finalPath)
	if err != nil {
		emitBackupLine(buildBackupStatusLine(requestID, "BACKUP FAILED", err.Error(), sourceSize, 0, compression, archiveDir, tarName, ""))
		return fmt.Errorf("backup: stat archive: %w", err)
	}

	emitBackupLine(buildBackupStatusLine(requestID, "BACKUP COMPLETE", "success", sourceSize, archiveInfo.Size(), compression, archiveDir, tarName, ""))

	if opts.StatePath != "" {
		_ = writeLastRun(opts.StatePath, now)
	}
	return nil
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

func buildBackupStatusLine(requestID, event, status string, sourceSize, compressedSize int64, compression, location, filename, failed string) string {
	event = strings.ToUpper(strings.TrimSpace(event))
	if event == "" {
		event = "BACKUP"
	}
	requestID = strings.ToUpper(strings.TrimSpace(requestID))
	compression = strings.ToUpper(strings.TrimSpace(compression))
	fields := []applog.Field{
		applog.F("request_id", requestID),
		applog.F("status", strings.TrimSpace(status)),
		applog.F("filesize", humanSize(sourceSize)),
		applog.F("compression", compression),
		applog.F("location", strings.TrimSpace(location)),
		applog.F("filename", strings.TrimSpace(filename)),
		applog.F("archivesize", humanSize(compressedSize)),
	}
	if strings.TrimSpace(failed) != "" {
		fields = append(fields, applog.F("failed", strings.TrimSpace(failed)))
	}
	level := "INFO"
	if strings.EqualFold(event, "BACKUP FAILED") {
		level = "ERROR"
	}
	return applog.Line(level, "backup", event, fields...)
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

func emitBackupLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	log.Println(line)
}

func writeTarGz(srcPath, srcName, tarPath string) error {
	f, err := os.Create(tarPath)
	if err != nil {
		return fmt.Errorf("backup: create tar: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("backup: stat source copy: %w", err)
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("backup: tar header: %w", err)
	}
	header.Name = srcName
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("backup: write header: %w", err)
	}

	file, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("backup: open copy: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("backup: tar write: %w", err)
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
