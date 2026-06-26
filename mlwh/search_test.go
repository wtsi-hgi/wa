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
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestSearchStudiesNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.SearchStudies(context.Background(), "abc", 100, 0)

		convey.Convey("when SearchStudies runs, then it returns an empty slice and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(studies, convey.ShouldResemble, []Study{})
		})
	})
}

func TestSearchSamplesNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		samples, err := client.SearchSamples(context.Background(), "abc", 100, 0)

		convey.Convey("when SearchSamples runs, then it returns an empty slice and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(samples, convey.ShouldResemble, []Sample{})
		})
	})
}

func TestCountStudySearchNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudySearch(context.Background(), "abc")

		convey.Convey("when CountStudySearch runs, then it returns Count{} and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

func TestCountSampleSearchNeverSyncedReturnsJoinedSentinel(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSampleSearch(context.Background(), "abc")

		convey.Convey("when CountSampleSearch runs, then it returns Count{} and both sentinels", func() {
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
			convey.So(count, convey.ShouldResemble, Count{})
		})
	})
}

func TestSearchStudiesMatchesTitleSubstringOrderedByLimsID(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with three studies by title", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "6568", "study-a", "Malaria genomics", "Genomics", "Sponsor A")
		seedStudyMirrorSearchRow(t, cache.DB(), 2, "6566", "study-b", "Malaria vaccine", "Vaccines", "Sponsor B")
		seedStudyMirrorSearchRow(t, cache.DB(), 3, "6567", "study-c", "Cancer atlas", "Oncology", "Sponsor C")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.SearchStudies(context.Background(), "malar", 100, 0)

		convey.Convey("when SearchStudies runs, then the two malaria studies are returned in id_study_lims order", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studyLimsIDs(studies), convey.ShouldResemble, []string{"6566", "6568"})
		})
	})
}

func TestSearchStudiesMatchesAcrossAllFourSearchableFields(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose only malaria hit is via programme", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 10, "7001", "kelch project", "Resistance markers", "Malaria Programme", "Sponsor P")
		seedStudyMirrorSearchRow(t, cache.DB(), 11, "7002", "unrelated", "Cardiology cohort", "Cardiology", "Sponsor Q")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.SearchStudies(context.Background(), "malaria", 100, 0)

		convey.Convey("when SearchStudies runs over all four fields, then the programme match is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studyLimsIDs(studies), convey.ShouldResemble, []string{"7001"})
		})
	})
}

func TestSearchStudiesShortTermReturnsEmptyWithoutMatching(t *testing.T) {
	convey.Convey("Given a synced SQLite cache that would otherwise match a 2-char term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 20, "8001", "mango", "Marine atlas", "Mammals", "Sponsor M")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.SearchStudies(context.Background(), "ma", 100, 0)

		convey.Convey("when SearchStudies runs with a length-2 term, then it returns an empty slice and no match", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studies, convey.ShouldResemble, []Study{})
		})
	})
}

func TestSearchStudiesHonoursLimitAndOffsetInLimsIDOrder(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with four matching studies", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 30, "9004", "alpha", "Study one", "Genomics", "Sponsor A")
		seedStudyMirrorSearchRow(t, cache.DB(), 31, "9001", "beta", "Study two", "Genomics", "Sponsor B")
		seedStudyMirrorSearchRow(t, cache.DB(), 32, "9003", "gamma", "Study three", "Genomics", "Sponsor C")
		seedStudyMirrorSearchRow(t, cache.DB(), 33, "9002", "delta", "Study four", "Genomics", "Sponsor D")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.SearchStudies(context.Background(), "study", 2, 1)

		convey.Convey("when SearchStudies runs with limit 2 offset 1, then it returns the second page in id_study_lims order", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studyLimsIDs(studies), convey.ShouldResemble, []string{"9002", "9003"})
		})
	})
}

func TestSearchStudiesTreatsPercentAsLiteralNotWildcard(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with a literal-percent title and a 5024 title", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 40, "1001", "discount study", "Up to 50% off", "Genomics", "Sponsor A")
		seedStudyMirrorSearchRow(t, cache.DB(), 41, "1002", "code study", "5024", "Genomics", "Sponsor B")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.SearchStudies(context.Background(), "50%", 100, 0)

		convey.Convey("when SearchStudies runs for \"50%\", then only the literal-percent study matches", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(studyLimsIDs(studies), convey.ShouldResemble, []string{"1001"})

			count, countErr := client.CountStudySearch(context.Background(), "50%")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(studies))
		})
	})
}

func TestSearchStudiesMatchesUnderscoreAndEscapeCharAsLiteralSubstring(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose titles exercise the LIKE wildcard and escape characters", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Row 1 holds a literal underscore; row 2 holds the same letters with a
		// different separator. If the underscore were treated as the LIKE single
		// -character wildcard rather than a literal, both rows would match "a_b";
		// the escape clause means only the literal-underscore row does.
		seedStudyMirrorSearchRow(t, cache.DB(), 50, "2001", "study-a", "code a_b end", "Genomics", "Sponsor A")
		seedStudyMirrorSearchRow(t, cache.DB(), 51, "2002", "study-b", "code axb end", "Genomics", "Sponsor B")
		// Row 3 holds a literal occurrence of whatever character the search uses
		// as its LIKE escape character; row 4 holds the same letters without it.
		// The escape character must be matched literally (substring), so only row
		// 3 matches a term containing it.
		seedStudyMirrorSearchRow(t, cache.DB(), 52, "2003", "study-c", "code wow"+searchLIKEEscapeChar+"yes end", "Genomics", "Sponsor C")
		seedStudyMirrorSearchRow(t, cache.DB(), 53, "2004", "study-d", "code wowyes end", "Genomics", "Sponsor D")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when SearchStudies runs for a term containing an underscore, then only the literal-underscore study matches", func() {
			studies, err := client.SearchStudies(context.Background(), "a_b", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(studyLimsIDs(studies), convey.ShouldResemble, []string{"2001"})

			count, countErr := client.CountStudySearch(context.Background(), "a_b")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(studies))
		})

		convey.Convey("when SearchStudies runs for a term containing the escape character, then only the literal-escape-char study matches", func() {
			studies, err := client.SearchStudies(context.Background(), "wow"+searchLIKEEscapeChar+"yes", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(studyLimsIDs(studies), convey.ShouldResemble, []string{"2003"})

			count, countErr := client.CountStudySearch(context.Background(), "wow"+searchLIKEEscapeChar+"yes")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(studies))
		})
	})
}

func TestCountStudySearchMatchesSearchStudiesCount(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with two studies matching \"malar\"", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "6568", "study-a", "Malaria genomics", "Genomics", "Sponsor A")
		seedStudyMirrorSearchRow(t, cache.DB(), 2, "6566", "study-b", "Malaria vaccine", "Vaccines", "Sponsor B")
		seedStudyMirrorSearchRow(t, cache.DB(), 3, "6567", "study-c", "Cancer atlas", "Oncology", "Sponsor C")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudySearch(context.Background(), "malar")

		convey.Convey("when CountStudySearch runs, then it returns Count{2}, equal to len(SearchStudies)", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 2})

			studies, searchErr := client.SearchStudies(context.Background(), "malar", 1000, 0)
			convey.So(searchErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(studies))
		})
	})
}

func TestCountStudySearchShortTermReturnsZeroWithoutMatching(t *testing.T) {
	convey.Convey("Given a synced SQLite cache that would otherwise match a 2-char term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 20, "8001", "mango", "Marine atlas", "Mammals", "Sponsor M")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountStudySearch(context.Background(), "ma")

		convey.Convey("when CountStudySearch runs with a length-2 term, then it returns Count{0} and no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 0})
		})
	})
}

// seedStudyMirrorSearchRow inserts a study_mirror row letting the caller set the
// four searchable fields (name, study_title, programme, faculty_sponsor)
// independently, so substring-search coverage can target each field.
func seedStudyMirrorSearchRow(t *testing.T, db *sql.DB, id int64, idStudyLims, name, studyTitle, programme, facultySponsor string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		idStudyLims,
		"study-uuid-"+idStudyLims,
		name,
		"EGAS"+idStudyLims,
		studyTitle,
		facultySponsor,
		"active",
		"strategy",
		"group",
		programme,
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
		t.Fatalf("seedStudyMirrorSearchRow(): %v", err)
	}
}

func studyLimsIDs(studies []Study) []string {
	ids := make([]string, len(studies))
	for index, study := range studies {
		ids[index] = study.IDStudyLims
	}

	return ids
}

func TestSearchSamplesMatchesSupplierNameOrderedByTmpID(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with three samples by supplier_name", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 3, "name-c", "OTHER-1", "Homo sapiens", "donor-c")
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "name-a", "ACME-001", "Homo sapiens", "donor-a")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-b", "ACME-002", "Homo sapiens", "donor-b")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)

		convey.Convey("when SearchSamples runs, then the two ACME samples are returned in id_sample_tmp order", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1, 2})
		})
	})
}

func TestSearchSamplesMatchesAcrossAllFourSearchableFields(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose only sapien hit is via common_name", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 10, "plain-name", "plain-supplier", "Homo sapiens", "plain-donor")
		seedSampleMirrorSearchRow(t, cache.DB(), 11, "other-name", "other-supplier", "Mus musculus", "other-donor")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		samples, err := client.SearchSamples(context.Background(), "sapien", 100, 0)

		convey.Convey("when SearchSamples runs over all four fields, then the common_name match is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{10})
		})
	})
}

func TestSearchSamplesExcludesTrigramFalsePositiveViaLIKEPostFilter(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with a genuine trigram false positive for the term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Row 1 contains the literal ASCII substring "kelvin".
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "kelvin sample", "supplier-1", "common-1", "donor-1")
		// Row 2 begins with the Kelvin sign U+212A ("Kelvin"). The FTS5
		// trigram tokenizer Unicode-case-folds U+212A to 'k', so an FTS5 MATCH
		// for "kelvin" surfaces this row as a candidate, but it does not contain
		// the ASCII substring "kelvin": SQLite LIKE folds only ASCII A-Z, so the
		// post-filter must exclude it.
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "Kelvin sample", "supplier-2", "common-2", "donor-2")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("the FTS5 MATCH alone surfaces the false positive's rowid as a candidate", func() {
			var matchedIDs []int64
			rows, qErr := cache.DB().Query(`SELECT rowid FROM sample_search WHERE sample_search MATCH ? ORDER BY rowid`, `"kelvin"`)
			convey.So(qErr, convey.ShouldBeNil)
			for rows.Next() {
				var id int64
				convey.So(rows.Scan(&id), convey.ShouldBeNil)
				matchedIDs = append(matchedIDs, id)
			}
			convey.So(rows.Close(), convey.ShouldBeNil)
			convey.So(matchedIDs, convey.ShouldResemble, []int64{1, 2})
		})

		samples, err := client.SearchSamples(context.Background(), "kelvin", 100, 0)

		convey.Convey("when SearchSamples runs, then the LIKE post-filter excludes the false positive", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})
		})
	})
}

func TestSearchSamplesShortTermReturnsEmptyWithoutMatching(t *testing.T) {
	convey.Convey("Given a synced SQLite cache that would otherwise match a 2-char term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 1, "ac-name", "ACME-001", "ac-common", "ac-donor")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		samples, err := client.SearchSamples(context.Background(), "ac", 100, 0)

		convey.Convey("when SearchSamples runs with a length-2 term, then it returns an empty slice and no match", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []Sample{})
		})
	})
}

func TestSearchSamplesHonoursLimitAndOffsetInTmpIDOrder(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with four matching samples", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 4, "name-4", "ACME-004", "common-4", "donor-4")
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "name-1", "ACME-001", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 3, "name-3", "ACME-003", "common-3", "donor-3")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-2", "ACME-002", "common-2", "donor-2")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		samples, err := client.SearchSamples(context.Background(), "acme", 2, 1)

		convey.Convey("when SearchSamples runs with limit 2 offset 1, then it returns the second page in id_sample_tmp order", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{2, 3})
		})
	})
}

func TestSearchSamplesReturnsFullRowsWithFanOut(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with a matching sample linked to two library-study pairings", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedHierarchyStudy(t, cache.DB(), 81, "6568")
		seedHierarchyStudy(t, cache.DB(), 82, "6569")
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "ACME-sample", "ACME-supplier", "Homo sapiens", "ACME-donor")
		seedLibrarySample(t, cache.DB(), "Standard", 1, "6568")
		seedLibrarySample(t, cache.DB(), "Chromium", 1, "6569")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		samples, err := client.SearchSamples(context.Background(), "acme", 100, 0)

		convey.Convey("when SearchSamples runs, then it returns the full row with its library/study fan-out populated", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldHaveLength, 1)
			convey.So(samples[0].IDSampleTmp, convey.ShouldEqual, int64(1))
			convey.So(samples[0].Name, convey.ShouldEqual, "ACME-sample")
			convey.So(samples[0].SupplierName, convey.ShouldEqual, "ACME-supplier")
			convey.So(samples[0].CommonName, convey.ShouldEqual, "Homo sapiens")
			convey.So(samples[0].Libraries, convey.ShouldResemble, []Library{
				{PipelineIDLims: "Standard", IDStudyLims: "6568"},
				{PipelineIDLims: "Chromium", IDStudyLims: "6569"},
			})
			convey.So(samples[0].Studies, convey.ShouldHaveLength, 2)
			convey.So(samples[0].Studies[0].IDStudyLims, convey.ShouldEqual, "6568")
			convey.So(samples[0].Studies[1].IDStudyLims, convey.ShouldEqual, "6569")
		})
	})
}

func TestSearchSamplesTreatsFTS5OperatorCharactersAsLiteralSubstring(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose rows exercise FTS5 operator and special characters", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// id 1 holds the literal phrase "alpha OR beta"; ids 2 and 3 hold only
		// one operand each. If the term were parsed as a boolean OR rather than
		// a literal phrase, ids 2 and 3 would match too; the quoted-phrase MATCH
		// plus the LIKE post-filter mean only the literal substring (id 1) does.
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "alpha OR beta sample", "supplier-1", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "alpha sample", "supplier-2", "common-2", "donor-2")
		seedSampleMirrorSearchRow(t, cache.DB(), 3, "beta sample", "supplier-3", "common-3", "donor-3")
		// id 4 holds an embedded double quote; id 5 a NEAR( token; id 6 a
		// leading asterisk (prefix operator); id 7 a leading hyphen (negation).
		// Each is a real FTS5 operator/special character that would error or
		// change semantics if not quoted as a literal phrase.
		seedSampleMirrorSearchRow(t, cache.DB(), 4, `say "hello" now`, "supplier-4", "common-4", "donor-4")
		seedSampleMirrorSearchRow(t, cache.DB(), 5, "NEAR(gene) region", "supplier-5", "common-5", "donor-5")
		seedSampleMirrorSearchRow(t, cache.DB(), 6, "*wildcard tail", "supplier-6", "common-6", "donor-6")
		seedSampleMirrorSearchRow(t, cache.DB(), 7, "-minus prefix", "supplier-7", "common-7", "donor-7")
		// id 8 contains only "wildcard" (no leading asterisk): a bare prefix
		// search "*wildcard*" would surface it, but the literal substring
		// "*wildcard" must not.
		seedSampleMirrorSearchRow(t, cache.DB(), 8, "plain wildcard tail", "supplier-8", "common-8", "donor-8")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		cases := []struct {
			name    string
			term    string
			matched []int64
		}{
			{"boolean OR is literal, not a disjunction", "alpha OR beta", []int64{1}},
			{"embedded double quote is literal", `"hello"`, []int64{4}},
			{"NEAR( token is literal", "NEAR(gene)", []int64{5}},
			{"leading asterisk is literal, not a prefix operator", "*wildcard", []int64{6}},
			{"leading hyphen is literal, not a negation", "-minus", []int64{7}},
		}

		for _, testCase := range cases {
			convey.Convey("when SearchSamples runs for "+testCase.name, func() {
				samples, err := client.SearchSamples(context.Background(), testCase.term, 100, 0)

				convey.So(err, convey.ShouldBeNil)
				convey.So(sampleTmpIDs(samples), convey.ShouldResemble, testCase.matched)
			})
		}
	})
}

func TestSearchSamplesMatchesUnderscoreAndEscapeCharAsLiteralSubstring(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose names exercise the LIKE wildcard and escape characters", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Row 1 holds a literal underscore in supplier_name; row 2 holds the same
		// letters with a different separator. If the underscore were the LIKE
		// single-character wildcard rather than a literal, both would match
		// "wid_get"; the escape clause means only the literal-underscore row does.
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "name-1", "wid_get supplier", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-2", "widxget supplier", "common-2", "donor-2")
		// Row 3 holds a literal occurrence of whatever character the search uses
		// as its LIKE escape character; row 4 holds the same letters without it.
		// The escape character must be matched literally (substring), so only row
		// 3 matches a term containing it.
		seedSampleMirrorSearchRow(t, cache.DB(), 3, "name-3", "wow"+searchLIKEEscapeChar+"yes supplier", "common-3", "donor-3")
		seedSampleMirrorSearchRow(t, cache.DB(), 4, "name-4", "wowyes supplier", "common-4", "donor-4")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when SearchSamples runs for a term containing an underscore, then only the literal-underscore sample matches", func() {
			samples, err := client.SearchSamples(context.Background(), "wid_get", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})

			count, countErr := client.CountSampleSearch(context.Background(), "wid_get")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})

		convey.Convey("when SearchSamples runs for a term containing the escape character, then only the literal-escape-char sample matches", func() {
			samples, err := client.SearchSamples(context.Background(), "wow"+searchLIKEEscapeChar+"yes", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{3})

			count, countErr := client.CountSampleSearch(context.Background(), "wow"+searchLIKEEscapeChar+"yes")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})
	})
}

func TestCountSampleSearchMatchesSearchSamplesCount(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with three samples matching \"acme\"", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 3, "name-c", "ACME-003", "common-c", "donor-c")
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "name-a", "ACME-001", "common-a", "donor-a")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-b", "ACME-002", "common-b", "donor-b")
		// A non-matching row guards against the count over-counting beyond the
		// LIKE post-filter.
		seedSampleMirrorSearchRow(t, cache.DB(), 4, "name-d", "OTHER-004", "common-d", "donor-d")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		count, err := client.CountSampleSearch(context.Background(), "acme")

		convey.Convey("when CountSampleSearch runs, then it returns Count{3}, equal to len(SearchSamples(ctx, \"acme\", 1000, 0))", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 3})

			samples, searchErr := client.SearchSamples(context.Background(), "acme", 1000, 0)
			convey.So(searchErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})
	})
}

func TestIncrementalSyncMakesNewSampleSearchable(t *testing.T) {
	convey.Convey("Given a SQLite cache cold-synced with sample A, when a second incremental sync adds sample B", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), disableSyncLock: true}
		coldBase := time.Date(2026, time.May, 20, 9, 0, 0, 0, time.UTC)

		// Cold sync builds the fts5 index from sample_mirror.
		coldRow := sampleSyncRowValues(1, coldBase, sampleSyncRowOverride{Name: "alpha-sample"})
		runSampleSyncForTest(t, client, coldRow)

		// An incremental sync (state exists, non-zero high_water, indexes not
		// dropped) adds a brand-new sample whose searchable name is distinctive.
		incrementalRow := sampleSyncRowValues(2, coldBase.Add(time.Hour), sampleSyncRowOverride{Name: "bravoUnique-sample"})
		runSampleSyncForTest(t, client, coldRow, incrementalRow)

		convey.Convey("then SearchSamples finds B and CountSampleSearch agrees", func() {
			samples, err := client.SearchSamples(context.Background(), "bravounique", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{2})

			count, countErr := client.CountSampleSearch(context.Background(), "bravounique")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})
	})
}

func TestIncrementalSyncReflectsUpdatedSearchableField(t *testing.T) {
	convey.Convey("Given a SQLite cache cold-synced with a sample, when a second incremental sync updates its searchable name", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), disableSyncLock: true}
		coldBase := time.Date(2026, time.May, 21, 9, 0, 0, 0, time.UTC)

		coldRow := sampleSyncRowValues(1, coldBase, sampleSyncRowOverride{Name: "zebraOriginal-sample"})
		runSampleSyncForTest(t, client, coldRow)

		updatedRow := sampleSyncRowValues(1, coldBase.Add(time.Hour), sampleSyncRowOverride{Name: "antelopeUpdated-sample"})
		runSampleSyncForTest(t, client, updatedRow)

		convey.Convey("then the new value matches and the old value no longer matches", func() {
			newSamples, err := client.SearchSamples(context.Background(), "antelopeupdated", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(newSamples), convey.ShouldResemble, []int64{1})

			oldSamples, oldErr := client.SearchSamples(context.Background(), "zebraoriginal", 100, 0)
			convey.So(oldErr, convey.ShouldBeNil)
			convey.So(oldSamples, convey.ShouldBeEmpty)

			oldCount, oldCountErr := client.CountSampleSearch(context.Background(), "zebraoriginal")
			convey.So(oldCountErr, convey.ShouldBeNil)
			convey.So(oldCount, convey.ShouldResemble, Count{})
		})
	})
}

// runSampleSyncForTest drives one sample sync run against the client's cache
// using an in-process source returning the given rows, picking up the existing
// sync_state so a second call exercises the incremental (indexes-not-dropped)
// path rather than another cold load.
func runSampleSyncForTest(t *testing.T, client *Client, rows ...[]driver.Value) {
	t.Helper()

	source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
		syncTableSample: {columns: sampleSyncSourceColumns, rows: rows},
	})
	defer func() { _ = source.Close() }()

	client.syncSource = source

	state, err := readSyncStateFromDB(context.Background(), client.cache.DB(), syncTableSample)
	if err != nil {
		t.Fatalf("runSampleSyncForTest() read sync state: %v", err)
	}

	if _, _, err = client.syncTableData(context.Background(), syncTableSample, state); err != nil {
		t.Fatalf("runSampleSyncForTest() sync: %v", err)
	}
}

func TestCountSampleSearchExcludesTrigramFalsePositiveLikeSearch(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with a trigram false positive for the term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Row 1 contains the literal ASCII substring "kelvin". Row 2 begins with
		// the Kelvin sign U+212A (spelled with an explicit rune escape so the
		// source bytes are unambiguous): the FTS5 trigram tokenizer Unicode-case-
		// folds U+212A to 'k', so an FTS5 MATCH for "kelvin" surfaces row 2 as a
		// candidate, but it does not contain the ASCII substring "kelvin" (SQLite
		// LIKE folds only ASCII A-Z). The count must apply the same LIKE
		// post-filter as the search and exclude row 2, so the count equals the
		// post-filtered search length, not the raw FTS5 MATCH candidate set.
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "kelvin sample", "supplier-1", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "Kelvin sample", "supplier-2", "common-2", "donor-2")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("the FTS5 MATCH alone surfaces both rows as candidates", func() {
			var matchedIDs []int64
			rows, qErr := cache.DB().Query(`SELECT rowid FROM sample_search WHERE sample_search MATCH ? ORDER BY rowid`, `"kelvin"`)
			convey.So(qErr, convey.ShouldBeNil)
			for rows.Next() {
				var id int64
				convey.So(rows.Scan(&id), convey.ShouldBeNil)
				matchedIDs = append(matchedIDs, id)
			}
			convey.So(rows.Close(), convey.ShouldBeNil)
			convey.So(matchedIDs, convey.ShouldResemble, []int64{1, 2})
		})

		count, err := client.CountSampleSearch(context.Background(), "kelvin")

		convey.Convey("when CountSampleSearch runs, then the LIKE post-filter narrows the count to the search length", func() {
			convey.So(err, convey.ShouldBeNil)

			samples, searchErr := client.SearchSamples(context.Background(), "kelvin", 1000, 0)
			convey.So(searchErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})
	})
}

// seedSampleMirrorSearchRow inserts a sample_mirror row letting the caller set
// the four searchable fields (name, supplier_name, common_name, donor_id)
// independently, so substring-search coverage can target each field. Callers
// must rebuildSampleSearchIndexForTest afterwards because sample_search is an
// external-content FTS5 table that is not auto-populated by direct inserts.
func seedSampleMirrorSearchRow(t *testing.T, db *sql.DB, id int64, name, supplierName, commonName, donorID string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		formatInt(id+100),
		"sample-uuid-"+formatInt(id),
		name,
		"sanger-"+formatInt(id),
		supplierName,
		"accession-"+formatInt(id),
		donorID,
		9606,
		commonName,
		"description-"+formatInt(id),
		formatSyncTime(time.Date(2026, time.May, 6, 16, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedSampleMirrorSearchRow(): %v", err)
	}
}

// rebuildSampleSearchIndexForTest repopulates the external-content FTS5
// sample_search table from sample_mirror, mirroring what the sample sync does.
func rebuildSampleSearchIndexForTest(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`INSERT INTO sample_search(sample_search) VALUES('rebuild')`); err != nil {
		t.Fatalf("rebuildSampleSearchIndexForTest(): %v", err)
	}
}

func sampleTmpIDs(samples []Sample) []int64 {
	ids := make([]int64, len(samples))
	for index, sample := range samples {
		ids[index] = sample.IDSampleTmp
	}

	return ids
}
