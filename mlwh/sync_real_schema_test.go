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
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

// TestSyncAgainstRealMLWHSchema exercises the full Client.Sync path against an
// upstream "MLWH" SQLite database whose table shapes faithfully match the real
// Sanger MLWH columns (in particular: the upstream `sample` table has NO
// `id_study_lims` column; `iseq_flowcell` links to `study` via
// `id_study_tmp` rather than carrying `id_study_lims`; product metric
// watermarks are `last_changed`; iRODS locations key products through
// `id_product`; and the study table uses the current
// `ega_dac_accession_number` spelling).
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
		seedRealMLWHProductMetricRow(t, source, 1001, 100, 9001, 1, 1, 1, 1, 1, time.Date(2026, time.May, 7, 9, 15, 0, 0, time.UTC))
		seedRealMLWHIRODSLocationRow(t, source, 1001, "/seq", "run/1", "/seq/run", "file.cram", time.Date(2026, time.May, 7, 9, 20, 0, 0, time.UTC))
		// sample 2 deliberately has no flowcell entry, so it produces no
		// library_samples study mapping while the rest of the sync still succeeds.

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true}

		reports, err := client.Sync(context.Background())

		convey.Convey("when Sync runs without restricting tables, then every table is synced and the study mapping is stored via library_samples", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 5)

			byTable := make(map[string]SyncReport, len(reports))
			for _, report := range reports {
				byTable[report.Table] = report
			}
			convey.So(byTable["sample"].Inserted, convey.ShouldEqual, 2)
			convey.So(byTable["study"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["iseq_flowcell"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["iseq_product_metrics"].Inserted, convey.ShouldEqual, 1)
			convey.So(byTable["seq_product_irods_locations"].Inserted, convey.ShouldEqual, 1)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 1)

			paths, pathErr := client.IRODSPathsForSample(context.Background(), "sample-a", 100, 0)
			convey.So(pathErr, convey.ShouldBeNil)
			convey.So(paths, convey.ShouldResemble, []IRODSPath{{
				IDProduct:  "product-1001",
				Collection: "/seq/run",
				DataObject: "1",
				IRODSPath:  "/seq/run/1",
			}})

			var studyLimsForSample1 string
			convey.So(cache.DB().QueryRow(`SELECT id_study_lims FROM library_samples WHERE id_sample_tmp = 1`).Scan(&studyLimsForSample1), convey.ShouldBeNil)
			convey.So(studyLimsForSample1, convey.ShouldEqual, "5001")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples WHERE id_sample_tmp = 2`), convey.ShouldEqual, 0)
		})
	})
}

func TestClientSyncSeqProductIRODSLocationsRealSourceExpandsCompositeProducts(t *testing.T) {
	convey.Convey("Given an upstream composite product iRODS location whose component products link to two samples", t, func() {
		source := openRealMLWHSchemaSource(t)
		base := time.Date(2026, time.May, 13, 9, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 50, "SQSCP", "7607", "uuid-study-50", "Study Fifty", "acc-st-50", base)
		seedRealMLWHFlowcellRow(t, source, 5001, "Custom", 9419243, 50, base.Add(time.Minute))
		seedRealMLWHFlowcellRow(t, source, 5002, "Custom", 9419244, 50, base.Add(2*time.Minute))
		seedRealMLWHProductMetricRow(t, source, 6001, 5001, 48522, 1, 1, 1, 1, 1, base.Add(3*time.Minute))
		seedRealMLWHProductMetricRow(t, source, 6002, 5002, 48522, 2, 1, 1, 1, 1, base.Add(4*time.Minute))
		seedRealMLWHCompositeProductMetricRow(t, source, 7001, "composite-product", `{"components":[{"id_run":48522,"position":1,"tag_index":1},{"id_run":48522,"position":2,"tag_index":1}]}`, base.Add(5*time.Minute))
		seedRealMLWHIRODSLocationProductRow(t, source, 8001, "composite-product", "/seq/illumina/runs/48/48522", "plex1/48522#1.cram", base.Add(6*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, "composite-product"), convey.ShouldEqual, 2)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND id_sample_tmp = ? AND id_study_lims = ?`, "composite-product", 9419243, "7607"), convey.ShouldEqual, 1)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ? AND id_sample_tmp = ? AND id_study_lims = ?`, "composite-product", 9419244, "7607"), convey.ShouldEqual, 1)
		convey.So(locationMirrorFileForTest(t, cache.DB(), "composite-product"), convey.ShouldEqual, "48522#1.cram")
	})
}

// TestAllStudiesAgainstRealMLWHSchema verifies that the synced-cache study list
// path tolerates the current upstream study column spelling used by MLWH.
func TestAllStudiesAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given a synced cache and an upstream database with the current MLWH study schema", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 10, "SQSCP", "5001", "uuid-study-1", "Study One", "acc-st-1", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableStudy)
		convey.So(err, convey.ShouldBeNil)

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then it returns the synced study from study_mirror", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "5001")
			convey.So(studies[0].EGADACAccessionNumber, convey.ShouldEqual, "")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)
		})
	})
}

func TestAllStudiesAgainstRealMLWHSchemaAllowsNullStudyStrings(t *testing.T) {
	convey.Convey("Given a synced cache and an upstream study row with nullable text fields", t, func() {
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
		_, err = syncSelectedTablesForTest(context.Background(), client, syncTableStudy)
		convey.So(err, convey.ShouldBeNil)

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.Convey("when AllStudies runs, then nullable upstream strings are normalized to empty strings after sync", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldHaveLength, 1)
			convey.So(studies[0].IDStudyLims, convey.ShouldEqual, "5002")
			convey.So(studies[0].AccessionNumber, convey.ShouldEqual, "")
			convey.So(studies[0].FacultySponsor, convey.ShouldEqual, "")
			convey.So(studies[0].EGADACAccessionNumber, convey.ShouldEqual, "")
		})
	})
}

// TestSampleResolverAgainstRealMLWHSchema verifies that syncing sample_mirror
// from the real MLWH schema supports later cache-only resolution, even though
// the upstream `sample` table has no `id_study_lims` column.
func TestSampleResolverAgainstRealMLWHSchema(t *testing.T) {
	convey.Convey("Given a synced cache and an upstream with the real MLWH schema", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHSampleRow(t, source, 5, "SQSCP", "5005", "b7daafb8-c59f-11ee-8fba-024224dd57f4", "name-5", "ssid-5", "supplier-5", "acc-5", "donor-5", 9606, "human", "desc-5", time.Date(2026, time.May, 7, 10, 0, 0, 0, time.UTC))
		seedRealMLWHStudyRow(t, source, 99, "SQSCP", "9999", "uuid-study-99", "Study Ninety Nine", "acc-st-99", time.Date(2026, time.May, 7, 8, 30, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 500, "Standard", 5, 99, time.Date(2026, time.May, 7, 10, 5, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		_, err := syncSelectedTablesForTest(context.Background(), client, syncTableSample)
		convey.So(err, convey.ShouldBeNil)

		match, err := client.ResolveSample(context.Background(), "b7daafb8-c59f-11ee-8fba-024224dd57f4")

		convey.Convey("when ResolveSample runs against the synced cache, then it returns the sample without relying on a removed sample.id_study_lims column", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindSampleUUID)
			convey.So(match.Sample, convey.ShouldNotBeNil)
			convey.So(match.Sample.UUIDSampleLims, convey.ShouldEqual, "b7daafb8-c59f-11ee-8fba-024224dd57f4")
			convey.So(match.Sample.Studies, convey.ShouldBeEmpty)
			convey.So(match.Sample.Libraries, convey.ShouldBeEmpty)
		})
	})
}

func TestClientSyncIseqFlowcellRealSourceSkipsNonSQSCPStudyRows(t *testing.T) {
	convey.Convey("B7.1: Given a real-source iseq_flowcell row linked to a non-SQSCP study", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 20, "GCLP", "6001", "uuid-study-20", "Non SQSCP Study", "acc-st-20", time.Date(2026, time.May, 7, 8, 0, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 200, "Standard", 21, 20, time.Date(2026, time.May, 7, 9, 0, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqFlowcell)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
		convey.So(reports[0].Updated, convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 0)
	})
}

func TestClientSyncProductTablesRealSourceSkipRowsWithoutSQSCPStudy(t *testing.T) {
	convey.Convey("B7.2: Given real-source product rows linked through a non-SQSCP study", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 30, "GCLP", "7001", "uuid-study-30", "Non SQSCP Study", "acc-st-30", time.Date(2026, time.May, 7, 8, 5, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 300, "Standard", 31, 30, time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC))
		seedRealMLWHProductMetricRow(t, source, 3001, 300, 9001, 1, 1, 1, 1, 1, time.Date(2026, time.May, 7, 9, 10, 0, 0, time.UTC))
		seedRealMLWHIRODSLocationRow(t, source, 3001, "/seq", "run/3001", "/seq/run/3001", "3001.cram", time.Date(2026, time.May, 7, 9, 15, 0, 0, time.UTC))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 2)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 0)
		convey.So(reports[1].Inserted, convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM iseq_product_metrics_mirror`), convey.ShouldEqual, 0)
		convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 0)
	})
}

func TestClientSyncProductMetricsRealSourceNormalizesNullableNumericFields(t *testing.T) {
	convey.Convey("B7.3: Given a real-source product metric row with nullable lane and QC fields", t, func() {
		source := openRealMLWHSchemaSource(t)
		seedRealMLWHStudyRow(t, source, 40, "SQSCP", "8001", "uuid-study-40", "Study Forty", "acc-st-40", time.Date(2026, time.May, 7, 8, 5, 0, 0, time.UTC))
		seedRealMLWHFlowcellRow(t, source, 400, "Standard", 41, 40, time.Date(2026, time.May, 7, 9, 5, 0, 0, time.UTC))
		_, err := source.Exec(
			`INSERT INTO iseq_product_metrics(id_iseq_pr_metrics_tmp, id_iseq_product, last_changed, id_iseq_flowcell_tmp, id_run, position, tag_index, qc, qc_lib, qc_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			4001,
			"product-4001",
			formatSyncTime(time.Date(2026, time.May, 7, 9, 10, 0, 0, time.UTC)),
			400,
			nil,
			nil,
			nil,
			nil,
			nil,
			nil,
		)
		convey.So(err, convey.ShouldBeNil)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableIseqProductMetrics)

		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)
		convey.So(reports[0].Inserted, convey.ShouldEqual, 1)

		var idRun, position, tagIndex, qc, qcLib, qcSeq int
		convey.So(cache.DB().QueryRow(`SELECT id_run, position, tag_index, qc, qc_lib, qc_seq FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, "product-4001").Scan(&idRun, &position, &tagIndex, &qc, &qcLib, &qcSeq), convey.ShouldBeNil)
		convey.So([]int{idRun, position, tagIndex, qc, qcLib, qcSeq}, convey.ShouldResemble, []int{0, 0, 0, 0, 0, 0})
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
		legacy_library_id    INTEGER,
		id_library_lims      TEXT,
		last_updated         TEXT NOT NULL
	)`)

	mustExec(t, db, `CREATE TABLE iseq_product_metrics (
		id_iseq_pr_metrics_tmp INTEGER PRIMARY KEY,
		id_iseq_product        TEXT NOT NULL,
		last_changed           TEXT NOT NULL,
		id_iseq_flowcell_tmp INTEGER,
		id_run               INTEGER,
		position             INTEGER,
		tag_index            INTEGER,
		iseq_composition_tmp TEXT,
		qc                   INTEGER,
		qc_lib               INTEGER,
		qc_seq               INTEGER
	)`)

	mustExec(t, db, `CREATE TABLE seq_product_irods_locations (
		id_seq_product_irods_locations_tmp INTEGER PRIMARY KEY,
		last_changed            TEXT NOT NULL,
		id_product              TEXT NOT NULL,
		irods_root_collection    TEXT NOT NULL,
		irods_data_relative_path TEXT NOT NULL,
		irods_secondary_data_relative_path TEXT
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
		`INSERT INTO iseq_flowcell(id_iseq_flowcell_tmp, pipeline_id_lims, id_sample_tmp, id_study_tmp, legacy_library_id, id_library_lims, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idTmp, pipelineIDLims, idSampleTmp, idStudyTmp, nil, nil, formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedRealMLWHFlowcellRow: %v", err)
	}
}

func seedRealMLWHProductMetricRow(t *testing.T, db *sql.DB, idProduct int64, idFlowcellTmp int64, idRun, position, tagIndex, qc, qcLib, qcSeq int, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics(id_iseq_pr_metrics_tmp, id_iseq_product, last_changed, id_iseq_flowcell_tmp, id_run, position, tag_index, iseq_composition_tmp, qc, qc_lib, qc_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idProduct,
		fmt.Sprintf("product-%d", idProduct),
		formatSyncTime(lastUpdated),
		idFlowcellTmp,
		idRun,
		position,
		tagIndex,
		fmt.Sprintf(`{"components":[{"id_run":%d,"position":%d,"tag_index":%d}]}`, idRun, position, tagIndex),
		qc,
		qcLib,
		qcSeq,
	)
	if err != nil {
		t.Fatalf("seedRealMLWHProductMetricRow: %v", err)
	}
}

func seedRealMLWHCompositeProductMetricRow(t *testing.T, db *sql.DB, idTmp int64, idProduct string, composition string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO iseq_product_metrics(id_iseq_pr_metrics_tmp, id_iseq_product, last_changed, id_iseq_flowcell_tmp, id_run, position, tag_index, iseq_composition_tmp, qc, qc_lib, qc_seq) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idTmp,
		idProduct,
		formatSyncTime(lastUpdated),
		nil,
		nil,
		nil,
		nil,
		composition,
		1,
		1,
		1,
	)
	if err != nil {
		t.Fatalf("seedRealMLWHCompositeProductMetricRow: %v", err)
	}
}

func seedRealMLWHIRODSLocationRow(t *testing.T, db *sql.DB, idProduct int64, rootCollection, relativePath, collection, fileName string, lastUpdated time.Time) {
	t.Helper()
	_ = collection
	_ = fileName

	seedRealMLWHIRODSLocationProductRow(t, db, idProduct, fmt.Sprintf("product-%d", idProduct), rootCollection, relativePath, lastUpdated)
}

func seedRealMLWHIRODSLocationProductRow(t *testing.T, db *sql.DB, idTmp int64, idProduct, rootCollection, relativePath string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO seq_product_irods_locations(id_seq_product_irods_locations_tmp, last_changed, id_product, irods_root_collection, irods_data_relative_path, irods_secondary_data_relative_path) VALUES (?, ?, ?, ?, ?, ?)`,
		idTmp,
		formatSyncTime(lastUpdated),
		idProduct,
		rootCollection,
		relativePath,
		nil,
	)
	if err != nil {
		t.Fatalf("seedRealMLWHIRODSLocationRow: %v", err)
	}
}

type sqliteJSONTableSource struct {
	db *sql.DB
}

func (source sqliteJSONTableSource) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return source.db.QueryContext(ctx, rewriteJSONTableQueryForSQLite(query), args...)
}

func rewriteJSONTableQueryForSQLite(query string) string {
	query = strings.Replace(query,
		`INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE`,
		`INNER JOIN json_each(path_ipm.iseq_composition_tmp, '$.components') component ON TRUE`,
		1,
	)
	query = strings.Replace(query,
		`ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index`,
		`ipm.id_run = CAST(json_extract(component.value, '$.id_run') AS INTEGER) AND ipm.position = CAST(json_extract(component.value, '$.position') AS INTEGER) AND ipm.tag_index = CAST(json_extract(component.value, '$.tag_index') AS INTEGER)`,
		1,
	)

	return query
}

func mustExec(t *testing.T, db *sql.DB, stmt string) {
	t.Helper()

	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("exec %q: %v", stmt, err)
	}
}
