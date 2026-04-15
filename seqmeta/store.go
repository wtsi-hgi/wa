/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package seqmeta

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const createWatermarksTableSQL = `
CREATE TABLE IF NOT EXISTS watermarks (
	query_key  TEXT    NOT NULL,
	entry_id   TEXT    NOT NULL,
	entry_hash TEXT    NOT NULL,
	updated_at TEXT    NOT NULL,
	tombstone  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (query_key, entry_id)
);`

// OpenStore opens a SQLite store and creates the schema on demand.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	if _, err := db.Exec(createWatermarksTableSQL); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}

	err := s.db.Close()
	s.db = nil

	return err
}

func (s *Store) WithLock(fn func() error) error {
	if s == nil {
		return fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return fn()
}

// LoadEntries returns all saved entries for one query key.
func (s *Store) LoadEntries(queryKey string) (map[string]StoredEntry, error) {
	entries := map[string]StoredEntry{}
	if s == nil || s.db == nil {
		return entries, fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	rows, err := s.db.Query(`
		SELECT entry_id, entry_hash, updated_at, tombstone
		FROM watermarks
		WHERE query_key = ?
	`, queryKey)
	if err != nil {
		return entries, fmt.Errorf("%w: %w", errStoreOperation, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var (
			entryID   string
			entryHash string
			updatedAt string
			tombstone int
		)

		if err := rows.Scan(&entryID, &entryHash, &updatedAt, &tombstone); err != nil {
			return entries, fmt.Errorf("%w: %w", errStoreOperation, err)
		}

		parsedTime, err := time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return entries, fmt.Errorf("%w: %w", errStoreOperation, err)
		}

		entries[entryID] = StoredEntry{
			EntryHash: entryHash,
			Tombstone: tombstone == 1,
			UpdatedAt: parsedTime,
		}
	}

	if err := rows.Err(); err != nil {
		return entries, fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	return entries, nil
}

// SaveEntries replaces the saved entry snapshot for one query key.
func (s *Store) SaveEntries(queryKey string, entries map[string]StoredEntry) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	if _, err := tx.Exec(`DELETE FROM watermarks WHERE query_key = ?`, queryKey); err != nil {
		_ = tx.Rollback()

		return fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	stmt, err := tx.Prepare(`
		INSERT INTO watermarks(query_key, entry_id, entry_hash, updated_at, tombstone)
		VALUES(?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()

		return fmt.Errorf("%w: %w", errStoreOperation, err)
	}
	defer func() {
		_ = stmt.Close()
	}()

	for entryID, entry := range entries {
		tombstone := 0
		if entry.Tombstone {
			tombstone = 1
		}

		if _, err := stmt.Exec(queryKey, entryID, entry.EntryHash, entry.UpdatedAt.UTC().Format(time.RFC3339Nano), tombstone); err != nil {
			_ = tx.Rollback()

			return fmt.Errorf("%w: %w", errStoreOperation, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	return nil
}
