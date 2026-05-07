CREATE TABLE IF NOT EXISTS enrich_cache (
	identifier  VARCHAR(255) NOT NULL PRIMARY KEY,
	type        VARCHAR(255) NOT NULL,
	body        BLOB         NOT NULL,
	fetched_at  VARCHAR(255) NOT NULL,
	ttl_seconds INT          NOT NULL,
	negative    INT          NOT NULL DEFAULT 0,
	partial     INT          NOT NULL DEFAULT 0
);

CREATE INDEX enrich_cache_fetched_at_idx
	ON enrich_cache(fetched_at);