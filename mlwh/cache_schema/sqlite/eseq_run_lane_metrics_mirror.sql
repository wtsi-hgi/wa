CREATE TABLE IF NOT EXISTS eseq_run_lane_metrics_mirror (
	id_eseq_rlm_tmp INTEGER NOT NULL PRIMARY KEY,
	id_run          INTEGER NOT NULL,
	run_name        TEXT    NOT NULL,
	lane            INTEGER NOT NULL,
	run_started     TEXT,
	run_complete    TEXT,
	last_updated    TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS eseq_run_lane_metrics_mirror_run_name_lane_idx
	ON eseq_run_lane_metrics_mirror(run_name, lane);

CREATE INDEX IF NOT EXISTS eseq_run_lane_metrics_mirror_id_run_idx
	ON eseq_run_lane_metrics_mirror(id_run);
