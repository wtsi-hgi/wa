CREATE TABLE IF NOT EXISTS iseq_run_status_mirror (
	id_run_status      INTEGER NOT NULL PRIMARY KEY,
	id_run             INTEGER NOT NULL,
	date               TEXT    NOT NULL,
	id_run_status_dict INTEGER NOT NULL,
	iscurrent          INTEGER NOT NULL,
	normalised_date    TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS iseq_run_status_mirror_id_run_idx
	ON iseq_run_status_mirror(id_run);

CREATE INDEX IF NOT EXISTS iseq_run_status_mirror_id_run_date_idx
	ON iseq_run_status_mirror(id_run, date);

CREATE INDEX IF NOT EXISTS iseq_run_status_mirror_normalised_date_idx
	ON iseq_run_status_mirror(normalised_date, id_run_status_dict, id_run);
