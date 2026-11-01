// SPDX-License-Identifier: AGPL-3.0-or-later

// Package store is the persistence module of the panel: it opens the
// database, runs the embedded migrations and gives access to the generated
// queries (NFR-002, NFR-013).
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver (D-038)

	"github.com/NightDragon1/rconductor/internal/store/sqlitedb"
)

// FileName is the name of the SQLite database inside the data directory.
const FileName = "rconductor.db"

// pragmas apply to every connection: WAL for concurrent readers, a busy
// timeout instead of immediate lock errors, enforced foreign keys, and
// write transactions that take the lock up front.
const pragmas = "?_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_txlock=immediate"

// Store is the open database of the panel.
type Store struct {
	DB *sql.DB
	// Path is the file path of the SQLite database.
	Path    string
	Queries *sqlitedb.Queries
}

// Open creates the data directory if needed and opens (or creates) the
// SQLite database in it. It does not run migrations.
func Open(ctx context.Context, dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	path := filepath.Join(dataDir, FileName)
	db, err := sql.Open("sqlite", path+pragmas)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Store{DB: db, Path: path, Queries: sqlitedb.New(db)}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }
