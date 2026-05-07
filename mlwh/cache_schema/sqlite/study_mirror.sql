CREATE TABLE IF NOT EXISTS study_mirror (
	id_study_tmp               INTEGER NOT NULL PRIMARY KEY,
	id_lims                    TEXT    NOT NULL,
	id_study_lims              TEXT    NOT NULL,
	uuid_study_lims            TEXT    NOT NULL,
	name                       TEXT    NOT NULL,
	accession_number           TEXT    NOT NULL,
	study_title                TEXT    NOT NULL,
	faculty_sponsor            TEXT    NOT NULL,
	state                      TEXT    NOT NULL,
	abstract                   TEXT    NOT NULL,
	abbreviation               TEXT    NOT NULL,
	description                TEXT    NOT NULL,
	data_release_strategy      TEXT    NOT NULL,
	data_access_group          TEXT    NOT NULL,
	hmdmc_number               TEXT    NOT NULL,
	programme                  TEXT    NOT NULL,
	created                    TEXT    NOT NULL,
	reference_genome           TEXT    NOT NULL,
	ethically_approved         INTEGER NOT NULL DEFAULT 0,
	study_type                 TEXT    NOT NULL,
	contains_human_dna         INTEGER NOT NULL DEFAULT 0,
	contaminated_human_dna     INTEGER NOT NULL DEFAULT 0,
	study_visibility           TEXT    NOT NULL,
	egadac_accession_number    TEXT    NOT NULL,
	ega_policy_accession_number TEXT   NOT NULL,
	data_release_timing        TEXT    NOT NULL,
	last_updated               TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS study_mirror_id_study_lims_idx
	ON study_mirror(id_study_lims);

CREATE INDEX IF NOT EXISTS study_mirror_id_lims_idx
	ON study_mirror(id_lims);

CREATE INDEX IF NOT EXISTS study_mirror_last_updated_idx
	ON study_mirror(last_updated);