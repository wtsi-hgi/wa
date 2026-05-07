CREATE TABLE IF NOT EXISTS library_samples (
	pipeline_id_lims VARCHAR(255) NOT NULL,
	id_sample_tmp    INT          NOT NULL,
	id_study_lims    VARCHAR(255) NOT NULL
);

CREATE INDEX library_samples_pipeline_id_lims_idx
	ON library_samples(pipeline_id_lims);

CREATE INDEX library_samples_id_study_lims_idx
	ON library_samples(id_study_lims);

CREATE INDEX library_samples_id_sample_tmp_idx
	ON library_samples(id_sample_tmp);