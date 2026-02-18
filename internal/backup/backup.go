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
)

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

// RunOnce performs a single backup:
// 1) copy main.log -> main.log.<timestamp>
// 2) truncate main.log
// 3) tar.gz the copy
// 4) delete the copy
// 5) move tar.gz to ArchiveDir
func RunOnce(opts Options) error {
	if strings.TrimSpace(opts.LogPath) == "" {
		return errors.New("backup: LogPath is required")
	}
	if strings.TrimSpace(opts.ArchiveDir) == "" {
		return errors.New("backup: ArchiveDir is required")
	}

	logPath := filepath.Clean(opts.LogPath)
	logDir := filepath.Dir(logPath)

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("backup: create log dir: %w", err)
	}
	if err := os.MkdirAll(opts.ArchiveDir, 0o755); err != nil {
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

	stamp := nowOrDefault(opts.Now)().Format("20060102_150405")
	base := filepath.Base(logPath)
	copyName := fmt.Sprintf("%s.%s", base, stamp)
	copyPath := filepath.Join(logDir, copyName)

	if err := copyFile(logPath, copyPath, info.Mode()); err != nil {
		return err
	}
	if err := os.Truncate(logPath, 0); err != nil {
		return fmt.Errorf("backup: truncate log: %w", err)
	}

	tarName := fmt.Sprintf("%s.%s.tar.gz", base, stamp)
	tarPath := filepath.Join(logDir, tarName)
	if err := writeTarGz(copyPath, copyName, tarPath); err != nil {
		return err
	}
	_ = os.Remove(copyPath)

	finalPath := filepath.Join(opts.ArchiveDir, tarName)
	if err := os.Rename(tarPath, finalPath); err != nil {
		return fmt.Errorf("backup: move archive: %w", err)
	}

	if opts.StatePath != "" {
		_ = writeLastRun(opts.StatePath, nowOrDefault(opts.Now)())
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
			log.Printf("[backup] triggered (%s)", reason)
			if err := RunOnce(opts); err != nil {
				log.Printf("[backup] failed: %v", err)
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
