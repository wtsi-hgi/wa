CREATE TABLE IF NOT EXISTS study_users_mirror (
	id_study_users_tmp BIGINT       NOT NULL PRIMARY KEY,
	id_study_tmp       BIGINT       NOT NULL,
	role               VARCHAR(255) NOT NULL,
	login              VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	email              VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	name               VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	last_updated       VARCHAR(255) NOT NULL
);

CREATE INDEX study_users_mirror_id_study_tmp_idx
	ON study_users_mirror(id_study_tmp);

CREATE INDEX study_users_mirror_login_idx
	ON study_users_mirror(login);

CREATE INDEX study_users_mirror_email_idx
	ON study_users_mirror(email);

CREATE INDEX study_users_mirror_name_idx
	ON study_users_mirror(name);

CREATE INDEX study_users_mirror_role_idx
	ON study_users_mirror(role);
