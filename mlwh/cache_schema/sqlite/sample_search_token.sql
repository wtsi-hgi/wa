CREATE TABLE IF NOT EXISTS sample_search_token (
	token         TEXT    NOT NULL,
	id_sample_tmp INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS sample_search_token_idx
	ON sample_search_token(token, id_sample_tmp);
