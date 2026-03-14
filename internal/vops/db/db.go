package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB is a thin wrapper around *sql.DB providing vOps-specific queries.
type DB struct {
	*sql.DB
}

// Open creates or opens the SQLite database at path, enables WAL mode,
// runs schema migrations, and returns a ready-to-use *DB.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("db: create dir %s: %w", dir, err)
	}

	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_cache_size=-64000&_foreign_keys=on"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}

	// SQLite is single-writer; one conn avoids SQLITE_BUSY contention.
	sqlDB.SetMaxOpenConns(1)

	if err := Migrate(sqlDB); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return &DB{sqlDB}, nil
}

// Close releases the underlying database connection.
func (d *DB) Close() error {
	return d.DB.Close()
}
