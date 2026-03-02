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

// ---------------------------------------------------------------------------
// CountIPAccounts
// ---------------------------------------------------------------------------

func TestCountIPAccounts(t *testing.T) {
	d := openTestDB(t)

	n, err := d.CountIPAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("empty DB: CountIPAccounts = %d, want 0", n)
	}

	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		_ = d.UpsertIPAccount(&db.IPAccount{
			IP: ip, FirstSeen: "2025-01-01", LastSeen: "2025-01-01",
			Status: "unknown", Hostnames: "[]", OpenPorts: "[]", Services: "{}",
			ThreatFlags: "[]", Tags: "[]", ThreatScore: -1, VTMalicious: -1, AbuseScore: -1,
		})
	}

	n, err = d.CountIPAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("after 3 inserts: CountIPAccounts = %d, want 3", n)
	}
}

// ---------------------------------------------------------------------------
// SearchIPAccounts / CountSearchIPAccounts
// ---------------------------------------------------------------------------

func TestSearchIPAccounts(t *testing.T) {
	d := openTestDB(t)

	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "192.168.1.1"} {
		_ = d.UpsertIPAccount(&db.IPAccount{
			IP: ip, FirstSeen: "2025-01-01", LastSeen: "2025-01-01",
			Country: "US", Status: "unknown", Hostnames: "[]", OpenPorts: "[]",
			Services: "{}", ThreatFlags: "[]", Tags: "[]",
			ThreatScore: -1, VTMalicious: -1, AbuseScore: -1,
		})
	}

	// Search by IP prefix
	results, err := d.SearchIPAccounts("10.0.0", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("search '10.0.0': got %d results, want 2", len(results))
	}

	// Count
	n, err := d.CountSearchIPAccounts("10.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("count search '10.0.0': got %d, want 2", n)
	}

	// Search by country
	results, err = d.SearchIPAccounts("US", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("search 'US': got %d results, want 3", len(results))
	}

	// Search no match
	results, err = d.SearchIPAccounts("99.99.99", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("search '99.99.99': got %d results, want 0", len(results))
	}
}

// ---------------------------------------------------------------------------
// BlockIP / UnblockIP / IsBlocked / ListBlockedIPs / ListBlockedAccounts
// ---------------------------------------------------------------------------

func TestBlockUnblockCycle(t *testing.T) {
	d := openTestDB(t)

	// Insert account first
	_ = d.UpsertIPAccount(&db.IPAccount{
		IP: "1.2.3.4", FirstSeen: "2025-01-01", LastSeen: "2025-01-01",
		Status: "unknown", Hostnames: "[]", OpenPorts: "[]", Services: "{}",
		ThreatFlags: "[]", Tags: "[]", ThreatScore: -1, VTMalicious: -1, AbuseScore: -1,
	})

	// Block
	if err := d.BlockIP("1.2.3.4", "test block"); err != nil {
		t.Fatal(err)
	}

	ok, err := d.IsBlocked("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("expected IP to be blocked")
	}

	// Verify account status
	acc, _ := d.GetIPAccount("1.2.3.4")
	if acc.Status != "blocked" {
		t.Errorf("status = %q, want blocked", acc.Status)
	}

	// ListBlockedIPs
	blocked, err := d.ListBlockedIPs()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocked) != 1 {
		t.Errorf("ListBlockedIPs: got %d, want 1", len(blocked))
	}
	if blocked[0].IP != "1.2.3.4" {
		t.Errorf("blocked IP = %q", blocked[0].IP)
	}

	// ListBlockedAccounts
	blockedAccs, err := d.ListBlockedAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(blockedAccs) != 1 {
		t.Errorf("ListBlockedAccounts: got %d, want 1", len(blockedAccs))
	}

	// Unblock
	if err := d.UnblockIP("1.2.3.4"); err != nil {
		t.Fatal(err)
	}

	ok, err = d.IsBlocked("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected IP to be unblocked")
	}

	// Account status should be reset
	acc, _ = d.GetIPAccount("1.2.3.4")
	if acc.Status != "unknown" {
		t.Errorf("status after unblock = %q, want unknown", acc.Status)
	}
}

func TestIsBlockedNotFound(t *testing.T) {
	d := openTestDB(t)
	ok, err := d.IsBlocked("99.99.99.99")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected false for unknown IP")
	}
}

// ---------------------------------------------------------------------------
// ListIngestedArchives
// ---------------------------------------------------------------------------

func TestListIngestedArchives(t *testing.T) {
	d := openTestDB(t)

	for _, name := range []string{"a.tar.gz", "b.tar.gz", "c.tar.gz"} {
		_ = d.MarkArchiveIngested(&db.IngestedArchive{
			Filename:   name,
			IngestedAt: "2025-01-01T00:00:00Z",
		})
	}

	all, err := d.ListIngestedArchives(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("list all: got %d, want 3", len(all))
	}

	limited, err := d.ListIngestedArchives(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Errorf("list limited: got %d, want 2", len(limited))
	}
}

// ---------------------------------------------------------------------------
// PurgeIntelCache
// ---------------------------------------------------------------------------

func TestPurgeIntelCache(t *testing.T) {
	d := openTestDB(t)

	_ = d.UpsertIntelCache("1.2.3.4", "abuseipdb", "2025-01-01", `{"score":50}`)
	_ = d.UpsertIntelCache("1.2.3.4", "virustotal", "2025-01-01", `{"m":3}`)
	_ = d.UpsertIntelCache("5.6.7.8", "abuseipdb", "2025-01-01", `{"score":10}`)

	// Purge single IP
	n, err := d.PurgeIntelCache("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("purge 1.2.3.4: got %d, want 2", n)
	}

	// Verify 5.6.7.8 still exists
	fa, _, err := d.GetIntelCache("5.6.7.8", "abuseipdb")
	if err != nil {
		t.Fatal(err)
	}
	if fa == "" {
		t.Error("5.6.7.8 cache should still exist")
	}

	// Purge all
	n, err = d.PurgeIntelCache("")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("purge all: got %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Aggregate chart queries (smoke tests)
// ---------------------------------------------------------------------------

func TestChartQueries(t *testing.T) {
	d := openTestDB(t)

	// Insert some data for chart queries to operate on
	_ = d.InsertRequestEvent(&db.RequestEvent{
		Archive: "test.tar.gz", Ts: "2025-01-01T12:00:00Z",
		IP: "1.2.3.4", Method: "GET", Path: "/rpc",
		Host: "api.example.com", Route: "rpc", Status: "ok", Country: "US",
	})
	_ = d.UpsertIPAccount(&db.IPAccount{
		IP: "1.2.3.4", FirstSeen: "2025-01-01", LastSeen: "2025-01-01",
		Country: "US", TotalRequests: 1, Status: "clean",
		Hostnames: "[]", OpenPorts: "[]", Services: "{}",
		ThreatFlags: "[]", Tags: "[]", ThreatScore: 0, VTMalicious: 0, AbuseScore: 0,
	})
	_ = d.InsertRateLimitEvent(&db.RateLimitEvent{
		Archive: "test.tar.gz", Ts: "2025-01-01T12:00:01Z",
		IP: "1.2.3.4", Event: "429", Reason: "429", RPS: 25.0, Burst: 100,
	})

	// Smoke test each chart query — just verify no errors
	if _, err := d.IPsOverTime(7); err != nil {
		t.Errorf("IPsOverTime: %v", err)
	}
	if _, err := d.RequestsOverTime(7); err != nil {
		t.Errorf("RequestsOverTime: %v", err)
	}
	if _, err := d.RateLimitsOverTime(7); err != nil {
		t.Errorf("RateLimitsOverTime: %v", err)
	}
	if _, err := d.TopCountries(5); err != nil {
		t.Errorf("TopCountries: %v", err)
	}
	if _, err := d.StatusBreakdown(); err != nil {
		t.Errorf("StatusBreakdown: %v", err)
	}
	if _, err := d.ThreatDistribution(); err != nil {
		t.Errorf("ThreatDistribution: %v", err)
	}
	if _, err := d.TopIPsByRequests(5); err != nil {
		t.Errorf("TopIPsByRequests: %v", err)
	}
	if _, err := d.RequestsByCountry(5); err != nil {
		t.Errorf("RequestsByCountry: %v", err)
	}
	if _, err := d.IPsOverTimeMulti(7); err != nil {
		t.Errorf("IPsOverTimeMulti: %v", err)
	}
	if _, err := d.EndpointSummary(5); err != nil {
		t.Errorf("EndpointSummary: %v", err)
	}
	if _, err := d.RequestsOverTimeMulti(7); err != nil {
		t.Errorf("RequestsOverTimeMulti: %v", err)
	}
}
