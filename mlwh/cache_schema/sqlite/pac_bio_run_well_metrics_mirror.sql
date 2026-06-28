CREATE TABLE IF NOT EXISTS pac_bio_run_well_metrics_mirror (
	id_pac_bio_rw_metrics_tmp INTEGER NOT NULL PRIMARY KEY,
	pac_bio_run_name          TEXT    NOT NULL,
	well_label                TEXT    NOT NULL,
	plate_number              INTEGER,
	run_start                 TEXT,
	run_complete              TEXT,
	well_complete             TEXT,
	qc_seq_date               TEXT,
	run_status                TEXT,
	well_status               TEXT,
	last_updated              TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS pac_bio_run_well_metrics_mirror_run_name_well_label_idx
	ON pac_bio_run_well_metrics_mirror(pac_bio_run_name, well_label);
