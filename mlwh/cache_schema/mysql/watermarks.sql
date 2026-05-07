CREATE TABLE IF NOT EXISTS watermarks (
	query_key  VARCHAR(255) NOT NULL,
	entry_id   VARCHAR(255) NOT NULL,
	entry_hash VARCHAR(255) NOT NULL,
	updated_at VARCHAR(255) NOT NULL,
	tombstone  INT          NOT NULL DEFAULT 0,
	PRIMARY KEY (query_key, entry_id)
);