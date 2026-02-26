package db_test

import (
	"path/filepath"
	"testing"

	"github.com/vNodesV/vProx/internal/vlog/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "vlog_test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenAndMigrate(t *testing.T) {
	d := openTestDB(t)
	// Stats should return zero counts on empty DB
	stats, err := d.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	for k, v := range stats {
		if v != 0 {
			t.Errorf("stats[%q]: got %d, want 0", k, v)
		}
	}
}

func TestUpsertAndGetIPAccount(t *testing.T) {
	d := openTestDB(t)

	acc := &db.IPAccount{
		IP:              "1.2.3.4",
		FirstSeen:       "2025-01-01T00:00:00Z",
		LastSeen:        "2025-01-02T00:00:00Z",
		TotalRequests:   42,
		RatelimitEvents: 5,
		Country:         "US",
		ASN:             "AS1234",
		Status:          "unknown",
		Hostnames:       "[]",
		OpenPorts:       "[]",
		Services:        "{}",
		ThreatFlags:     "[]",
		Tags:            "[]",
		ThreatScore:     -1,
		VTMalicious:     -1,
		AbuseScore:      -1,
	}

	if err := d.UpsertIPAccount(acc); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := d.GetIPAccount("1.2.3.4")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected account, got nil")
	}
	if got.IP != "1.2.3.4" {
		t.Errorf("ip: got %q, want 1.2.3.4", got.IP)
	}
	if got.TotalRequests != 42 {
		t.Errorf("total_requests: got %d, want 42", got.TotalRequests)
	}
	if got.Country != "US" {
		t.Errorf("country: got %q, want US", got.Country)
	}
}

func TestGetIPAccountNotFound(t *testing.T) {
	d := openTestDB(t)
	acc, err := d.GetIPAccount("99.99.99.99")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if acc != nil {
		t.Error("expected nil for missing IP")
	}
}

func TestInsertRequestEvent(t *testing.T) {
	d := openTestDB(t)
	e := &db.RequestEvent{
		Archive:   "backup.20250101_120000.tar.gz",
		Ts:        "2025-01-01T12:00:00Z",
		RequestID: "req-abc123",
		IP:        "10.0.0.1",
		Method:    "GET",
		Path:      "/rpc",
		Host:      "api.example.com",
		Route:     "rpc",
		Status:    "ok",
		Country:   "DE",
	}
	if err := d.InsertRequestEvent(e); err != nil {
		t.Fatalf("insert request_event: %v", err)
	}

	events, err := d.ListRequestEventsByIP("10.0.0.1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Method != "GET" {
		t.Errorf("method: got %q, want GET", events[0].Method)
	}
}

func TestInsertRateLimitEvent(t *testing.T) {
	d := openTestDB(t)
	e := &db.RateLimitEvent{
		Archive: "backup.20250101_120000.tar.gz",
		Ts:      "2025-01-01T12:00:01Z",
		IP:      "10.0.0.2",
		Event:   "429",
		Reason:  "429",
		RPS:     25.0,
		Burst:   100,
	}
	if err := d.InsertRateLimitEvent(e); err != nil {
		t.Fatalf("insert ratelimit_event: %v", err)
	}

	events, err := d.ListRateLimitEventsByIP("10.0.0.2", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].RPS != 25.0 {
		t.Errorf("rps: got %f, want 25.0", events[0].RPS)
	}
}

func TestIsArchiveIngested(t *testing.T) {
	d := openTestDB(t)
	const name = "backup.20250101_120000.tar.gz"

	ok, err := d.IsArchiveIngested(name)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if ok {
		t.Error("should not be ingested yet")
	}

	if err := d.MarkArchiveIngested(&db.IngestedArchive{
		Filename:       name,
		IngestedAt:     "2025-01-01T12:05:00Z",
		RequestCount:   100,
		RatelimitCount: 3,
		SizeBytes:      4096,
	}); err != nil {
		t.Fatalf("mark ingested: %v", err)
	}

	ok, err = d.IsArchiveIngested(name)
	if err != nil {
		t.Fatalf("check after mark: %v", err)
	}
	if !ok {
		t.Error("should be ingested after mark")
	}
}

func TestIntelCache(t *testing.T) {
	d := openTestDB(t)

	// Not found returns empty strings
	fa, data, err := d.GetIntelCache("1.2.3.4", "abuseipdb")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if fa != "" || data != "" {
		t.Errorf("expected empty strings for missing cache, got %q %q", fa, data)
	}

	// Upsert and retrieve
	if err := d.UpsertIntelCache("1.2.3.4", "abuseipdb", "2025-01-01T00:00:00Z", `{"score":75}`); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	fa, data, err = d.GetIntelCache("1.2.3.4", "abuseipdb")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fa != "2025-01-01T00:00:00Z" {
		t.Errorf("fetched_at: got %q", fa)
	}
	if data != `{"score":75}` {
		t.Errorf("data: got %q", data)
	}
}

func TestListIPAccounts(t *testing.T) {
	d := openTestDB(t)

	for i, ip := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		_ = d.UpsertIPAccount(&db.IPAccount{
			IP: ip, FirstSeen: "2025-01-01T00:00:00Z", LastSeen: "2025-01-01T00:00:00Z",
			Status: "unknown", Hostnames: "[]", OpenPorts: "[]", Services: "{}",
			ThreatFlags: "[]", Tags: "[]", ThreatScore: int64(i * 10),
			VTMalicious: -1, AbuseScore: -1,
		})
	}

	accs, err := d.ListIPAccounts(10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(accs) != 3 {
		t.Errorf("expected 3 accounts, got %d", len(accs))
	}

	top, err := d.ListTopThreatAccounts(2)
	if err != nil {
		t.Fatalf("top threat: %v", err)
	}
	if len(top) != 2 {
		t.Errorf("expected 2 top threat accounts, got %d", len(top))
	}
	// First should be highest score (3.3.3.3 with score 20)
	if top[0].ThreatScore < top[1].ThreatScore {
		t.Error("top threat accounts should be ordered descending by threat_score")
	}
}
