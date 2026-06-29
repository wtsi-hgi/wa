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

const (
	countListFetchAll = 1_000_000

	// e1SampleWithData is the Sanger name of the E1 sample that owns lanes and
	// iRODS rows, so the sample-scoped lane/iRODS counts have data to size.
	e1SampleWithData = "e1-sample-1"

	// e1RunID is the run on which the E1 samples were sequenced.
	e1RunID = "70001"

	// e1LibraryID and e1LibraryLimsID are the library identifiers set on the E1
	// library_samples rows so the library-id / library-lims-id counts match rows.
	e1LibraryID     = "91001"
	e1LibraryLimsID = "LIB-E1-1"

	// The find-by fields seedSampleMirrorSearchRow stamps on sample 1, used so each
	// find count resolves to exactly that one sample (count == 1 == len(list)).
	e1FindSangerID     = "sanger-1"
	e1FindIDSampleLims = "101"
	e1FindAccession    = "accession-1"
	e1FindSupplier     = "e1-supplier-1"

	// e1SoloLibraryType is a library type with exactly one sample, so the
	// find-by-library-type cross-check resolves to a unique match.
	e1SoloLibraryType = "SoloLib"
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

// B2: the file-type-filtered iRODS counts must equal the filtered list length for
// every file_type (including a valid-but-unmatched one), so the count<->list
// grain identity holds under the filter as it does unfiltered.
func TestCountIRODSPathsByFileTypeMatchesFilteredListLength(t *testing.T) {
	convey.Convey("Given a study and sample with iRODS objects across several suffixes", t, func() {
		client, _, cleanup := newHierarchyTestClient(t)
		defer cleanup()

		ctx := context.Background()
		seedSyncState(t, client.cache.DB(), syncTableSeqProductIRODSLocations, time.Date(2026, time.June, 25, 9, 0, 0, 0, time.UTC))
		seedHierarchyStudy(t, client.cache.DB(), 101, "S1")
		seedHierarchySample(t, client.cache.DB(), 1, "S1", "S1STDY1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-a", "/seq/s1", "a.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-b", "/seq/s1", "b.cram", 1, "S1")
		seedIRODSLocationMirrorRow(t, client.cache.DB(), "p-c", "/seq/s1", "c.bai", 1, "S1")

		convey.Convey("when each filtered count is compared to its filtered list length, then they agree for cram, bai and the unmatched bam", func() {
			mismatched := []string{}
			for _, fileType := range []string{"cram", "bai", "bam"} {
				studyCount, studyCountErr := client.CountIRODSPathsForStudyByFileType(ctx, "S1", fileType)
				convey.So(studyCountErr, convey.ShouldBeNil)
				studyList, studyListErr := client.IRODSPathsForStudyByFileType(ctx, "S1", fileType, countListFetchAll, 0)
				convey.So(studyListErr, convey.ShouldBeNil)
				if studyCount.Count != len(studyList) {
					mismatched = append(mismatched, "study:"+fileType)
				}

				sampleCount, sampleCountErr := client.CountIRODSPathsForSampleByFileType(ctx, "S1STDY1", fileType)
				convey.So(sampleCountErr, convey.ShouldBeNil)
				sampleList, sampleListErr := client.IRODSPathsForSampleByFileType(ctx, "S1STDY1", fileType, countListFetchAll, 0)
				convey.So(sampleListErr, convey.ShouldBeNil)
				if sampleCount.Count != len(sampleList) {
					mismatched = append(mismatched, "sample:"+fileType)
				}
			}

			convey.So(mismatched, convey.ShouldBeEmpty)
		})
	})
}

// B3 acceptance test 2 (count half): CountIRODSPathsForRun honours the file_type
// filter, so the run's cram count is 4 (the four .cram of the six objects).
func TestCountIRODSPathsForRunByFileTypeMatchesFilteredList(t *testing.T) {
	convey.Convey("Given run 52553 with four .cram and two .bai iRODS objects", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3RunIRODSScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		ctx := context.Background()
		count, countErr := client.CountIRODSPathsForRun(ctx, "52553", "cram")
		list, listErr := client.IRODSPathsForRun(ctx, "52553", "cram", countListFetchAll, 0)

		convey.Convey("when CountIRODSPathsForRun(52553, cram) is taken, then it is 4 and equals the filtered list length", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 4)
			convey.So(count.Count, convey.ShouldEqual, len(list))
		})
	})
}

// B3 acceptance test 5: CountIRODSPathsForRun(run, "") equals
// len(IRODSPathsForRun(run, "", all)) -- the run-scope count is the same join with
// no LIMIT, so the count and the all-rows list cannot drift (with or without a
// file_type filter).
func TestCountIRODSPathsForRunEqualsListLength(t *testing.T) {
	convey.Convey("Given run 52553 with six iRODS data objects", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedB3RunIRODSScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		ctx := context.Background()
		count, countErr := client.CountIRODSPathsForRun(ctx, "52553", "")
		list, listErr := client.IRODSPathsForRun(ctx, "52553", "", countListFetchAll, 0)

		convey.Convey("when both are taken, then the count equals the all-rows list length (6)", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(listErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(list))
			convey.So(count.Count, convey.ShouldEqual, 6)
		})
	})
}

// C2 acceptance test 1: CountStudyManifest counts the distinct (id_run, position,
// tag_index) products that ARE the manifest's row grain, so for study S1 with 3
// distinct products it is Count{3} AND equal to len(StudyManifest("S1","",false,
// all).Rows) -- the count and the all-rows list are sized over the same SELECT
// DISTINCT product set and cannot drift.
func TestCountStudyManifestMatchesManifestListLengthC2(t *testing.T) {
	convey.Convey("Given study S1 with 3 distinct Illumina products", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestS1Scenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		ctx := context.Background()
		count, countErr := client.CountStudyManifest(ctx, "S1")
		manifest, manifestErr := client.StudyManifest(ctx, "S1", "", false, manifestAllRows, 0)

		convey.Convey("when CountStudyManifest(\"S1\") is taken, then it is Count{3} and equals len(StudyManifest(\"S1\",\"\",false,all).Rows)", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(manifestErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 3})
			convey.So(count.Count, convey.ShouldEqual, len(manifest.Rows))
		})
	})
}

// C2 acceptance test 2 (never-synced half): a never-synced cache returns Count{}
// joined with both ErrCacheNeverSynced and ErrNotFound, matching
// CountSamplesForStudy's cascade.
func TestCountStudyManifestNeverSyncedReturnsJoinedSentinelC2(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudyManifest(context.Background(), "S1")

		convey.Convey("when CountStudyManifest runs, then it returns Count{} and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

// C2 acceptance test 2 (unknown-study half): an unknown study on a synced cache
// returns ErrNotFound (and not the never-synced sentinel), matching
// CountSamplesForStudy.
func TestCountStudyManifestUnknownStudyReturnsNotFoundC2(t *testing.T) {
	convey.Convey("Given a synced cache with no such study", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedManifestSyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudyManifest(context.Background(), "S1")

		convey.Convey("when CountStudyManifest runs for an unknown study, then it returns ErrNotFound and Count{}", func() {
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeFalse)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

// C2 acceptance test 2 (synced-empty half): a known study on a synced cache with
// no products returns Count{0} and no error, matching CountSamplesForStudy.
func TestCountStudyManifestSyncedStudyWithNoProductsReturnsZeroC2(t *testing.T) {
	convey.Convey("Given a synced study S1 with metadata but no products", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 211, "S1")
		seedManifestSyncState(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		ctx := context.Background()
		count, countErr := client.CountStudyManifest(ctx, "S1")
		manifest, manifestErr := client.StudyManifest(ctx, "S1", "", false, manifestAllRows, 0)

		convey.Convey("when CountStudyManifest runs, then it returns Count{0}, no error, equal to the empty manifest's row count", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(manifestErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
			convey.So(count.Count, convey.ShouldEqual, len(manifest.Rows))
		})
	})
}

// e1Count names one new /count endpoint under test: its Client count method and
// the corresponding all-rows list-length, so the E1 cross-check
// (count == len(list-all)) can be asserted for every count uniformly. zeroIs
// NotFound records whether a synced cache with no matching rows yields ErrNotFound
// (the resolve-first / find lists, whose empty result is not_found) rather than
// Count{0} (the parent-exists lists), so the synced-empty assertion matches each
// count's list exactly.
type e1Count struct {
	name           string
	count          func(c *Client) (Count, error)
	listLen        func(c *Client) (int, error)
	zeroIsNotFound bool
}

// e1CountCases enumerates the fifteen new /count endpoints added for E1, bound to
// the e1CountScenario fixture identifiers. Each pairs the count with its list so a
// single table drives the count<->list cross-check, the never-synced cascade, and
// the synced-empty behaviour for all of them.
func e1CountCases() []e1Count {
	ctx := context.Background()

	return []e1Count{
		{
			name:    "CountIRODSPathsForStudy",
			count:   func(c *Client) (Count, error) { return c.CountIRODSPathsForStudy(ctx, "E1") },
			listLen: func(c *Client) (int, error) { return listLen(c.IRODSPathsForStudy(ctx, "E1", countListFetchAll, 0)) },
		},
		{
			name:  "CountIRODSPathsForSample",
			count: func(c *Client) (Count, error) { return c.CountIRODSPathsForSample(ctx, e1SampleWithData) },
			listLen: func(c *Client) (int, error) {
				return listLen(c.IRODSPathsForSample(ctx, e1SampleWithData, countListFetchAll, 0))
			},
		},
		{
			name:    "CountRunsForStudy",
			count:   func(c *Client) (Count, error) { return c.CountRunsForStudy(ctx, "E1") },
			listLen: func(c *Client) (int, error) { return listLen(c.RunsForStudy(ctx, "E1", countListFetchAll, 0)) },
		},
		{
			name:    "CountLibrariesForStudy",
			count:   func(c *Client) (Count, error) { return c.CountLibrariesForStudy(ctx, "E1") },
			listLen: func(c *Client) (int, error) { return listLen(c.LibrariesForStudy(ctx, "E1", countListFetchAll, 0)) },
		},
		{
			name:  "CountLanesForSample",
			count: func(c *Client) (Count, error) { return c.CountLanesForSample(ctx, e1SampleWithData) },
			listLen: func(c *Client) (int, error) {
				return listLen(c.LanesForSample(ctx, e1SampleWithData, countListFetchAll, 0))
			},
		},
		{
			name:           "CountSamplesForRun",
			count:          func(c *Client) (Count, error) { return c.CountSamplesForRun(ctx, e1RunID) },
			listLen:        func(c *Client) (int, error) { return listLen(c.SamplesForRun(ctx, e1RunID, countListFetchAll, 0)) },
			zeroIsNotFound: true,
		},
		{
			name:  "CountSamplesForLibrary",
			count: func(c *Client) (Count, error) { return c.CountSamplesForLibrary(ctx, "Standard", "E1") },
			listLen: func(c *Client) (int, error) {
				return listLen(c.SamplesForLibrary(ctx, "Standard", "E1", countListFetchAll, 0))
			},
		},
		{
			name:  "CountSamplesForLibraryID",
			count: func(c *Client) (Count, error) { return c.CountSamplesForLibraryID(ctx, e1LibraryID) },
			listLen: func(c *Client) (int, error) {
				return listLen(c.SamplesForLibraryID(ctx, e1LibraryID, countListFetchAll, 0))
			},
			zeroIsNotFound: true,
		},
		{
			name:  "CountSamplesForLibraryLimsID",
			count: func(c *Client) (Count, error) { return c.CountSamplesForLibraryLimsID(ctx, e1LibraryLimsID) },
			listLen: func(c *Client) (int, error) {
				return listLen(c.SamplesForLibraryLimsID(ctx, e1LibraryLimsID, countListFetchAll, 0))
			},
			zeroIsNotFound: true,
		},
		{
			name:  "CountSamplesForLibraryType",
			count: func(c *Client) (Count, error) { return c.CountSamplesForLibraryType(ctx, "Standard") },
			listLen: func(c *Client) (int, error) {
				return listLen(c.SamplesForLibraryType(ctx, "Standard", countListFetchAll, 0))
			},
		},
		{
			name:           "CountFindSamplesBySangerID",
			count:          func(c *Client) (Count, error) { return c.CountFindSamplesBySangerID(ctx, e1FindSangerID) },
			listLen:        func(c *Client) (int, error) { return listLen(c.FindSamplesBySangerID(ctx, e1FindSangerID)) },
			zeroIsNotFound: true,
		},
		{
			name:           "CountFindSamplesByIDSampleLims",
			count:          func(c *Client) (Count, error) { return c.CountFindSamplesByIDSampleLims(ctx, e1FindIDSampleLims) },
			listLen:        func(c *Client) (int, error) { return listLen(c.FindSamplesByIDSampleLims(ctx, e1FindIDSampleLims)) },
			zeroIsNotFound: true,
		},
		{
			name:           "CountFindSamplesByAccessionNumber",
			count:          func(c *Client) (Count, error) { return c.CountFindSamplesByAccessionNumber(ctx, e1FindAccession) },
			listLen:        func(c *Client) (int, error) { return listLen(c.FindSamplesByAccessionNumber(ctx, e1FindAccession)) },
			zeroIsNotFound: true,
		},
		{
			name:           "CountFindSamplesBySupplierName",
			count:          func(c *Client) (Count, error) { return c.CountFindSamplesBySupplierName(ctx, e1FindSupplier) },
			listLen:        func(c *Client) (int, error) { return listLen(c.FindSamplesBySupplierName(ctx, e1FindSupplier)) },
			zeroIsNotFound: true,
		},
		{
			// FindSamplesByLibraryType requires a UNIQUE match, so the cross-check
			// targets the single-sample "SoloLib" library type (the multi-sample
			// "Standard" type, used by CountSamplesForLibraryType above, would make
			// the Find list raise ErrAmbiguous rather than return one row).
			name:           "CountFindSamplesByLibraryType",
			count:          func(c *Client) (Count, error) { return c.CountFindSamplesByLibraryType(ctx, e1SoloLibraryType) },
			listLen:        func(c *Client) (int, error) { return listLen(c.FindSamplesByLibraryType(ctx, e1SoloLibraryType)) },
			zeroIsNotFound: true,
		},
	}
}

// listLen adapts a (slice, error) list result to (len, error) so a count can be
// compared against its list length without per-list boilerplate.
func listLen[T any](items []T, err error) (int, error) {
	return len(items), err
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

// E1 acceptance test 1: for every new /count, count == len(list-all) on a seeded
// fixture, exercised against the real Client list and count methods.
func TestCountEndpointsMatchListLength(t *testing.T) {
	convey.Convey("Given the seeded E1 count fixture", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedE1CountScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		cases := e1CountCases()

		convey.Convey("when each count and its list-all are taken, then count == len(list) and the data is non-empty", func() {
			mismatches := []string{}
			emptyCounts := []string{}

			for _, tc := range cases {
				count, countErr := tc.count(client)
				length, listErr := tc.listLen(client)
				if countErr != nil || listErr != nil {
					mismatches = append(mismatches, tc.name+": error")

					continue
				}
				if count.Count != length {
					mismatches = append(mismatches, tc.name)
				}
				if count.Count == 0 {
					emptyCounts = append(emptyCounts, tc.name)
				}
			}

			convey.So(mismatches, convey.ShouldBeEmpty)
			convey.So(emptyCounts, convey.ShouldBeEmpty)
		})
	})
}

// seedE1CountScenario builds the single fixture every new /count is cross-checked
// against: study "E1" with five samples linked through the "Standard" library,
// the first two also carrying the e1LibraryID / e1LibraryLimsID identifiers; sample
// 1 owns Illumina product-metrics (run 70001, two lanes) and two distinct iRODS
// data objects, and a second sample owns one more lane/product so the run, lane,
// iRODS, library and sample lists are all non-empty. Distinct sample-finder fields
// (sanger id, LIMS id, accession, supplier) on sample 1 give each find count a
// unique match. Sync state is stamped for every feeding table so the counts return
// data rather than the never-synced sentinel.
func seedE1CountScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 301, "E1")

	for id := int64(1); id <= 5; id++ {
		seedSampleMirrorSearchRow(t, db, id, "e1-sample-"+formatInt(id), "e1-supplier-"+formatInt(id), "Homo sapiens", "e1-donor-"+formatInt(id))
		seedLibrarySample(t, db, "Standard", id, "E1")
	}

	// A sixth sample in a single-sample "SoloLib" library type, so the
	// find-by-library-type count resolves to a unique match for the cross-check.
	seedSampleMirrorSearchRow(t, db, 6, "e1-sample-6", "e1-supplier-6", "Homo sapiens", "e1-donor-6")
	seedLibrarySample(t, db, e1SoloLibraryType, 6, "E1")

	// Set the library identifiers on the first two rows so the library-id and
	// library-lims-id counts (which filter on those columns) match rows.
	if _, err := db.Exec(`UPDATE library_samples SET library_id = ?, id_library_lims = ? WHERE id_sample_tmp IN (1, 2) AND id_study_lims = 'E1'`, e1LibraryID, e1LibraryLimsID); err != nil {
		t.Fatalf("seedE1CountScenario() set library identifiers: %v", err)
	}

	// Sample 1: two lanes on run 70001 and two distinct iRODS data objects.
	seedIseqProductMetricsMirrorRow(t, db, 7001, 1, 70001, 1, 1, "E1")
	seedIseqProductMetricsMirrorRow(t, db, 7002, 1, 70001, 2, 1, "E1")
	seedIRODSLocationMirrorRow(t, db, "7001", "/seq/70001", "70001_1#1.cram", 1, "E1")
	seedIRODSLocationMirrorRow(t, db, "7002", "/seq/70001", "70001_2#1.cram", 1, "E1")

	// Sample 2: one further lane/product on the same run, so the run has two
	// distinct samples and the study has runs/libraries to count.
	seedIseqProductMetricsMirrorRow(t, db, 7003, 2, 70001, 3, 1, "E1")
	seedIRODSLocationMirrorRow(t, db, "7003", "/seq/70001", "70001_3#1.cram", 2, "E1")

	seedE1CountSyncState(t, db)
}

// E1 acceptance test 3: against a synced-but-empty parent, the parent-exists counts
// return Count{0} with no error, while the resolve-first / find counts (whose list
// reports not_found on an empty result) return ErrNotFound, matching each count's
// list exactly.
func TestCountEndpointsSyncedEmptyParentReturnZero(t *testing.T) {
	convey.Convey("Given a synced cache whose parents exist but have no children", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// The study and sample the parent-scoped counts resolve exist, but carry no
		// libraries, runs, lanes or iRODS rows; the run/library-identifier/find
		// targets match nothing on the otherwise synced cache. The sample resolves by
		// the e1SampleWithData NAME but is seeded under id 42, so its derived
		// sanger/LIMS/accession fields and its supplier do NOT match the find
		// constants (those finds must report not_found, like their lists).
		seedHierarchyStudy(t, cache.DB(), 311, "E1")
		seedSampleMirrorSearchRow(t, cache.DB(), 42, e1SampleWithData, "no-match-supplier", "Homo sapiens", "e1-donor-42")
		seedE1CountSyncState(t, cache.DB())

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		cases := e1CountCases()

		convey.Convey("when each count runs, then Count{0}+nil for parent-exists counts and ErrNotFound for the resolve-first/find counts", func() {
			zeroFailures := []string{}
			notFoundFailures := []string{}

			for _, tc := range cases {
				count, err := tc.count(client)
				if tc.zeroIsNotFound {
					if !errors.Is(err, ErrNotFound) || errors.Is(err, ErrCacheNeverSynced) {
						notFoundFailures = append(notFoundFailures, tc.name)
					}

					continue
				}
				if err != nil || count != (Count{Count: 0}) {
					zeroFailures = append(zeroFailures, tc.name)
				}
			}

			convey.So(zeroFailures, convey.ShouldBeEmpty)
			convey.So(notFoundFailures, convey.ShouldBeEmpty)
		})
	})
}

// seedE1CountSyncState stamps a synced sync_state for every table the E1 counts'
// cascades consult, so each count reads data and never returns the never-synced
// sentinel.
func seedE1CountSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	base := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	seedSyncState(t, db, syncTableStudy, base)
	seedSyncState(t, db, syncTableSample, base.Add(1*time.Minute))
	seedSyncState(t, db, syncTableIseqFlowcell, base.Add(2*time.Minute))
	seedSyncState(t, db, syncTableIseqProductMetrics, base.Add(3*time.Minute))
	seedSyncState(t, db, syncTableSeqProductIRODSLocations, base.Add(4*time.Minute))
}

// E1 acceptance test 2: every new /count returns the same ErrCacheNeverSynced +
// ErrNotFound joined sentinel as its list on a never-synced cache.
func TestCountEndpointsNeverSyncedReturnJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		cases := e1CountCases()

		convey.Convey("when each count runs, then it returns Count{} joined with both sentinels", func() {
			missingNeverSynced := []string{}
			missingNotFound := []string{}
			nonZero := []string{}

			for _, tc := range cases {
				count, err := tc.count(client)
				if !errors.Is(err, ErrCacheNeverSynced) {
					missingNeverSynced = append(missingNeverSynced, tc.name)
				}
				if !errors.Is(err, ErrNotFound) {
					missingNotFound = append(missingNotFound, tc.name)
				}
				if count != (Count{}) {
					nonZero = append(nonZero, tc.name)
				}
			}

			convey.So(missingNeverSynced, convey.ShouldBeEmpty)
			convey.So(missingNotFound, convey.ShouldBeEmpty)
			convey.So(nonZero, convey.ShouldBeEmpty)
		})
	})
}
