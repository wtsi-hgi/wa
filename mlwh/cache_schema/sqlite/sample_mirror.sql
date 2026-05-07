CREATE TABLE IF NOT EXISTS sample_mirror (
	id_sample_tmp     INTEGER NOT NULL PRIMARY KEY,
	id_lims           TEXT    NOT NULL,
	id_sample_lims    TEXT    NOT NULL,
	uuid_sample_lims  TEXT    NOT NULL,
	id_study_lims     TEXT    NOT NULL,
	name              TEXT    NOT NULL,
	sanger_id         TEXT    NOT NULL,
	sanger_sample_id  TEXT    NOT NULL,
	supplier_name     TEXT    NOT NULL,
	accession_number  TEXT    NOT NULL,
	donor_id          TEXT    NOT NULL,
	library_type      TEXT    NOT NULL,
	taxon_id          INTEGER NOT NULL,
	common_name       TEXT    NOT NULL,
	description       TEXT    NOT NULL,
	last_updated      TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS sample_mirror_id_sample_lims_idx
	ON sample_mirror(id_sample_lims);

CREATE INDEX IF NOT EXISTS sample_mirror_uuid_sample_lims_idx
	ON sample_mirror(uuid_sample_lims);

CREATE INDEX IF NOT EXISTS sample_mirror_name_idx
	ON sample_mirror(name);

CREATE INDEX IF NOT EXISTS sample_mirror_sanger_sample_id_idx
	ON sample_mirror(sanger_sample_id);

CREATE INDEX IF NOT EXISTS sample_mirror_supplier_name_idx
	ON sample_mirror(supplier_name);

CREATE INDEX IF NOT EXISTS sample_mirror_accession_number_idx
	ON sample_mirror(accession_number);

CREATE INDEX IF NOT EXISTS sample_mirror_donor_id_idx
	ON sample_mirror(donor_id);

CREATE INDEX IF NOT EXISTS sample_mirror_last_updated_idx
	ON sample_mirror(last_updated);