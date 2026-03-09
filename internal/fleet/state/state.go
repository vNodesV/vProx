// Package state manages fleet module persistence via SQLite.
// Schema: deployments (history) + registered_chains (external chain monitoring).
package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// DB wraps *sql.DB with fleet-specific queries.
type DB struct{ *sql.DB }

// Deployment records one fleet operation (deploy run).
type Deployment struct {
	ID        int64
	Chain     string
	Component string
	VM        string
	Status    string // pending|running|done|failed
	Output    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// RegisteredChain is an externally-monitored chain (not managed by fleet scripts).
type RegisteredChain struct {
	Chain   string    `json:"chain"`
	RPCURL  string    `json:"rpc_url"`
	RESTURL string    `json:"rest_url"`
	Note    string    `json:"note"`
	AddedAt time.Time `json:"added_at"`
}

// Open creates or opens the fleet SQLite database at path.
// Applies WAL mode and runs schema migration.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("fleet/state: mkdir %s: %w", filepath.Dir(path), err)
	}

	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=on"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("fleet/state: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1) // SQLite is single-writer

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS deployments (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			chain      TEXT    NOT NULL,
			component  TEXT    NOT NULL,
			vm         TEXT    NOT NULL,
			status     TEXT    NOT NULL DEFAULT 'pending',
			output     TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS registered_chains (
			chain    TEXT PRIMARY KEY,
			rpc_url  TEXT NOT NULL,
			rest_url TEXT NOT NULL DEFAULT '',
			note     TEXT NOT NULL DEFAULT '',
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	return err
}

// InsertDeployment records a new deploy operation and returns its ID.
func (d *DB) InsertDeployment(chain, component, vm string) (int64, error) {
	r, err := d.Exec(
		`INSERT INTO deployments (chain,component,vm,status) VALUES (?,?,?,'pending')`,
		chain, component, vm,
	)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// UpdateDeployment updates the status and output of a deployment record.
func (d *DB) UpdateDeployment(id int64, status, output string) error {
	_, err := d.Exec(
		`UPDATE deployments SET status=?, output=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		status, output, id,
	)
	return err
}

// ListDeployments returns the 50 most recent deployments for chain.
// Pass chain="" to return all deployments.
func (d *DB) ListDeployments(chain string) ([]Deployment, error) {
	var rows *sql.Rows
	var err error
	if chain == "" {
		rows, err = d.Query(
			`SELECT id,chain,component,vm,status,COALESCE(output,''),created_at,updated_at
			 FROM deployments ORDER BY id DESC LIMIT 50`,
		)
	} else {
		rows, err = d.Query(
			`SELECT id,chain,component,vm,status,COALESCE(output,''),created_at,updated_at
			 FROM deployments WHERE chain=? ORDER BY id DESC LIMIT 50`,
			chain,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Deployment, 0)
	for rows.Next() {
		var dep Deployment
		if err := rows.Scan(
			&dep.ID, &dep.Chain, &dep.Component, &dep.VM,
			&dep.Status, &dep.Output, &dep.CreatedAt, &dep.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, dep)
	}
	return out, rows.Err()
}

// AddRegisteredChain inserts or replaces an externally-monitored chain.
func (d *DB) AddRegisteredChain(chain, rpcURL, restURL, note string) error {
	_, err := d.Exec(
		`INSERT OR REPLACE INTO registered_chains (chain,rpc_url,rest_url,note) VALUES (?,?,?,?)`,
		chain, rpcURL, restURL, note,
	)
	return err
}

// RemoveRegisteredChain deletes an externally-monitored chain.
func (d *DB) RemoveRegisteredChain(chain string) error {
	_, err := d.Exec(`DELETE FROM registered_chains WHERE chain=?`, chain)
	return err
}

// ListRegisteredChains returns all externally-monitored chains.
func (d *DB) ListRegisteredChains() ([]RegisteredChain, error) {
	rows, err := d.Query(
		`SELECT chain,rpc_url,COALESCE(rest_url,''),COALESCE(note,''),added_at
		 FROM registered_chains ORDER BY chain`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RegisteredChain, 0)
	for rows.Next() {
		var rc RegisteredChain
		if err := rows.Scan(&rc.Chain, &rc.RPCURL, &rc.RESTURL, &rc.Note, &rc.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}
