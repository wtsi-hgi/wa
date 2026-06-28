CREATE TABLE IF NOT EXISTS pac_bio_run_well_metrics_mirror (
	id_pac_bio_rw_metrics_tmp BIGINT       NOT NULL PRIMARY KEY,
	pac_bio_run_name          VARCHAR(255) NOT NULL,
	well_label                VARCHAR(255) NOT NULL,
	plate_number              INT,
	run_start                 VARCHAR(255),
	run_complete              VARCHAR(255),
	well_complete             VARCHAR(255),
	qc_seq_date               VARCHAR(255),
	run_status                VARCHAR(255),
	well_status               VARCHAR(255),
	last_updated              VARCHAR(255) NOT NULL
);

CREATE INDEX pac_bio_run_well_metrics_mirror_run_name_well_label_idx
	ON pac_bio_run_well_metrics_mirror(pac_bio_run_name, well_label);
