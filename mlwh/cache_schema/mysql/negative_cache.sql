CREATE TABLE IF NOT EXISTS negative_cache (
	raw         VARCHAR(255) NOT NULL PRIMARY KEY,
	reason      TEXT         NOT NULL,
	fetched_at  VARCHAR(255) NOT NULL,
	ttl_seconds INT          NOT NULL
);