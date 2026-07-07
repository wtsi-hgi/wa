CREATE TABLE IF NOT EXISTS iseq_flowcell_mirror (
	id_iseq_flowcell_tmp INTEGER NOT NULL PRIMARY KEY,
	entity_type           TEXT    NOT NULL,
	pipeline_id_lims      TEXT,
	id_sample_tmp         INTEGER NOT NULL,
	id_study_tmp          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS iseq_flowcell_mirror_entity_type_idx
	ON iseq_flowcell_mirror(entity_type);
