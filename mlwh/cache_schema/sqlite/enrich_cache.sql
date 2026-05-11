CREATE TABLE IF NOT EXISTS enrich_cache (
	identifier  TEXT    NOT NULL PRIMARY KEY,
	type        TEXT    NOT NULL,
	body        BLOB    NOT NULL,
	fetched_at  TEXT    NOT NULL,
	ttl_seconds INTEGER NOT NULL,
	negative    INTEGER NOT NULL DEFAULT 0,
	partial     INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS enrich_cache_fetched_at_idx
	ON enrich_cache(fetched_at);