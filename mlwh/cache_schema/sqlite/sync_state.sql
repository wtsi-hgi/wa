CREATE TABLE IF NOT EXISTS sync_state (
	table_name      TEXT    NOT NULL PRIMARY KEY,
	high_water      TEXT    NOT NULL,
	last_run        TEXT    NOT NULL,
	resume_cursor   TEXT,
	indexes_dropped INTEGER NOT NULL DEFAULT 0
);