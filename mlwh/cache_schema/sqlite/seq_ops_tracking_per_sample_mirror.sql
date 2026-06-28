CREATE TABLE IF NOT EXISTS seq_ops_tracking_per_sample_mirror (
	id_sample_lims        TEXT NOT NULL COLLATE NOCASE,
	sanger_sample_id      TEXT NOT NULL COLLATE NOCASE,
	sanger_sample_name    TEXT NOT NULL COLLATE NOCASE,
	study_id              TEXT NOT NULL COLLATE NOCASE,
	programme             TEXT NOT NULL,
	faculty_sponsor       TEXT NOT NULL,
	library_type          TEXT NOT NULL,
	platform              TEXT NOT NULL,
	manifest_created      TEXT,
	manifest_uploaded     TEXT,
	labware_received      TEXT,
	order_made            TEXT,
	working_dilution      TEXT,
	library_start         TEXT,
	library_complete      TEXT,
	sequencing_run_start  TEXT,
	sequencing_qc_complete TEXT
);

CREATE INDEX IF NOT EXISTS seq_ops_tracking_per_sample_mirror_id_sample_lims_idx
	ON seq_ops_tracking_per_sample_mirror(id_sample_lims);

CREATE INDEX IF NOT EXISTS seq_ops_tracking_per_sample_mirror_sanger_sample_name_idx
	ON seq_ops_tracking_per_sample_mirror(sanger_sample_name);

CREATE INDEX IF NOT EXISTS seq_ops_tracking_per_sample_mirror_study_id_idx
	ON seq_ops_tracking_per_sample_mirror(study_id);
