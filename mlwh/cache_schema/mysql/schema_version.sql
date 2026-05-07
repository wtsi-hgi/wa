CREATE TABLE IF NOT EXISTS schema_version (
	version    INT          NOT NULL PRIMARY KEY,
	applied_at VARCHAR(255) NOT NULL
);