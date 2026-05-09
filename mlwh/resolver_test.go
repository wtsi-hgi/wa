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
	"errors"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

func TestResolveRunRejectsNonNumericWithoutSQL(t *testing.T) {
	convey.Convey("Given a non-numeric run identifier", t, func() {
		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		sourceMock.ExpectClose()
		convey.Reset(func() {
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		client := &Client{syncSource: sourceDB}

		match, err := client.ResolveRun(context.Background(), "abc")

		convey.Convey("when ResolveRun executes, then it rejects the identifier before any query", func() {
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
			convey.So(match, convey.ShouldResemble, Match{})
		})
	})
}

func TestResolveRunReturnsCanonicalRunIDForMetricsMatch(t *testing.T) {
	convey.Convey("Given a numeric run identifier present in iseq_product_metrics", t, func() {
		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_run FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`)).
			WithArgs(12345).
			WillReturnRows(sqlmock.NewRows([]string{"id_run"}).AddRow(12345))
		sourceMock.ExpectClose()

		client := &Client{syncSource: sourceDB}

		match, err := client.ResolveRun(context.Background(), "12345")

		convey.Convey("when ResolveRun executes, then it returns the run match", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindRunID)
			convey.So(match.Canonical, convey.ShouldEqual, "12345")
			convey.So(match.Run, convey.ShouldNotBeNil)
			convey.So(match.Run.IDRun, convey.ShouldEqual, 12345)
		})
	})
}

func TestResolveRunReturnsNotFoundWhenMetricsRowMissing(t *testing.T) {
	convey.Convey("Given a numeric run identifier absent from iseq_product_metrics", t, func() {
		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_run FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`)).
			WithArgs(12345).
			WillReturnRows(sqlmock.NewRows([]string{"id_run"}))
		sourceMock.ExpectClose()

		client := &Client{syncSource: sourceDB}

		match, err := client.ResolveRun(context.Background(), "12345")

		convey.Convey("when ResolveRun executes, then it returns ErrNotFound", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(match, convey.ShouldResemble, Match{})
		})
	})
}

func TestResolveLibraryReturnsCacheMatchWithoutMLWHQuery(t *testing.T) {
	convey.Convey("Given a warm cache containing the requested pipeline_id_lims", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		_, err := cache.DB().Exec(`INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Standard", 51, "study-51")
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 15, 0, 0, 0, time.UTC))

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		sourceMock.ExpectClose()
		convey.Reset(func() {
			convey.So(sourceDB.Close(), convey.ShouldBeNil)
			convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		})

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		match, err := client.ResolveLibrary(context.Background(), "Standard")

		convey.Convey("when ResolveLibrary executes, then it uses only the cache row", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(match.Kind, convey.ShouldEqual, KindLibraryType)
			convey.So(match.Canonical, convey.ShouldEqual, "Standard")
			convey.So(match.Library, convey.ShouldNotBeNil)
			convey.So(match.Library.PipelineIDLims, convey.ShouldEqual, "Standard")
		})
	})
}

func TestResolveLibraryColdCacheSyncsBeforeLookup(t *testing.T) {
	convey.Convey("Given a cold cache and a library row inserted only during sync", t, func() {
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
					return errors.New("unexpected sync tables")
				}

				_, err := tx.ExecContext(ctx, `INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES (?, ?, ?)`, "Bespoke", 61, "study-61")
				if err != nil {
					return err
				}

				syncEntered <- struct{}{}
				<-releaseSync

				return nil
			},
		}

		go func() {
			match, err := client.ResolveLibrary(context.Background(), "Bespoke")
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
			convey.So(match.Canonical, convey.ShouldEqual, "Bespoke")
			convey.So(match.Library, convey.ShouldNotBeNil)
			convey.So(match.Library.PipelineIDLims, convey.ShouldEqual, "Bespoke")
		case <-time.After(time.Second):
			convey.So("resolve did not complete", convey.ShouldEqual, "")
		}
	})
}

func TestResolveLibraryReturnsNotFoundOnWarmCacheMiss(t *testing.T) {
	convey.Convey("Given a warm cache without a matching pipeline_id_lims row", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 15, 30, 0, 0, time.UTC))

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncRunner: func(context.Context, *sql.Tx, []string) error {
				return errors.New("unexpected sync invocation")
			},
		}

		match, err := client.ResolveLibrary(context.Background(), "Unknown")

		convey.Convey("when ResolveLibrary executes, then it returns ErrNotFound", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(match, convey.ShouldResemble, Match{})
		})
	})
}

func TestResolveLibraryDocCommentMentionsColdCacheGuidance(t *testing.T) {
	convey.Convey("Given the resolver source file", t, func() {
		resolverPath := "resolver.go"

		content, err := os.ReadFile(resolverPath)

		convey.Convey("when the ResolveLibrary doc comment is read, then it mentions first call and wa mlwh sync", func() {
			convey.So(err, convey.ShouldBeNil)
			text := string(content)
			match := regexp.MustCompile(`(?s)// ResolveLibrary.*?func \(c \*Client\) ResolveLibrary`).FindString(text)
			convey.So(match, convey.ShouldContainSubstring, "first call")
			convey.So(match, convey.ShouldContainSubstring, "wa mlwh sync")
		})
	})
}

func TestResolveStudyUUIDMatch(t *testing.T) {
	convey.Convey("Given a UUID-shaped study identifier with a cache match", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		const raw = "b7daafb8-c59f-11ee-8fba-024224dd57f4"
		seedStudyMirrorRow(t, cache.DB(), 11, "6568", raw, "Study 11", "EGAS00001001111")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyUUID)
		convey.So(match.Canonical, convey.ShouldEqual, "6568")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.UUIDStudyLims, convey.ShouldEqual, raw)
	})
}

func TestResolveStudyLimsIDMatch(t *testing.T) {
	convey.Convey("Given a numeric study identifier matching id_study_lims", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 12, "6568", "study-uuid-12", "Study 12", "EGAS00001001212")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyLimsID)
		convey.So(match.Canonical, convey.ShouldEqual, "6568")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.IDStudyLims, convey.ShouldEqual, "6568")
	})
}

func TestResolveStudyAccessionFallback(t *testing.T) {
	convey.Convey("Given a study accession that misses UUID and id_study_lims but matches accession_number", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 13, "7013", "study-uuid-13", "Study 13", "EGAS00001005445")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), "EGAS00001005445")

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyAccession)
		convey.So(match.Canonical, convey.ShouldEqual, "7013")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.AccessionNumber, convey.ShouldEqual, "EGAS00001005445")
	})
}

func TestResolveStudyNameReturnsAmbiguousForTwoMatches(t *testing.T) {
	convey.Convey("Given a study name shared by exactly two studies", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 14, "6514", "study-uuid-14", "Some Title", "EGAS00001001414")
		seedStudyMirrorRow(t, cache.DB(), 15, "6515", "study-uuid-15", "Some Title", "EGAS00001001515")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), "Some Title")

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "6514")
		convey.So(err.Error(), convey.ShouldContainSubstring, "6515")
		convey.So(match, convey.ShouldResemble, Match{})
	})
}

func TestResolveStudyNameReturnsCanonicalLimsID(t *testing.T) {
	convey.Convey("Given a unique study name match", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 16, "6516", "study-uuid-16", "Some Title", "EGAS00001001616")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), "Some Title")

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyName)
		convey.So(match.Canonical, convey.ShouldEqual, "6516")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.Name, convey.ShouldEqual, "Some Title")
	})
}

func TestResolveStudyNameIsCaseSensitiveByDefault(t *testing.T) {
	convey.Convey("Given a differently cased study name without opts", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 17, "6517", "study-uuid-17", "Some Title", "EGAS00001001717")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), "some title")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(match, convey.ShouldResemble, Match{})
	})
}

func TestResolveStudyCaseInsensitiveNameOption(t *testing.T) {
	convey.Convey("Given a differently cased study name with the case-insensitive option", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorRow(t, cache.DB(), 18, "6518", "study-uuid-18", "Some Title", "EGAS00001001818")

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveStudy(context.Background(), "some title", WithCaseInsensitiveStudyName())

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyName)
		convey.So(match.Canonical, convey.ShouldEqual, "6518")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.Name, convey.ShouldEqual, "Some Title")
	})
}

func TestResolveStudyColdCacheSyncsBeforeLookup(t *testing.T) {
	convey.Convey("Given a cold cache and a study row inserted only during sync", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		syncEntered := make(chan struct{}, 1)
		releaseSync := make(chan struct{})
		resultCh := make(chan Match, 1)
		errCh := make(chan error, 1)

		client := &Client{
			cache:       cache,
			cacheReader: cacheReadDB(cache),
			syncSource:  cache.DB(),
			syncRunner: func(ctx context.Context, tx *sql.Tx, tables []string) error {
				if len(tables) != 1 || tables[0] != syncTableStudy {
					return errors.New("unexpected sync tables")
				}

				if upsertErr := upsertStudyMirror(ctx, tx, "sqlite", studySyncRow{
					Study: Study{
						IDStudyTmp:                19,
						IDLims:                    "SQSCP",
						IDStudyLims:               "6519",
						UUIDStudyLims:             "study-uuid-19",
						Name:                      "Study 19",
						AccessionNumber:           "EGAS00001001919",
						StudyTitle:                "Study title 19",
						FacultySponsor:            "Faculty sponsor 19",
						State:                     "active",
						Abstract:                  "abstract",
						Abbreviation:              "abbr",
						Description:               "description",
						DataReleaseStrategy:       "strategy",
						DataAccessGroup:           "group",
						HMDMCNumber:               "hmdmc",
						Programme:                 "programme",
						Created:                   "2026-05-06",
						ReferenceGenome:           "GRCh38",
						EthicallyApproved:         true,
						StudyType:                 "study-type",
						ContainsHumanDNA:          false,
						ContaminatedHumanDNA:      false,
						StudyVisibility:           "public",
						EGADACAccessionNumber:     "EGAD0001",
						EGAPolicyAccessionNumber:  "EGAP0001",
						DataReleaseTiming:         "immediate",
					},
					LastUpdated: time.Date(2026, time.May, 6, 16, 5, 0, 0, time.UTC),
				}); upsertErr != nil {
					return upsertErr
				}

				syncEntered <- struct{}{}
				<-releaseSync

				return nil
			},
		}

		go func() {
			match, err := client.ResolveStudy(context.Background(), "6519")
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
			convey.So(match.Kind, convey.ShouldEqual, KindStudyLimsID)
			convey.So(match.Canonical, convey.ShouldEqual, "6519")
			convey.So(match.Study, convey.ShouldNotBeNil)
			convey.So(match.Study.IDStudyLims, convey.ShouldEqual, "6519")
		case <-time.After(time.Second):
			convey.So("resolve did not complete", convey.ShouldEqual, "")
		}
	})
}

func seedStudyMirrorRow(t *testing.T, db *sql.DB, id int64, idStudyLims, uuidStudyLims, name, accession string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, abstract, abbreviation, description, data_release_strategy, data_access_group, hmdmc_number, programme, created, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, egadac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		idStudyLims,
		uuidStudyLims,
		name,
		accession,
		"Study title "+idStudyLims,
		"Faculty sponsor "+idStudyLims,
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
		formatSyncTime(time.Date(2026, time.May, 6, 16, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedStudyMirrorRow(): %v", err)
	}
}

func TestClassifyIdentifierUUIDDispatchesOnlyUUIDResolvers(t *testing.T) {
	convey.Convey("Given a UUID-shaped identifier that misses study and matches sample", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		const raw = "b7daafb8-c59f-11ee-8fba-024224dd57f4"
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE uuid_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleUUIDQuery)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).AddRow(sampleResolverRow(21, raw, "9521", "SANGER-UUID", "sanger-id-21", "supplier-21", "accession-21", "donor-21")...))

		match, err := client.ClassifyIdentifier(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSampleUUID)
		convey.So(match.Canonical, convey.ShouldEqual, "SANGER-UUID")
		convey.So(match.Sample, convey.ShouldNotBeNil)
		convey.So(match.Sample.UUIDSampleLims, convey.ShouldEqual, raw)
	})
}

func TestClassifyIdentifierIntegerDispatchesStudyThenSampleThenRun(t *testing.T) {
	convey.Convey("Given a pure-integer identifier whose run step is the first hit", t, func() {
		client, roMock, sourceMock, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		const raw = "12345"
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableStudy).
			WillReturnRows(sqlmock.NewRows([]string{"found"}).AddRow(1))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT 1 FROM sync_state WHERE table_name = ? LIMIT 1`)).
			WithArgs(syncTableSample).
			WillReturnRows(sqlmock.NewRows([]string{"found"}))
		sourceMock.ExpectQuery(regexp.QuoteMeta(sampleLimsIDQuery)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		sourceMock.ExpectQuery(regexp.QuoteMeta(`SELECT id_run FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`)).
			WithArgs(12345).
			WillReturnRows(sqlmock.NewRows([]string{"id_run"}).AddRow(12345))

		match, err := client.ClassifyIdentifier(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindRunID)
		convey.So(match.Canonical, convey.ShouldEqual, raw)
		convey.So(match.Run, convey.ShouldNotBeNil)
		convey.So(match.Run.IDRun, convey.ShouldEqual, 12345)
	})
}

func TestClassifyIdentifierTextStopsAtStudyAccessionHit(t *testing.T) {
	convey.Convey("Given a text identifier matching study accession_number", t, func() {
		client, roMock, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		const raw = "EGAS00001005445"
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' LIMIT 1`)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()).AddRow(studyResolverRow(31, "7031", "study-uuid-31", "Study 31", raw)...))

		match, err := client.ClassifyIdentifier(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyAccession)
		convey.So(match.Canonical, convey.ShouldEqual, "7031")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.AccessionNumber, convey.ShouldEqual, raw)
	})
}

func TestClassifyIdentifierRejectsLIMSProviderConstantWithoutSQL(t *testing.T) {
	convey.Convey("Given a LIMS provider constant", t, func() {
		client, _, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		match, err := client.ClassifyIdentifier(context.Background(), "SQSCP")

		convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		convey.So(match, convey.ShouldResemble, Match{})
	})
}

func TestClassifyIdentifierTextPrefersStudyOverDonorID(t *testing.T) {
	convey.Convey("Given a text identifier that matches both study.name and donor_id", t, func() {
		client, roMock, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()
		client.syncSource = nil

		const raw = "Shared Identifier"
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' LIMIT 1`)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE name = ? AND id_lims = 'SQSCP' ORDER BY id_study_tmp LIMIT 2`)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(studyResolverColumns()).AddRow(studyResolverRow(41, "7041", "study-uuid-41", raw, "EGAS00001004141")...))

		match, err := client.ClassifyIdentifier(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindStudyName)
		convey.So(match.Canonical, convey.ShouldEqual, "7041")
		convey.So(match.Study, convey.ShouldNotBeNil)
		convey.So(match.Study.Name, convey.ShouldEqual, raw)
	})
}

func TestClassifyIdentifierPropagatesUpstreamImpairedWithoutFallback(t *testing.T) {
	convey.Convey("Given a pure-integer identifier whose first dispatched query fails upstream", t, func() {
		client, roMock, _, cleanup := newMySQLResolverTestClient(t)
		defer cleanup()

		roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + studyMirrorSelectColumns + ` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`)).
			WithArgs("12345").
			WillReturnError(errors.New("lock wait timeout exceeded"))

		match, err := client.ClassifyIdentifier(context.Background(), "12345")

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeFalse)
		convey.So(match, convey.ShouldResemble, Match{})
	})
}

func newMySQLResolverTestClient(t *testing.T) (*Client, sqlmock.Sqlmock, sqlmock.Sqlmock, func()) {
	t.Helper()

	rwDB, rwMock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() rw: %v", err)
	}

	roDB, roMock, err := sqlmock.New()
	if err != nil {
		_ = rwDB.Close()
		t.Fatalf("sqlmock.New() ro: %v", err)
	}

	sourceDB, sourceMock, err := sqlmock.New()
	if err != nil {
		_ = roDB.Close()
		_ = rwDB.Close()
		t.Fatalf("sqlmock.New() source: %v", err)
	}

	client := &Client{
		cache:       &mysqlCache{rwDB: rwDB, roDB: roDB},
		cacheReader: roDB,
		syncSource:  sourceDB,
	}

	cleanup := func() {
		rwMock.ExpectClose()
		roMock.ExpectClose()
		sourceMock.ExpectClose()

		convey.So(rwDB.Close(), convey.ShouldBeNil)
		convey.So(roDB.Close(), convey.ShouldBeNil)
		convey.So(sourceDB.Close(), convey.ShouldBeNil)
		convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	}

	return client, roMock, sourceMock, cleanup
}

func studyResolverColumns() []string {
	return []string{
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
	}
}

func studyResolverRow(id int64, idStudyLims, uuidStudyLims, name, accession string) []driver.Value {
	return []driver.Value{
		id,
		"SQSCP",
		idStudyLims,
		uuidStudyLims,
		name,
		accession,
		"Study title " + idStudyLims,
		"Faculty sponsor " + idStudyLims,
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
	}
}
