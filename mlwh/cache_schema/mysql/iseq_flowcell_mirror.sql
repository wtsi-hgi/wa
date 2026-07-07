CREATE TABLE IF NOT EXISTS iseq_flowcell_mirror (
	id_iseq_flowcell_tmp BIGINT       NOT NULL PRIMARY KEY,
	entity_type           VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	pipeline_id_lims      VARCHAR(255) COLLATE {{MYSQL_TEXT_COLLATION}},
	id_sample_tmp         BIGINT       NOT NULL,
	id_study_tmp          BIGINT       NOT NULL
);

CREATE INDEX iseq_flowcell_mirror_entity_type_idx
	ON iseq_flowcell_mirror(entity_type);
