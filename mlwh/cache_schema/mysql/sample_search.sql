CREATE FULLTEXT INDEX sample_mirror_search_ftx
	ON sample_mirror (name, supplier_name, common_name, donor_id)
	WITH PARSER ngram;
