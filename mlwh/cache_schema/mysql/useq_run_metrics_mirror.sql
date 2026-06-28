CREATE TABLE IF NOT EXISTS useq_run_metrics_mirror (
	id_useq_run_metrics_tmp BIGINT       NOT NULL PRIMARY KEY,
	id_run                  BIGINT       NOT NULL,
	run_name                VARCHAR(255) NOT NULL,
	run_status              VARCHAR(255),
	run_start               VARCHAR(255),
	run_complete            VARCHAR(255),
	last_updated            VARCHAR(255) NOT NULL
);

CREATE INDEX useq_run_metrics_mirror_run_name_idx
	ON useq_run_metrics_mirror(run_name);

CREATE INDEX useq_run_metrics_mirror_id_run_idx
	ON useq_run_metrics_mirror(id_run);
