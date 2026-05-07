CREATE TABLE IF NOT EXISTS watermarks (
	query_key  TEXT    NOT NULL,
	entry_id   TEXT    NOT NULL,
	entry_hash TEXT    NOT NULL,
	updated_at TEXT    NOT NULL,
	tombstone  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (query_key, entry_id)
);