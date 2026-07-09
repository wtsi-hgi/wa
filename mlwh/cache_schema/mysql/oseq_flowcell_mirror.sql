CREATE TABLE IF NOT EXISTS oseq_flowcell_mirror (
	id_oseq_flowcell_tmp BIGINT       NOT NULL PRIMARY KEY,
	id_sample_tmp        BIGINT       NOT NULL,
	id_study_lims        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	experiment_name      VARCHAR(255) NOT NULL,
	run_id               BIGINT,
	run_uuid             VARCHAR(255) NOT NULL,
	last_updated         VARCHAR(255) NOT NULL,
	normalised_date      VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX oseq_flowcell_mirror_id_sample_tmp_idx
	ON oseq_flowcell_mirror(id_sample_tmp);

CREATE INDEX oseq_flowcell_mirror_id_study_lims_idx
	ON oseq_flowcell_mirror(id_study_lims);

CREATE INDEX oseq_flowcell_mirror_experiment_name_idx
	ON oseq_flowcell_mirror(experiment_name);

CREATE INDEX oseq_flowcell_mirror_last_updated_idx
	ON oseq_flowcell_mirror(last_updated);

CREATE INDEX oseq_flowcell_mirror_normalised_date_idx
	ON oseq_flowcell_mirror(normalised_date);
