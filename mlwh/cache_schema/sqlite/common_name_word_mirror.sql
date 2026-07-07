CREATE TABLE IF NOT EXISTS common_name_word_mirror (
	word        TEXT NOT NULL,
	common_name TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS common_name_word_mirror_word_idx
	ON common_name_word_mirror(word);
