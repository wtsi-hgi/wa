CREATE TABLE IF NOT EXISTS iseq_product_metrics_mirror (
	id_iseq_product      INTEGER NOT NULL PRIMARY KEY,
	id_iseq_flowcell_tmp INTEGER NOT NULL,
	id_run               INTEGER NOT NULL,
	position             INTEGER NOT NULL,
	tag_index            INTEGER NOT NULL,
	id_sample_tmp        INTEGER NOT NULL,
	id_study_lims        TEXT    NOT NULL COLLATE NOCASE,
	qc                   INTEGER NOT NULL,
	qc_lib               INTEGER NOT NULL,
	qc_seq               INTEGER NOT NULL,
	last_updated         TEXT    NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS iseq_product_metrics_mirror_id_run_position_tag_index_idx
	ON iseq_product_metrics_mirror(id_run, position, tag_index);

CREATE INDEX IF NOT EXISTS iseq_product_metrics_mirror_id_sample_tmp_id_run_position_tag_index_idx
	ON iseq_product_metrics_mirror(id_sample_tmp, id_run, position, tag_index);

CREATE INDEX IF NOT EXISTS iseq_product_metrics_mirror_id_iseq_flowcell_tmp_idx
	ON iseq_product_metrics_mirror(id_iseq_flowcell_tmp);

CREATE INDEX IF NOT EXISTS iseq_product_metrics_mirror_id_study_lims_id_run_position_idx
	ON iseq_product_metrics_mirror(id_study_lims, id_run, position);