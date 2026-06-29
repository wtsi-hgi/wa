CREATE TABLE IF NOT EXISTS study_users_mirror (
	id_study_users_tmp INTEGER NOT NULL PRIMARY KEY,
	id_study_tmp       INTEGER NOT NULL,
	role               TEXT    NOT NULL,
	login              TEXT    NOT NULL COLLATE NOCASE,
	email              TEXT    NOT NULL COLLATE NOCASE,
	name               TEXT    NOT NULL COLLATE NOCASE,
	last_updated       TEXT    NOT NULL
);

CREATE INDEX IF NOT EXISTS study_users_mirror_id_study_tmp_idx
	ON study_users_mirror(id_study_tmp);

CREATE INDEX IF NOT EXISTS study_users_mirror_login_idx
	ON study_users_mirror(login);

CREATE INDEX IF NOT EXISTS study_users_mirror_email_idx
	ON study_users_mirror(email);

CREATE INDEX IF NOT EXISTS study_users_mirror_name_idx
	ON study_users_mirror(name);

CREATE INDEX IF NOT EXISTS study_users_mirror_role_idx
	ON study_users_mirror(role);
