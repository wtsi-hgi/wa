CREATE TABLE IF NOT EXISTS eseq_run_lane_metrics_mirror (
	id_run       INTEGER NOT NULL,
	lane         INTEGER NOT NULL,
	run_started  TEXT,
	run_complete TEXT,
	last_updated TEXT    NOT NULL,
	PRIMARY KEY (id_run, lane)
);

CREATE INDEX IF NOT EXISTS eseq_run_lane_metrics_mirror_id_run_idx
	ON eseq_run_lane_metrics_mirror(id_run);
