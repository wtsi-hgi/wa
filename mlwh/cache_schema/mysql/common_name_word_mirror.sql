CREATE TABLE IF NOT EXISTS common_name_word_mirror (
	word        VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}},
	common_name VARCHAR(255) NOT NULL COLLATE {{MYSQL_TEXT_COLLATION}}
);

CREATE INDEX common_name_word_mirror_word_idx
	ON common_name_word_mirror(word);
