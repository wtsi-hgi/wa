CREATE TABLE IF NOT EXISTS oseq_flowcell_mirror (
	id_oseq_flowcell_tmp INTEGER NOT NULL PRIMARY KEY,
	id_sample_tmp        INTEGER NOT NULL,
	id_study_lims        TEXT    NOT NULL COLLATE NOCASE,
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_id_sample_tmp_idx
	ON oseq_flowcell_mirror(id_sample_tmp);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_id_study_lims_idx
	ON oseq_flowcell_mirror(id_study_lims);
