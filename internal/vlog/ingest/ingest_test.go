package ingest_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vNodesV/vProx/internal/vlog/db"
	"github.com/vNodesV/vProx/internal/vlog/ingest"
)

func TestParseLogLine_Access(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantIP   string
		wantPath string
		wantMeth string
		wantNil  bool
	}{
		{
			// Real vProx LineLifecycle format (current)
			name:     "vProx lifecycle format",
			line:     `10:23AM NEW ID=API1A2B3C4D5E6F7G8H9I0J1K2 status=COMPLETED method=GET from=1.2.3.4 count=5 to=API.EXAMPLE.COM endpoint=/rpc latency=12ms userAgent="curl/7.64.1" country=US module=vProx`,
			wantIP:   "1.2.3.4",
			wantPath: "/rpc",
			wantMeth: "GET",
		},
		{
			// vProxWeb module also accepted
			name:     "vProxWeb module accepted",
			line:     `10:23AM NEW ID=REQ1A2B from=5.6.7.8 method=POST endpoint=/rest country=US module=vProxWeb`,
			wantIP:   "5.6.7.8",
			wantPath: "/rest",
			wantMeth: "POST",
		},
		{
			// Legacy format (module=access) for backward compatibility
			name:     "legacy access line with ua alias",
			line:     `10:23AM INF request ip=5.6.7.8 method=POST path=/rest ua="curl/7.64.1" module=access`,
			wantIP:   "5.6.7.8",
			wantPath: "/rest",
			wantMeth: "POST",
		},
		{
			name:    "non-request module skipped",
			line:    `10:23AM NEW status=STARTED module=backup`,
			wantNil: true,
		},
		{
			name:    "empty line skipped",
			line:    "",
			wantNil: true,
		},
		{
			name:    "comment line skipped",
			line:    "# this is a comment",
			wantNil: true,
		},
		{
			// Legacy module=proxy also accepted
			name:     "legacy proxy module accepted",
			line:     `10:23AM INF request ip=9.10.11.12 method=GET path=/api module=proxy`,
			wantIP:   "9.10.11.12",
			wantPath: "/api",
			wantMeth: "GET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ingest.ParseLogLine(tt.line, "backup.20250101_120000.tar.gz", "2025-01-01T12:00:00Z")
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil, got event with IP=%q", ev.IP)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected non-nil event, got nil")
			}
			if ev.IP != tt.wantIP {
				t.Errorf("IP: got %q, want %q", ev.IP, tt.wantIP)
			}
			if ev.Path != tt.wantPath {
				t.Errorf("Path: got %q, want %q", ev.Path, tt.wantPath)
			}
			if ev.Method != tt.wantMeth {
				t.Errorf("Method: got %q, want %q", ev.Method, tt.wantMeth)
			}
		})
	}
}

func TestParseRateLimitLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantIP    string
		wantEvent string
		wantRPS   float64
		wantNil   bool
	}{
		{
			name:      "valid rate limit event",
			line:      `{"ts":"2025-01-01T12:00:00Z","level":"ERROR","event":"429","reason":"429","ip":"1.2.3.4","method":"GET","path":"/rpc","host":"api.example.com","rps":25.0,"burst":100}`,
			wantIP:    "1.2.3.4",
			wantEvent: "429",
			wantRPS:   25.0,
		},
		{
			name:    "empty line",
			line:    "",
			wantNil: true,
		},
		{
			name:    "invalid json",
			line:    `{"broken":`,
			wantNil: true,
		},
		{
			name:      "ua alias fallback",
			line:      `{"ts":"2025-01-01T12:00:01Z","event":"429","ip":"5.6.7.8","ua":"Mozilla/5.0","method":"GET","path":"/rpc","host":"x","rps":10.0,"burst":50}`,
			wantIP:    "5.6.7.8",
			wantEvent: "429",
			wantRPS:   10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ingest.ParseRateLimitLine(tt.line, "backup.20250101_120000.tar.gz")
			if tt.wantNil {
				if ev != nil {
					t.Errorf("expected nil, got event IP=%q", ev.IP)
				}
				return
			}
			if ev == nil {
				t.Fatal("expected non-nil event, got nil")
			}
			if ev.IP != tt.wantIP {
				t.Errorf("IP: got %q, want %q", ev.IP, tt.wantIP)
			}
			if ev.Event != tt.wantEvent {
				t.Errorf("Event: got %q, want %q", ev.Event, tt.wantEvent)
			}
			if ev.RPS != tt.wantRPS {
				t.Errorf("RPS: got %f, want %f", ev.RPS, tt.wantRPS)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// New + IngestAll + IngestOne
// ---------------------------------------------------------------------------

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func writeTestArchive(t *testing.T, dir, name string, logContent string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	data := []byte(logContent)
	hdr := &tar.Header{Name: "main.log", Size: int64(len(data)), Mode: 0o644}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gz.Close()
	return path
}

func TestIngestAll(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	logLine := `10:23AM NEW ID=API1A2B3C4D5E6F7G8H9I0J1K2 status=COMPLETED method=GET from=1.2.3.4 count=5 to=API.EXAMPLE.COM endpoint=/rpc latency=12ms userAgent="curl/7.64.1" country=US module=vProx`
	writeTestArchive(t, archiveDir, "backup.20250101_120000.tar.gz", logLine)
	writeTestArchive(t, archiveDir, "backup.20250102_120000.tar.gz", logLine)

	ing := ingest.New(d, archiveDir)

	n, err := ing.IngestAll()
	if err != nil {
		t.Fatalf("IngestAll: %v", err)
	}
	if n != 2 {
		t.Errorf("IngestAll processed = %d, want 2", n)
	}

	// Verify events were inserted
	events, err := d.ListRequestEventsByIP("1.2.3.4", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Errorf("events for 1.2.3.4 = %d, want 2", len(events))
	}

	// Verify IP account was upserted
	acc, err := d.GetIPAccount("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if acc == nil {
		t.Fatal("expected IP account, got nil")
	}
	if acc.TotalRequests != 2 {
		t.Errorf("TotalRequests = %d, want 2", acc.TotalRequests)
	}
}

func TestIngestOneDedup(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	logLine := `10:23AM NEW ID=API1 status=COMPLETED method=GET from=5.6.7.8 endpoint=/rpc country=CA module=vProx`
	archivePath := writeTestArchive(t, archiveDir, "backup.20250101_120000.tar.gz", logLine)

	ing := ingest.New(d, archiveDir)

	// First ingest
	skipped, err := ing.IngestOne(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if skipped {
		t.Error("first ingest should not be skipped")
	}

	// Second ingest — should be skipped
	skipped, err = ing.IngestOne(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if !skipped {
		t.Error("second ingest should be skipped (dedup)")
	}

	// Verify only 1 event (not duplicated)
	events, err := d.ListRequestEventsByIP("5.6.7.8", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("events = %d, want 1 (no duplicate)", len(events))
	}
}

func TestIngestAllForce(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	logLine := `10:23AM NEW ID=API1 status=COMPLETED method=GET from=9.8.7.6 endpoint=/rpc country=DE module=vProx`
	writeTestArchive(t, archiveDir, "backup.20250101_120000.tar.gz", logLine)

	ing := ingest.New(d, archiveDir)

	// First normal ingest
	n, err := ing.IngestAll()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("IngestAll = %d, want 1", n)
	}

	// Force re-ingest
	n, err = ing.IngestAllForce()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("IngestAllForce = %d, want 1", n)
	}
}

func TestListPending(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	writeTestArchive(t, archiveDir, "backup.20250101_120000.tar.gz", "data")
	writeTestArchive(t, archiveDir, "backup.20250102_120000.tar.gz", "data")

	ing := ingest.New(d, archiveDir)

	pending, err := ing.ListPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("pending = %d, want 2", len(pending))
	}

	// Ingest one
	ing.IngestOne(pending[0])

	pending, err = ing.ListPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Errorf("after ingesting 1: pending = %d, want 1", len(pending))
	}
}

func TestIngestMalformedTarGz(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	// Write invalid gzip file
	badPath := filepath.Join(archiveDir, "backup.20250101_120000.tar.gz")
	os.WriteFile(badPath, []byte("not a gzip file"), 0o644)

	ing := ingest.New(d, archiveDir)
	_, err := ing.IngestOne(badPath)
	if err == nil {
		t.Error("expected error for malformed tar.gz")
	}
}

func TestIngestEmptyArchive(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	// Write an empty tar.gz (valid but no entries)
	path := filepath.Join(archiveDir, "backup.20250101_120000.tar.gz")
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	tw.Close()
	gz.Close()
	f.Close()

	ing := ingest.New(d, archiveDir)
	skipped, err := ing.IngestOne(path)
	if err != nil {
		t.Fatalf("IngestOne empty archive: %v", err)
	}
	if skipped {
		t.Error("should not be skipped (first ingest)")
	}
}

func TestIngestRateLimitEvents(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()

	// Create archive with rate-limit.jsonl
	path := filepath.Join(archiveDir, "backup.20250101_120000.tar.gz")
	f, _ := os.Create(path)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	rlLine := `{"ts":"2025-01-01T12:00:00Z","event":"429","reason":"429","ip":"10.0.0.1","method":"GET","path":"/rpc","host":"api.example.com","rps":25.0,"burst":100}` + "\n"
	data := []byte(rlLine)
	hdr := &tar.Header{Name: "rate-limit.jsonl", Size: int64(len(data)), Mode: 0o644}
	tw.WriteHeader(hdr)
	tw.Write(data)
	tw.Close()
	gz.Close()
	f.Close()

	ing := ingest.New(d, archiveDir)
	skipped, err := ing.IngestOne(path)
	if err != nil {
		t.Fatalf("IngestOne: %v", err)
	}
	if skipped {
		t.Error("should not be skipped")
	}

	events, err := d.ListRateLimitEventsByIP("10.0.0.1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("ratelimit events = %d, want 1", len(events))
	}
}

// ---------------------------------------------------------------------------
// Watcher
// ---------------------------------------------------------------------------

func TestWatcherStartStop(t *testing.T) {
	d := openTestDB(t)
	archiveDir := t.TempDir()
	ing := ingest.New(d, archiveDir)

	w := ingest.NewWatcher(ing, 1) // 1s interval
	w.Start()
	time.Sleep(100 * time.Millisecond) // let goroutine start
	w.Stop()
	// Verify no panic or hang
}
