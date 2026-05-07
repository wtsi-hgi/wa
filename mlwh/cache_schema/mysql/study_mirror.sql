CREATE TABLE IF NOT EXISTS study_mirror (
	id_study_tmp                INT          NOT NULL PRIMARY KEY,
	id_lims                     VARCHAR(255) NOT NULL,
	id_study_lims               VARCHAR(255) NOT NULL,
	uuid_study_lims             VARCHAR(255) NOT NULL,
	name                        VARCHAR(255) NOT NULL,
	accession_number            VARCHAR(255) NOT NULL,
	study_title                 TEXT         NOT NULL,
	faculty_sponsor             VARCHAR(255) NOT NULL,
	state                       VARCHAR(255) NOT NULL,
	abstract                    TEXT         NOT NULL,
	abbreviation                VARCHAR(255) NOT NULL,
	description                 TEXT         NOT NULL,
	data_release_strategy       VARCHAR(255) NOT NULL,
	data_access_group           VARCHAR(255) NOT NULL,
	hmdmc_number                VARCHAR(255) NOT NULL,
	programme                   VARCHAR(255) NOT NULL,
	created                     VARCHAR(255) NOT NULL,
	reference_genome            VARCHAR(255) NOT NULL,
	ethically_approved          INT          NOT NULL DEFAULT 0,
	study_type                  VARCHAR(255) NOT NULL,
	contains_human_dna          INT          NOT NULL DEFAULT 0,
	contaminated_human_dna      INT          NOT NULL DEFAULT 0,
	study_visibility            VARCHAR(255) NOT NULL,
	egadac_accession_number     VARCHAR(255) NOT NULL,
	ega_policy_accession_number VARCHAR(255) NOT NULL,
	data_release_timing         VARCHAR(255) NOT NULL,
	last_updated                VARCHAR(255) NOT NULL
);

CREATE INDEX study_mirror_id_study_lims_idx
	ON study_mirror(id_study_lims);

CREATE INDEX study_mirror_id_lims_idx
	ON study_mirror(id_lims);

CREATE INDEX study_mirror_last_updated_idx
	ON study_mirror(last_updated);