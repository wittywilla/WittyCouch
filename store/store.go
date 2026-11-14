// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 WittyWilla and contributors

// Package store is WittyCouch's own database (module API 5): a SQLite file in the module's own
// folder (WITTYMODULE_DATA), with migrations WittyCouch owns. It refers to the server's user ids as
// they are. It starts empty: nothing is brought over from a server's database.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// File is the database's name in the module's folder.
const File = "wittycouch.db"

// migrations are the schema, in order; each runs once.
var migrations = []string{
	// 1: each person's own list of PCs running Sunshine. The certificate here is the PC's own,
	// which isn't secret: each person's key is in a file (identities/u<id>).
	`CREATE TABLE IF NOT EXISTS hosts (
	   id INTEGER PRIMARY KEY,
	   user_id INTEGER NOT NULL,
	   name TEXT NOT NULL, address TEXT NOT NULL, port INTEGER NOT NULL DEFAULT 0,
	   uuid TEXT NOT NULL, mac TEXT NOT NULL DEFAULT '', cert TEXT NOT NULL DEFAULT '',
	   public INTEGER NOT NULL DEFAULT 0, paired_at INTEGER, created_at INTEGER NOT NULL,
	   UNIQUE(user_id, uuid));
	 CREATE INDEX IF NOT EXISTS hosts_user ON hosts(user_id);`,
}

// DB is WittyCouch's database.
type DB struct{ *sql.DB }

// Open opens (and makes, or brings up to date) the database in dir.
func Open(dir string) (*DB, error) {
	if dir == "" {
		return nil, fmt.Errorf("no data folder (WITTYMODULE_DATA): WittyCouch needs the data permission")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, File)+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d := &DB{db}
	if err := d.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate(ctx context.Context) error {
	if _, err := d.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	if err := d.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema`).Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schema`); err != nil {
			tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema(version) VALUES(?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
