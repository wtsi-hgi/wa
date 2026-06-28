CREATE TABLE IF NOT EXISTS useq_product_metrics_mirror (
	id_useq_product   VARCHAR(255) NOT NULL PRIMARY KEY,
	id_useq_wafer_tmp BIGINT       NOT NULL,
	id_sample_tmp     BIGINT       NOT NULL,
	id_study_lims     VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	qc                INT,
	qc_seq            INT,
	qc_lib            INT,
	last_updated      VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX useq_product_metrics_mirror_id_sample_tmp_idx
	ON useq_product_metrics_mirror(id_sample_tmp);

CREATE INDEX useq_product_metrics_mirror_id_study_lims_idx
	ON useq_product_metrics_mirror(id_study_lims);

CREATE INDEX useq_product_metrics_mirror_id_useq_wafer_tmp_idx
	ON useq_product_metrics_mirror(id_useq_wafer_tmp);
