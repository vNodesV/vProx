package ingest

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vNodesV/vProx/internal/vlog/db"
)

// ---------------------------------------------------------------------------
// Ingester
// ---------------------------------------------------------------------------

// Ingester scans vProx backup archives and loads their log data into the
// vLog database.
type Ingester struct {
	db          *db.DB
	archivesDir string
}

// New returns an Ingester that reads archives from archivesDir and writes
// parsed events into d.
func New(d *db.DB, archivesDir string) *Ingester {
	return &Ingester{db: d, archivesDir: archivesDir}
}

// ---------------------------------------------------------------------------
// IngestAll
// ---------------------------------------------------------------------------

// IngestAll scans archivesDir for *.tar.gz files not yet recorded in
// ingested_archives, sorts them oldest-first by filename (the
// backup.YYYYMMDD_HHMMSS naming guarantees lexicographic == chronological),
// and ingests each one sequentially.
func (ing *Ingester) IngestAll() (processed int, err error) {
	matches, err := filepath.Glob(filepath.Join(ing.archivesDir, "*.tar.gz"))
	if err != nil {
		return 0, fmt.Errorf("ingest: glob %s: %w", ing.archivesDir, err)
	}

	sort.Strings(matches) // oldest first

	for _, path := range matches {
		skipped, err := ing.IngestOne(path)
		if err != nil {
			return processed, fmt.Errorf("ingest %s: %w", filepath.Base(path), err)
		}
		if !skipped {
			processed++
		}
	}
	return processed, nil
}

// ---------------------------------------------------------------------------
// IngestOne
// ---------------------------------------------------------------------------

// IngestAllForce re-ingests all *.tar.gz archives in archivesDir regardless
// of prior ingestion state, overwriting existing records.
func (ing *Ingester) IngestAllForce() (processed int, err error) {
	matches, err := filepath.Glob(filepath.Join(ing.archivesDir, "*.tar.gz"))
	if err != nil {
		return 0, fmt.Errorf("ingest: glob %s: %w", ing.archivesDir, err)
	}
	sort.Strings(matches)
	for _, path := range matches {
		if err := ing.ingestCore(path); err != nil {
			return processed, fmt.Errorf("ingest %s: %w", filepath.Base(path), err)
		}
		processed++
	}
	return processed, nil
}

// IngestOne ingests a single archive file by absolute path.
// If the archive has already been ingested it returns (true, nil).
func (ing *Ingester) IngestOne(path string) (skipped bool, err error) {
	name := filepath.Base(path)

	already, err := ing.db.IsArchiveIngested(name)
	if err != nil {
		return false, err
	}
	if already {
		return true, nil
	}

	return false, ing.ingestCore(path)
}

// ingestCore performs the full ingestion of a single archive (no dedup check).
func (ing *Ingester) ingestCore(path string) error {
	name := filepath.Base(path)

	// File size for bookkeeping.
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", name, err)
	}
	sizeBytes := info.Size()

	// Derive archive timestamp from filename; fall back to mtime.
	archiveTS := extractArchiveTS(name)
	if archiveTS == "" {
		archiveTS = info.ModTime().UTC().Format(time.RFC3339)
	}

	// Open archive.
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip %s: %w", name, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	var requests []*db.RequestEvent
	var ratelimits []*db.RateLimitEvent

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next %s: %w", name, err)
		}

		entryName := filepath.Base(hdr.Name)

		switch {
		case strings.HasSuffix(entryName, ".log"):
			// Handles main.log and any chain-specific *.log files.
			data, err := io.ReadAll(io.LimitReader(tr, 64<<20))
			if err != nil {
				return fmt.Errorf("read %s in %s: %w", entryName, name, err)
			}
			for _, line := range strings.Split(string(data), "\n") {
				if ev := ParseLogLine(line, name, archiveTS); ev != nil {
					requests = append(requests, ev)
				}
			}

		case entryName == "rate-limit.jsonl":
			data, err := io.ReadAll(io.LimitReader(tr, 64<<20))
			if err != nil {
				return fmt.Errorf("read rate-limit.jsonl in %s: %w", name, err)
			}
			for _, line := range strings.Split(string(data), "\n") {
				if ev := ParseRateLimitLine(line, name); ev != nil {
					ratelimits = append(ratelimits, ev)
				}
			}
		}
	}

	// --- Insert all events inside a transaction. ---
	tx, err := ing.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after commit

	for _, e := range requests {
		const q = `INSERT INTO request_events (
			archive, ts, request_id, ip, method, path, host, route,
			status, country, asn, user_agent
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		if _, err := tx.Exec(q,
			e.Archive, e.Ts, e.RequestID, e.IP, e.Method, e.Path, e.Host, e.Route,
			e.Status, e.Country, e.ASN, e.UserAgent,
		); err != nil {
			return fmt.Errorf("insert request_event in %s: %w", name, err)
		}
		isWS := strings.HasPrefix(strings.ToLower(e.Route), "ws")
		if err := ing.db.UpsertHostTraffic(tx, e.Host, isWS); err != nil {
			return fmt.Errorf("upsert host traffic in %s: %w", name, err)
		}
	}

	for _, e := range ratelimits {
		const q = `INSERT INTO ratelimit_events (
			archive, ts, request_id, ip, event, reason, method, path, host,
			country, asn, user_agent, rps, burst
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
		if _, err := tx.Exec(q,
			e.Archive, e.Ts, e.RequestID, e.IP, e.Event, e.Reason,
			e.Method, e.Path, e.Host,
			e.Country, e.ASN, e.UserAgent, e.RPS, e.Burst,
		); err != nil {
			return fmt.Errorf("insert ratelimit_event in %s: %w", name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx %s: %w", name, err)
	}

	// --- Upsert IP accounts outside the event-insert tx. ---
	if err := ing.upsertAccounts(requests, ratelimits, archiveTS); err != nil {
		return fmt.Errorf("upsert accounts %s: %w", name, err)
	}

	// --- Mark archive as ingested. ---
	now := time.Now().UTC().Format(time.RFC3339)
	return ing.db.MarkArchiveIngested(&db.IngestedArchive{
		Filename:       name,
		IngestedAt:     now,
		RequestCount:   int64(len(requests)),
		RatelimitCount: int64(len(ratelimits)),
		SizeBytes:      sizeBytes,
	})
}

// ---------------------------------------------------------------------------
// IP account roll-up
// ---------------------------------------------------------------------------

// ipStats accumulates per-IP counters during a single archive ingestion.
type ipStats struct {
	firstSeen string
	lastSeen  string
	requests  int64
	ratelimit int64
	country   string
	asn       string
}

// upsertAccounts merges per-IP statistics from the just-ingested events
// into the ip_accounts table (insert-or-update).
func (ing *Ingester) upsertAccounts(reqs []*db.RequestEvent, rls []*db.RateLimitEvent, archiveTS string) error {
	m := make(map[string]*ipStats)

	ensure := func(ip string) *ipStats {
		s, ok := m[ip]
		if !ok {
			s = &ipStats{firstSeen: archiveTS, lastSeen: archiveTS}
			m[ip] = s
		}
		return s
	}

	for _, e := range reqs {
		if e.IP == "" {
			continue
		}
		s := ensure(e.IP)
		s.requests++
		if e.Country != "" {
			s.country = e.Country
		}
		if e.ASN != "" {
			s.asn = e.ASN
		}
	}

	for _, e := range rls {
		if e.IP == "" {
			continue
		}
		s := ensure(e.IP)
		s.ratelimit++
		if e.Country != "" {
			s.country = e.Country
		}
		if e.ASN != "" {
			s.asn = e.ASN
		}
	}

	for ip, s := range m {
		existing, err := ing.db.GetIPAccount(ip)
		if err != nil {
			return err
		}

		if existing != nil {
			// Merge: widen time window, accumulate counts, update geo.
			if existing.FirstSeen != "" && existing.FirstSeen < s.firstSeen {
				s.firstSeen = existing.FirstSeen
			}
			if existing.LastSeen != "" && existing.LastSeen > s.lastSeen {
				s.lastSeen = existing.LastSeen
			}
			s.requests += existing.TotalRequests
			s.ratelimit += existing.RatelimitEvents
			if s.country == "" {
				s.country = existing.Country
			}
			if s.asn == "" {
				s.asn = existing.ASN
			}

			existing.FirstSeen = s.firstSeen
			existing.LastSeen = s.lastSeen
			existing.TotalRequests = s.requests
			existing.RatelimitEvents = s.ratelimit
			existing.Country = s.country
			existing.ASN = s.asn

			if err := ing.db.UpsertIPAccount(existing); err != nil {
				return err
			}
		} else {
			acct := &db.IPAccount{
				IP:              ip,
				FirstSeen:       s.firstSeen,
				LastSeen:        s.lastSeen,
				TotalRequests:   s.requests,
				RatelimitEvents: s.ratelimit,
				Country:         s.country,
				ASN:             s.asn,
				Status:          "active",
			}
			if err := ing.db.UpsertIPAccount(acct); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ListPending
// ---------------------------------------------------------------------------

// ListPending returns absolute paths of *.tar.gz files in archivesDir that
// have not yet been ingested, sorted oldest-first by filename.
func (ing *Ingester) ListPending() ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(ing.archivesDir, "*.tar.gz"))
	if err != nil {
		return nil, fmt.Errorf("ingest: glob %s: %w", ing.archivesDir, err)
	}
	sort.Strings(matches) // oldest first

	var pending []string
	for _, path := range matches {
		already, err := ing.db.IsArchiveIngested(filepath.Base(path))
		if err != nil {
			return nil, err
		}
		if !already {
			pending = append(pending, path)
		}
	}
	return pending, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractArchiveTS parses the embedded timestamp from a vProx backup
// filename (e.g. "backup.20240115_093045.tar.gz") and returns it as an
// ISO 8601 / RFC 3339 string. Returns "" on parse failure.
func extractArchiveTS(filename string) string {
	// Strip extension(s): backup.20240115_093045.tar.gz → backup.20240115_093045
	base := strings.TrimSuffix(filename, ".tar.gz")
	// Split on '.' → ["backup", "20240115_093045"]
	parts := strings.SplitN(base, ".", 2)
	if len(parts) < 2 {
		return ""
	}
	t, err := time.Parse("20060102_150405", parts[1])
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
