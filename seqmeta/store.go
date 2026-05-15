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
	"encoding/json"
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

const createEnrichCacheTableSQL = `
CREATE TABLE IF NOT EXISTS enrich_cache (
	identifier  TEXT    NOT NULL PRIMARY KEY,
	type        TEXT    NOT NULL,
	body        BLOB    NOT NULL,
	fetched_at  TEXT    NOT NULL,
	ttl_seconds INTEGER NOT NULL,
	cache_version INTEGER NOT NULL DEFAULT 0,
	negative    INTEGER NOT NULL DEFAULT 0,
	partial     INTEGER NOT NULL DEFAULT 0
);`

const createEnrichCacheFetchedAtIndexSQL = `
CREATE INDEX IF NOT EXISTS enrich_cache_fetched_at_idx
	ON enrich_cache(fetched_at);`

const enrichCacheCurrentVersion = 2

const addEnrichCacheVersionColumnSQL = `
ALTER TABLE enrich_cache
	ADD COLUMN cache_version INTEGER NOT NULL DEFAULT 0;`

// OpenStore opens a SQLite store and creates the schema on demand.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	tx, err := db.Begin()
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	if _, err := tx.Exec(createWatermarksTableSQL); err != nil {
		_ = tx.Rollback()
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	if _, err := tx.Exec(createEnrichCacheTableSQL); err != nil {
		_ = tx.Rollback()
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	if err := ensureEnrichCacheVersionColumn(tx); err != nil {
		_ = tx.Rollback()
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	if _, err := tx.Exec(createEnrichCacheFetchedAtIndexSQL); err != nil {
		_ = tx.Rollback()
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	if err := tx.Commit(); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("%w: %w", ErrStoreOpen, err)
	}

	return &Store{db: db}, nil
}

func ensureEnrichCacheVersionColumn(tx *sql.Tx) error {
	rows, err := tx.Query(`PRAGMA table_info(enrich_cache)`)
	if err != nil {
		return err
	}

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)

		if err = rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			_ = rows.Close()

			return err
		}
		if name == "cache_version" {
			closeErr := rows.Close()
			if err = rows.Err(); err != nil {
				return err
			}

			return closeErr
		}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()

		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}

	_, err = tx.Exec(addEnrichCacheVersionColumnSQL)

	return err
}

func enrichStudyIDBodyPattern(queryID string) (string, error) {
	encodedQueryID, err := json.Marshal(queryID)
	if err != nil {
		return "", err
	}

	return `%"id_study_lims":` + string(encodedQueryID) + `%`, nil
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

// LoadEnrichCache returns one raw enrich cache row by identifier.
// Callers are responsible for checking whether the row is still fresh.
func (s *Store) LoadEnrichCache(identifier string) (*enrichCacheEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	var entry *enrichCacheEntry

	err := s.WithLock(func() error {
		loaded, err := s.loadEnrichCache(identifier)
		if err != nil {
			return err
		}

		entry = loaded

		return nil
	})
	if err != nil {
		return nil, err
	}

	return entry, nil
}

func (s *Store) loadEnrichCache(identifier string) (*enrichCacheEntry, error) {
	row := s.db.QueryRow(`
		SELECT identifier, type, body, fetched_at, ttl_seconds, cache_version, negative, partial
		FROM enrich_cache
		WHERE identifier = ?
	`, identifier)

	var (
		entry     enrichCacheEntry
		fetchedAt string
		negative  int
		partial   int
		ttl       int64
	)

	err := row.Scan(&entry.Identifier, &entry.Type, &entry.Body, &fetchedAt, &ttl, &entry.Version, &negative, &partial)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}

		return nil, fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	entry.FetchedAt, err = time.Parse(time.RFC3339Nano, fetchedAt)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	entry.TTL = time.Duration(ttl)
	entry.Negative = negative == 1
	entry.Partial = partial == 1

	return &entry, nil
}

// SaveEnrichCache upserts one raw enrich cache row.
func (s *Store) SaveEnrichCache(entry enrichCacheEntry) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	return s.WithLock(func() error {
		return s.saveEnrichCache(entry)
	})
}

func (s *Store) saveEnrichCache(entry enrichCacheEntry) error {
	negative := 0
	if entry.Negative {
		negative = 1
	}

	partial := 0
	if entry.Partial {
		partial = 1
	}

	_, err := s.db.Exec(`
		INSERT INTO enrich_cache(identifier, type, body, fetched_at, ttl_seconds, cache_version, negative, partial)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(identifier) DO UPDATE SET
			type = excluded.type,
			body = excluded.body,
			fetched_at = excluded.fetched_at,
			ttl_seconds = excluded.ttl_seconds,
			cache_version = excluded.cache_version,
			negative = excluded.negative,
			partial = excluded.partial
	`, entry.Identifier, entry.Type, entry.Body, entry.FetchedAt.UTC().Format(time.RFC3339Nano), int64(entry.TTL), enrichCacheCurrentVersion, negative, partial)
	if err != nil {
		return fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	return nil
}

// DeleteEnrichCache removes one enrich cache row by identifier.
func (s *Store) DeleteEnrichCache(identifier string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	return s.WithLock(func() error {
		return s.deleteEnrichCache(identifier)
	})
}

func (s *Store) deleteEnrichCache(identifier string) error {
	if _, err := s.db.Exec(`DELETE FROM enrich_cache WHERE identifier = ?`, identifier); err != nil {
		return fmt.Errorf("%w: %w", errStoreOperation, err)
	}

	return nil
}

// InvalidateEnrichFor removes enrich cache rows affected by a diff mutation.
func (s *Store) InvalidateEnrichFor(queryKind, queryID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: store is closed", errStoreOperation)
	}

	return s.WithLock(func() error {
		return s.invalidateEnrichFor(queryKind, queryID)
	})
}

func (s *Store) invalidateEnrichFor(queryKind, queryID string) error {
	switch queryKind {
	case "study_samples":
		studyIDBodyPattern, err := enrichStudyIDBodyPattern(queryID)
		if err != nil {
			return fmt.Errorf("%w: %w", errStoreOperation, err)
		}

		if _, err := s.db.Exec(`
			DELETE FROM enrich_cache
			WHERE (identifier = ? AND type IN (?, ?)) OR CAST(body AS TEXT) LIKE ?
		`, queryID, IdentifierStudyID, IdentifierStudyAccession, studyIDBodyPattern); err != nil {
			return fmt.Errorf("%w: %w", errStoreOperation, err)
		}

		return nil
	case "sample_files":
		return s.deleteEnrichCache(queryID)
	default:
		return nil
	}
}
