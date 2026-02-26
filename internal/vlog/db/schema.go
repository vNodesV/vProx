package db

import (
	"database/sql"
	"fmt"
	"strings"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS ip_accounts (
	ip                TEXT PRIMARY KEY,
	first_seen        TEXT NOT NULL,
	last_seen         TEXT NOT NULL,
	total_requests    INTEGER NOT NULL DEFAULT 0,
	ratelimit_events  INTEGER NOT NULL DEFAULT 0,
	country           TEXT NOT NULL DEFAULT '',
	asn               TEXT NOT NULL DEFAULT '',
	org               TEXT NOT NULL DEFAULT '',
	hostnames         TEXT NOT NULL DEFAULT '[]',
	open_ports        TEXT NOT NULL DEFAULT '[]',
	services          TEXT NOT NULL DEFAULT '{}',
	vt_malicious      INTEGER NOT NULL DEFAULT -1,
	vt_data           TEXT NOT NULL DEFAULT '',
	abuse_score       INTEGER NOT NULL DEFAULT -1,
	abuse_data        TEXT NOT NULL DEFAULT '',
	shodan_data       TEXT NOT NULL DEFAULT '',
	threat_score      INTEGER NOT NULL DEFAULT -1,
	threat_flags      TEXT NOT NULL DEFAULT '[]',
	intel_updated_at  TEXT NOT NULL DEFAULT '',
	notes             TEXT NOT NULL DEFAULT '',
	tags              TEXT NOT NULL DEFAULT '[]',
	status            TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE IF NOT EXISTS request_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	archive     TEXT NOT NULL,
	ts          TEXT NOT NULL,
	request_id  TEXT NOT NULL DEFAULT '',
	ip          TEXT NOT NULL,
	method      TEXT NOT NULL DEFAULT '',
	path        TEXT NOT NULL DEFAULT '',
	host        TEXT NOT NULL DEFAULT '',
	route       TEXT NOT NULL DEFAULT '',
	status      TEXT NOT NULL DEFAULT '',
	country     TEXT NOT NULL DEFAULT '',
	asn         TEXT NOT NULL DEFAULT '',
	user_agent  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ratelimit_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	archive     TEXT NOT NULL,
	ts          TEXT NOT NULL,
	request_id  TEXT NOT NULL DEFAULT '',
	ip          TEXT NOT NULL,
	event       TEXT NOT NULL DEFAULT '',
	reason      TEXT NOT NULL DEFAULT '',
	method      TEXT NOT NULL DEFAULT '',
	path        TEXT NOT NULL DEFAULT '',
	host        TEXT NOT NULL DEFAULT '',
	country     TEXT NOT NULL DEFAULT '',
	asn         TEXT NOT NULL DEFAULT '',
	user_agent  TEXT NOT NULL DEFAULT '',
	rps         REAL NOT NULL DEFAULT 0,
	burst       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS ingested_archives (
	filename        TEXT PRIMARY KEY,
	ingested_at     TEXT NOT NULL,
	request_count   INTEGER NOT NULL DEFAULT 0,
	ratelimit_count INTEGER NOT NULL DEFAULT 0,
	size_bytes      INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS intel_cache (
	ip         TEXT NOT NULL,
	source     TEXT NOT NULL,
	fetched_at TEXT NOT NULL,
	data       TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (ip, source)
);

CREATE INDEX IF NOT EXISTS idx_request_events_ip ON request_events(ip);
CREATE INDEX IF NOT EXISTS idx_request_events_ts ON request_events(ts);
CREATE INDEX IF NOT EXISTS idx_ratelimit_events_ip ON ratelimit_events(ip);
CREATE INDEX IF NOT EXISTS idx_ratelimit_events_ts ON ratelimit_events(ts);
CREATE INDEX IF NOT EXISTS idx_ip_accounts_status ON ip_accounts(status);
CREATE INDEX IF NOT EXISTS idx_ip_accounts_threat_score ON ip_accounts(threat_score);
`

// Migrate executes the schema DDL against db, creating all tables and
// indexes if they do not already exist.
func Migrate(db *sql.DB) error {
	for _, stmt := range strings.Split(schemaSQL, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}
