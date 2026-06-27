CREATE TABLE IF NOT EXISTS sample_search_token (
	token         VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	id_sample_tmp BIGINT       NOT NULL
);

CREATE INDEX sample_search_token_idx
	ON sample_search_token(token, id_sample_tmp);
