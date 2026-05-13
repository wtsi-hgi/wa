CREATE TABLE IF NOT EXISTS iseq_product_metrics_mirror (
	id_iseq_product      VARCHAR(255) NOT NULL PRIMARY KEY,
	id_iseq_flowcell_tmp BIGINT NOT NULL,
	id_run               BIGINT NOT NULL,
	position             INT    NOT NULL,
	tag_index            INT    NOT NULL,
	id_sample_tmp        BIGINT NOT NULL,
	id_study_lims        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	qc                   INT    NOT NULL,
	qc_lib               INT    NOT NULL,
	qc_seq               INT    NOT NULL,
	last_updated         VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX iseq_product_metrics_mirror_id_run_position_tag_index_idx
	ON iseq_product_metrics_mirror(id_run, position, tag_index);

CREATE INDEX ipm_mirror_sample_run_position_tag_idx
	ON iseq_product_metrics_mirror(id_sample_tmp, id_run, position, tag_index);

CREATE INDEX iseq_product_metrics_mirror_id_iseq_flowcell_tmp_idx
	ON iseq_product_metrics_mirror(id_iseq_flowcell_tmp);

CREATE INDEX iseq_product_metrics_mirror_id_study_lims_id_run_position_idx
	ON iseq_product_metrics_mirror(id_study_lims, id_run, position);