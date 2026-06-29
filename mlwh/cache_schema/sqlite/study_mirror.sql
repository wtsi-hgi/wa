CREATE TABLE IF NOT EXISTS study_mirror (
	id_study_tmp               INTEGER NOT NULL PRIMARY KEY,
	id_lims                    TEXT    NOT NULL,
	id_study_lims              TEXT    NOT NULL COLLATE NOCASE,
	uuid_study_lims            TEXT    NOT NULL COLLATE NOCASE,
	name                       TEXT    NOT NULL COLLATE NOCASE,
	accession_number           TEXT    NOT NULL COLLATE NOCASE,
	study_title                TEXT    NOT NULL,
	faculty_sponsor            TEXT    NOT NULL,
	state                      TEXT    NOT NULL,
	data_release_strategy      TEXT    NOT NULL,
	data_access_group          TEXT    NOT NULL,
	programme                  TEXT    NOT NULL,
	reference_genome           TEXT    NOT NULL,
	ethically_approved         INTEGER NOT NULL DEFAULT 0,
	study_type                 TEXT    NOT NULL,
	contains_human_dna         INTEGER NOT NULL DEFAULT 0,
	contaminated_human_dna     INTEGER NOT NULL DEFAULT 0,
	study_visibility           TEXT    NOT NULL,
	ega_dac_accession_number   TEXT    NOT NULL,
	ega_policy_accession_number TEXT   NOT NULL,
	data_release_timing        TEXT    NOT NULL,
	last_updated               TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS study_mirror_id_study_lims_idx
	ON study_mirror(id_study_lims);

CREATE INDEX IF NOT EXISTS study_mirror_uuid_study_lims_idx
	ON study_mirror(uuid_study_lims);

CREATE INDEX IF NOT EXISTS study_mirror_accession_number_idx
	ON study_mirror(accession_number);

CREATE INDEX IF NOT EXISTS study_mirror_name_idx
	ON study_mirror(name);

CREATE INDEX IF NOT EXISTS study_mirror_faculty_sponsor_idx
	ON study_mirror(faculty_sponsor);