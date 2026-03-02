// Package counter manages per-IP access counts with periodic disk persistence.
//
// Thread-safe: all operations are guarded by an internal mutex.
// The background ticker flushes dirty counters to disk at a configurable interval.
package counter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SaveInterval controls how often dirty counters are flushed to disk.
const SaveInterval = 1 * time.Second

var (
	mu    sync.Mutex
	data  = make(map[string]int64)
	dirty bool // true when in-memory counts differ from disk
)

// Load reads persisted access counts from a JSON file.
// Missing or empty files are silently ignored. Invalid entries
// (empty keys, negative counts) are filtered out.
func Load(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var raw map[string]int64
	if err := json.Unmarshal(b, &raw); err != nil {
		return
	}
	clean := make(map[string]int64, len(raw))
	for ip, qty := range raw {
		if strings.TrimSpace(ip) == "" || qty < 0 {
			continue
		}
		clean[ip] = qty
	}
	mu.Lock()
	data = clean
	mu.Unlock()
}

// Save writes current access counts to a JSON file atomically (write-rename).
func Save(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	return saveLocked(path)
}

// Reset clears all counters and persists the empty state to disk.
func Reset(path string) error {
	mu.Lock()
	data = make(map[string]int64)
	err := saveLocked(path)
	mu.Unlock()
	return err
}

// Increment atomically increments the counter for the given IP and
// returns the new count. Marks the counter as dirty for background flush.
func Increment(ip string) int64 {
	mu.Lock()
	data[ip]++
	qty := data[ip]
	dirty = true
	mu.Unlock()
	return qty
}

// Snapshot returns a copy of the current counters (for diagnostics / info).
func Snapshot() map[string]int64 {
	mu.Lock()
	defer mu.Unlock()
	cp := make(map[string]int64, len(data))
	for k, v := range data {
		cp[k] = v
	}
	return cp
}

// StartTicker runs a background goroutine that flushes dirty counters
// to disk every SaveInterval. Returns a stop function that performs a
// final flush and stops the ticker.
func StartTicker(path string) func() {
	ticker := time.NewTicker(SaveInterval)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				if dirty {
					_ = saveLocked(path)
					dirty = false
				}
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	return func() {
		ticker.Stop()
		close(done)
		// Final flush
		mu.Lock()
		if dirty {
			_ = saveLocked(path)
			dirty = false
		}
		mu.Unlock()
	}
}

// saveLocked writes counts to disk. Caller must hold mu.
func saveLocked(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	buf := make(map[string]int64, len(data))
	for ip, qty := range data {
		if strings.TrimSpace(ip) == "" || qty < 0 {
			continue
		}
		buf[ip] = qty
	}
	b, err := json.MarshalIndent(buf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
