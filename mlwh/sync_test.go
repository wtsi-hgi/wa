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
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

var sampleSyncSourceColumns = []string{
	"id_sample_tmp",
	"id_lims",
	"id_sample_lims",
	"uuid_sample_lims",
	"name",
	"sanger_sample_id",
	"supplier_name",
	"accession_number",
	"donor_id",
	"taxon_id",
	"common_name",
	"description",
	"id_study_lims",
	"last_updated",
}

// sampleSyncSourceQuery mirrors the upstream sync.go SELECT against MLWH and
// must use the iseq_flowcell-correlated id_study_lims subquery because the
// real `sample` table has no `id_study_lims` column.
var sampleSyncSourceQuery = `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, ` + sampleStudyLimsSubquery + `, last_updated FROM sample WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_sample_tmp`

var studySyncSourceColumns = []string{
	"id_study_tmp",
	"id_lims",
	"id_study_lims",
	"uuid_study_lims",
	"name",
	"accession_number",
	"study_title",
	"faculty_sponsor",
	"state",
	"abstract",
	"abbreviation",
	"description",
	"data_release_strategy",
	"data_access_group",
	"hmdmc_number",
	"programme",
	"created",
	"reference_genome",
	"ethically_approved",
	"study_type",
	"contains_human_dna",
	"contaminated_human_dna",
	"study_visibility",
	"egadac_accession_number",
	"ega_policy_accession_number",
	"data_release_timing",
	"last_updated",
}

func TestClientSyncSampleColdCachePopulatesMirrorsAndWatermark(t *testing.T) {
	convey.Convey("Given a cold cache and three SQSCP sample rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		t1 := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		t3 := t2.Add(10 * time.Minute)

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleSyncSourceQuery)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(1, "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", 9606, "human", "desc-a", "study-a", formatSyncTime(t1)).
				AddRow(2, "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", "acc-b", "donor-b", 9606, "human", "desc-b", "study-b", formatSyncTime(t2)).
				AddRow(3, "SQSCP", "103", "sample-uuid-3", "sample-c", "sanger-c", "supplier-c", "acc-c", "donor-c", 9606, "human", "desc-c", "study-c", formatSyncTime(t3)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := client.Sync(context.Background(), "sample")

		convey.Convey("when Sync runs, then it mirrors the rows, populates donor_samples and advances the watermark", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Table, convey.ShouldEqual, "sample")
			convey.So(reports[0].Inserted, convey.ShouldEqual, 3)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(reports[0].HighWater, convey.ShouldHappenOnOrBetween, t3, t3)

			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 3)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 3)
			convey.So(readSyncHighWater(t, cache.DB(), "sample"), convey.ShouldHappenOnOrBetween, t3, t3)

			var sampleName, supplierName, donorID string
			err = cache.DB().QueryRow(`SELECT name, supplier_name, donor_id FROM sample_mirror WHERE id_sample_tmp = ?`, 3).Scan(&sampleName, &supplierName, &donorID)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleName, convey.ShouldEqual, "sample-c")
			convey.So(supplierName, convey.ShouldEqual, "supplier-c")
			convey.So(donorID, convey.ShouldEqual, "donor-c")

			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSampleWarmCacheUsesHighWaterFilter(t *testing.T) {
	convey.Convey("Given a warm sample cache with an existing high water mark", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t2 := time.Date(2026, time.May, 6, 10, 10, 0, 0, time.UTC)
		t3 := t2.Add(10 * time.Minute)
		seedSampleMirrorRow(t, cache.DB(), 2, "sample-b", "supplier-b", "donor-b", t2)
		seedDonorSampleRow(t, cache.DB(), "donor-b", 2, "study-b")
		seedSyncState(t, cache.DB(), "sample", t2)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleSyncSourceQuery)).
			WithArgs(formatSyncTime(t2)).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(3, "SQSCP", "103", "sample-uuid-3", "sample-c", "sanger-c", "supplier-c", "acc-c", "donor-c", 9606, "human", "desc-c", "study-c", formatSyncTime(t3)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := client.Sync(context.Background(), "sample")

		convey.Convey("when Sync runs, then it queries from the saved high water and upserts only the new row", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 1)
			convey.So(reports[0].Updated, convey.ShouldEqual, 0)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 2)
			convey.So(readSyncHighWater(t, cache.DB(), "sample"), convey.ShouldHappenOnOrBetween, t3, t3)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncSampleRollbackLeavesMirrorAndWatermarkUnchanged(t *testing.T) {
	convey.Convey("Given a sync failure after one new sample row is written", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 10, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)
		t3 := t2.Add(10 * time.Minute)

		seedSampleMirrorRow(t, cache.DB(), 1, "sample-a", "supplier-a", "donor-a", t1)
		seedDonorSampleRow(t, cache.DB(), "donor-a", 1, "study-a")
		seedSyncState(t, cache.DB(), "sample", t1)

		_, err := cache.DB().Exec(`CREATE TRIGGER fail_second_donor_insert BEFORE INSERT ON donor_samples WHEN NEW.id_sample_tmp = 3 BEGIN SELECT RAISE(FAIL, 'forced donor insert failure'); END;`)
		convey.So(err, convey.ShouldBeNil)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleSyncSourceQuery)).
			WithArgs(formatSyncTime(t1)).
			WillReturnRows(sqlmock.NewRows(sampleSyncSourceColumns).
				AddRow(2, "SQSCP", "102", "sample-uuid-2", "sample-b", "sanger-b", "supplier-b", "acc-b", "donor-b", 9606, "human", "desc-b", "study-b", formatSyncTime(t2)).
				AddRow(3, "SQSCP", "103", "sample-uuid-3", "sample-c", "sanger-c", "supplier-c", "acc-c", "donor-c", 9606, "human", "desc-c", "study-c", formatSyncTime(t3)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		_, err = client.Sync(context.Background(), "sample")

		convey.Convey("when the transaction rolls back, then the prior mirror rows and watermark remain unchanged", func() {
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples`), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror WHERE id_sample_tmp IN (2, 3)`), convey.ShouldEqual, 0)
			convey.So(readSyncHighWater(t, cache.DB(), "sample"), convey.ShouldHappenOnOrBetween, t1, t1)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncIseqFlowcellPopulatesDistinctLibrarySamples(t *testing.T) {
	convey.Convey("Given a cold cache and duplicate iseq_flowcell triples in the source", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 11, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT pipeline_id_lims, id_sample_tmp, id_study_lims, last_updated FROM iseq_flowcell WHERE last_updated >= ? ORDER BY last_updated, pipeline_id_lims, id_sample_tmp, id_study_lims`)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows([]string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"}).
				AddRow("lib-a", 11, "study-a", formatSyncTime(t1)).
				AddRow("lib-a", 11, "study-a", formatSyncTime(t2)).
				AddRow("lib-b", 12, "study-b", formatSyncTime(t2)))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := client.Sync(context.Background(), "iseq_flowcell")

		convey.Convey("when Sync runs, then library_samples stores one row per distinct triple", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples`), convey.ShouldEqual, 2)

			rows, queryErr := cache.DB().Query(`SELECT pipeline_id_lims, id_sample_tmp, id_study_lims FROM library_samples ORDER BY pipeline_id_lims, id_sample_tmp, id_study_lims`)
			convey.So(queryErr, convey.ShouldBeNil)
			defer func() { _ = rows.Close() }()

			var triples []string
			for rows.Next() {
				var pipelineID, studyID string
				var sampleID int64
				convey.So(rows.Scan(&pipelineID, &sampleID, &studyID), convey.ShouldBeNil)
				triples = append(triples, pipelineID+":"+studyID+":"+formatInt(sampleID))
			}

			convey.So(rows.Err(), convey.ShouldBeNil)
			convey.So(triples, convey.ShouldResemble, []string{"lib-a:study-a:11", "lib-b:study-b:12"})
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncStudyColdCachePopulatesMirrorAndWatermark(t *testing.T) {
	convey.Convey("Given a cold cache and two SQSCP study rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).
				AddRow(studyRowValues(2, "SQSCP", "202", "study-uuid-2", "study-b", "acc-b", t2)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		reports, err := client.Sync(context.Background(), "study")

		convey.Convey("when Sync runs, then it mirrors the study rows and stores the latest watermark", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(reports, convey.ShouldHaveLength, 1)
			convey.So(reports[0].Inserted, convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 2)
			convey.So(readSyncHighWater(t, cache.DB(), "study"), convey.ShouldHappenOnOrBetween, t2, t2)

			var title, sponsor string
			err = cache.DB().QueryRow(`SELECT study_title, faculty_sponsor FROM study_mirror WHERE id_study_tmp = ?`, 2).Scan(&title, &sponsor)
			convey.So(err, convey.ShouldBeNil)
			convey.So(title, convey.ShouldEqual, "Study title 2")
			convey.So(sponsor, convey.ShouldEqual, "Faculty sponsor 2")

			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncStudySkipsNonSQSCPSourceRows(t *testing.T) {
	convey.Convey("Given a study source that returns a non-SQSCP row", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		t1 := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).
				AddRow(studyRowValues(2, "GCLP", "202", "study-uuid-2", "study-b", "acc-b", t2)...))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		_, err = client.Sync(context.Background(), "study")

		convey.Convey("when Sync runs, then only SQSCP rows are written to study_mirror", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM study_mirror`), convey.ShouldEqual, 1)

			var idLims string
			err = cache.DB().QueryRow(`SELECT id_lims FROM study_mirror LIMIT 1`).Scan(&idLims)
			convey.So(err, convey.ShouldBeNil)
			convey.So(idLims, convey.ShouldEqual, "SQSCP")
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func TestClientSyncWritesSyncStateAfterCommit(t *testing.T) {
	convey.Convey("Given mocked cache and study source handles", t, func() {
		cacheDB, cacheMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = cacheDB.Close() }()

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = sourceDB.Close() }()

		t1 := time.Date(2026, time.May, 6, 12, 0, 0, 0, time.UTC)
		t2 := t1.Add(10 * time.Minute)

		cacheMock.MatchExpectationsInOrder(true)
		cacheMock.ExpectBegin()
		cacheMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water FROM sync_state WHERE table_name = ?`)).WithArgs("study").WillReturnRows(sqlmock.NewRows([]string{"high_water"}))
		cacheMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM study_mirror WHERE id_study_tmp = ? LIMIT 1`)).WithArgs(int64(1)).WillReturnRows(sqlmock.NewRows([]string{"found"}))
		cacheMock.ExpectExec(`INSERT INTO study_mirror`).WithArgs(studyMirrorArgs(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).WillReturnResult(sqlmock.NewResult(1, 1))
		cacheMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM study_mirror WHERE id_study_tmp = ? LIMIT 1`)).WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"found"}))
		cacheMock.ExpectExec(`INSERT INTO study_mirror`).WithArgs(studyMirrorArgs(2, "SQSCP", "202", "study-uuid-2", "study-b", "acc-b", t2)...).WillReturnResult(sqlmock.NewResult(1, 1))
		cacheMock.ExpectCommit()
		cacheMock.ExpectExec(`INSERT INTO sync_state`).WithArgs("study", formatSyncTime(t2), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`)).
			WithArgs(formatSyncTime(time.Time{})).
			WillReturnRows(sqlmock.NewRows(studySyncSourceColumns).
				AddRow(studyRowValues(1, "SQSCP", "201", "study-uuid-1", "study-a", "acc-a", t1)...).
				AddRow(studyRowValues(2, "SQSCP", "202", "study-uuid-2", "study-b", "acc-b", t2)...))

		client := &Client{cache: &sqliteCache{rwDB: cacheDB, roDB: cacheDB}, syncSource: sourceDB}

		_, err = client.Sync(context.Background(), "study")

		convey.Convey("when Sync succeeds, then commit happens before sync_state is updated", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(cacheMock.ExpectationsWereMet(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})
	})
}

func studyMirrorArgs(id int64, idLims, idStudyLims, uuidStudyLims, name, accession string, lastUpdated time.Time) []driver.Value {
	return studyRowValues(id, idLims, idStudyLims, uuidStudyLims, name, accession, lastUpdated)
}

func TestResolveLibraryColdCacheTriggersSyncBeforeLookup(t *testing.T) {
	convey.Convey("Given a cold cache and a library row materialized only by sync", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		syncEntered := make(chan struct{}, 1)
		releaseSync := make(chan struct{})
		resultCh := make(chan Match, 1)
		errCh := make(chan error, 1)

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(ctx context.Context, tx *sql.Tx, tables []string) error {
				if len(tables) != 1 || tables[0] != syncTableIseqFlowcell {
					return fmt.Errorf("unexpected sync tables: %v", tables)
				}

				_, err := tx.ExecContext(ctx, `INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 11, "study-a")
				if err != nil {
					return err
				}

				syncEntered <- struct{}{}
				<-releaseSync

				return nil
			},
		}

		go func() {
			match, err := client.ResolveLibrary(context.Background(), "Standard")
			if err != nil {
				errCh <- err
				return
			}

			resultCh <- match
		}()

		<-syncEntered

		select {
		case err := <-errCh:
			convey.So(err, convey.ShouldBeNil)
		case <-resultCh:
			convey.So("resolve returned before sync commit", convey.ShouldEqual, "")
		case <-time.After(50 * time.Millisecond):
		}

		close(releaseSync)

		select {
		case err := <-errCh:
			convey.So(err, convey.ShouldBeNil)
		case match := <-resultCh:
			convey.So(match.Kind, convey.ShouldEqual, KindLibraryType)
			convey.So(match.Canonical, convey.ShouldEqual, "Standard")
			convey.So(match.Library, convey.ShouldNotBeNil)
			convey.So(match.Library.PipelineIDLims, convey.ShouldEqual, "Standard")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM library_samples WHERE pipeline_id_lims = ?`, "Standard"), convey.ShouldEqual, 1)
		case <-time.After(time.Second):
			convey.So("resolve did not complete", convey.ShouldEqual, "")
		}
	})
}

func TestResolveSampleColdCacheTriggersSyncAtDonorStep(t *testing.T) {
	convey.Convey("Given a cold cache and a donor match materialized only by sync", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(ctx context.Context, tx *sql.Tx, tables []string) error {
				if len(tables) != 1 || tables[0] != syncTableSample {
					return fmt.Errorf("unexpected sync tables: %v", tables)
				}

				_, err := tx.ExecContext(ctx, `INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, id_study_lims, name, sanger_id, sanger_sample_id, supplier_name, accession_number, donor_id, library_type, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, 21, "SQSCP", "121", "sample-uuid-21", "study-21", "SANGER-21", "SANGER-21", "sanger-id-21", "supplier-21", "accession-21", "DONOR-X", "", 9606, "human", "desc-21", formatSyncTime(time.Date(2026, time.May, 6, 13, 0, 0, 0, time.UTC)))
				if err != nil {
					return err
				}

				_, err = tx.ExecContext(ctx, `INSERT INTO donor_samples(donor_id, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "DONOR-X", 21, "study-21")
				return err
			},
		}

		match, err := client.ResolveSample(context.Background(), "DONOR-X")

		convey.Convey("when the donor_id step is reached, then Sync runs first and the canonical Sanger name is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindDonorID)
			convey.So(match.Canonical, convey.ShouldEqual, "SANGER-21")
			convey.So(match.Sample, convey.ShouldNotBeNil)
			convey.So(match.Sample.Name, convey.ShouldEqual, "SANGER-21")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror WHERE donor_id = ?`, "DONOR-X"), convey.ShouldEqual, 1)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM donor_samples WHERE donor_id = ?`, "DONOR-X"), convey.ShouldEqual, 1)
		})
	})
}

func TestResolveLibraryAndSampleWarmCacheSkipSync(t *testing.T) {
	convey.Convey("Given warm resolver-backed caches with matching rows already present", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorRow(t, cache.DB(), 31, "SANGER-31", "supplier-31", "DONOR-WARM", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		seedDonorSampleRow(t, cache.DB(), "DONOR-WARM", 31, "study-31")
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))

		_, err := cache.DB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 31, "study-31")
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 14, 5, 0, 0, time.UTC))

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return fmt.Errorf("unexpected sync invocation")
			},
		}

		libraryMatch, libraryErr := client.ResolveLibrary(context.Background(), "Standard")
		sampleMatch, sampleErr := client.ResolveSample(context.Background(), "DONOR-WARM")

		convey.Convey("when the resolvers run against a warm cache, then they use cached rows without syncing again", func() {
			convey.So(libraryErr, convey.ShouldBeNil)
			convey.So(libraryMatch.Kind, convey.ShouldEqual, KindLibraryType)
			convey.So(libraryMatch.Canonical, convey.ShouldEqual, "Standard")

			convey.So(sampleErr, convey.ShouldBeNil)
			convey.So(sampleMatch.Kind, convey.ShouldEqual, KindDonorID)
			convey.So(sampleMatch.Canonical, convey.ShouldEqual, "SANGER-31")
		})
	})
}

func openSQLiteSyncTestCache(t *testing.T) Cache {
	t.Helper()

	cache, err := OpenCache(context.Background(), CacheConfig{Path: filepath.Join(t.TempDir(), "sync.sqlite")})
	if err != nil {
		t.Fatalf("OpenCache(): %v", err)
	}

	return cache
}

func seedSampleMirrorRow(t *testing.T, db *sql.DB, id int64, name, supplierName, donorID string, lastUpdated time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, id_study_lims, name, sanger_id, sanger_sample_id, supplier_name, accession_number, donor_id, library_type, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		formatInt(id+100),
		"seed-sample-uuid",
		"",
		name,
		name,
		"seed-sanger",
		supplierName,
		"seed-accession",
		donorID,
		"",
		9606,
		"human",
		"seed-description",
		formatSyncTime(lastUpdated),
	)
	if err != nil {
		t.Fatalf("seedSampleMirrorRow(): %v", err)
	}
}

func seedDonorSampleRow(t *testing.T, db *sql.DB, donorID string, idSampleTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO donor_samples(donor_id, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, donorID, idSampleTmp, idStudyLims)
	if err != nil {
		t.Fatalf("seedDonorSampleRow(): %v", err)
	}
}

func seedSyncState(t *testing.T, db *sql.DB, table string, highWater time.Time) {
	t.Helper()

	if err := writeSyncState(context.Background(), db, "sqlite", table, highWater); err != nil {
		t.Fatalf("seedSyncState(): %v", err)
	}
}

func studyRowValues(id int64, idLims, idStudyLims, uuidStudyLims, name, accession string, lastUpdated time.Time) []driver.Value {
	return []driver.Value{
		id,
		idLims,
		idStudyLims,
		uuidStudyLims,
		name,
		accession,
		"Study title " + formatInt(id),
		"Faculty sponsor " + formatInt(id),
		"active",
		"abstract",
		"abbr",
		"description",
		"strategy",
		"group",
		"hmdmc",
		"programme",
		"2026-05-06",
		"GRCh38",
		true,
		"study-type",
		false,
		false,
		"public",
		"EGAD0001",
		"EGAP0001",
		"immediate",
		formatSyncTime(lastUpdated),
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("countRows(%q): %v", query, err)
	}

	return count
}

func formatInt(value int64) string {
	return strconv.FormatInt(value, 10)
}

func readSyncHighWater(t *testing.T, db *sql.DB, table string) time.Time {
	t.Helper()

	var raw string
	if err := db.QueryRow(`SELECT high_water FROM sync_state WHERE table_name = ?`, table).Scan(&raw); err != nil {
		t.Fatalf("readSyncHighWater(%s): %v", table, err)
	}

	highWater, err := parseSyncTimeString(raw)
	if err != nil {
		t.Fatalf("parseSyncTimeString(%s): %v", raw, err)
	}

	return highWater
}
