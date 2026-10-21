CREATE TABLE IF NOT EXISTS sync_outcome (
	table_name      VARCHAR(255) NOT NULL PRIMARY KEY,
	last_success_at VARCHAR(255),
	last_error      TEXT,
	last_error_at   VARCHAR(255),
	blocked_by      TEXT
);