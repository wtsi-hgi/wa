CREATE TABLE IF NOT EXISTS eseq_run_mirror (
	id_eseq_run_tmp BIGINT       NOT NULL PRIMARY KEY,
	run_name        VARCHAR(255) NOT NULL,
	run_status      VARCHAR(255),
	run_start       VARCHAR(255),
	run_complete    VARCHAR(255),
	last_updated    VARCHAR(255) NOT NULL
);

CREATE INDEX eseq_run_mirror_run_name_idx
	ON eseq_run_mirror(run_name);
