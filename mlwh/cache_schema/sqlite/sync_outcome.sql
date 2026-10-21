CREATE TABLE IF NOT EXISTS sync_outcome (
	table_name      TEXT NOT NULL PRIMARY KEY,
	last_success_at TEXT,
	last_error      TEXT,
	last_error_at   TEXT,
	blocked_by      TEXT
);