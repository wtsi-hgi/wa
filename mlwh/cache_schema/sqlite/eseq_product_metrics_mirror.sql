CREATE TABLE IF NOT EXISTS eseq_product_metrics_mirror (
	id_eseq_product      TEXT    NOT NULL PRIMARY KEY,
	id_eseq_flowcell_tmp INTEGER NOT NULL,
	id_sample_tmp        INTEGER NOT NULL,
	id_study_lims        TEXT    NOT NULL COLLATE NOCASE,
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
