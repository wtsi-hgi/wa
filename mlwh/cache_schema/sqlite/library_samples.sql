CREATE TABLE IF NOT EXISTS library_samples (
	pipeline_id_lims TEXT    NOT NULL COLLATE NOCASE,
	id_sample_tmp    INTEGER NOT NULL,
	id_study_lims    TEXT    NOT NULL COLLATE NOCASE,
	library_id       TEXT    NOT NULL DEFAULT '' COLLATE NOCASE,
	id_library_lims  TEXT    NOT NULL DEFAULT '' COLLATE NOCASE,
	UNIQUE(pipeline_id_lims, id_sample_tmp, id_study_lims),
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS library_samples_id_sample_tmp_id_study_lims_idx
	ON library_samples(id_sample_tmp, id_study_lims);

CREATE INDEX IF NOT EXISTS library_samples_id_study_lims_pipeline_id_lims_id_sample_tmp_idx
	ON library_samples(id_study_lims, pipeline_id_lims, id_sample_tmp);