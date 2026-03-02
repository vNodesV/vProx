package counter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func resetState() {
	mu.Lock()
	data = make(map[string]int64)
	dirty = false
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// Increment + Snapshot
// ---------------------------------------------------------------------------

func TestIncrementAndSnapshot(t *testing.T) {
	resetState()

	if n := Increment("1.2.3.4"); n != 1 {
		t.Errorf("first Increment = %d, want 1", n)
	}
	if n := Increment("1.2.3.4"); n != 2 {
		t.Errorf("second Increment = %d, want 2", n)
	}
	if n := Increment("5.6.7.8"); n != 1 {
		t.Errorf("new IP Increment = %d, want 1", n)
	}

	snap := Snapshot()
	if snap["1.2.3.4"] != 2 {
		t.Errorf("snapshot[1.2.3.4] = %d, want 2", snap["1.2.3.4"])
	}
	if snap["5.6.7.8"] != 1 {
		t.Errorf("snapshot[5.6.7.8] = %d, want 1", snap["5.6.7.8"])
	}
}

// ---------------------------------------------------------------------------
// Load / Save roundtrip
// ---------------------------------------------------------------------------

func TestLoadSaveRoundtrip(t *testing.T) {
	resetState()

	dir := t.TempDir()
	p := filepath.Join(dir, "counts.json")

	Increment("10.0.0.1")
	Increment("10.0.0.1")
	Increment("10.0.0.2")

	if err := Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Clear and reload
	resetState()
	Load(p)

	snap := Snapshot()
	if snap["10.0.0.1"] != 2 {
		t.Errorf("after reload: [10.0.0.1] = %d, want 2", snap["10.0.0.1"])
	}
	if snap["10.0.0.2"] != 1 {
		t.Errorf("after reload: [10.0.0.2] = %d, want 1", snap["10.0.0.2"])
	}
}

func TestLoadEmptyPath(t *testing.T) {
	resetState()
	Load("") // should not panic
	if len(Snapshot()) != 0 {
		t.Error("Load empty path should not add data")
	}
}

func TestLoadMissingFile(t *testing.T) {
	resetState()
	Load("/nonexistent/path.json") // should not panic
	if len(Snapshot()) != 0 {
		t.Error("Load missing file should not add data")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	resetState()
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(p, []byte("{{{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	Load(p)
	if len(Snapshot()) != 0 {
		t.Error("Load invalid JSON should not add data")
	}
}

func TestLoadFiltersInvalidEntries(t *testing.T) {
	resetState()
	dir := t.TempDir()
	p := filepath.Join(dir, "counts.json")
	data := map[string]int64{
		"good-ip":  5,
		"":         10, // empty key
		"negative": -1, // negative count
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	Load(p)
	snap := Snapshot()
	if snap["good-ip"] != 5 {
		t.Errorf("good-ip = %d, want 5", snap["good-ip"])
	}
	if _, ok := snap[""]; ok {
		t.Error("empty key should be filtered out")
	}
	if _, ok := snap["negative"]; ok {
		t.Error("negative count should be filtered out")
	}
}

func TestSaveEmptyPath(t *testing.T) {
	resetState()
	if err := Save(""); err != nil {
		t.Errorf("Save empty path should return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Reset
// ---------------------------------------------------------------------------

func TestReset(t *testing.T) {
	resetState()

	dir := t.TempDir()
	p := filepath.Join(dir, "counts.json")

	Increment("1.2.3.4")
	Increment("1.2.3.4")

	if err := Reset(p); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	snap := Snapshot()
	if len(snap) != 0 {
		t.Errorf("after Reset: snapshot has %d entries, want 0", len(snap))
	}

	// Verify on-disk state is also reset
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var ondisk map[string]int64
	if err := json.Unmarshal(b, &ondisk); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(ondisk) != 0 {
		t.Errorf("on-disk after Reset has %d entries", len(ondisk))
	}
}

// ---------------------------------------------------------------------------
// Concurrent Increment (race detector)
// ---------------------------------------------------------------------------

func TestConcurrentIncrement(t *testing.T) {
	resetState()

	const goroutines = 10
	const iterations = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				Increment("shared-ip")
			}
		}()
	}
	wg.Wait()

	snap := Snapshot()
	want := int64(goroutines * iterations)
	if snap["shared-ip"] != want {
		t.Errorf("concurrent Increment: got %d, want %d", snap["shared-ip"], want)
	}
}

// ---------------------------------------------------------------------------
// StartTicker
// ---------------------------------------------------------------------------

func TestStartTicker(t *testing.T) {
	resetState()

	dir := t.TempDir()
	p := filepath.Join(dir, "ticker.json")

	stop := StartTicker(p)

	Increment("ticker-ip")

	// Wait enough for at least one tick (SaveInterval = 1s)
	time.Sleep(1500 * time.Millisecond)

	// File should exist with data
	b, err := os.ReadFile(p)
	if err != nil {
		// Ticker may not have flushed yet; that's acceptable in CI
		// but we still call stop to test the final flush
		stop()
		b, err = os.ReadFile(p)
		if err != nil {
			t.Fatalf("file not created after stop: %v", err)
		}
	} else {
		stop()
	}

	var ondisk map[string]int64
	if err := json.Unmarshal(b, &ondisk); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ondisk["ticker-ip"] != 1 {
		t.Errorf("ticker file: ticker-ip = %d, want 1", ondisk["ticker-ip"])
	}
}

func TestStartTickerStopFlushes(t *testing.T) {
	resetState()

	dir := t.TempDir()
	p := filepath.Join(dir, "flush.json")

	stop := StartTicker(p)

	Increment("final-flush-ip")
	// Stop immediately (before any tick)
	stop()

	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("file not created by final flush: %v", err)
	}
	var ondisk map[string]int64
	if err := json.Unmarshal(b, &ondisk); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ondisk["final-flush-ip"] != 1 {
		t.Errorf("after stop flush: got %d, want 1", ondisk["final-flush-ip"])
	}
}
