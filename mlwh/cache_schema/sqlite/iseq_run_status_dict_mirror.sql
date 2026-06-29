CREATE TABLE IF NOT EXISTS iseq_run_status_dict_mirror (
	id_run_status_dict INTEGER NOT NULL PRIMARY KEY,
	description        TEXT    NOT NULL,
	temporal_index     INTEGER
);
