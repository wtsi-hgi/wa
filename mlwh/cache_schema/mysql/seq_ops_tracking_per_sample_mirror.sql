CREATE TABLE IF NOT EXISTS seq_ops_tracking_per_sample_mirror (
	id_sample_lims        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	sanger_sample_id      VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	sanger_sample_name    VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	study_id              VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	programme             VARCHAR(255) NOT NULL,
	faculty_sponsor       VARCHAR(255) NOT NULL,
	library_type          VARCHAR(255) NOT NULL,
	platform              VARCHAR(255) NOT NULL,
	manifest_created      VARCHAR(255),
	manifest_uploaded     VARCHAR(255),
	labware_received      VARCHAR(255),
	order_made            VARCHAR(255),
	working_dilution      VARCHAR(255),
	library_start         VARCHAR(255),
	library_complete      VARCHAR(255),
	sequencing_run_start  VARCHAR(255),
	sequencing_qc_complete VARCHAR(255)
);

CREATE INDEX seq_ops_tracking_per_sample_mirror_id_sample_lims_idx
	ON seq_ops_tracking_per_sample_mirror(id_sample_lims);

CREATE INDEX seq_ops_tracking_per_sample_mirror_sanger_sample_name_idx
	ON seq_ops_tracking_per_sample_mirror(sanger_sample_name);

CREATE INDEX seq_ops_tracking_per_sample_mirror_study_id_idx
	ON seq_ops_tracking_per_sample_mirror(study_id);
