CREATE TABLE IF NOT EXISTS eseq_product_metrics_mirror (
	id_eseq_product      VARCHAR(255) NOT NULL PRIMARY KEY,
	id_eseq_flowcell_tmp BIGINT       NOT NULL,
	id_run               BIGINT       NOT NULL,
	id_sample_tmp        BIGINT       NOT NULL,
	id_study_lims        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	is_sequencing_control INT,
	qc                   INT,
	qc_seq               INT,
	qc_lib               INT,
	last_updated         VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX eseq_product_metrics_mirror_id_sample_tmp_idx
	ON eseq_product_metrics_mirror(id_sample_tmp);

CREATE INDEX eseq_product_metrics_mirror_id_study_lims_idx
	ON eseq_product_metrics_mirror(id_study_lims);

CREATE INDEX eseq_product_metrics_mirror_id_run_idx
	ON eseq_product_metrics_mirror(id_run);

CREATE INDEX eseq_product_metrics_mirror_study_sample_product_qc_idx
	ON eseq_product_metrics_mirror(id_study_lims, id_sample_tmp, id_eseq_product, qc);
