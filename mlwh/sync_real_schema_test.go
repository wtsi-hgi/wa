/*******************************************************************************
 * Copyright (c) 2026 Genome Research Ltd.
 *
 * Author: Sendu Bala <sb10@sanger.ac.uk>
 *
 * Permission is hereby granted, free of charge, to any person obtaining
 * a copy of this software and associated documentation files (the
 * "Software"), to deal in the Software without restriction, including
 * without limitation the rights to use, copy, modify, merge, publish,
 * distribute, sublicense, and/or sell copies of the Software, and to
 * permit persons to whom the Software is furnished to do so, subject to
 * the following conditions:
 *
 * The above copyright notice and this permission notice shall be included
 * in all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
 * EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
 * MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.
 * IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY
 * CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT,
 * TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE
 * SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
 ******************************************************************************/

package mlwh

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

// TestSyncAgainstRealMLWHSchema exercises the full Client.Sync path against an
// upstream "MLWH" SQLite database whose table shapes faithfully match the real
// Sanger MLWH columns (in particular: the upstream `sample` table has NO
// `id_study_lims` column; `iseq_flowcell` links to `study` via
// `id_study_tmp` rather than carrying `id_study_lims`; and the study table
// uses the current `ega_dac_accession_number` spelling).
// This regression test would have caught the production bug where
// `wa mlwh sync --env development` failed with
// `Unknown column 'id_study_lims' in 'field list'` because the previous sync
// query selected `id_study_lims` from `sample` directly.
func TestSyncAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given an upstream database with the real MLWH schema (no id_study_lims on sample)", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHSampleRow(t, source, 1, "SQSCP", "1001", "uuid-sample-1", "sample-a", "ssid-a", "supplier-a", "acc-sa", "donor-a", 9606, "human", "desc-a", time.Date(2026, time.May, 7, 9, 0, 0, 0, time.UTC))
		seedRealMLWHSampleRow(t, source, 2, "SQSCP", "1002", "uuid-sample-2", "sample-b", "ssid-b", "supplier-b", "acc-sb", "donor-b", 9606, "human", "desc-b", time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC))
		seedRealMLWHStudyRow(t, source, 10, "SQSCP", "5001", "uuid-study-1", "Study One", "acc-st-1", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 100, "Standard", 1, 10, time.Date(2026, time.May, 7, 9, 10, 0, 0, time.UTC))
		// sample 2 deliberately has no flowcell entry, so its mirrored
		// id_study_lims must be the empty string rather than failing the sync.

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := client.Sync(context.Background())

		convey.Convey("when Sync runs without restricting tables, then every table is synced and id_study_lims is resolved through study.id_study_lims", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 3)

			byTable := make(map[string]SyncReport, len(reports))
			for _, report := range reports {
				byTable[report.Table] = report
			}
			convey.So(byTable["sample"].Inserted, convey.ShouldEqual, 2)
			convey.So(byTable["study"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["iseq_flowcell"].Inserted, convey.ShouldEqual, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)

			var studyLimsForSample1, studyLimsForSample2 string
			convey.So(cache.DB().QueryRow(`SELECT id_study_lims FROM sample_mirror WHERE id_sample_tmp = 1`).Scan(&studyLimsForSample1), convey.ShouldBeNil)
			convey.So(studyLimsForSample1, convey.ShouldEqual, "5001")
			convey.So(cache.DB().QueryRow(`SELECT id_study_lims FROM sample_mirror WHERE id_sample_tmp = 2`).Scan(&studyLimsForSample2), convey.ShouldBeNil)
			convey.So(studyLimsForSample2, convey.ShouldEqual, "")
		})
	})
}

// TestAllStudiesAgainstRealMLWHSchema verifies that the cold-cache study list
// path tolerates the current upstream study column spelling used by MLWH.
func TestAllStudiesAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given a cold cache and an upstream database with the current MLWH study schema", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 10, "SQSCP", "5001", "uuid-study-1", "Study One", "acc-st-1", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then it returns the study and read-through populates study_mirror", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "5001")
			convey.So(studies[0].EGADACAccessionNumber, convey.ShouldEqual, "")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
		})
	})
}

func TestAllStudiesAgainstRealMLWHSchemaAllowsNullStudyStrings(t *testing.T) {
	convey.Convey("Given a cold cache and an upstream study row with nullable text fields", t, func() {
		source := openRealMLWHSchemaSource(t)
		_, err := source.Exec(
			`INSERT INTO study(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			11,
			"SQSCP",
			"5002",
			"uuid-study-2",
			"Study Two",
			nil,
			"Study title 2",
			nil,
			"active",
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
			true,
			nil,
			false,
			false,
			nil,
			nil,
			nil,
			nil,
			formatSyncTime(time.Date(2026, time.May, 7, 8, 10, 0, 0, time.UTC)),
		)
		convey.So(err, convey.ShouldBeNil)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then nullable upstream strings are normalized to empty strings", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "5002")
			convey.So(studies[0].AccessionNumber, convey.ShouldEqual, "")
			convey.So(studies[0].FacultySponsor, convey.ShouldEqual, "")
			convey.So(studies[0].EGADACAccessionNumber, convey.ShouldEqual, "")
		})
	})
}

// TestUpstreamSampleResolverAgainstRealMLWHSchema verifies that the cold-cache
// resolver path that queries `FROM sample` directly works against the real
// MLWH schema (i.e. the embedded id_study_lims subquery against iseq_flowcell
// is valid). The previous code selected `sample.id_study_lims` and failed.
func TestUpstreamSampleResolverAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given a cold cache and an upstream with the real MLWH schema", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHSampleRow(t, source, 5, "SQSCP", "5005", "b7daafb8-c59f-11ee-8fba-024224dd57f4", "name-5", "ssid-5", "supplier-5", "acc-5", "donor-5", 9606, "human", "desc-5", time.Date(2026, time.May, 7, 10, 0, 0, 0, time.UTC))
		seedRealMLWHStudyRow(t, source, 99, "SQSCP", "9999", "uuid-study-99", "Study Ninety Nine", "acc-st-99", time.Date(2026, time.May, 7, 8, 30, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 500, "Standard", 5, 99, time.Date(2026, time.May, 7, 10, 5, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		match, err := client.ResolveSample(context.Background(), "b7daafb8-c59f-11ee-8fba-024224dd57f4")

		convey.Convey("when ResolveSample runs against the upstream, then it returns the sample with id_study_lims sourced through study.id_study_lims", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindSampleUUID)
			convey.So(match.Sample, convey.ShouldNotBeNil)
			convey.So(match.Sample.UUIDSampleLims, convey.ShouldEqual, "b7daafb8-c59f-11ee-8fba-024224dd57f4")
			convey.So(match.Sample.IDStudyLims, convey.ShouldEqual, "9999")
		})
	})
}

func openRealMLWHSchemaSource(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "real-mlwh.sqlite"))
	if err != nil {
		t.Fatalf("open real MLWH source sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// `sample` deliberately has NO id_study_lims column to mirror the real
	// Sanger MLWH schema: studies link to samples via iseq_flowcell (or
	// stock_resource for PacBio etc.), not via a column on sample itself.
	mustExec(t, db, `CREATE TABLE sample (
		id_sample_tmp    INTEGER PRIMARY KEY,
		id_lims          TEXT NOT NULL,
		id_sample_lims   TEXT NOT NULL,
		uuid_sample_lims TEXT NOT NULL,
		name             TEXT NOT NULL,
		sanger_sample_id TEXT NOT NULL,
		supplier_name    TEXT NOT NULL,
		accession_number TEXT NOT NULL,
		donor_id         TEXT NOT NULL,
		taxon_id         INTEGER NOT NULL,
		common_name      TEXT NOT NULL,
		description      TEXT NOT NULL,
		last_updated     TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE study (
		id_study_tmp                INTEGER PRIMARY KEY,
		id_lims                     TEXT NOT NULL,
		id_study_lims               TEXT NOT NULL,
		uuid_study_lims             TEXT NOT NULL,
		name                        TEXT NOT NULL,
		accession_number            TEXT,
		study_title                 TEXT,
		faculty_sponsor             TEXT,
		state                       TEXT NOT NULL,
		abstract                    TEXT,
		abbreviation                TEXT,
		description                 TEXT,
		data_release_strategy       TEXT,
		data_access_group           TEXT,
		hmdmc_number                TEXT,
		programme                   TEXT,
		created                     TEXT,
		reference_genome            TEXT,
		ethically_approved          INTEGER NOT NULL,
		study_type                  TEXT,
		contains_human_dna          INTEGER NOT NULL,
		contaminated_human_dna      INTEGER NOT NULL,
		study_visibility            TEXT,
		ega_dac_accession_number    TEXT,
		ega_policy_accession_number TEXT,
		data_release_timing         TEXT,
		last_updated                TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE iseq_flowcell (
		id_iseq_flowcell_tmp INTEGER PRIMARY KEY,
		pipeline_id_lims     TEXT NOT NULL,
		id_sample_tmp        INTEGER NOT NULL,
		id_study_tmp         INTEGER NOT NULL,
		last_updated         TEXT NOT NULL
	)`)

	return db
}

func seedRealMLWHSampleRow(t *testing.T, db *sql.DB, idTmp int64, idLims, idSampleLims, uuidLims, name, sangerSampleID, supplier, accession, donor string, taxon int, commonName, description string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp, idLims, idSampleLims, uuidLims, name, sangerSampleID, supplier, accession, donor, taxon, commonName, description, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHSampleRow: %v", err)
	}
}

func seedRealMLWHStudyRow(t *testing.T, db *sql.DB, idTmp int64, idLims, idStudyLims, uuidLims, name, accession string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, '', '', 'active', '', '', '', '', '', '', '', '', '', 1, '', 0, 0, '', '', '', '', ?)`,
		idTmp, idLims, idStudyLims, uuidLims, name, accession, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHStudyRow: %v", err)
	}
}

func seedRealMLWHFlowcellRow(t *testing.T, db *sql.DB, idTmp int64, pipelineIDLims string, idSampleTmp int64, idStudyTmp int64, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_flowcell(id_iseq_flowcell_tmp, pipeline_id_lims, id_sample_tmp, id_study_tmp, last_updated) VALUES (?, ?, ?, ?, ?)`,
		idTmp, pipelineIDLims, idSampleTmp, idStudyTmp, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHFlowcellRow: %v", err)
	}
}

func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()

	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}
