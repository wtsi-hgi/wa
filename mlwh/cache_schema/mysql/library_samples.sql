CREATE TABLE IF NOT EXISTS library_samples (
	pipeline_id_lims VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	id_sample_tmp    BIGINT       NOT NULL,
	id_study_lims    VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	library_id       VARCHAR(255) NOT NULL DEFAULT '' COLLATE {{MYSQL_TEXT_COLLATION}},
	id_library_lims  VARCHAR(255) NOT NULL DEFAULT '' COLLATE {{MYSQL_TEXT_COLLATION}},
	UNIQUE KEY library_samples_pipeline_id_lims_id_sample_tmp_id_study_lims_uq (pipeline_id_lims, id_sample_tmp, id_study_lims),
	CHECK(id_study_lims <> '')
);

CREATE INDEX library_samples_id_sample_tmp_id_study_lims_idx
	ON library_samples(id_sample_tmp, id_study_lims);

CREATE INDEX library_samples_id_study_lims_pipeline_id_lims_id_sample_tmp_idx
	ON library_samples(id_study_lims, pipeline_id_lims, id_sample_tmp);