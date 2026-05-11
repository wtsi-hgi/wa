CREATE TABLE IF NOT EXISTS sample_mirror (
	id_sample_tmp    INT          NOT NULL PRIMARY KEY,
	id_lims          VARCHAR(255) NOT NULL,
	id_sample_lims   VARCHAR(255) NOT NULL,
	uuid_sample_lims VARCHAR(255) NOT NULL,
	id_study_lims    VARCHAR(255) NOT NULL,
	name             VARCHAR(255) NOT NULL,
	sanger_id        VARCHAR(255) NOT NULL,
	sanger_sample_id VARCHAR(255) NOT NULL,
	supplier_name    VARCHAR(255) NOT NULL,
	accession_number VARCHAR(255) NOT NULL,
	donor_id         VARCHAR(255) NOT NULL,
	library_type     VARCHAR(255) NOT NULL,
	taxon_id         INT          NOT NULL,
	common_name      VARCHAR(255) NOT NULL,
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

CREATE INDEX sample_mirror_last_updated_idx
	ON sample_mirror(last_updated);