CREATE TABLE IF NOT EXISTS eseq_run_lane_metrics_mirror (
	id_eseq_rlm_tmp BIGINT       NOT NULL PRIMARY KEY,
	run_name        VARCHAR(255) NOT NULL,
	lane            INT          NOT NULL,
	run_complete    VARCHAR(255),
	last_updated    VARCHAR(255) NOT NULL
);

CREATE INDEX eseq_run_lane_metrics_mirror_run_name_lane_idx
	ON eseq_run_lane_metrics_mirror(run_name, lane);
