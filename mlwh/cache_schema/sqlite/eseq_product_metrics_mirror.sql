CREATE TABLE IF NOT EXISTS eseq_product_metrics_mirror (
	id_eseq_product      TEXT    NOT NULL PRIMARY KEY,
	id_eseq_flowcell_tmp INTEGER NOT NULL,
	id_run               INTEGER NOT NULL,
	id_sample_tmp        INTEGER NOT NULL,
	id_study_lims        TEXT    NOT NULL COLLATE NOCASE,
	is_sequencing_control INTEGER,
	qc                   INTEGER,
	qc_seq               INTEGER,
	qc_lib               INTEGER,
	last_updated         TEXT    NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS eseq_product_metrics_mirror_id_sample_tmp_idx
	ON eseq_product_metrics_mirror(id_sample_tmp);

CREATE INDEX IF NOT EXISTS eseq_product_metrics_mirror_id_study_lims_idx
	ON eseq_product_metrics_mirror(id_study_lims);

CREATE INDEX IF NOT EXISTS eseq_product_metrics_mirror_id_run_idx
	ON eseq_product_metrics_mirror(id_run);

CREATE INDEX IF NOT EXISTS eseq_product_metrics_mirror_study_sample_product_qc_idx
	ON eseq_product_metrics_mirror(id_study_lims, id_sample_tmp, id_eseq_product, qc);
