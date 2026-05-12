CREATE TABLE IF NOT EXISTS seq_product_irods_locations_mirror (
	id_iseq_product          VARCHAR(255) NOT NULL PRIMARY KEY,
	irods_root_collection    TEXT         NOT NULL,
	irods_data_relative_path TEXT         NOT NULL,
	irods_collection         TEXT         NOT NULL,
	irods_file_name          VARCHAR(255) NOT NULL,
	id_sample_tmp            BIGINT       NOT NULL,
	id_study_lims            VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	last_updated             VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX seq_product_irods_locations_mirror_id_sample_tmp_idx
	ON seq_product_irods_locations_mirror(id_sample_tmp);

CREATE INDEX seq_product_irods_locations_mirror_id_study_lims_id_sample_tmp_idx
	ON seq_product_irods_locations_mirror(id_study_lims, id_sample_tmp);