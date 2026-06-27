CREATE TABLE IF NOT EXISTS sample_mirror (
	id_sample_tmp    BIGINT       NOT NULL PRIMARY KEY,
	id_lims          VARCHAR(255) NOT NULL,
	id_sample_lims   VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	uuid_sample_lims VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	name             VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	sanger_sample_id VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	supplier_name    VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	accession_number VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	donor_id         VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	taxon_id         INT          NOT NULL,
	common_name      VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	description      TEXT         NOT NULL,
	last_updated     VARCHAR(255) NOT NULL
);

CREATE INDEX sample_mirror_id_sample_lims_idx
	ON sample_mirror(id_sample_lims);

CREATE INDEX sample_mirror_uuid_sample_lims_idx
	ON sample_mirror(uuid_sample_lims);

CREATE INDEX sample_mirror_name_idx
	ON sample_mirror(name);

CREATE INDEX sample_mirror_sanger_sample_id_idx
	ON sample_mirror(sanger_sample_id);

CREATE INDEX sample_mirror_supplier_name_idx
	ON sample_mirror(supplier_name);

CREATE INDEX sample_mirror_accession_number_idx
	ON sample_mirror(accession_number);

CREATE INDEX sample_mirror_donor_id_idx
	ON sample_mirror(donor_id);

CREATE INDEX sample_mirror_common_name_idx
	ON sample_mirror(common_name);

CREATE INDEX sample_mirror_last_updated_idx
	ON sample_mirror(last_updated);