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
	"database/sql/driver"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// The 2026-10-22 Unified Warehouse release lets a Sapio study share
// uuid_study_lims with a Sequencescape (id_lims SQSCP) study. These tests put
// such a pair in the cache, the Sapio row first so an unfiltered read would see
// it, and check that every study_mirror read still answers with the SQSCP study.
const (
	sharedStudyUUID         = "5b1c3a2e-4f6d-11ee-9c1a-024224dd57f4"
	sharedStudyLimsID       = "6100"
	sharedStudySQSCPName    = "Sequencescape study"
	sharedStudySQSCPAcc     = "EGAS00001006100"
	sharedStudySapioAcc     = "EGAS00000000000"
	sharedStudySapioOnly    = "Sapio Only Sponsor"
	sharedStudySQSCPSponsor = "Sequencescape Sponsor"
	sharedStudySampleID     = int64(61001)
)

func TestSyncStudyFromSourceWhereSapioStudySharesUUID(t *testing.T) {
	convey.Convey("Given a source study table unique on (uuid_study_lims, id_lims) with a Sapio and a SQSCP study sharing a uuid", t, func() {
		source := openRealMLWHSchemaSource(t)
		mustExec(t, source, `CREATE UNIQUE INDEX study_uuid_study_lims_id_lims ON study(uuid_study_lims, id_lims)`)
		updated := time.Date(2026, time.October, 22, 17, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 1, "Sapio", sharedStudyLimsID, sharedStudyUUID, "Sapio study", sharedStudySapioAcc, updated)
		seedRealMLWHStudyRow(t, source, 2, sqscpIDLims, sharedStudyLimsID, sharedStudyUUID, sharedStudySQSCPName, sharedStudySQSCPAcc, updated)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableStudy)

		convey.Convey("when the study table syncs, then only the SQSCP study reaches the cache and the uuid resolves to it", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror WHERE uuid_study_lims = ?`, sharedStudyUUID), convey.ShouldEqual, 1)

			match, resolveErr := client.ResolveStudy(context.Background(), sharedStudyUUID)
			convey.So(resolveErr, convey.ShouldBeNil)
			convey.So(match.Study.IDLims, convey.ShouldEqual, sqscpIDLims)
			convey.So(match.Study.AccessionNumber, convey.ShouldEqual, sharedStudySQSCPAcc)
		})
	})
}

func TestSyncUltimagenFromSourceWithNewUseqWaferColumns(t *testing.T) {
	convey.Convey("Given a source useq_wafer carrying the five nullable columns added on 2026-10-22", t, func() {
		source := openRealMLWHSchemaSource(t)
		for _, column := range []string{"application_type", "uga_primer", "ugb_primer", "converted_illumina_tag_sequence", "converted_illumina_tag2_sequence"} {
			mustExec(t, source, `ALTER TABLE useq_wafer ADD COLUMN `+column+` TEXT`)
		}

		base := time.Date(2026, time.October, 22, 17, 0, 0, 0, time.UTC)
		seedRealMLWHStudyRow(t, source, 74, sqscpIDLims, "7401", "uuid-study-74", "Study Ultimagen", "acc-st-74", base)
		seedRealMLWHUseqWaferRow(t, source, 9400, 941, 74)
		mustExec(t, source, `INSERT INTO useq_wafer(id_useq_wafer_tmp, id_sample_tmp, id_study_tmp, application_type, uga_primer, ugb_primer, converted_illumina_tag_sequence, converted_illumina_tag2_sequence) VALUES (9401, 942, 74, 'WGS', 'ACGT', 'TGCA', 'AACC', 'GGTT')`)
		seedRealMLWHUseqProductMetricRow(t, source, "useq-product-null-new-columns", 9400, 74001, base.Add(time.Minute))
		seedRealMLWHUseqProductMetricRow(t, source, "useq-product-set-new-columns", 9401, 74001, base.Add(2*time.Minute))
		seedRealMLWHIRODSLocationNullRelativePathRow(t, source, 84001, "useq-product-set-new-columns", "ultimagen", "/seq/ultimagen/runs/r2", base, base.Add(3*time.Minute))

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sqliteJSONTableSource{db: source}, disableSyncLock: true}
		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableUseqProductMetrics, syncTableSeqProductIRODSLocations)

		convey.Convey("when Ultimagen product metrics and iRODS locations sync, then both wafers' products and the iRODS row arrive", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM useq_product_metrics_mirror WHERE id_study_lims = '7401'`), convey.ShouldEqual, 2)

			row := readIRODSLocationMirrorRowForTest(t, cache.DB(), "useq-product-set-new-columns")
			convey.So(row.idSampleTmp, convey.ShouldEqual, 942)
			convey.So(row.idStudyLims, convey.ShouldEqual, "7401")
		})
	})
}

func TestSharedStudyUUIDReadsReturnOnlySQSCPStudySQLite(t *testing.T) {
	convey.Convey("Given a SQLite cache holding a Sapio study that shares a SQSCP study's uuid and id_study_lims", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSharedStudyUUIDScenario(t, cache)
		assertSharedStudyUUIDReads(&Client{cache: cache, cacheReader: cacheReadDB(cache)})
	})
}

func TestSharedStudyUUIDReadsReturnOnlySQSCPStudyMySQL(t *testing.T) {
	cfg, skipReason := loadMySQLCacheConfigForTest(t)
	if skipReason != "" {
		t.Skip(skipReason)
	}

	convey.Convey("Given a MySQL cache holding a Sapio study that shares a SQSCP study's uuid and id_study_lims", t, func() {
		cache := openMySQLCacheForTest(t, cfg)

		seedSharedStudyUUIDScenario(t, cache)
		assertSharedStudyUUIDReads(&Client{cache: cache, cacheReader: cacheReadDB(cache)})
	})
}

func seedSharedStudyUUIDScenario(t *testing.T, cache Cache) {
	t.Helper()

	db := cache.DB()
	updated := time.Date(2026, time.October, 22, 17, 0, 0, 0, time.UTC)

	seedStudyMirrorWithLIMS(t, db, studyRowValues(1, "Sapio", sharedStudyLimsID, sharedStudyUUID, "Sapio study", sharedStudySapioAcc, updated), sharedStudySapioOnly)
	seedStudyMirrorWithLIMS(t, db, studyRowValues(2, sqscpIDLims, sharedStudyLimsID, sharedStudyUUID, sharedStudySQSCPName, sharedStudySQSCPAcc, updated), sharedStudySQSCPSponsor)

	seedManifestSampleRow(t, db, sharedStudySampleID, "shared-study-sample", "shared-supplier", "EGAN-shared", "shared-sanger")
	seedLibrarySample(t, db, "Standard", sharedStudySampleID, sharedStudyLimsID)
	seedExportIRODSRow(t, db, exportIRODSSeedRow{
		IDSeqProductLocation: 1, IDProduct: "shared-product", Collection: "/seq/6100", FileName: "6100_1#1.cram",
		IDSampleTmp: sharedStudySampleID, StudyID: sharedStudyLimsID, IDRun: 6100, Position: 1, TagIndex: 1,
		IsDeliverable: sql.NullInt64{Int64: 1, Valid: true},
	})

	for _, table := range supportedSyncTables {
		if err := writeSyncState(context.Background(), db, cache.Dialect(), table, updated, nil, false); err != nil {
			t.Fatalf("writeSyncState(%s): %v", table, err)
		}
	}
}

func seedStudyMirrorWithLIMS(t *testing.T, db *sql.DB, values []driver.Value, facultySponsor string) {
	t.Helper()

	args := make([]any, len(values))
	for index, value := range values {
		args[index] = value
	}

	if _, err := db.Exec(`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...); err != nil {
		t.Fatalf("seedStudyMirrorWithLIMS: %v", err)
	}

	if _, err := db.Exec(`UPDATE study_mirror SET faculty_sponsor = ? WHERE id_study_tmp = ?`, facultySponsor, values[0]); err != nil {
		t.Fatalf("seedStudyMirrorWithLIMS sponsor: %v", err)
	}
}

func assertSharedStudyUUIDReads(client *Client) {
	ctx := context.Background()

	convey.Convey("when the shared uuid is resolved or classified, then the SQSCP study is returned", func() {
		for _, resolve := range []func(context.Context, string) (Match, error){client.ResolveStudy, client.ClassifyIdentifier} {
			match, err := resolve(ctx, sharedStudyUUID)
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindStudyUUID)
			convey.So(match.Study, convey.ShouldNotBeNil)
			convey.So(match.Study.IDLims, convey.ShouldEqual, sqscpIDLims)
			convey.So(match.Study.Name, convey.ShouldEqual, sharedStudySQSCPName)
		}
	})

	convey.Convey("when latest data is listed by a sponsor only the Sapio study has, then nothing is returned", func() {
		rows, err := client.LatestDataForFacultySponsor(ctx, sharedStudySapioOnly, "", 10, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldBeEmpty)

		rows, err = client.LatestDataForFacultySponsor(ctx, sharedStudySQSCPSponsor, "", 10, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldHaveLength, 1)
	})

	convey.Convey("when the study's latest data is listed, then its one data object carries the SQSCP study name", func() {
		rows, err := client.LatestDataForStudy(ctx, sharedStudyLimsID, "", 10, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldHaveLength, 1)
		convey.So(rows[0].StudyName, convey.ShouldEqual, sharedStudySQSCPName)
	})

	convey.Convey("when the study's iRODS paths are listed, then the one path carries the SQSCP study accession", func() {
		paths, err := client.IRODSPathsForStudy(ctx, sharedStudyLimsID, 10, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(paths, convey.ShouldHaveLength, 1)
		convey.So(paths[0].StudyAccessionNumber, convey.ShouldEqual, sharedStudySQSCPAcc)
	})

	convey.Convey("when the study's sample CRAMs are exported, then the one row carries the SQSCP study accession", func() {
		result, err := client.Export(ctx, ExportRelationship{Children: "sample-crams", ParentKind: "study"}, sharedStudyLimsID, ExportOptions{
			Columns: []string{"name", "study_accession_number"},
		})
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Rows, convey.ShouldResemble, [][]string{{"shared-study-sample", sharedStudySQSCPAcc}})
	})

	convey.Convey("when the study's iRODS objects are exported, then the one row carries the SQSCP study accession", func() {
		result, err := client.Export(ctx, ExportRelationship{Children: "irods", ParentKind: "study"}, sharedStudyLimsID, ExportOptions{
			Columns: []string{"irods_path", "study_accession_number"},
		})
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Rows, convey.ShouldResemble, [][]string{{"/seq/6100/6100_1#1.cram", sharedStudySQSCPAcc}})
	})
}
