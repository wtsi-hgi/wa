CREATE TABLE IF NOT EXISTS oseq_flowcell_mirror (
	id_oseq_flowcell_tmp BIGINT       NOT NULL PRIMARY KEY,
	id_sample_tmp        BIGINT       NOT NULL,
	id_study_lims        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	CHECK(id_study_lims <> '')
);

CREATE INDEX oseq_flowcell_mirror_id_sample_tmp_idx
	ON oseq_flowcell_mirror(id_sample_tmp);

CREATE INDEX oseq_flowcell_mirror_id_study_lims_idx
	ON oseq_flowcell_mirror(id_study_lims);
