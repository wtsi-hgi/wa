CREATE TABLE IF NOT EXISTS study_mirror (
	id_study_tmp                BIGINT       NOT NULL PRIMARY KEY,
	id_lims                     VARCHAR(255) NOT NULL,
	id_study_lims               VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	uuid_study_lims             VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	name                        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	accession_number            VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	study_title                 TEXT         NOT NULL,
	faculty_sponsor             VARCHAR(255) NOT NULL,
	state                       VARCHAR(255) NOT NULL,
	data_release_strategy       VARCHAR(255) NOT NULL,
	data_access_group           VARCHAR(255) NOT NULL,
	programme                   VARCHAR(255) NOT NULL,
	reference_genome            VARCHAR(255) NOT NULL,
	ethically_approved          INT          NOT NULL DEFAULT 0,
	study_type                  VARCHAR(255) NOT NULL,
	contains_human_dna          INT          NOT NULL DEFAULT 0,
	contaminated_human_dna      INT          NOT NULL DEFAULT 0,
	study_visibility            VARCHAR(255) NOT NULL,
	ega_dac_accession_number    VARCHAR(255) NOT NULL,
	ega_policy_accession_number VARCHAR(255) NOT NULL,
	data_release_timing         VARCHAR(255) NOT NULL,
	last_updated                VARCHAR(255) NOT NULL
);

CREATE INDEX study_mirror_id_study_lims_idx
	ON study_mirror(id_study_lims);

CREATE INDEX study_mirror_uuid_study_lims_idx
	ON study_mirror(uuid_study_lims);

CREATE INDEX study_mirror_accession_number_idx
	ON study_mirror(accession_number);

CREATE INDEX study_mirror_name_idx
	ON study_mirror(name);

CREATE INDEX study_mirror_faculty_sponsor_idx
	ON study_mirror(faculty_sponsor);