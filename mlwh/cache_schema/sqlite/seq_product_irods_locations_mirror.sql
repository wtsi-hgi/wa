CREATE TABLE IF NOT EXISTS seq_product_irods_locations_mirror (
	id_iseq_product          TEXT    NOT NULL PRIMARY KEY,
	irods_root_collection    TEXT    NOT NULL,
	irods_data_relative_path TEXT    NOT NULL,
	irods_collection         TEXT    NOT NULL,
	irods_file_name          TEXT    NOT NULL,
	id_sample_tmp            INTEGER NOT NULL,
	id_study_lims            TEXT    NOT NULL COLLATE NOCASE,
	last_updated             TEXT    NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS seq_product_irods_locations_mirror_id_sample_tmp_idx
	ON seq_product_irods_locations_mirror(id_sample_tmp);

CREATE INDEX IF NOT EXISTS seq_product_irods_locations_mirror_id_study_lims_id_sample_tmp_idx
	ON seq_product_irods_locations_mirror(id_study_lims, id_sample_tmp);