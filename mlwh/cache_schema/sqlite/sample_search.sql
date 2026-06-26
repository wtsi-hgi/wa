CREATE VIRTUAL TABLE IF NOT EXISTS sample_search USING fts5(
	name,
	supplier_name,
	common_name,
	donor_id,
	content='sample_mirror',
	content_rowid='id_sample_tmp',
	tokenize='trigram'
);
