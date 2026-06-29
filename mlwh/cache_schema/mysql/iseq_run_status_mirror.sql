CREATE TABLE IF NOT EXISTS iseq_run_status_mirror (
	id_run_status      BIGINT       NOT NULL PRIMARY KEY,
	id_run             BIGINT       NOT NULL,
	date               VARCHAR(255) NOT NULL,
	id_run_status_dict BIGINT       NOT NULL,
	iscurrent          INT          NOT NULL
);

CREATE INDEX iseq_run_status_mirror_id_run_idx
	ON iseq_run_status_mirror(id_run);

CREATE INDEX iseq_run_status_mirror_id_run_date_idx
	ON iseq_run_status_mirror(id_run, date);
