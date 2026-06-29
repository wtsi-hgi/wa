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
	"net/http"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const availabilityFetchAll = 1_000_000

// b3SampleIDs names the samples seeded for the B3 availability scenario so the
// tests refer to them by intent rather than by bare surrogate key.
const (
	b3IlluminaSampleA = int64(1) // with data: Illumina product + 4 study-scoped iRODS rows
	b3IlluminaSampleB = int64(2) // with data: Illumina product + 1 study-scoped iRODS row
	b3PacBioSample    = int64(3) // with data: PacBio product + 1 study-scoped iRODS row
	b3ONTSample       = int64(4) // without data: ONT (oseq_flowcell only, no iRODS)
	b3RegisteredOnly  = int64(5) // without data: library link only, no products
	b3SharedWithS2    = int64(6) // member of S2 only, with iRODS under S2 (scoping fixture)
)

// c1SampleIDs names the samples seeded for the C1 windowed-count scenario, each
// a with-data Illumina sample whose single study-scoped iRODS row carries a
// distinct created timestamp, so a half-open [since, until) window over the
// created column selects a known subset of distinct samples.
const (
	c1CreatedJun20 = int64(21) // with data: iRODS created 2026-06-20 (before the since boundary)
	c1CreatedJun25 = int64(22) // with data: iRODS created 2026-06-25 (in window)
	c1CreatedJun26 = int64(23) // with data: iRODS created 2026-06-26 (in window)
)

// c1 iRODS created timestamps for the windowed-count scenario.
var (
	c1Created20 = time.Date(2026, time.June, 20, 9, 0, 0, 0, time.UTC)
	c1Created25 = time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC)
	c1Created26 = time.Date(2026, time.June, 26, 9, 0, 0, 0, time.UTC)
)

// b1SampleIDs names the samples seeded for the B1 study-overview scenario.
const (
	b1WithDataA        = int64(11) // with data: Illumina, iRODS created 2026-06-01 (out of 7-day window), run 52553
	b1WithDataB        = int64(12) // with data: Illumina, iRODS created 2026-06-25 (in window), run 52553
	b1WithDataC        = int64(13) // with data: Illumina, iRODS created 2026-06-26 (newest, in window), run 52554
	b1SequencedNoData  = int64(14) // sequenced-no-data: iseq product-metrics in S1, no iRODS, run 52554
	b1RegisteredOnly   = int64(15) // registered: library link only, no products, no iRODS
	b1SharedWithS2Only = int64(16) // member of S2 only, iRODS under S2 (study-scoping fixture)
)

// b1NowFixed is the fixed "now" the B1 recency tests inject so the half-open
// [now-7d, now) window over the iRODS created column is deterministic.
var b1NowFixed = time.Date(2026, time.June, 28, 0, 0, 0, 0, time.UTC)

// b1 iRODS created timestamps, two inside the [2026-06-21, 2026-06-28) window
// (b1WithDataB, b1WithDataC) and one before it (b1WithDataA).
var (
	b1CreatedOld    = time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	b1CreatedRecent = time.Date(2026, time.June, 25, 10, 0, 0, 0, time.UTC)
	b1CreatedNewest = time.Date(2026, time.June, 26, 10, 0, 0, 0, time.UTC)
)

// b1OldestLastRun is the oldest feeding-table last_run in the B1 scenario, so the
// overview's cache_synced_at equals it.
var b1OldestLastRun = time.Date(2026, time.June, 27, 6, 0, 0, 0, time.UTC)

// d1RunOverviewRun is the Illumina NPG run the D1 run-overview scenario seeds: it
// carries 4 distinct samples across 2 studies and 6 iRODS data objects.
const d1RunOverviewRun = 52553

// d1SampleIDs names the samples seeded for the D1 run-overview scenario. Four are
// on the overview run (three under S1, one under S2, so the run spans 2 distinct
// studies); a fifth is on a different run, proving run scoping excludes it.
const (
	d1SampleA      = int64(71) // on run 52553, study S1, 3 iRODS data objects
	d1SampleB      = int64(72) // on run 52553, study S1, 2 iRODS data objects
	d1SampleC      = int64(73) // on run 52553, study S1, 1 iRODS data object
	d1SampleD      = int64(74) // on run 52553, study S2 (second distinct study)
	d1OtherRunOnly = int64(75) // on run 52554 only, with its own iRODS row (excluded)
)

// d1 iRODS created timestamps for the run-overview scenario: the earliest and the
// latest define the sequencing_date_range the overview reports.
var (
	d1CreatedEarliest = time.Date(2026, time.June, 20, 8, 0, 0, 0, time.UTC)
	d1CreatedMiddle   = time.Date(2026, time.June, 24, 12, 0, 0, 0, time.UTC)
	d1CreatedLatest   = time.Date(2026, time.June, 26, 18, 0, 0, 0, time.UTC)
)

// b1MidSequencingSample is the single S1 sample for the mid-sequencing scenario:
// it has iseq product-metrics in S1 but no iRODS row anywhere, so the study has
// linked samples yet zero iRODS data objects.
const b1MidSequencingSample = int64(21)

// d1NotYetInIRODSRun is the Illumina NPG run for the not-yet-in-iRODS scenario:
// it has iseq product-metrics rows but no iRODS rows joined to it.
const d1NotYetInIRODSRun = 52554

// d1NotYetInIRODSSample is the single sample on d1NotYetInIRODSRun; it has
// product-metrics on the run but no iRODS row, so the run resolves yet has zero
// iRODS data objects.
const d1NotYetInIRODSSample = int64(81)

// SamplesWithData and SamplesWithoutData must mirror SamplesForStudy's
// never-synced cascade: a never-synced cache yields both sentinels.
func TestSamplesWithDataNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withData, withErr := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)
		withoutData, withoutErr := client.SamplesWithoutData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when either list runs, then it returns both sentinels and an empty slice", func() {
			convey.So(errors.Is(withErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(withErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(withData, convey.ShouldBeEmpty)

			convey.So(errors.Is(withoutErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(withoutErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(withoutData, convey.ShouldBeEmpty)
		})
	})
}

// B2 acceptance test 3: a never-synced cache yields the same joined sentinel
// (ErrCacheNeverSynced + ErrNotFound) as CountSamplesForStudy.
func TestCountSamplesWithDataNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesWithData(context.Background(), "S1")

		convey.Convey("when CountSamplesWithData runs, then it returns Count{} and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

// C1 acceptance test 3: a malformed since makes the HTTP handler return 400
// bad_request before the queryer is reached (the fake queryer's count func
// panics if invoked, proving the query layer is not entered).
func TestCountSamplesWithDataSinceMalformedSinceReturns400(t *testing.T) {
	convey.Convey("Given the samples-with-data/count endpoint over a fake queryer that panics if its count func is reached", t, func() {
		queryer := &serverFakeQueryer{
			countSamplesWithDataFunc: func(_ context.Context, _ string) (Count, error) {
				panic("queryer reached despite malformed since")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/samples-with-data/count?since=not-a-date")

		convey.Convey("when GET ...?since=not-a-date is served, then status is 400 bad_request and the queryer is not reached", func() {
			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})

	convey.Convey("Given the same endpoint with a malformed until", t, func() {
		queryer := &serverFakeQueryer{
			countSamplesWithDataFunc: func(_ context.Context, _ string) (Count, error) {
				panic("queryer reached despite malformed until")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/samples-with-data/count?since=2026-06-21T00:00:00Z&until=nope")

		convey.Convey("when GET ...&until=nope is served, then status is 400 bad_request and the queryer is not reached", func() {
			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})
}

// C1 acceptance test 4: a never-synced cache yields the same joined sentinel
// (ErrCacheNeverSynced + ErrNotFound) as the all-time count.
func TestCountSamplesWithDataSinceNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesWithDataSince(context.Background(), "S1", "2026-06-21T00:00:00Z", "")

		convey.Convey("when CountSamplesWithDataSince runs, then it returns Count{} and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

// C2 (handler path): a malformed since on the samples-with-data LIST endpoint
// makes the HTTP handler return 400 bad_request before the queryer is reached.
// The fake queryer's SamplesWithData panics if invoked, proving the query layer
// is not entered. The handler path is new for this endpoint (the list gained
// since/until in 2.5), so it is covered explicitly here.
func TestSamplesWithDataSinceListMalformedSinceReturns400(t *testing.T) {
	convey.Convey("Given the samples-with-data list endpoint over a fake queryer whose list func panics if reached", t, func() {
		queryer := &serverFakeQueryer{}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/samples-with-data?since=not-a-date")

		convey.Convey("when GET ...?since=not-a-date is served, then status is 400 bad_request and the queryer is not reached", func() {
			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})

	convey.Convey("Given the same list endpoint with a malformed until", t, func() {
		queryer := &serverFakeQueryer{}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/samples-with-data?since=2026-06-21T00:00:00Z&until=nope")

		convey.Convey("when GET ...&until=nope is served, then status is 400 bad_request and the queryer is not reached", func() {
			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})
}

// A well-formed until WITHOUT a since must be rejected with 400 bad_request
// before the queryer is reached on BOTH the count and list endpoints: until is
// only meaningful as the upper bound of a [since, until) window, so an until-only
// request would otherwise silently return the all-time result (a superset of what
// the caller asked for). The fake queryer's funcs panic if reached, proving the
// 400 fires before the query layer.
func TestSamplesWithDataSinceUntilWithoutSinceReturns400(t *testing.T) {
	const validUntil = "2026-06-01T00:00:00Z"

	convey.Convey("Given the samples-with-data/count endpoint over a fake queryer that panics if its count func is reached", t, func() {
		queryer := &serverFakeQueryer{
			countSamplesWithDataFunc: func(_ context.Context, _ string) (Count, error) {
				panic("queryer reached despite until without since")
			},
		}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/samples-with-data/count?until="+validUntil)

		convey.Convey("when GET ...?until=<valid> with no since is served, then status is 400 bad_request and the queryer is not reached", func() {
			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})

	convey.Convey("Given the samples-with-data list endpoint over a fake queryer whose list func panics if reached", t, func() {
		queryer := &serverFakeQueryer{}

		response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/study/S1/samples-with-data?until="+validUntil)

		convey.Convey("when GET ...?until=<valid> with no since is served, then status is 400 bad_request and the queryer is not reached", func() {
			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})
}

// B1 acceptance test 4: a never-synced cache returns an error satisfying both
// ErrCacheNeverSynced and ErrNotFound.
func TestStudyOverviewNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview runs, then it returns both sentinels and a zero-value overview", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(overview, convey.ShouldResemble, StudyOverview{})
		})
	})
}

// D1 acceptance test 2: a never-synced cache returns an error satisfying both
// ErrCacheNeverSynced and ErrNotFound (the run space's never-synced cascade).
func TestRunOverviewNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		overview, err := client.RunOverview(context.Background(), "52553")

		convey.Convey("when RunOverview runs, then it returns both sentinels and a zero-value overview", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(overview, convey.ShouldResemble, RunOverview{})
		})
	})
}

// A direct Client caller that passes an until WITHOUT a since gets a clear error
// rather than the silently-wrong all-time result: until is only meaningful as the
// upper bound of a [since, until) window, so the typed methods reject it defensively
// (the same rule the HTTP handler enforces with a 400). The empty-both case keeps
// reusing the all-time path (covered by the *WithoutSinceMatchesAllTime tests), so
// the error must NOT fire there. No cache is needed: the guard precedes any query.
func TestSamplesWithDataSinceUntilWithoutSinceReturnsError(t *testing.T) {
	const validUntil = "2026-06-01T00:00:00Z"

	convey.Convey("Given a Client and a valid until with an empty since", t, func() {
		client := &Client{}

		convey.Convey("when CountSamplesWithDataSince is called with an empty since and that until, then it returns errUntilRequiresSince", func() {
			count, err := client.CountSamplesWithDataSince(context.Background(), "S1", "", validUntil)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, errUntilRequiresSince), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})

		convey.Convey("when SamplesWithDataSince is called with an empty since and that until, then it returns errUntilRequiresSince", func() {
			list, err := client.SamplesWithDataSince(context.Background(), "S1", "", validUntil, availabilityFetchAll, 0)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, errUntilRequiresSince), convey.ShouldBeTrue)
			convey.So(list, convey.ShouldBeNil)
		})
	})
}

// B3 acceptance test 1: the with-data and without-data lists partition S1's
// linked samples disjointly into 3 and 2, summing to samples_total (5).
func TestSamplesWithAndWithoutDataPartitionStudy(t *testing.T) {
	convey.Convey("Given study S1 with 3 samples with data and 2 without", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withData, withErr := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)
		withoutData, withoutErr := client.SamplesWithoutData(context.Background(), "S1", availabilityFetchAll, 0)
		total, totalErr := client.CountSamplesForStudy(context.Background(), "S1")

		convey.Convey("when both lists are taken, then they are disjoint, sized 3 and 2, and their union equals samples_total (5)", func() {
			convey.So(withErr, convey.ShouldBeNil)
			convey.So(withoutErr, convey.ShouldBeNil)
			convey.So(totalErr, convey.ShouldBeNil)

			convey.So(len(withData), convey.ShouldEqual, 3)
			convey.So(len(withoutData), convey.ShouldEqual, 2)
			convey.So(total.Count, convey.ShouldEqual, 5)
			convey.So(len(withData)+len(withoutData), convey.ShouldEqual, total.Count)

			withIDs := sampleWithDataIDs(withData)
			for id := range sampleWithDataIDs(withoutData) {
				_, overlap := withIDs[id]
				convey.So(overlap, convey.ShouldBeFalse)
			}

			// The shared S2-only sample must not appear in either S1 list.
			_, inWith := withIDs[b3SharedWithS2]
			_, inWithout := sampleWithDataIDs(withoutData)[b3SharedWithS2]
			convey.So(inWith, convey.ShouldBeFalse)
			convey.So(inWithout, convey.ShouldBeFalse)
		})
	})
}

// B3 acceptance test 2: the PacBio sample appears in SamplesWithData with
// "PacBio" among its platforms.
func TestSamplesWithDataPacBioSampleCarriesPacBioPlatform(t *testing.T) {
	convey.Convey("Given the PacBio sample in S1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withData, err := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when it appears in SamplesWithData, then its platforms contain \"PacBio\"", func() {
			convey.So(err, convey.ShouldBeNil)

			platforms, found := sampleWithDataPlatforms(withData, b3PacBioSample)
			convey.So(found, convey.ShouldBeTrue)
			convey.So(platforms, convey.ShouldContain, "PacBio")
		})
	})
}

// B3 acceptance test 3: the ONT sample linked to S1 (no iRODS) appears in
// SamplesWithoutData with platforms == ["ONT"], not folded into a bare "no data".
func TestSamplesWithoutDataONTSampleCarriesONTPlatform(t *testing.T) {
	convey.Convey("Given the ONT sample linked to S1 with no iRODS rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withoutData, err := client.SamplesWithoutData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when SamplesWithoutData is called, then it appears with platforms == [\"ONT\"]", func() {
			convey.So(err, convey.ShouldBeNil)

			platforms, found := sampleWithDataPlatforms(withoutData, b3ONTSample)
			convey.So(found, convey.ShouldBeTrue)
			convey.So(platforms, convey.ShouldResemble, []string{"ONT"})
		})
	})
}

// B3 acceptance test 4: a registered-only sample (library link, no products)
// appears in SamplesWithoutData with platforms == [].
func TestSamplesWithoutDataRegisteredSampleHasEmptyPlatforms(t *testing.T) {
	convey.Convey("Given a registered-only sample in S1 (library link, no products)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withoutData, err := client.SamplesWithoutData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when SamplesWithoutData is called, then it appears with an empty platforms list", func() {
			convey.So(err, convey.ShouldBeNil)

			platforms, found := sampleWithDataPlatforms(withoutData, b3RegisteredOnly)
			convey.So(found, convey.ShouldBeTrue)
			convey.So(platforms, convey.ShouldResemble, []string{})
		})
	})
}

// The samples-with-data partition is distinct-sample, not data-object: sample A
// has four study-scoped iRODS rows yet contributes exactly one with-data row.
func TestSamplesWithDataIsDistinctSamplePartition(t *testing.T) {
	convey.Convey("Given S1 where one with-data sample has four iRODS rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withData, err := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when SamplesWithData is called, then the multi-object sample appears exactly once", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(withData), convey.ShouldEqual, 3)

			matches := 0
			for _, row := range withData {
				if row.Sample.IDSampleTmp == b3IlluminaSampleA {
					matches++
				}
			}
			convey.So(matches, convey.ShouldEqual, 1)

			platforms, found := sampleWithDataPlatforms(withData, b3IlluminaSampleA)
			convey.So(found, convey.ShouldBeTrue)
			convey.So(platforms, convey.ShouldResemble, []string{"Illumina"})
		})
	})
}

// An unknown study on a synced cache yields ErrNotFound for both partitions.
func TestSamplesWithDataUnknownStudyReturnsNotFound(t *testing.T) {
	convey.Convey("Given a synced cache without the requested study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilitySyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		_, withErr := client.SamplesWithData(context.Background(), "NOPE", availabilityFetchAll, 0)
		_, withoutErr := client.SamplesWithoutData(context.Background(), "NOPE", availabilityFetchAll, 0)

		convey.Convey("when either list runs, then it returns ErrNotFound", func() {
			convey.So(errors.Is(withErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(withErr, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(errors.Is(withoutErr, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(withoutErr, ErrCacheNeverSynced), convey.ShouldBeFalse)
		})
	})
}

// A synced study with no linked samples yields empty lists and no error.
func TestSamplesWithDataSyncedEmptyStudyReturnsEmpty(t *testing.T) {
	convey.Convey("Given a synced cache with a known but empty study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 200, "S1")
		seedB3AvailabilitySyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		withData, withErr := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)
		withoutData, withoutErr := client.SamplesWithoutData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when either list runs, then it returns an empty slice and no error", func() {
			convey.So(withErr, convey.ShouldBeNil)
			convey.So(withData, convey.ShouldBeEmpty)
			convey.So(withoutErr, convey.ShouldBeNil)
			convey.So(withoutData, convey.ShouldBeEmpty)
		})
	})
}

// B2 acceptance test 1: CountSamplesWithData counts distinct samples, not iRODS
// data objects. S1 has 3 samples with study-scoped iRODS rows; one of them
// (sample A) carries 4 rows, yet the count is 3.
func TestCountSamplesWithDataCountsDistinctSamplesNotDataObjects(t *testing.T) {
	convey.Convey("Given S1 with 3 samples-with-data, one carrying 4 iRODS rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesWithData(context.Background(), "S1")

		convey.Convey("when CountSamplesWithData is called, then it returns Count{3} (distinct samples)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 3})
		})
	})
}

// B2 acceptance test 2 (also the deferred B3.6 cross-check): the bare count
// equals the length of the SamplesWithData(all) list for the same study.
func TestCountSamplesWithDataEqualsListLength(t *testing.T) {
	convey.Convey("Given S1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, countErr := client.CountSamplesWithData(context.Background(), "S1")
		withData, listErr := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when both are taken, then the count equals the list length", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(withData))
		})
	})
}

// An unknown study on a synced cache yields ErrNotFound (not the never-synced
// sentinel), matching CountSamplesForStudy's cascade.
func TestCountSamplesWithDataUnknownStudyReturnsNotFound(t *testing.T) {
	convey.Convey("Given a synced cache without the requested study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilitySyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesWithData(context.Background(), "NOPE")

		convey.Convey("when CountSamplesWithData runs, then it returns ErrNotFound", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

// A synced study with no linked samples yields Count{0} and no error, matching
// CountSamplesForStudy.
func TestCountSamplesWithDataSyncedEmptyStudyReturnsZeroNoError(t *testing.T) {
	convey.Convey("Given a synced cache with a known but empty study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 200, "S1")
		seedB3AvailabilitySyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesWithData(context.Background(), "S1")

		convey.Convey("when CountSamplesWithData runs, then it returns Count{0} and no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
		})
	})
}

// C1 acceptance test 1: with iRODS created at 2026-06-20, 2026-06-25 and
// 2026-06-26 (three distinct samples), since=2026-06-21 selects the two later
// samples, so CountSamplesWithDataSince returns Count{2}.
func TestCountSamplesWithDataSinceCountsWindowBoundary(t *testing.T) {
	convey.Convey("Given S1 with iRODS created on 2026-06-20, 2026-06-25 and 2026-06-26", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedC1WindowedScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesWithDataSince(context.Background(), "S1", "2026-06-21T00:00:00Z", "")

		convey.Convey("when called with since=2026-06-21T00:00:00Z, then it returns Count{2}", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 2})
		})
	})
}

// C1 acceptance test 2: with a row whose created == since and another whose
// created == until, the since row is included and the until row excluded (the
// half-open [since, until) rule).
func TestCountSamplesWithDataSinceHalfOpenBoundary(t *testing.T) {
	convey.Convey("Given S1 with one iRODS row created == since and another created == until", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedC1WindowedScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		// since is the c1Created25 row's exact timestamp (included); until is the
		// c1Created26 row's exact timestamp (excluded). The c1Created20 row is
		// before since (excluded), so only the since row remains.
		since := c1Created25.Format(utcRFC3339Layout)
		until := c1Created26.Format(utcRFC3339Layout)
		count, err := client.CountSamplesWithDataSince(context.Background(), "S1", since, until)

		convey.Convey("when called with that since and until, then only the since row is counted (since included, until excluded)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})
	})
}

// CountSamplesWithDataSince with an empty since reuses the all-time path, so it
// equals CountSamplesWithData for the same study (the two never diverge).
func TestCountSamplesWithDataSinceWithoutSinceMatchesAllTime(t *testing.T) {
	convey.Convey("Given the B3 availability scenario for S1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		windowed, windowedErr := client.CountSamplesWithDataSince(context.Background(), "S1", "", "")
		allTime, allTimeErr := client.CountSamplesWithData(context.Background(), "S1")

		convey.Convey("when called with an empty since, then it equals the all-time CountSamplesWithData", func() {
			convey.So(windowedErr, convey.ShouldBeNil)
			convey.So(allTimeErr, convey.ShouldBeNil)
			convey.So(windowed, convey.ShouldResemble, allTime)
			convey.So(windowed, convey.ShouldResemble, Count{Count: 3})
		})
	})
}

// CountSamplesWithDataSince round-trips through the HTTP server and RemoteClient
// to the same Count as the local Client (the windowed variant of the shared
// /study/:id/samples-with-data/count endpoint), so local and remote agree.
func TestCountSamplesWithDataSinceRemoteRoundTrip(t *testing.T) {
	convey.Convey("Given the C1 windowed scenario served over HTTP", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedC1WindowedScenario(t, cache.DB())
		local := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		localCount, localErr := local.CountSamplesWithDataSince(context.Background(), "S1", "2026-06-21T00:00:00Z", "")
		remoteCount, remoteErr := remote.CountSamplesWithDataSince(context.Background(), "S1", "2026-06-21T00:00:00Z", "")

		convey.Convey("when called locally and remotely with since=2026-06-21, then both return Count{2}", func() {
			convey.So(localErr, convey.ShouldBeNil)
			convey.So(remoteErr, convey.ShouldBeNil)
			convey.So(localCount, convey.ShouldResemble, Count{Count: 2})
			convey.So(remoteCount, convey.ShouldResemble, localCount)
		})
	})
}

// C2 acceptance test 1: with S1 as in C1.1 (iRODS created on 2026-06-20,
// 2026-06-25 and 2026-06-26), SamplesWithDataSince with since=2026-06-21 returns
// the two in-window samples, and the list length equals
// CountSamplesWithDataSince for the same window (the list<->count cross-check).
func TestSamplesWithDataSinceListsWindowSamplesAndMatchesCount(t *testing.T) {
	convey.Convey("Given S1 with iRODS created on 2026-06-20, 2026-06-25 and 2026-06-26", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedC1WindowedScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		since := "2026-06-21T00:00:00Z"
		list, listErr := client.SamplesWithDataSince(context.Background(), "S1", since, "", availabilityFetchAll, 0)
		count, countErr := client.CountSamplesWithDataSince(context.Background(), "S1", since, "")

		convey.Convey("when SamplesWithDataSince is called with since=2026-06-21, then it lists the 2 in-window samples and its length equals the windowed count", func() {
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(len(list), convey.ShouldEqual, 2)
			convey.So(len(list), convey.ShouldEqual, count.Count)

			ids := sampleWithDataIDs(list)
			_, hasJun25 := ids[c1CreatedJun25]
			_, hasJun26 := ids[c1CreatedJun26]
			_, hasJun20 := ids[c1CreatedJun20]
			convey.So(hasJun25, convey.ShouldBeTrue)
			convey.So(hasJun26, convey.ShouldBeTrue)
			convey.So(hasJun20, convey.ShouldBeFalse)
		})
	})
}

// C2 acceptance test 2: with a row whose created == since and another whose
// created == until, the listed in-window samples obey the half-open rule (the
// since row is included, the until row excluded) -- the list mirror of C1.2.
func TestSamplesWithDataSinceHalfOpenBoundaryMembership(t *testing.T) {
	convey.Convey("Given S1 with one iRODS row created == since and another created == until", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedC1WindowedScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		// since is the c1Created25 row's exact timestamp (included); until is the
		// c1Created26 row's exact timestamp (excluded); the c1Created20 row is
		// before since (excluded), so only the since (Jun25) sample is in window.
		since := c1Created25.Format(utcRFC3339Layout)
		until := c1Created26.Format(utcRFC3339Layout)
		list, listErr := client.SamplesWithDataSince(context.Background(), "S1", since, until, availabilityFetchAll, 0)

		convey.Convey("when listed with that since and until, then only the since-boundary sample is in the window (since included, until excluded)", func() {
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(len(list), convey.ShouldEqual, 1)

			ids := sampleWithDataIDs(list)
			_, hasSince := ids[c1CreatedJun25]
			_, hasUntil := ids[c1CreatedJun26]
			_, hasBefore := ids[c1CreatedJun20]
			convey.So(hasSince, convey.ShouldBeTrue)
			convey.So(hasUntil, convey.ShouldBeFalse)
			convey.So(hasBefore, convey.ShouldBeFalse)
		})
	})
}

// SamplesWithDataSince with an empty since reuses the all-time SamplesWithData
// path, so it equals SamplesWithData for the same study (the windowed and
// all-time list paths never diverge).
func TestSamplesWithDataSinceWithoutSinceMatchesAllTime(t *testing.T) {
	convey.Convey("Given the B3 availability scenario for S1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3AvailabilityScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		windowed, windowedErr := client.SamplesWithDataSince(context.Background(), "S1", "", "", availabilityFetchAll, 0)
		allTime, allTimeErr := client.SamplesWithData(context.Background(), "S1", availabilityFetchAll, 0)

		convey.Convey("when called with an empty since, then it equals the all-time SamplesWithData", func() {
			convey.So(windowedErr, convey.ShouldBeNil)
			convey.So(allTimeErr, convey.ShouldBeNil)
			convey.So(windowed, convey.ShouldResemble, allTime)
			convey.So(len(windowed), convey.ShouldEqual, 3)
		})
	})
}

// seedB3AvailabilityScenario builds the shared B3/B1 availability fixture used by
// the samples-with/without-data tests (and reused by items 2.2-2.5). Study "S1"
// has five linked samples: two Illumina and one PacBio with study-scoped iRODS
// rows (the "with data" partition), one ONT sample present only in
// oseq_flowcell_mirror, and one registered-only sample with a library link and
// no products (the "without data" partition). A sixth sample belongs to study
// "S2" only and has its iRODS row scoped to S2, so S1 queries must never see it.
func seedB3AvailabilityScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	created := time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC)

	seedHierarchyStudy(t, db, 101, "S1")
	seedHierarchyStudy(t, db, 102, "S2")

	for _, id := range []int64{b3IlluminaSampleA, b3IlluminaSampleB, b3PacBioSample, b3ONTSample, b3RegisteredOnly} {
		seedHierarchySample(t, db, id, "S1", "sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, "S1")
	}

	// Two Illumina samples with study-scoped iRODS rows; sample A carries four
	// rows so the with-data partition stays distinct-sample, not data-object.
	seedIseqProductMetricsMirrorRow(t, db, 1001, b3IlluminaSampleA, 52553, 1, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "1001", "/seq/52553", "52553_1#1.cram", b3IlluminaSampleA, "S1", created, "illumina")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "1002", "/seq/52553", "52553_1#2.cram", b3IlluminaSampleA, "S1", created, "illumina")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "1003", "/seq/52553", "52553_1#3.cram", b3IlluminaSampleA, "S1", created, "illumina")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "1004", "/seq/52553", "52553_1#4.cram", b3IlluminaSampleA, "S1", created, "illumina")

	seedIseqProductMetricsMirrorRow(t, db, 2001, b3IlluminaSampleB, 52553, 2, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "2001", "/seq/52553", "52553_2#1.cram", b3IlluminaSampleB, "S1", created, "illumina")

	// One PacBio sample with a study-scoped iRODS row; its platform must derive
	// from pac_bio_product_metrics membership, not from the iRODS platform value.
	seedPacBioProductMetricsMirrorRow(t, db, "3001", b3PacBioSample, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "3001", "/seq/pacbio", "m3001.bc1001.bam", b3PacBioSample, "S1", created, "pacbio")

	// One ONT sample: identity/study only, no products and no iRODS.
	seedOseqFlowcellMirrorRow(t, db, 4001, b3ONTSample, "S1")

	// One registered-only sample: a library link with no products and no iRODS.
	// (Seeded in the loop above via seedLibrarySample; no further rows.)

	// A sixth sample belongs to S2 only, with its iRODS row scoped to S2, so S1
	// queries must exclude it (study scoping).
	seedHierarchySample(t, db, b3SharedWithS2, "S2", "sample-"+formatInt(b3SharedWithS2))
	seedLibrarySample(t, db, "Standard", b3SharedWithS2, "S2")
	seedIseqProductMetricsMirrorRow(t, db, 6001, b3SharedWithS2, 52553, 3, 1, "S2")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "6001", "/seq/52553", "52553_3#1.cram", b3SharedWithS2, "S2", created, "illumina")

	seedB3AvailabilitySyncState(t, db)
}

func sampleWithDataPlatforms(rows []SampleWithData, idSampleTmp int64) ([]string, bool) {
	for _, row := range rows {
		if row.Sample.IDSampleTmp == idSampleTmp {
			return row.Platforms, true
		}
	}

	return nil, false
}

// SamplesWithDataSince round-trips through the HTTP server and RemoteClient to
// the same list as the local Client (the windowed variant of the shared
// /study/:id/samples-with-data endpoint), so local and remote agree.
func TestSamplesWithDataSinceRemoteRoundTrip(t *testing.T) {
	convey.Convey("Given the C1 windowed scenario served over HTTP", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedC1WindowedScenario(t, cache.DB())
		local := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		since := "2026-06-21T00:00:00Z"
		localList, localErr := local.SamplesWithDataSince(context.Background(), "S1", since, "", availabilityFetchAll, 0)
		remoteList, remoteErr := remote.SamplesWithDataSince(context.Background(), "S1", since, "", availabilityFetchAll, 0)

		convey.Convey("when called locally and remotely with since=2026-06-21, then both list the 2 in-window samples and agree", func() {
			convey.So(localErr, convey.ShouldBeNil)
			convey.So(remoteErr, convey.ShouldBeNil)
			convey.So(len(localList), convey.ShouldEqual, 2)
			convey.So(remoteList, convey.ShouldResemble, localList)
		})
	})
}

// seedC1WindowedScenario builds the C1 windowed samples-with-data fixture: study
// "S1" with three with-data Illumina samples whose study-scoped iRODS rows are
// created on 2026-06-20, 2026-06-25 and 2026-06-26 (three distinct samples,
// distinct dates). It reuses the B3 seeder helpers and the shared availability
// sync-state stamp so the windowed count returns data rather than the
// never-synced sentinel.
func seedC1WindowedScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 131, "S1")

	seedC1WithDataSample(t, db, c1CreatedJun20, 1, c1Created20)
	seedC1WithDataSample(t, db, c1CreatedJun25, 2, c1Created25)
	seedC1WithDataSample(t, db, c1CreatedJun26, 3, c1Created26)

	seedB3AvailabilitySyncState(t, db)
}

// seedC1WithDataSample links one Illumina with-data sample to S1 with a single
// study-scoped iRODS row created at the given time.
func seedC1WithDataSample(t *testing.T, db *sql.DB, idSampleTmp int64, productSeq int, created time.Time) {
	t.Helper()

	seedHierarchySample(t, db, idSampleTmp, "S1", "c1-sample-"+formatInt(idSampleTmp))
	seedLibrarySample(t, db, "Standard", idSampleTmp, "S1")

	product := formatInt(int64(5100 + productSeq))
	seedIseqProductMetricsMirrorRow(t, db, int64(5100+productSeq), idSampleTmp, 52560, productSeq, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, product, "/seq/52560", "52560_"+product+".cram", idSampleTmp, "S1", created, "illumina")
}

// D1 acceptance test 1: a run with 4 distinct samples across 2 studies and 6 iRODS
// data objects reports those exact figures and a sequencing_date_range spanning
// the min/max iRODS created.
func TestRunOverviewReportsRunFigures(t *testing.T) {
	convey.Convey("Given run 52553 with 4 distinct samples across 2 studies and 6 iRODS data objects", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedD1RunOverviewScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		overview, err := client.RunOverview(context.Background(), "52553")

		convey.Convey("when RunOverview is called, then samples=4, studies=2, data_objects=6 and the date range spans the min/max created", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.IDRun, convey.ShouldEqual, d1RunOverviewRun)
			convey.So(overview.Samples, convey.ShouldEqual, 4)
			convey.So(overview.Studies, convey.ShouldEqual, 2)
			convey.So(overview.DataObjects, convey.ShouldEqual, 6)

			convey.So(overview.SequencingDateRange, convey.ShouldNotBeNil)
			convey.So(overview.SequencingDateRange.Earliest, convey.ShouldEqual, "2026-06-20T08:00:00Z")
			convey.So(overview.SequencingDateRange.Latest, convey.ShouldEqual, "2026-06-26T18:00:00Z")
			convey.So(overview.CacheSyncedAt, convey.ShouldNotBeEmpty)
		})
	})
}

// D1 acceptance test 3: an :id that is not a valid Illumina run (a numeric run id
// absent from the synced cache) returns ErrNotFound, the existing Run/ResolveRun
// not-found behaviour (and NOT the never-synced sentinel on a synced cache).
func TestRunOverviewInvalidRunReturnsNotFound(t *testing.T) {
	convey.Convey("Given a synced cache whose iseq product-metrics has no row for the requested run", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedD1RunOverviewScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		overview, err := client.RunOverview(context.Background(), "99999")

		convey.Convey("when RunOverview runs for the unknown run, then it returns ErrNotFound and a zero-value overview", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(overview, convey.ShouldResemble, RunOverview{})
		})
	})
}

// seedD1RunOverviewScenario builds the D1 run-overview fixture. Run 52553 carries
// four distinct samples across two studies (three under S1, one under S2) and six
// study/run-scoped iRODS data objects whose created timestamps span 2026-06-20 to
// 2026-06-26. Each iRODS row joins to its run via the shared id_iseq_product, the
// same run->iRODS link the overview uses (iseq_product_metrics_mirror.id_run ->
// seq_product_irods_locations_mirror.id_iseq_product). A fifth sample on a
// different run (52554), with its own iRODS row, proves run scoping excludes other
// runs. It reuses the shared availability seeder helpers and sync-state stamp so
// the run resolves rather than returning the never-synced sentinel.
func seedD1RunOverviewScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 171, "S1")
	seedHierarchyStudy(t, db, 172, "S2")

	for _, id := range []int64{d1SampleA, d1SampleB, d1SampleC} {
		seedHierarchySample(t, db, id, "S1", "d1-sample-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, "S1")
	}
	seedHierarchySample(t, db, d1SampleD, "S2", "d1-sample-"+formatInt(d1SampleD))
	seedLibrarySample(t, db, "Standard", d1SampleD, "S2")

	// Sample A: 3 iRODS data objects on run 52553 (earliest, middle, latest), so
	// the run's created range spans 2026-06-20 to 2026-06-26.
	seedIseqProductMetricsMirrorRow(t, db, 7101, d1SampleA, d1RunOverviewRun, 1, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7101", "/seq/52553", "52553_1#1.cram", d1SampleA, "S1", d1CreatedEarliest, "illumina")
	seedIseqProductMetricsMirrorRow(t, db, 7102, d1SampleA, d1RunOverviewRun, 1, 2, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7102", "/seq/52553", "52553_1#2.cram", d1SampleA, "S1", d1CreatedMiddle, "illumina")
	seedIseqProductMetricsMirrorRow(t, db, 7103, d1SampleA, d1RunOverviewRun, 1, 3, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7103", "/seq/52553", "52553_1#3.cram", d1SampleA, "S1", d1CreatedLatest, "illumina")

	// Sample B: 2 iRODS data objects on run 52553.
	seedIseqProductMetricsMirrorRow(t, db, 7201, d1SampleB, d1RunOverviewRun, 2, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7201", "/seq/52553", "52553_2#1.cram", d1SampleB, "S1", d1CreatedMiddle, "illumina")
	seedIseqProductMetricsMirrorRow(t, db, 7202, d1SampleB, d1RunOverviewRun, 2, 2, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7202", "/seq/52553", "52553_2#2.cram", d1SampleB, "S1", d1CreatedMiddle, "illumina")

	// Sample C: 1 iRODS data object on run 52553 (6 data objects total: 3+2+1).
	seedIseqProductMetricsMirrorRow(t, db, 7301, d1SampleC, d1RunOverviewRun, 3, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7301", "/seq/52553", "52553_3#1.cram", d1SampleC, "S1", d1CreatedMiddle, "illumina")

	// Sample D: on run 52553 under the second study S2, with no iRODS row, so it
	// adds a distinct sample and a distinct study without adding a data object.
	seedIseqProductMetricsMirrorRow(t, db, 7401, d1SampleD, d1RunOverviewRun, 4, 1, "S2")

	// A fifth sample on a different run (52554) with its own iRODS row: run scoping
	// must exclude it from run 52553's samples/studies/data objects.
	seedHierarchySample(t, db, d1OtherRunOnly, "S1", "d1-sample-"+formatInt(d1OtherRunOnly))
	seedLibrarySample(t, db, "Standard", d1OtherRunOnly, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 7501, d1OtherRunOnly, 52554, 1, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "7501", "/seq/52554", "52554_1#1.cram", d1OtherRunOnly, "S1", d1CreatedLatest, "illumina")

	seedB3AvailabilitySyncState(t, db)
}

// RunOverview on a run that has been sequenced (it has iseq product-metrics) but
// whose data has not yet reached iRODS must report data_objects=0 with no
// sequencing date range, not error.
func TestRunOverviewSequencedNoIRODSHasNoDateRange(t *testing.T) {
	convey.Convey("Given a synced run 52554 with product-metrics but no iRODS rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 173, "S1")
		seedHierarchySample(t, cache.DB(), d1NotYetInIRODSSample, "S1", "d1-sample-"+formatInt(d1NotYetInIRODSSample))
		seedLibrarySample(t, cache.DB(), "Standard", d1NotYetInIRODSSample, "S1")
		seedIseqProductMetricsMirrorRow(t, cache.DB(), 8101, d1NotYetInIRODSSample, d1NotYetInIRODSRun, 1, 1, "S1")
		seedB3AvailabilitySyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		overview, err := client.RunOverview(context.Background(), "52554")

		convey.Convey("when RunOverview is called, then it reports the run's one sample with no iRODS and no date range", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.IDRun, convey.ShouldEqual, d1NotYetInIRODSRun)
			convey.So(overview.Samples, convey.ShouldEqual, 1)
			convey.So(overview.DataObjects, convey.ShouldEqual, 0)
			convey.So(overview.SequencingDateRange, convey.ShouldBeNil)
		})
	})
}

// seedB3AvailabilitySyncState marks every feeding table synced, so the B3
// availability queries return data rather than the never-synced sentinel.
func seedB3AvailabilitySyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	seedSyncState(t, db, syncTableStudy, base)
	seedSyncState(t, db, syncTableSample, base.Add(1*time.Minute))
	seedSyncState(t, db, syncTableIseqFlowcell, base.Add(2*time.Minute))
	seedSyncState(t, db, syncTableIseqProductMetrics, base.Add(3*time.Minute))
	seedSyncState(t, db, syncTableSeqProductIRODSLocations, base.Add(4*time.Minute))
	seedSyncState(t, db, syncTablePacBioProductMetrics, base.Add(5*time.Minute))
	seedSyncState(t, db, syncTableOseqFlowcell, base.Add(6*time.Minute))
}

func sampleWithDataIDs(rows []SampleWithData) map[int64]struct{} {
	ids := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		ids[row.Sample.IDSampleTmp] = struct{}{}
	}

	return ids
}

// B1 acceptance test 1: the full figures for a five-sample study, including the
// distinct-sample partition and the sorted library types.
func TestStudyOverviewReportsFullFigures(t *testing.T) {
	convey.Convey("Given study S1 with 3 with-data, 1 sequenced-no-data and 1 registered sample", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB1OverviewScenario(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview is called, then every aggregate figure matches the scenario", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 5)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 3)
			convey.So(overview.SamplesWithoutData, convey.ShouldEqual, 2)
			convey.So(overview.SamplesSequencedNoData, convey.ShouldEqual, 1)
			convey.So(overview.DataObjects, convey.ShouldEqual, 7)
			convey.So(overview.Runs, convey.ShouldEqual, 2)
			convey.So(overview.Libraries, convey.ShouldEqual, 2)
			convey.So(overview.LibraryTypes, convey.ShouldResemble, []string{"Chromium", "Standard"})
		})
	})
}

// F1 acceptance test 1: a study with name / accession_number / faculty_sponsor /
// data_access_group set and 5 linked samples populates those four metadata fields
// (read from study_mirror) ALONGSIDE the existing counts. seedB1OverviewScenario
// seeds S1 via seedHierarchyStudy(111, "S1"), so the four fields carry that row's
// deterministic values.
func TestStudyOverviewPopulatesStudyMetadataAlongsideCounts(t *testing.T) {
	convey.Convey("Given study S1 with study metadata set and 5 linked samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB1OverviewScenario(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview is called, then the four metadata fields are populated alongside the counts", func() {
			convey.So(err, convey.ShouldBeNil)

			// The four metadata fields come from study_mirror (seedHierarchyStudy 111).
			convey.So(overview.Name, convey.ShouldEqual, "Study S1")
			convey.So(overview.AccessionNumber, convey.ShouldEqual, "EGAS0000S1")
			convey.So(overview.FacultySponsor, convey.ShouldEqual, "Faculty sponsor 111")
			convey.So(overview.DataAccessGroup, convey.ShouldEqual, "group")

			// The existing counts are still correct (the four fields are additive).
			convey.So(overview.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 5)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 3)
			convey.So(overview.SamplesWithoutData, convey.ShouldEqual, 2)
			convey.So(overview.SamplesSequencedNoData, convey.ShouldEqual, 1)
			convey.So(overview.DataObjects, convey.ShouldEqual, 7)
		})
	})
}

// B1 acceptance test 2: newest_data_added is the latest iRODS created, and
// added_last_7_days counts only the distinct samples added in [now-7d, now).
func TestStudyOverviewRecencyUsesHalfOpenWindow(t *testing.T) {
	convey.Convey("Given S1 iRODS created on 2026-06-01, 2026-06-25, 2026-06-26 and now=2026-06-28", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB1OverviewScenario(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview is called, then newest_data_added is the 2026-06-26 row and added_last_7_days counts the two in-window samples", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.NewestDataAdded, convey.ShouldEqual, "2026-06-26T10:00:00Z")
			convey.So(overview.AddedLast7Days, convey.ShouldEqual, 2)

			convey.So(overview.SequencingDateRange, convey.ShouldNotBeNil)
			convey.So(overview.SequencingDateRange.Earliest, convey.ShouldEqual, "2026-06-01T10:00:00Z")
			convey.So(overview.SequencingDateRange.Latest, convey.ShouldEqual, "2026-06-26T10:00:00Z")
		})
	})
}

// B1 acceptance test 3: a sample of S1 whose iRODS data is only under S2 is not
// counted in S1's samples_with_data (study scoping).
func TestStudyOverviewStudyScopingExcludesCrossStudyData(t *testing.T) {
	convey.Convey("Given a sample shared with S2 whose iRODS data is only under S2", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB1OverviewScenario(t, cache.DB())
		// Link the S2-only sample into S1 as well, but give it no S1-scoped iRODS
		// rows: its only iRODS data stays under S2.
		seedLibrarySample(t, cache.DB(), "Standard", b1SharedWithS2Only, "S1")
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview is called, then that sample is linked but not counted with data", func() {
			convey.So(err, convey.ShouldBeNil)
			// Now six samples are linked to S1, but only the three with S1-scoped
			// iRODS rows count as with-data.
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 6)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 3)
			// The cross-study sample has iseq product-metrics only under S2, so it
			// is registered (not sequenced-no-data) within S1.
			convey.So(overview.SamplesSequencedNoData, convey.ShouldEqual, 1)
		})
	})
}

// seedB1OverviewScenario builds the B1 study-overview fixture: study "S1" with
// five linked samples partitioning into 3 with-data (Illumina), 1
// sequenced-no-data (product-metrics in S1 but no iRODS) and 1 registered-only;
// 7 study-scoped iRODS data objects across 2 runs (52553, 52554); library types
// {Standard, Chromium}; and iRODS created times on 2026-06-01, 2026-06-25 and
// 2026-06-26. A sixth sample belongs to study "S2" only with its iRODS row scoped
// to S2, so S1's overview must exclude it (study scoping). It reuses the same
// seeder helpers as the B3 scenario rather than building a parallel fixture, but
// uses explicit per-table last_run timestamps so cache_synced_at is the oldest.
func seedB1OverviewScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 111, "S1")
	seedHierarchyStudy(t, db, 112, "S2")

	for _, id := range []int64{b1WithDataA, b1WithDataB, b1WithDataC, b1SequencedNoData, b1RegisteredOnly} {
		seedHierarchySample(t, db, id, "S1", "ov-sample-"+formatInt(id))
	}

	// Library types present across S1: one Chromium, the rest Standard, so the
	// sorted distinct library types are exactly ["Chromium", "Standard"].
	seedLibrarySample(t, db, "Chromium", b1WithDataA, "S1")
	seedLibrarySample(t, db, "Standard", b1WithDataB, "S1")
	seedLibrarySample(t, db, "Standard", b1WithDataC, "S1")
	seedLibrarySample(t, db, "Standard", b1SequencedNoData, "S1")
	seedLibrarySample(t, db, "Standard", b1RegisteredOnly, "S1")

	// Three with-data Illumina samples: 7 study-scoped iRODS data objects total
	// (1 + 3 + 3) across two runs (52553, 52554), each sample's objects sharing
	// one created timestamp so the distinct created dates map to distinct samples.
	seedIseqProductMetricsMirrorRow(t, db, 1101, b1WithDataA, 52553, 1, 1, "S1")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "1101", "/seq/52553", "52553_1#1.cram", b1WithDataA, "S1", b1CreatedOld, "illumina")

	seedIseqProductMetricsMirrorRow(t, db, 1201, b1WithDataB, 52553, 2, 1, "S1")
	for _, product := range []string{"1201", "1202", "1203"} {
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, product, "/seq/52553", "52553_2#"+product+".cram", b1WithDataB, "S1", b1CreatedRecent, "illumina")
	}

	seedIseqProductMetricsMirrorRow(t, db, 1301, b1WithDataC, 52554, 1, 1, "S1")
	for _, product := range []string{"1301", "1302", "1303"} {
		seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, product, "/seq/52554", "52554_1#"+product+".cram", b1WithDataC, "S1", b1CreatedNewest, "illumina")
	}

	// One sequenced-no-data sample: iseq product-metrics in S1 (on run 52554, so
	// runs stays 2) but no iRODS rows.
	seedIseqProductMetricsMirrorRow(t, db, 1401, b1SequencedNoData, 52554, 2, 1, "S1")

	// One registered-only sample: a library link with no products and no iRODS
	// (seeded above via seedLibrarySample; no further rows).

	// A sixth sample belongs to S2 only, its iRODS row scoped to S2, so S1's
	// overview must never count it.
	seedHierarchySample(t, db, b1SharedWithS2Only, "S2", "ov-sample-"+formatInt(b1SharedWithS2Only))
	seedLibrarySample(t, db, "Standard", b1SharedWithS2Only, "S2")
	seedIseqProductMetricsMirrorRow(t, db, 1601, b1SharedWithS2Only, 52553, 3, 1, "S2")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "1601", "/seq/52553", "52553_3#1.cram", b1SharedWithS2Only, "S2", b1CreatedNewest, "illumina")

	seedB1OverviewSyncState(t, db)
}

// TestSyncNullUpstreamCreatedDoesNotPoisonAggregates is a regression test for the
// PR #24 review finding: when an upstream seq_product_irods_locations row has a
// NULL created, it must be mirrored as NULL (not the zero time), so that the
// MIN/MAX(created) aggregates feeding sequencing_date_range, newest_data_added and
// a sample's delivered_at reflect only real timestamps and are never skewed to
// year 0001 by a single unknown-creation row. The existence-based counts
// (data_objects, samples_with_data) must still include the NULL-created row,
// because a NULL creation time does not mean "no data".
func TestSyncNullUpstreamCreatedDoesNotPoisonAggregates(t *testing.T) {
	convey.Convey("Given upstream iRODS rows with a mix of real and NULL created times", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		realCreated := time.Date(2026, time.June, 20, 9, 0, 0, 0, time.UTC)
		lastUpdated := time.Date(2026, time.June, 20, 12, 0, 0, 0, time.UTC)

		// Study S1: one sample with a real-created iRODS row AND a NULL-created
		// iRODS row. Study S2: one sample whose ONLY iRODS row has a NULL created.
		const (
			s1Sample = int64(7001)
			s2Sample = int64(7002)
		)

		seedHierarchyStudy(t, cache.DB(), 701, "S1")
		seedHierarchyStudy(t, cache.DB(), 702, "S2")
		seedHierarchySample(t, cache.DB(), s1Sample, "S1", "null-created-s1")
		seedHierarchySample(t, cache.DB(), s2Sample, "S2", "null-created-s2")
		seedLibrarySample(t, cache.DB(), "Standard", s1Sample, "S1")
		seedLibrarySample(t, cache.DB(), "Standard", s2Sample, "S2")
		seedIseqProductMetricsMirrorRow(t, cache.DB(), 7101, s1Sample, 70001, 1, 1, "S1")
		seedIseqProductMetricsMirrorRow(t, cache.DB(), 7201, s2Sample, 70002, 1, 1, "S2")
		seedB1OverviewSyncState(t, cache.DB())

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSeqProductIRODSLocations: {
				columns: seqProductIRODSLocationsSyncSourceColumns,
				rows: [][]driver.Value{
					{int64(1), "p-s1-real", "/seq/70001", "70001#1.cram", s1Sample, "S1", formatSyncTime(lastUpdated), formatSyncTime(realCreated), "illumina"},
					{int64(2), "p-s1-null", "/seq/70001", "70001#2.cram", s1Sample, "S1", formatSyncTime(lastUpdated), nil, "illumina"},
					{int64(3), "p-s2-null", "/seq/70002", "70002#1.cram", s2Sample, "S2", formatSyncTime(lastUpdated), nil, "illumina"},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: source, disableSyncLock: true, now: func() time.Time { return b1NowFixed }}

		reports, err := syncSelectedTablesForTest(context.Background(), client, syncTableSeqProductIRODSLocations)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reports, convey.ShouldHaveLength, 1)

		convey.Convey("then the NULL-created rows are mirrored as NULL, not the zero time", func() {
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 3)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE created IS NULL`), convey.ShouldEqual, 2)
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE created = ?`, "0001-01-01T00:00:00Z"), convey.ShouldEqual, 0)
		})

		convey.Convey("then S1's existence counts include the NULL-created row but its date range reflects only the real created", func() {
			overview, overviewErr := client.StudyOverview(context.Background(), "S1")
			convey.So(overviewErr, convey.ShouldBeNil)
			convey.So(overview.DataObjects, convey.ShouldEqual, 2)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 1)

			convey.So(overview.SequencingDateRange, convey.ShouldNotBeNil)
			convey.So(overview.SequencingDateRange.Earliest, convey.ShouldEqual, "2026-06-20T09:00:00Z")
			convey.So(overview.SequencingDateRange.Latest, convey.ShouldEqual, "2026-06-20T09:00:00Z")
			convey.So(overview.NewestDataAdded, convey.ShouldEqual, "2026-06-20T09:00:00Z")
		})

		convey.Convey("then S2 (only NULL-created data) counts the row but omits the date range and newest_data_added", func() {
			overview, overviewErr := client.StudyOverview(context.Background(), "S2")
			convey.So(overviewErr, convey.ShouldBeNil)
			convey.So(overview.DataObjects, convey.ShouldEqual, 1)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 1)

			convey.So(overview.SequencingDateRange, convey.ShouldBeNil)
			convey.So(overview.NewestDataAdded, convey.ShouldBeEmpty)
		})

		convey.Convey("then the S2 sample is delivered but its delivered_at is empty, not year 0001", func() {
			baseline, baselineErr := client.deriveSampleBaseline(context.Background(), s2Sample)
			convey.So(baselineErr, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "delivered")
			convey.So(baseline.DeliveredAt, convey.ShouldBeEmpty)
		})

		convey.Convey("then the S1 sample's delivered_at is the real created, not the zero time", func() {
			baseline, baselineErr := client.deriveSampleBaseline(context.Background(), s1Sample)
			convey.So(baselineErr, convey.ShouldBeNil)
			convey.So(baseline.BaselinePhase, convey.ShouldEqual, "delivered")
			convey.So(baseline.DeliveredAt, convey.ShouldEqual, "2026-06-20T09:00:00Z")
		})
	})
}

// B1 acceptance test 5: an unknown study id on a synced cache returns ErrNotFound
// (not the never-synced sentinel).
func TestStudyOverviewUnknownStudyReturnsNotFound(t *testing.T) {
	convey.Convey("Given a synced cache without the requested study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB1OverviewSyncState(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "NOPE")

		convey.Convey("when StudyOverview runs, then it returns ErrNotFound", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(overview, convey.ShouldResemble, StudyOverview{})
		})
	})
}

// B1 acceptance test 6: a synced study with no samples returns a StudyOverview
// with all counts 0 and cache_synced_at populated (the oldest feeding last_run).
func TestStudyOverviewSyncedEmptyStudyIsAllZeroWithCacheSyncedAt(t *testing.T) {
	convey.Convey("Given a synced cache with a known but empty study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 220, "S1")
		seedB1OverviewSyncState(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview runs, then all counts are 0 and cache_synced_at is the oldest feeding last_run", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 0)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 0)
			convey.So(overview.SamplesWithoutData, convey.ShouldEqual, 0)
			convey.So(overview.SamplesSequencedNoData, convey.ShouldEqual, 0)
			convey.So(overview.DataObjects, convey.ShouldEqual, 0)
			convey.So(overview.Runs, convey.ShouldEqual, 0)
			convey.So(overview.Libraries, convey.ShouldEqual, 0)
			convey.So(overview.LibraryTypes, convey.ShouldResemble, []string{})
			convey.So(overview.SequencingDateRange, convey.ShouldBeNil)
			convey.So(overview.NewestDataAdded, convey.ShouldBeEmpty)
			convey.So(overview.AddedLast7Days, convey.ShouldEqual, 0)
			convey.So(overview.CacheSyncedAt, convey.ShouldEqual, b1OldestLastRun.Format(utcRFC3339Layout))
		})
	})
}

// F1 acceptance test 2: a synced study that exists but has ZERO linked samples
// still populates the four metadata fields (read from study_mirror), with the
// counts at 0 and cache_synced_at populated. seedHierarchyStudy(220, "S1") gives
// the four fields their deterministic values.
func TestStudyOverviewEmptyStudyStillPopulatesStudyMetadata(t *testing.T) {
	convey.Convey("Given a synced cache with a known but empty study S1", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 220, "S1")
		seedB1OverviewSyncState(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview runs, then the four metadata fields are populated, counts are 0 and cache_synced_at is set", func() {
			convey.So(err, convey.ShouldBeNil)

			convey.So(overview.Name, convey.ShouldEqual, "Study S1")
			convey.So(overview.AccessionNumber, convey.ShouldEqual, "EGAS0000S1")
			convey.So(overview.FacultySponsor, convey.ShouldEqual, "Faculty sponsor 220")
			convey.So(overview.DataAccessGroup, convey.ShouldEqual, "group")

			convey.So(overview.IDStudyLims, convey.ShouldEqual, "S1")
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 0)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 0)
			convey.So(overview.SamplesWithoutData, convey.ShouldEqual, 0)
			convey.So(overview.SamplesSequencedNoData, convey.ShouldEqual, 0)
			convey.So(overview.DataObjects, convey.ShouldEqual, 0)
			convey.So(overview.CacheSyncedAt, convey.ShouldEqual, b1OldestLastRun.Format(utcRFC3339Layout))
		})
	})
}

// StudyOverview on a synced study whose only sample has been sequenced (it has
// iseq product-metrics) but whose data has not yet reached iRODS must report
// data_objects=0 with no sequencing date range, not error: a study mid-sequencing
// is a valid, expected state.
func TestStudyOverviewSequencedNoIRODSHasNoDateRange(t *testing.T) {
	convey.Convey("Given a synced study S1 whose one sample has product-metrics but no iRODS row", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 230, "S1")
		seedHierarchySample(t, cache.DB(), b1MidSequencingSample, "S1", "ov-sample-"+formatInt(b1MidSequencingSample))
		seedLibrarySample(t, cache.DB(), "Standard", b1MidSequencingSample, "S1")
		seedIseqProductMetricsMirrorRow(t, cache.DB(), 2101, b1MidSequencingSample, 52553, 1, 1, "S1")
		seedB1OverviewSyncState(t, cache.DB())
		client := newB1OverviewClient(cache)

		overview, err := client.StudyOverview(context.Background(), "S1")

		convey.Convey("when StudyOverview is called, then the sample is sequenced-no-data with no iRODS and no date range", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(overview.SamplesTotal, convey.ShouldEqual, 1)
			convey.So(overview.SamplesWithData, convey.ShouldEqual, 0)
			convey.So(overview.SamplesSequencedNoData, convey.ShouldEqual, 1)
			convey.So(overview.DataObjects, convey.ShouldEqual, 0)
			convey.So(overview.SequencingDateRange, convey.ShouldBeNil)
			convey.So(overview.NewestDataAdded, convey.ShouldBeEmpty)
		})
	})
}

// seedB1OverviewSyncState stamps each feeding table with an explicit last_run so
// the overview's cache_synced_at (the oldest last_run across the feeding tables)
// is deterministic. The study table is stamped oldest, so cache_synced_at equals
// b1OldestLastRun.
func seedB1OverviewSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC)
	seedSyncStateRun(t, db, syncTableStudy, highWater, b1OldestLastRun)
	seedSyncStateRun(t, db, syncTableSample, highWater, b1OldestLastRun.Add(1*time.Hour))
	// iseq_flowcell feeds the empty-study cascade (as in CountSamplesForStudy) but
	// is not in the cache_synced_at feeding set, so its last_run does not move the
	// oldest.
	seedSyncStateRun(t, db, syncTableIseqFlowcell, highWater, b1OldestLastRun.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqProductMetrics, highWater, b1OldestLastRun.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableSeqProductIRODSLocations, highWater, b1OldestLastRun.Add(3*time.Hour))
}

// newB1OverviewClient builds a Client over the seeded cache with "now" fixed to
// b1NowFixed, so the half-open added_last_7_days window is deterministic.
func newB1OverviewClient(cache Cache) *Client {
	return &Client{cache: cache, cacheReader: cacheReadDB(cache), now: func() time.Time { return b1NowFixed }}
}
