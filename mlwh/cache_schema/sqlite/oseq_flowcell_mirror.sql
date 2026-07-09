CREATE TABLE IF NOT EXISTS oseq_flowcell_mirror (
	id_oseq_flowcell_tmp INTEGER NOT NULL PRIMARY KEY,
	id_sample_tmp        INTEGER NOT NULL,
	id_study_lims        TEXT    NOT NULL COLLATE NOCASE,
	experiment_name      TEXT    NOT NULL,
	run_id               INTEGER,
	run_uuid             TEXT    NOT NULL,
	last_updated         TEXT    NOT NULL,
	normalised_date      TEXT    NOT NULL,
	CHECK(id_study_lims <> '')
);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_id_sample_tmp_idx
	ON oseq_flowcell_mirror(id_sample_tmp);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_id_study_lims_idx
	ON oseq_flowcell_mirror(id_study_lims);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_experiment_name_idx
	ON oseq_flowcell_mirror(experiment_name);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_last_updated_idx
	ON oseq_flowcell_mirror(last_updated);

CREATE INDEX IF NOT EXISTS oseq_flowcell_mirror_normalised_date_idx
	ON oseq_flowcell_mirror(normalised_date);
