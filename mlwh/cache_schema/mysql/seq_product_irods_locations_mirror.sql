CREATE TABLE IF NOT EXISTS seq_product_irods_locations_mirror (
	id_iseq_product          VARCHAR(255) NOT NULL,
	irods_root_collection    TEXT         NOT NULL,
	irods_data_relative_path TEXT         NOT NULL,
	irods_collection         TEXT         NOT NULL,
	irods_file_name          VARCHAR(255) NOT NULL,
	id_sample_tmp            BIGINT       NOT NULL,
	id_study_lims            VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	last_updated             VARCHAR(255) NOT NULL,
	created                  VARCHAR(255),
	platform                 VARCHAR(255) NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX seq_product_irods_locations_mirror_id_sample_tmp_idx
	ON seq_product_irods_locations_mirror(id_sample_tmp);

CREATE INDEX spi_mirror_study_lims_sample_tmp_idx
	ON seq_product_irods_locations_mirror(id_study_lims, id_sample_tmp);

CREATE INDEX spi_mirror_study_lims_created_idx
	ON seq_product_irods_locations_mirror(id_study_lims, created);

CREATE INDEX spi_mirror_study_lims_iseq_product_idx
	ON seq_product_irods_locations_mirror(id_study_lims, id_iseq_product);