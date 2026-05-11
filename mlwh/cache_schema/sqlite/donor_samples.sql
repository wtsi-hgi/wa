CREATE TABLE IF NOT EXISTS donor_samples (
	donor_id      TEXT    NOT NULL,
	id_sample_tmp INTEGER NOT NULL,
	id_study_lims TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS donor_samples_donor_id_idx
	ON donor_samples(donor_id);