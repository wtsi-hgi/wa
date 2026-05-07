CREATE TABLE IF NOT EXISTS schema_version (
	version    INTEGER NOT NULL PRIMARY KEY,
	applied_at TEXT    NOT NULL
);