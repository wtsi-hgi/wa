CREATE TABLE IF NOT EXISTS library_samples (
	pipeline_id_lims TEXT    NOT NULL,
	id_sample_tmp    INTEGER NOT NULL,
	id_study_lims    TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS library_samples_pipeline_id_lims_idx
	ON library_samples(pipeline_id_lims);

CREATE INDEX IF NOT EXISTS library_samples_id_study_lims_idx
	ON library_samples(id_study_lims);

CREATE INDEX IF NOT EXISTS library_samples_id_sample_tmp_idx
	ON library_samples(id_sample_tmp);