CREATE TABLE IF NOT EXISTS sync_state (
	table_name      VARCHAR(255) NOT NULL PRIMARY KEY,
	high_water      VARCHAR(255) NOT NULL,
	last_run        VARCHAR(255) NOT NULL,
	resume_cursor   TEXT,
	indexes_dropped INT          NOT NULL DEFAULT 0
);