CREATE TABLE IF NOT EXISTS negative_cache (
	raw         TEXT    NOT NULL PRIMARY KEY,
	reason      TEXT    NOT NULL,
	fetched_at  TEXT    NOT NULL,
	ttl_seconds INTEGER NOT NULL
);