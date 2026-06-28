CREATE TABLE IF NOT EXISTS pac_bio_product_metrics_mirror (
	id_pac_bio_product        VARCHAR(255) NOT NULL PRIMARY KEY,
	id_pac_bio_rw_metrics_tmp BIGINT       NOT NULL,
	id_sample_tmp             BIGINT       NOT NULL,
	id_study_lims             VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	qc                        INT,
	last_updated              VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX pac_bio_product_metrics_mirror_id_sample_tmp_idx
	ON pac_bio_product_metrics_mirror(id_sample_tmp);

CREATE INDEX pac_bio_product_metrics_mirror_id_study_lims_idx
	ON pac_bio_product_metrics_mirror(id_study_lims);
