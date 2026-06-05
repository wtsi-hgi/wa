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
	"database/sql/driver"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/smartystreets/goconvey/convey"
)

var (
	sampleUUIDQuery      = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE uuid_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
	sampleLimsIDQuery    = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP' LIMIT 1`
	sampleNameQuery      = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`
	sampleSangerIDQuery  = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`
	sampleSupplierQuery  = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`
	sampleAccessionQuery = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`
)

func TestResolveSampleUUIDMatch(t *testing.T) {
	convey.Convey("Given a UUID-shaped identifier with a cache match", t, func() {
		client, _, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		const raw = "b7daafb8-c59f-11ee-8fba-024224dd57f4"
		seedSampleMirrorRow(t, client.cache.DB(), 1, "7607STDY14643771", "supplier-1", "donor-1", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET uuid_sample_lims = ?, id_sample_lims = ? WHERE id_sample_tmp = ?`, raw, "9575305", 1)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))

		match, err := client.ResolveSample(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSampleUUID)
		convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643771")
		convey.So(match.Sample, convey.ShouldNotBeNil)
		convey.So(match.Sample.UUIDSampleLims, convey.ShouldEqual, raw)
	})
}

func TestResolveSampleLimsIDFallsBackAfterUUIDMiss(t *testing.T) {
	convey.Convey("Given a pure-integer sample identifier matching id_sample_lims in cache", t, func() {
		client, _, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		const raw = "9575305"
		seedSampleMirrorRow(t, client.cache.DB(), 2, "7607STDY14643771", "supplier-2", "donor-2", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET id_sample_lims = ?, uuid_sample_lims = ? WHERE id_sample_tmp = ?`, raw, "sample-uuid-2", 2)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))

		match, err := client.ResolveSample(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSampleLimsID)
		convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643771")
	})
}

func TestResolveSampleNameStepUsesSangerNameQuery(t *testing.T) {
	convey.Convey("Given a text identifier that only matches the name step in cache", t, func() {
		client, _, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		const raw = "7607STDY14643771"
		seedSampleMirrorRow(t, client.cache.DB(), 3, raw, "supplier-3", "donor-3", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET id_sample_lims = ?, uuid_sample_lims = ? WHERE id_sample_tmp = ?`, "9575306", "sample-uuid-3", 3)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))

		match, err := client.ResolveSample(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSangerSampleName)
		convey.So(match.Canonical, convey.ShouldEqual, raw)
	})
}

func TestResolveSampleWarmCacheMissReturnsNotFoundWithoutNeverSynced(t *testing.T) {
	convey.Convey("Given a fully-synced cache and a missing sample identifier", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveSample(context.Background(), "missing")

		convey.So(match, convey.ShouldResemble, Match{})
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
	})
}

func TestResolveSampleSupplierNameFallback(t *testing.T) {
	convey.Convey("Given a text identifier that only matches supplier_name in cache", t, func() {
		client, _, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		const raw = "Hek_R1"
		seedSampleMirrorRow(t, client.cache.DB(), 4, "7607STDY14643771", raw, "donor-4", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		_, err := client.cache.DB().Exec(`UPDATE sample_mirror SET id_sample_lims = ?, uuid_sample_lims = ?, sanger_sample_id = ? WHERE id_sample_tmp = ?`, "9575307", "sample-uuid-4", "sanger-id-4", 4)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))

		match, err := client.ResolveSample(context.Background(), raw)

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSupplierName)
		convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643771")
		convey.So(match.Sample, convey.ShouldNotBeNil)
		convey.So(match.Sample.SupplierName, convey.ShouldEqual, raw)
	})
}

func TestResolveSampleWarmCacheUsesDonorCacheOnly(t *testing.T) {
	convey.Convey("Given a warm cache whose donor lookup has the only match", t, func() {
		cachePath := filepath.Join(t.TempDir(), "resolver.sqlite")
		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorRow(t, cache.DB(), 31, "canonical-sample-31", "supplier-31", "donor-31", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		seedDonorSampleRow(t, cache.DB(), "donor-31", 31, "study-31")
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		match, err := client.ResolveSample(context.Background(), "donor-31")

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindDonorID)
		convey.So(match.Canonical, convey.ShouldEqual, "canonical-sample-31")
		sourceMock.ExpectClose()
		convey.So(sourceDB.Close(), convey.ShouldBeNil)
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestResolveSampleDonorStepReturnsAmbiguousForTwoMatches(t *testing.T) {
	convey.Convey("Given two samples sharing the same donor_id in a warm cache", t, func() {
		cachePath := filepath.Join(t.TempDir(), "resolver.sqlite")
		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorRow(t, cache.DB(), 51, "sample-51", "supplier-51", "DON1", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		seedSampleMirrorRow(t, cache.DB(), 52, "sample-52", "supplier-52", "DON1", time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))
		seedDonorSampleRow(t, cache.DB(), "DON1", 51, "study-51")
		seedDonorSampleRow(t, cache.DB(), "DON1", 52, "study-52")
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 2, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveSample(context.Background(), "DON1")

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "51")
		convey.So(err.Error(), convey.ShouldContainSubstring, "52")
		convey.So(match, convey.ShouldResemble, Match{})
	})
}

func TestResolveSampleCrossColumnCascadeReturnsAmbiguous(t *testing.T) {
	convey.Convey("Given different samples matched by different text steps for the same raw input", t, func() {
		cachePath := filepath.Join(t.TempDir(), "resolver.sqlite")
		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorRow(t, cache.DB(), 10, "X", "supplier-10", "donor-10", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		seedSampleMirrorRow(t, cache.DB(), 20, "sample-20", "supplier-20", "donor-20", time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))
		_, err = cache.DB().Exec(`UPDATE sample_mirror SET id_sample_lims = ?, uuid_sample_lims = ?, accession_number = ?, sanger_sample_id = ? WHERE id_sample_tmp = ?`, "1010", "sample-uuid-10", "accession-10", "sanger-id-10", 10)
		convey.So(err, convey.ShouldBeNil)
		_, err = cache.DB().Exec(`UPDATE sample_mirror SET id_sample_lims = ?, uuid_sample_lims = ?, accession_number = ?, sanger_sample_id = ? WHERE id_sample_tmp = ?`, "2020", "sample-uuid-20", "accession-20", "X", 20)
		convey.So(err, convey.ShouldBeNil)
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 2, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		match, err := client.ResolveSample(context.Background(), "X")

		convey.So(errors.Is(err, ErrAmbiguous), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "10")
		convey.So(err.Error(), convey.ShouldContainSubstring, "20")
		convey.So(err.Error(), convey.ShouldContainSubstring, "X")
		convey.So(match, convey.ShouldResemble, Match{})
	})
}

func TestResolveSampleWarmCacheUsesSampleMirrorForNameMatch(t *testing.T) {
	convey.Convey("Given a warm cache with a direct sample name match", t, func() {
		cachePath := filepath.Join(t.TempDir(), "resolver.sqlite")
		cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorRow(t, cache.DB(), 41, "7607STDY14643771", "supplier-41", "donor-41", time.Date(2026, time.May, 6, 14, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 1, 0, 0, time.UTC))

		sourceDB, sourceMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

		match, err := client.ResolveSample(context.Background(), "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSangerSampleName)
		convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643771")
		convey.So(match.Sample, convey.ShouldNotBeNil)
		convey.So(match.Sample.Name, convey.ShouldEqual, "7607STDY14643771")
		sourceMock.ExpectClose()
		convey.So(sourceDB.Close(), convey.ShouldBeNil)
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestResolveSampleRejectsLIMSProviderConstants(t *testing.T) {
	convey.Convey("Given a LIMS provider constant", t, func() {
		client, _, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		_, err := client.ResolveSample(context.Background(), "SQSCP")

		convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "SQSCP")
		convey.So(err.Error(), convey.ShouldContainSubstring, "LIMS provider constant")
	})
}

func TestResolveSampleWarmCacheMissReturnsNotFoundWithoutNegativeCache(t *testing.T) {
	convey.Convey("Given a warm cache and a miss across every direct step", t, func() {
		client, sourceMock, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		seedSyncState(t, client.cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 14, 2, 0, 0, time.UTC))

		const raw = "missing-id"

		_, firstErr := client.ResolveSample(context.Background(), raw)
		_, secondErr := client.ResolveSample(context.Background(), raw)

		convey.So(errors.Is(firstErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(secondErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestResolveSampleFixtureShapedSupplierNameFallbackWhenIndexesAvailable(t *testing.T) {
	convey.Convey("Given a fixture-shaped value matching supplier_name in a fully indexed cache", t, func() {
		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		const raw = "gallery-beta"
		roMock.ExpectQuery(regexp.QuoteMeta(sampleNameQuery)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(sampleSangerIDQuery)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
		roMock.ExpectQuery(regexp.QuoteMeta(sampleSupplierQuery)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()).AddRow(
				sampleResolverRow(72, "sample-uuid-72", "sample-lims-72", "7607STDY14643772", "sanger-72", raw, "accession-72", "donor-72")...,
			))
		roMock.ExpectQuery(regexp.QuoteMeta(sampleAccessionQuery)).
			WithArgs(raw).
			WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))

		client := &Client{cacheReader: roDB}

		match, resolveErr := client.ResolveSample(context.Background(), raw)

		convey.So(resolveErr, convey.ShouldBeNil)
		convey.So(match.Kind, convey.ShouldEqual, KindSupplierName)
		convey.So(match.Canonical, convey.ShouldEqual, "7607STDY14643772")

		roMock.ExpectClose()
		convey.So(roDB.Close(), convey.ShouldBeNil)
		convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestResolveSampleWarmCacheMissForMySQLCacheReturnsNotFoundWithoutNegativeCache(t *testing.T) {
	convey.Convey("Given a warm MySQL cache and a donor miss", t, func() {
		rwDB, rwMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)

		const raw = "missing-id"
		cacheMissRows := sqlmock.NewRows(sampleResolverColumns())

		expectWarmCacheMySQLMiss := func() {
			for _, query := range []string{
				sampleNameQuery,
				sampleSangerIDQuery,
				sampleSupplierQuery,
				sampleAccessionQuery,
			} {
				roMock.ExpectQuery(regexp.QuoteMeta(query)).
					WithArgs(raw).
					WillReturnRows(sqlmock.NewRows(sampleResolverColumns()))
			}
			roMock.ExpectQuery(regexp.QuoteMeta(`SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`)).
				WithArgs(syncTableSample).
				WillReturnRows(sqlmock.NewRows([]string{"high_water", "resume_cursor", "indexes_dropped"}).AddRow("2026-01-02T03:04:05Z", nil, 0))
			roMock.ExpectQuery(regexp.QuoteMeta(`SELECT ` + sampleMirrorSelectColumns + ` FROM donor_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = donor_samples.id_sample_tmp WHERE donor_samples.donor_id = ? ORDER BY sample_mirror.id_sample_tmp LIMIT 2`)).
				WithArgs(raw).
				WillReturnRows(cacheMissRows)
		}

		expectWarmCacheMySQLMiss()
		expectWarmCacheMySQLMiss()

		client := &Client{cache: &mysqlCache{rwDB: rwDB, roDB: roDB}, cacheReader: roDB}

		_, firstErr := client.ResolveSample(context.Background(), raw)
		_, secondErr := client.ResolveSample(context.Background(), raw)

		convey.So(errors.Is(firstErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(secondErr, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(firstErr, ErrUpstreamImpaired), convey.ShouldBeFalse)

		rwMock.ExpectClose()
		roMock.ExpectClose()
		convey.So(rwDB.Close(), convey.ShouldBeNil)
		convey.So(roDB.Close(), convey.ShouldBeNil)
		convey.So(rwMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
	})
}

func TestResolveSampleWrapsCacheErrors(t *testing.T) {
	convey.Convey("Given a cache database error on the first resolver step", t, func() {
		client, _, cleanup := newResolverSampleTestClient(t)
		defer cleanup()

		roDB, roMock, err := sqlmock.New()
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			roMock.ExpectClose()
			convey.So(roDB.Close(), convey.ShouldBeNil)
			convey.So(roMock.ExpectationsWereMet(), convey.ShouldBeNil)
		}()

		client.cacheReader = roDB
		roMock.ExpectQuery(regexp.QuoteMeta(sampleNameQuery)).
			WithArgs("broken-upstream").
			WillReturnError(errors.New("network down"))

		_, err = client.ResolveSample(context.Background(), "broken-upstream")

		convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeFalse)
	})
}

func newResolverSampleTestClient(t *testing.T) (*Client, sqlmock.Sqlmock, func()) {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "resolver.sqlite")
	cache, err := OpenCache(context.Background(), CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("OpenCache(): %v", err)
	}

	sourceDB, sourceMock, err := sqlmock.New()
	if err != nil {
		_ = cache.Close()
		t.Fatalf("sqlmock.New(): %v", err)
	}

	client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: sourceDB}

	cleanup := func() {
		convey.So(sourceMock.ExpectationsWereMet(), convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)
	}

	return client, sourceMock, cleanup
}

func sampleResolverColumns() []string {
	return []string{
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
	}
}

func sampleResolverRow(id int64, uuidSampleLims, idSampleLims, name, sangerSampleID, supplierName, accessionNumber, donorID string) []driver.Value {
	return []driver.Value{id, "SQSCP", idSampleLims, uuidSampleLims, name, sangerSampleID, supplierName, accessionNumber, donorID, 9606, "human", "description"}
}
