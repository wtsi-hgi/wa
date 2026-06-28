CREATE TABLE IF NOT EXISTS useq_run_metrics_mirror (
	id_useq_run_metrics_tmp INTEGER NOT NULL PRIMARY KEY,
	id_run                  INTEGER NOT NULL,
	run_name                TEXT    NOT NULL,
	run_status              TEXT,
	run_start               TEXT,
	run_complete            TEXT,
	last_updated            TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS useq_run_metrics_mirror_run_name_idx
	ON useq_run_metrics_mirror(run_name);

CREATE INDEX IF NOT EXISTS useq_run_metrics_mirror_id_run_idx
	ON useq_run_metrics_mirror(id_run);
