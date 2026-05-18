CREATE TABLE IF NOT EXISTS donor_samples (
	donor_id      TEXT    NOT NULL COLLATE NOCASE,
	id_sample_tmp INTEGER NOT NULL,
	UNIQUE(donor_id, id_sample_tmp)
);