CREATE TABLE IF NOT EXISTS iseq_run_status_dict_mirror (
	id_run_status_dict BIGINT       NOT NULL PRIMARY KEY,
	description        VARCHAR(255) NOT NULL,
	temporal_index     INT
);
