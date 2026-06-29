CREATE TABLE IF NOT EXISTS eseq_run_mirror (
	id_eseq_run_tmp INTEGER NOT NULL PRIMARY KEY,
	run_name        TEXT    NOT NULL,
	run_status      TEXT,
	run_start       TEXT,
	run_complete    TEXT,
	last_updated    TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS eseq_run_mirror_run_name_idx
	ON eseq_run_mirror(run_name);
