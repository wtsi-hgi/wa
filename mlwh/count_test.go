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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestCountSerialisesAsCountEnvelope(t *testing.T) {
	convey.Convey("Given a Count value", t, func() {
		encoded, err := json.Marshal(Count{Count: 7})

		convey.Convey("when marshalled to JSON, then it is a {\"count\": N} envelope", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(string(encoded), convey.ShouldEqual, `{"count":7}`)
		})
	})
}

// F2 acceptance test 2: CountSamplesForStudy equals len(SamplesForStudy) for the
// distinct-sample join, even when a sample is linked through several libraries.
func TestCountSamplesForStudyMatchesDistinctSamplesForStudy(t *testing.T) {
	convey.Convey("Given study 6568 with 13 distinct samples across its libraries", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 81, "6568")

		for id := int64(1); id <= 13; id++ {
			seedSampleMirrorSearchRow(t, cache.DB(), id, "name-"+formatInt(id), "supplier-"+formatInt(id), "Homo sapiens", "donor-"+formatInt(id))
			seedLibrarySample(t, cache.DB(), "Standard", id, "6568")
		}

		// Sample 1 is linked through a second library so the join produces a
		// duplicate sample row; DISTINCT collapses it and the count must too.
		seedLibrarySample(t, cache.DB(), "Chromium", 1, "6568")

		// A sample belonging to a different study must not be counted.
		seedHierarchyStudy(t, cache.DB(), 82, "6569")
		seedSampleMirrorSearchRow(t, cache.DB(), 99, "other", "other-supplier", "Homo sapiens", "other-donor")
		seedLibrarySample(t, cache.DB(), "Standard", 99, "6569")

		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 17, 1, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 2, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesForStudy(context.Background(), "6568")
		samples, samplesErr := client.SamplesForStudy(context.Background(), "6568", 1_000_000, 0)

		convey.Convey("when CountSamplesForStudy runs, then it returns Count{Count: 13} == len(SamplesForStudy)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samplesErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 13})
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})
	})
}

// F2 acceptance test 3: a synced study with no samples returns Count{0}, no error.
func TestCountSamplesForStudyEmptyStudyReturnsZeroNoError(t *testing.T) {
	convey.Convey("Given a synced cache with a known study but no linked samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 11, "6568")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 16, 30, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableIseqFlowcell, time.Date(2026, time.May, 6, 16, 31, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 16, 32, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesForStudy(context.Background(), "6568")
		samples, samplesErr := client.SamplesForStudy(context.Background(), "6568", 1_000_000, 0)

		convey.Convey("when CountSamplesForStudy runs, then it returns Count{Count: 0} and no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
			convey.So(samplesErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})
	})
}

// F2 acceptance test 4: never-synced CountStudies returns the joined sentinel.
func TestCountStudiesNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudies(context.Background())

		convey.Convey("when CountStudies runs, then it returns Count{} and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

// CountSamplesForStudy must also surface the joined sentinel on a never-synced
// cache, mirroring SamplesForStudy, so the count==len equality holds there too.
func TestCountSamplesForStudyNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSamplesForStudy(context.Background(), "6568")
		samples, samplesErr := client.SamplesForStudy(context.Background(), "6568", 1_000_000, 0)

		convey.Convey("when CountSamplesForStudy runs, then it returns Count{} and both sentinels like SamplesForStudy", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
			convey.So(errors.Is(samplesErr, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})
	})
}

// F2 acceptance test 1: CountStudies counts only SQSCP study_mirror rows.
func TestCountStudiesCountsOnlySQSCPStudies(t *testing.T) {
	convey.Convey("Given a synced cache with 7 SQSCP studies and some non-SQSCP studies", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		for id := int64(1); id <= 7; id++ {
			seedHierarchyStudy(t, cache.DB(), id, "65"+formatInt(id))
		}

		seedNonSQSCPStudy(t, cache.DB(), 101, "70001")
		seedNonSQSCPStudy(t, cache.DB(), 102, "70002")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudies(context.Background())

		convey.Convey("when CountStudies runs, then it returns Count{Count: 7}", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 7})
		})
	})
}

// seedNonSQSCPStudy inserts a study_mirror row with a non-SQSCP id_lims so it is
// excluded from the SQSCP-only study count.
func seedNonSQSCPStudy(t *testing.T, db *sql.DB, idStudyTmp int64, idStudyLims string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		idStudyTmp,
		"OTHER",
		idStudyLims,
		"study-uuid-"+idStudyLims,
		"Study "+idStudyLims,
		"EGAS"+idStudyLims,
		"title-"+idStudyLims,
		"sponsor",
		"active",
		"strategy",
		"group",
		"programme",
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
		t.Fatalf("seedNonSQSCPStudy(): %v", err)
	}
}
