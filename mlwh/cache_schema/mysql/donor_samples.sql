CREATE TABLE IF NOT EXISTS donor_samples (
	donor_id      VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	id_sample_tmp BIGINT       NOT NULL,
	UNIQUE KEY donor_samples_donor_id_id_sample_tmp_uq (donor_id, id_sample_tmp)
);