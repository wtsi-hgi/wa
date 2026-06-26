CREATE VIRTUAL TABLE IF NOT EXISTS sample_search USING fts5(
	name,
	supplier_name,
	common_name,
	donor_id,
	content='sample_mirror',
	content_rowid='id_sample_tmp',
	tokenize='trigram'
);

-- An external-content fts5 table is not auto-maintained by writes to its
-- content table, so these triggers propagate sample_mirror inserts, updates,
-- and deletes into sample_search using the fts5 'delete'/insert protocol. They
-- keep incrementally-synced samples searchable. The cold-load path drops these
-- triggers before its bulk insert (so they do not fire per row) and recreates
-- them at finalize alongside the 'rebuild', mirroring the secondary-index
-- drop/recreate discipline.
CREATE TRIGGER IF NOT EXISTS sample_search_ai AFTER INSERT ON sample_mirror BEGIN
	INSERT INTO sample_search(rowid, name, supplier_name, common_name, donor_id)
	VALUES (new.id_sample_tmp, new.name, new.supplier_name, new.common_name, new.donor_id);
END;
CREATE TRIGGER IF NOT EXISTS sample_search_ad AFTER DELETE ON sample_mirror BEGIN
	INSERT INTO sample_search(sample_search, rowid, name, supplier_name, common_name, donor_id)
	VALUES ('delete', old.id_sample_tmp, old.name, old.supplier_name, old.common_name, old.donor_id);
END;
CREATE TRIGGER IF NOT EXISTS sample_search_au AFTER UPDATE ON sample_mirror BEGIN
	INSERT INTO sample_search(sample_search, rowid, name, supplier_name, common_name, donor_id)
	VALUES ('delete', old.id_sample_tmp, old.name, old.supplier_name, old.common_name, old.donor_id);
	INSERT INTO sample_search(rowid, name, supplier_name, common_name, donor_id)
	VALUES (new.id_sample_tmp, new.name, new.supplier_name, new.common_name, new.donor_id);
END;
