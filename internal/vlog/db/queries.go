package db

import (
	"database/sql"
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Domain structs
// ---------------------------------------------------------------------------

// IPAccount mirrors the ip_accounts table.
type IPAccount struct {
	IP              string
	FirstSeen       string
	LastSeen        string
	TotalRequests   int64
	RatelimitEvents int64
	Country         string
	ASN             string
	Org             string
	Hostnames       string // JSON array
	OpenPorts       string // JSON array of ints
	Services        string // JSON object
	VTMalicious     int64
	VTData          string
	AbuseScore      int64
	AbuseData       string
	ShodanData      string
	ThreatScore     int64
	ThreatFlags     string // JSON array
	IntelUpdatedAt  string
	Notes           string
	Tags            string // JSON array
	Status          string
	// OSINT fields (populated by OSINTStream)
	RDNS           string  // comma-joined PTR records
	AbuseEmail     string  // abuse contact email
	Moniker        string  // Cosmos RPC moniker
	ChainID        string  // Cosmos chain/network ID
	PingMs         float64 // TCP latency to first open port (-1 = untested)
	Protocol       string  // "https", "http", or ""
	OSINTUpdatedAt string
}

// RequestEvent mirrors the request_events table.
type RequestEvent struct {
	ID        int64
	Archive   string
	Ts        string
	RequestID string
	IP        string
	Method    string
	Path      string
	Host      string
	Route     string
	Status    string
	Country   string
	ASN       string
	UserAgent string
}

// RateLimitEvent mirrors the ratelimit_events table.
type RateLimitEvent struct {
	ID        int64
	Archive   string
	Ts        string
	RequestID string
	IP        string
	Event     string
	Reason    string
	Method    string
	Path      string
	Host      string
	Country   string
	ASN       string
	UserAgent string
	RPS       float64
	Burst     int64
}

// IngestedArchive mirrors the ingested_archives table.
type IngestedArchive struct {
	Filename       string
	IngestedAt     string
	RequestCount   int64
	RatelimitCount int64
	SizeBytes      int64
}

// BlockedIP mirrors the blocked_ips table.
type BlockedIP struct {
	ID         int64
	IP         string
	BlockedAt  string
	Reason     string
	UFWApplied bool
}

// ---------------------------------------------------------------------------
// IP accounts
// ---------------------------------------------------------------------------

// UpsertIPAccount inserts or replaces the given IP account row.
func (d *DB) UpsertIPAccount(a *IPAccount) error {
	const q = `INSERT OR REPLACE INTO ip_accounts (
		ip, first_seen, last_seen, total_requests, ratelimit_events,
		country, asn, org, hostnames, open_ports, services,
		vt_malicious, vt_data, abuse_score, abuse_data, shodan_data,
		threat_score, threat_flags, intel_updated_at,
		notes, tags, status,
		rdns, abuse_email, moniker, chain_id, ping_ms, protocol, osint_updated_at
	) VALUES (
		?, ?, ?, ?, ?,
		?, ?, ?, ?, ?, ?,
		?, ?, ?, ?, ?,
		?, ?, ?,
		?, ?, ?,
		?, ?, ?, ?, ?, ?, ?
	)`
	_, err := d.Exec(q,
		a.IP, a.FirstSeen, a.LastSeen, a.TotalRequests, a.RatelimitEvents,
		a.Country, a.ASN, a.Org, a.Hostnames, a.OpenPorts, a.Services,
		a.VTMalicious, a.VTData, a.AbuseScore, a.AbuseData, a.ShodanData,
		a.ThreatScore, a.ThreatFlags, a.IntelUpdatedAt,
		a.Notes, a.Tags, a.Status,
		a.RDNS, a.AbuseEmail, a.Moniker, a.ChainID, a.PingMs, a.Protocol, a.OSINTUpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert ip_account %s: %w", a.IP, err)
	}
	return nil
}

// GetIPAccount returns the account for ip, or (nil, nil) if not found.
func (d *DB) GetIPAccount(ip string) (*IPAccount, error) {
	const q = `SELECT
		ip, first_seen, last_seen, total_requests, ratelimit_events,
		country, asn, org, hostnames, open_ports, services,
		vt_malicious, vt_data, abuse_score, abuse_data, shodan_data,
		threat_score, threat_flags, intel_updated_at,
		notes, tags, status,
		rdns, abuse_email, moniker, chain_id, ping_ms, protocol, osint_updated_at
	FROM ip_accounts WHERE ip = ?`

	a := &IPAccount{}
	err := d.QueryRow(q, ip).Scan(
		&a.IP, &a.FirstSeen, &a.LastSeen, &a.TotalRequests, &a.RatelimitEvents,
		&a.Country, &a.ASN, &a.Org, &a.Hostnames, &a.OpenPorts, &a.Services,
		&a.VTMalicious, &a.VTData, &a.AbuseScore, &a.AbuseData, &a.ShodanData,
		&a.ThreatScore, &a.ThreatFlags, &a.IntelUpdatedAt,
		&a.Notes, &a.Tags, &a.Status,
		&a.RDNS, &a.AbuseEmail, &a.Moniker, &a.ChainID, &a.PingMs, &a.Protocol, &a.OSINTUpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ip_account %s: %w", ip, err)
	}
	return a, nil
}

// ListIPAccounts returns up to limit accounts starting at offset,
// ordered by last_seen descending.
func (d *DB) ListIPAccounts(limit, offset int) ([]*IPAccount, error) {
	const q = `SELECT
		ip, first_seen, last_seen, total_requests, ratelimit_events,
		country, asn, org, hostnames, open_ports, services,
		vt_malicious, vt_data, abuse_score, abuse_data, shodan_data,
		threat_score, threat_flags, intel_updated_at,
		notes, tags, status
	FROM ip_accounts ORDER BY last_seen DESC LIMIT ? OFFSET ?`

	rows, err := d.Query(q, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list ip_accounts: %w", err)
	}
	defer rows.Close()

	var out []*IPAccount
	for rows.Next() {
		a := &IPAccount{}
		if err := rows.Scan(
			&a.IP, &a.FirstSeen, &a.LastSeen, &a.TotalRequests, &a.RatelimitEvents,
			&a.Country, &a.ASN, &a.Org, &a.Hostnames, &a.OpenPorts, &a.Services,
			&a.VTMalicious, &a.VTData, &a.AbuseScore, &a.AbuseData, &a.ShodanData,
			&a.ThreatScore, &a.ThreatFlags, &a.IntelUpdatedAt,
			&a.Notes, &a.Tags, &a.Status,
		); err != nil {
			return nil, fmt.Errorf("list ip_accounts scan: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Blocked IPs
// ---------------------------------------------------------------------------

// BlockIP inserts a blocked_ips row and sets ip_accounts.status to "blocked".
func (d *DB) BlockIP(ip, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.Exec(
		`INSERT INTO blocked_ips (ip, blocked_at, reason) VALUES (?, ?, ?)`,
		ip, now, reason,
	)
	if err != nil {
		return fmt.Errorf("block ip %s: %w", ip, err)
	}
	_, err = d.Exec(
		`UPDATE ip_accounts SET status = 'blocked' WHERE ip = ?`, ip,
	)
	if err != nil {
		return fmt.Errorf("block ip update status %s: %w", ip, err)
	}
	return nil
}

// UnblockIP removes the blocked_ips row and resets ip_accounts.status to "unknown"
// (only if it was "blocked").
func (d *DB) UnblockIP(ip string) error {
	_, err := d.Exec(`DELETE FROM blocked_ips WHERE ip = ?`, ip)
	if err != nil {
		return fmt.Errorf("unblock ip %s: %w", ip, err)
	}
	_, err = d.Exec(
		`UPDATE ip_accounts SET status = 'unknown' WHERE ip = ? AND status = 'blocked'`, ip,
	)
	if err != nil {
		return fmt.Errorf("unblock ip update status %s: %w", ip, err)
	}
	return nil
}

// IsBlocked returns true if ip has an entry in the blocked_ips table.
func (d *DB) IsBlocked(ip string) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM blocked_ips WHERE ip = ?`, ip).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("is blocked %s: %w", ip, err)
	}
	return n > 0, nil
}

// ListBlockedIPs returns all blocked IPs ordered by blocked_at descending.
func (d *DB) ListBlockedIPs() ([]BlockedIP, error) {
	const q = `SELECT id, ip, blocked_at, reason, ufw_applied
		FROM blocked_ips ORDER BY blocked_at DESC`
	rows, err := d.Query(q)
	if err != nil {
		return nil, fmt.Errorf("list blocked_ips: %w", err)
	}
	defer rows.Close()
	var out []BlockedIP
	for rows.Next() {
		var b BlockedIP
		if err := rows.Scan(&b.ID, &b.IP, &b.BlockedAt, &b.Reason, &b.UFWApplied); err != nil {
			return nil, fmt.Errorf("scan blocked_ip: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Request events
// ---------------------------------------------------------------------------

// InsertRequestEvent inserts a single request event row.
func (d *DB) InsertRequestEvent(e *RequestEvent) error {
	const q = `INSERT INTO request_events (
		archive, ts, request_id, ip, method, path, host, route,
		status, country, asn, user_agent
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := d.Exec(q,
		e.Archive, e.Ts, e.RequestID, e.IP, e.Method, e.Path, e.Host, e.Route,
		e.Status, e.Country, e.ASN, e.UserAgent,
	)
	if err != nil {
		return fmt.Errorf("insert request_event: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Rate-limit events
// ---------------------------------------------------------------------------

// InsertRateLimitEvent inserts a single rate-limit event row.
func (d *DB) InsertRateLimitEvent(e *RateLimitEvent) error {
	const q = `INSERT INTO ratelimit_events (
		archive, ts, request_id, ip, event, reason, method, path, host,
		country, asn, user_agent, rps, burst
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := d.Exec(q,
		e.Archive, e.Ts, e.RequestID, e.IP, e.Event, e.Reason,
		e.Method, e.Path, e.Host,
		e.Country, e.ASN, e.UserAgent, e.RPS, e.Burst,
	)
	if err != nil {
		return fmt.Errorf("insert ratelimit_event: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Ingested archives
// ---------------------------------------------------------------------------

// IsArchiveIngested returns true if filename has already been ingested.
func (d *DB) IsArchiveIngested(filename string) (bool, error) {
	var n int
	err := d.QueryRow(
		`SELECT COUNT(*) FROM ingested_archives WHERE filename = ?`, filename,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check ingested %s: %w", filename, err)
	}
	return n > 0, nil
}

// MarkArchiveIngested records the archive as ingested.
func (d *DB) MarkArchiveIngested(a *IngestedArchive) error {
	const q = `INSERT OR REPLACE INTO ingested_archives (
		filename, ingested_at, request_count, ratelimit_count, size_bytes
	) VALUES (?, ?, ?, ?, ?)`
	_, err := d.Exec(q,
		a.Filename, a.IngestedAt, a.RequestCount, a.RatelimitCount, a.SizeBytes,
	)
	if err != nil {
		return fmt.Errorf("mark ingested %s: %w", a.Filename, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Intel cache
// ---------------------------------------------------------------------------

// UpsertIntelCache inserts or replaces a cached intelligence result.
func (d *DB) UpsertIntelCache(ip, source, fetchedAt, data string) error {
	const q = `INSERT OR REPLACE INTO intel_cache (ip, source, fetched_at, data)
		VALUES (?, ?, ?, ?)`
	_, err := d.Exec(q, ip, source, fetchedAt, data)
	if err != nil {
		return fmt.Errorf("upsert intel_cache %s/%s: %w", ip, source, err)
	}
	return nil
}

// GetIntelCache returns cached intel for ip+source. If not found both
// fetchedAt and data are empty strings and err is nil.
func (d *DB) GetIntelCache(ip, source string) (fetchedAt, data string, err error) {
	err = d.QueryRow(
		`SELECT fetched_at, data FROM intel_cache WHERE ip = ? AND source = ?`,
		ip, source,
	).Scan(&fetchedAt, &data)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get intel_cache %s/%s: %w", ip, source, err)
	}
	return fetchedAt, data, nil
}

// ListRequestEventsByIP returns the most recent limit request events for ip.
func (d *DB) ListRequestEventsByIP(ip string, limit int) ([]*RequestEvent, error) {
	const q = `SELECT id, archive, ts, request_id, ip, method, path, host, route,
		status, country, asn, user_agent
	FROM request_events WHERE ip = ? ORDER BY ts DESC LIMIT ?`
	rows, err := d.Query(q, ip, limit)
	if err != nil {
		return nil, fmt.Errorf("list request_events ip=%s: %w", ip, err)
	}
	defer rows.Close()
	var out []*RequestEvent
	for rows.Next() {
		e := &RequestEvent{}
		if err := rows.Scan(&e.ID, &e.Archive, &e.Ts, &e.RequestID, &e.IP, &e.Method,
			&e.Path, &e.Host, &e.Route, &e.Status, &e.Country, &e.ASN, &e.UserAgent); err != nil {
			return nil, fmt.Errorf("scan request_event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListRateLimitEventsByIP returns the most recent limit rate-limit events for ip.
func (d *DB) ListRateLimitEventsByIP(ip string, limit int) ([]*RateLimitEvent, error) {
	const q = `SELECT id, archive, ts, request_id, ip, event, reason, method, path, host,
		country, asn, user_agent, rps, burst
	FROM ratelimit_events WHERE ip = ? ORDER BY ts DESC LIMIT ?`
	rows, err := d.Query(q, ip, limit)
	if err != nil {
		return nil, fmt.Errorf("list ratelimit_events ip=%s: %w", ip, err)
	}
	defer rows.Close()
	var out []*RateLimitEvent
	for rows.Next() {
		e := &RateLimitEvent{}
		if err := rows.Scan(&e.ID, &e.Archive, &e.Ts, &e.RequestID, &e.IP, &e.Event,
			&e.Reason, &e.Method, &e.Path, &e.Host, &e.Country, &e.ASN, &e.UserAgent,
			&e.RPS, &e.Burst); err != nil {
			return nil, fmt.Errorf("scan ratelimit_event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListTopThreatAccounts returns the top limit accounts by threat_score descending.
// Only accounts with threat_score >= 0 are included.
func (d *DB) ListTopThreatAccounts(limit int) ([]*IPAccount, error) {
	const q = `SELECT
		ip, first_seen, last_seen, total_requests, ratelimit_events,
		country, asn, org, hostnames, open_ports, services,
		vt_malicious, vt_data, abuse_score, abuse_data, shodan_data,
		threat_score, threat_flags, intel_updated_at, notes, tags, status
	FROM ip_accounts WHERE threat_score >= 0 ORDER BY threat_score DESC LIMIT ?`
	rows, err := d.Query(q, limit)
	if err != nil {
		return nil, fmt.Errorf("list top threat accounts: %w", err)
	}
	defer rows.Close()
	var out []*IPAccount
	for rows.Next() {
		a := &IPAccount{}
		if err := rows.Scan(
			&a.IP, &a.FirstSeen, &a.LastSeen, &a.TotalRequests, &a.RatelimitEvents,
			&a.Country, &a.ASN, &a.Org, &a.Hostnames, &a.OpenPorts, &a.Services,
			&a.VTMalicious, &a.VTData, &a.AbuseScore, &a.AbuseData, &a.ShodanData,
			&a.ThreatScore, &a.ThreatFlags, &a.IntelUpdatedAt, &a.Notes, &a.Tags, &a.Status,
		); err != nil {
			return nil, fmt.Errorf("scan top threat account: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountIPAccounts returns the total number of IP accounts.
func (d *DB) CountIPAccounts() (int64, error) {
	var n int64
	err := d.QueryRow(`SELECT COUNT(*) FROM ip_accounts`).Scan(&n)
	return n, err
}

// ListIngestedArchives returns up to limit ingested archives ordered by
// ingested_at descending. If limit <= 0 all rows are returned.
func (d *DB) ListIngestedArchives(limit int) ([]*IngestedArchive, error) {
	var (
		rows *sql.Rows
		err  error
	)
	const base = `SELECT filename, ingested_at, request_count, ratelimit_count, size_bytes
		FROM ingested_archives ORDER BY ingested_at DESC`
	if limit > 0 {
		rows, err = d.Query(base+` LIMIT ?`, limit)
	} else {
		rows, err = d.Query(base)
	}
	if err != nil {
		return nil, fmt.Errorf("list ingested_archives: %w", err)
	}
	defer rows.Close()

	var out []*IngestedArchive
	for rows.Next() {
		a := &IngestedArchive{}
		if err := rows.Scan(&a.Filename, &a.IngestedAt, &a.RequestCount, &a.RatelimitCount, &a.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan ingested_archive: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// PurgeIntelCache deletes cached intelligence entries.
// If ip is non-empty only entries for that IP are removed; if empty all
// entries are removed. Returns the number of rows deleted.
func (d *DB) PurgeIntelCache(ip string) (int64, error) {
	var res sql.Result
	var err error
	if ip != "" {
		res, err = d.Exec(`DELETE FROM intel_cache WHERE ip = ?`, ip)
	} else {
		res, err = d.Exec(`DELETE FROM intel_cache`)
	}
	if err != nil {
		return 0, fmt.Errorf("purge intel_cache: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ---------------------------------------------------------------------------
// Aggregate stats
// ---------------------------------------------------------------------------

// Stats returns high-level counts useful for the dashboard.
// Keys: total_ips, total_requests, total_ratelimit_events, total_archives,
// flagged_ips.
func (d *DB) Stats() (map[string]int64, error) {
	m := map[string]int64{
		"total_ips":              0,
		"total_requests":         0,
		"total_ratelimit_events": 0,
		"total_archives":         0,
		"flagged_ips":            0,
	}

	queries := []struct {
		key string
		sql string
	}{
		{"total_ips", `SELECT COUNT(*) FROM ip_accounts`},
		{"total_requests", `SELECT COUNT(*) FROM request_events`},
		{"total_ratelimit_events", `SELECT COUNT(*) FROM ratelimit_events`},
		{"total_archives", `SELECT COUNT(*) FROM ingested_archives`},
		{"flagged_ips", `SELECT COUNT(*) FROM ip_accounts WHERE threat_score > 0`},
	}

	for _, q := range queries {
		var n int64
		if err := d.QueryRow(q.sql).Scan(&n); err != nil {
			return nil, fmt.Errorf("stats %s: %w", q.key, err)
		}
		m[q.key] = n
	}
	return m, nil
}
