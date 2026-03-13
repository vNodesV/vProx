package configwizard

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// writeConfig serializes v as TOML and writes it to path with backup semantics.
// Interactive mode prompts before overwriting an existing file.
func writeConfig(path string, v any) error {
	return writeConfigMode(path, v, true)
}

// writeConfigNoPrompt writes v to path and always overwrites existing files.
// If the destination exists, it is copied to <path>.bak before overwrite.
// Used by the web wizard where interactive stdin prompts are not available.
func writeConfigNoPrompt(path string, v any) error {
	return writeConfigMode(path, v, false)
}

func writeConfigMode(path string, v any, interactive bool) error {
	// Render TOML.
	buf := &bytes.Buffer{}
	enc := toml.NewEncoder(buf)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("toml encode: %w", err)
	}
	data := buf.Bytes()
	fileMode := configFileMode(path, 0)

	// Check existing file.
	if info, err := os.Stat(path); err == nil {
		fileMode = configFileMode(path, info.Mode().Perm())
		fmt.Printf("\n  ⚠  File already exists: %s\n", path)
		if interactive {
			fmt.Printf("  Diff preview (new values):\n")
			printPreview(data, 20)
			if !confirm("Overwrite?", false) {
				fmt.Printf("  ↩ skipped — existing file unchanged\n")
				return nil
			}
		} else {
			fmt.Printf("  ↻ overwriting existing file (web mode)\n")
		}
		// Backup.
		bak := path + ".bak"
		if err := copyFile(path, bak, fileMode); err != nil {
			return fmt.Errorf("backup %s: %w", bak, err)
		}
		fmt.Printf("  ✓ backup → %s\n", bak)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	// Write atomically via temp file + rename.
	tmp := path + ".tmp"
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("prepare %s: %w", tmp, err)
	}
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}

	fmt.Printf("  ✓ written: %s\n", path)
	return nil
}

// copyFile copies src to dst, creating dst if needed.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Chmod(mode)
}

func configFileMode(path string, existing os.FileMode) os.FileMode {
	if isSensitiveConfig(path) {
		return 0o600
	}
	if existing != 0 {
		return existing
	}
	return 0o644
}

func isSensitiveConfig(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	return strings.HasSuffix(lower, "config/vlog/vlog.toml")
}

// printPreview prints the first n lines of data with indentation.
func printPreview(data []byte, n int) {
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[:n]
		lines = append(lines, "  ...")
	}
	for _, l := range lines {
		if l != "" {
			fmt.Printf("    %s\n", l)
		}
	}
}

// configPath joins home, "config", and the given relative path segments.
func configPath(home string, parts ...string) string {
	all := append([]string{home, "config"}, parts...)
	return filepath.Join(all...)
}
