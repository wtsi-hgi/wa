CREATE TABLE IF NOT EXISTS pac_bio_product_metrics_mirror (
	id_pac_bio_product        TEXT    NOT NULL PRIMARY KEY,
	id_pac_bio_rw_metrics_tmp INTEGER NOT NULL,
	id_sample_tmp             INTEGER NOT NULL,
	id_study_lims             TEXT    NOT NULL COLLATE NOCASE,
	qc                        INTEGER,
	last_updated              TEXT    NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS pac_bio_product_metrics_mirror_id_sample_tmp_idx
	ON pac_bio_product_metrics_mirror(id_sample_tmp);

CREATE INDEX IF NOT EXISTS pac_bio_product_metrics_mirror_id_study_lims_idx
	ON pac_bio_product_metrics_mirror(id_study_lims);
