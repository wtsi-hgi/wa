CREATE TABLE IF NOT EXISTS donor_samples (
	donor_id      VARCHAR(255) NOT NULL,
	id_sample_tmp INT          NOT NULL,
	id_study_lims VARCHAR(255) NOT NULL
);

CREATE INDEX donor_samples_donor_id_idx
	ON donor_samples(donor_id);