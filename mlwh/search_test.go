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
	"slices"
	"strings"
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

func TestSampleTokenPrefixBoundsComputesPrefixSuccessor(t *testing.T) {
	convey.Convey("Given sampleTokenPrefixBounds over representative search terms", t, func() {
		convey.Convey("when the term ends in an ordinary byte (donor), then the lower bound is the term and the upper increments only the last byte", func() {
			lower, upper, hasUpper := sampleTokenPrefixBounds("donor")
			convey.So(hasUpper, convey.ShouldBeTrue)
			convey.So(lower, convey.ShouldEqual, "donor")
			convey.So(upper, convey.ShouldEqual, "donos")
		})

		convey.Convey("when the term is mixed case, then the lower bound is lowercased (tokens are stored lowercased)", func() {
			lower, upper, hasUpper := sampleTokenPrefixBounds("AcMe")
			convey.So(hasUpper, convey.ShouldBeTrue)
			convey.So(lower, convey.ShouldEqual, "acme")
			convey.So(upper, convey.ShouldEqual, "acmf")
		})

		convey.Convey("when the term ends in 'z' or '9' (the top of the token byte range), then the successor is still the next byte", func() {
			_, zUpper, zHas := sampleTokenPrefixBounds("buzz")
			convey.So(zHas, convey.ShouldBeTrue)
			convey.So(zUpper, convey.ShouldEqual, "bu"+string([]byte{'z', 'z' + 1}))

			_, nineUpper, nineHas := sampleTokenPrefixBounds("rs9")
			convey.So(nineHas, convey.ShouldBeTrue)
			convey.So(nineUpper, convey.ShouldEqual, "rs"+string([]byte{'9' + 1}))
		})

		convey.Convey("when the term is empty, then there is no finite upper bound (degenerate open range)", func() {
			lower, upper, hasUpper := sampleTokenPrefixBounds("")
			convey.So(hasUpper, convey.ShouldBeFalse)
			convey.So(lower, convey.ShouldEqual, "")
			convey.So(upper, convey.ShouldEqual, "")
		})
	})
}

func TestBytePrefixSuccessorHandlesCarryDropAndDegenerateInput(t *testing.T) {
	convey.Convey("Given bytePrefixSuccessor over byte prefixes", t, func() {
		convey.Convey("when the last byte is below 0xFF, then only that byte is incremented", func() {
			successor, ok := bytePrefixSuccessor("donor")
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(successor, convey.ShouldEqual, "donos")
		})

		convey.Convey("when trailing bytes are 0xFF, then they are dropped and the increment carries to the last byte below 0xFF", func() {
			successor, ok := bytePrefixSuccessor(string([]byte{'a', 'b', 0xFF, 0xFF}))
			convey.So(ok, convey.ShouldBeTrue)
			convey.So(successor, convey.ShouldEqual, "ac")
		})

		convey.Convey("when every byte is 0xFF, then there is no finite successor", func() {
			successor, ok := bytePrefixSuccessor(string([]byte{0xFF, 0xFF, 0xFF}))
			convey.So(ok, convey.ShouldBeFalse)
			convey.So(successor, convey.ShouldEqual, "")
		})

		convey.Convey("when the prefix is empty, then there is no finite successor", func() {
			successor, ok := bytePrefixSuccessor("")
			convey.So(ok, convey.ShouldBeFalse)
			convey.So(successor, convey.ShouldEqual, "")
		})
	})
}

func TestSampleTokenPrefixQuerySelectsRangeOrOpenForm(t *testing.T) {
	convey.Convey("Given sampleTokenPrefixQuery choosing between the range and open-ended SQL", t, func() {
		convey.Convey("when the term has a finite successor, then the half-open range SQL and two bound args are returned", func() {
			query, bounds := sampleTokenPrefixQuery("acme", sampleSearchTokenPageSQL, sampleSearchTokenPageOpenSQL)
			convey.So(query, convey.ShouldEqual, sampleSearchTokenPageSQL)
			convey.So(bounds, convey.ShouldResemble, []any{"acme", "acmf"})
		})

		// An empty term is the reachable no-successor case (any [a-z0-9] token has a
		// successor); the open-ended `token >= ?` SQL with a single lower bound is
		// then chosen rather than a fabricated finite range.
		convey.Convey("when the term has no finite successor, then the open-ended SQL and a single lower-bound arg are returned", func() {
			query, bounds := sampleTokenPrefixQuery("", sampleSearchCountSQL, sampleSearchCountOpenSQL)
			convey.So(query, convey.ShouldEqual, sampleSearchCountOpenSQL)
			convey.So(bounds, convey.ShouldResemble, []any{""})
		})
	})
}

// TestSampleSearchQueryBoundsTokenisesTermLikeStoredValues pins the query-token
// derivation that drives the AND search: a term is split into the same distinct
// lowercased [a-z0-9] words as stored values (sampleSearchTokens), duplicate
// tokens collapse, and a term made only of separators or non-ASCII yields no
// bounds (nothing to query). Each emitted bound is the half-open [token, successor)
// range, so every bound's Upper exists (the input is always [a-z0-9]) and the
// invalid-UTF-8 successor case never arises.
func TestSampleSearchQueryBoundsTokenisesTermLikeStoredValues(t *testing.T) {
	convey.Convey("Given sampleSearchQueryBounds over representative terms", t, func() {
		convey.Convey("when the term is a single [a-z0-9] word, then one [token, successor) bound is returned", func() {
			bounds := sampleSearchQueryBounds("mus")
			convey.So(bounds, convey.ShouldResemble, []sampleSearchTokenBound{{Lower: "mus", Upper: "mut"}})
		})

		convey.Convey("when the term splits on a non-token byte (Hek_R1), then one bound per distinct word is returned", func() {
			bounds := sampleSearchQueryBounds("Hek_R1")
			convey.So(bounds, convey.ShouldResemble, []sampleSearchTokenBound{
				{Lower: "hek", Upper: "hel"},
				{Lower: "r1", Upper: "r2"},
			})
		})

		convey.Convey("when a word repeats, then the duplicate token collapses to one bound", func() {
			bounds := sampleSearchQueryBounds("mus mus")
			convey.So(bounds, convey.ShouldResemble, []sampleSearchTokenBound{{Lower: "mus", Upper: "mut"}})
		})

		// A non-ASCII term tokenises to its [a-z0-9] runs (here "caf"); the é is a
		// separator, so no bound is ever formed from a non-ASCII byte and
		// bytePrefixSuccessor only sees ASCII.
		convey.Convey("when the term carries a non-ASCII rune (café), then only its [a-z0-9] token yields a bound", func() {
			bounds := sampleSearchQueryBounds("café")
			convey.So(bounds, convey.ShouldResemble, []sampleSearchTokenBound{{Lower: "caf", Upper: "cag"}})
		})

		convey.Convey("when the term is all separators or all non-ASCII, then no bounds are returned (nothing to query)", func() {
			convey.So(sampleSearchQueryBounds("___"), convey.ShouldBeEmpty)
			convey.So(sampleSearchQueryBounds("ÿÿÿ"), convey.ShouldBeEmpty)
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

// TestSearchSamplesMatchesMultiTokenSupplierName is the 260627-6 regression: a
// term that the tokeniser splits into several words (a supplier_name like
// "Hek_R1" -> tokens "hek","r1") must match a sample that has a word-prefix for
// EVERY query token, even though the term is neither a single token nor a single
// token prefix. Before the fix SearchSamples/CountSampleSearch short-circuited
// such a term (its '_' lies outside [a-z0-9]) to an empty result and never
// queried, so the sample the user could resolve by `wa mlwh info Hek_R1` was
// invisible to search.
func TestSearchSamplesMatchesMultiTokenSupplierName(t *testing.T) {
	convey.Convey("Given a synced SQLite cache holding the Hek_R1 sample and decoys", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// id 1 is the target: supplier_name "Hek_R1" tokenises to "hek" + "r1".
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "7607STDY14643771", "Hek_R1", "Homo sapiens", "donor-1")
		// id 2 has a "hek*" word but no "r1*" word, so the AND must reject it.
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-2", "HEK293-clone", "Homo sapiens", "donor-2")
		// id 3 has an "r1*" word but no "hek*" word, so the AND must reject it too.
		seedSampleMirrorSearchRow(t, cache.DB(), 3, "name-3", "R1-batch", "Mus musculus", "donor-3")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when SearchSamples runs for the literal supplier_name Hek_R1, then only the sample with both words matches", func() {
			samples, err := client.SearchSamples(context.Background(), "Hek_R1", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})
		})

		convey.Convey("when CountSampleSearch runs for Hek_R1, then it counts exactly the one sample matching all tokens", func() {
			count, err := client.CountSampleSearch(context.Background(), "Hek_R1")
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})

			samples, searchErr := client.SearchSamples(context.Background(), "Hek_R1", 1000, 0)
			convey.So(searchErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})
	})
}

// TestSearchSamplesMultiWordTermRequiresEveryTokenAsWordPrefix locks in the
// logical-AND, word-prefix semantics for a natural two-word term: "Mus muscu"
// tokenises to "mus","muscu" and must match a "Mus musculus" sample (a word
// prefix-matches each token) while a sample that only satisfies one token is
// rejected. Single-word behaviour ("mus" alone) is unchanged.
func TestSearchSamplesMultiWordTermRequiresEveryTokenAsWordPrefix(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with multi-word common_name fixtures", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// id 1 "Mus musculus" -> "mus","musculus": prefix-matches both "mus" and
		// "muscu".
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "name-1", "supplier-1", "Mus musculus", "donor-1")
		// id 2 "Mus spretus" -> "mus","spretus": prefix-matches "mus" but not
		// "muscu", so the two-word AND rejects it.
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-2", "supplier-2", "Mus spretus", "donor-2")
		// id 3 "Homo sapiens" -> matches neither token.
		seedSampleMirrorSearchRow(t, cache.DB(), 3, "name-3", "supplier-3", "Homo sapiens", "donor-3")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when the term is two words (Mus muscu), then only the sample with a word-prefix for each token matches", func() {
			samples, err := client.SearchSamples(context.Background(), "Mus muscu", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})

			count, countErr := client.CountSampleSearch(context.Background(), "Mus muscu")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})

		convey.Convey("when the term is a single word (mus), then every Mus sample still matches (single-word behaviour preserved)", func() {
			samples, err := client.SearchSamples(context.Background(), "mus", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1, 2})
		})
	})
}

// TestSearchSamplesMultiWordCountEqualsPagedResults proves CountSampleSearch
// agrees with len(SearchSamples(...all)) for a multi-word term across a larger
// match set, so the multi-token count uses the same AND as the multi-token page.
func TestSearchSamplesMultiWordCountEqualsPagedResults(t *testing.T) {
	convey.Convey("Given a synced SQLite cache where several samples share two query words", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// Five samples carry both "hek" and "r1" words; one carries only "hek".
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "name-1", "Hek_R1", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-2", "Hek_R1_a", "common-2", "donor-2")
		seedSampleMirrorSearchRow(t, cache.DB(), 3, "hek R1", "supplier-3", "common-3", "donor-3")
		seedSampleMirrorSearchRow(t, cache.DB(), 4, "name-4", "supplier-4", "hek r1cell", "donor-4")
		seedSampleMirrorSearchRow(t, cache.DB(), 5, "name-5", "supplier-5", "common-5", "hek-r1")
		seedSampleMirrorSearchRow(t, cache.DB(), 6, "name-6", "HEK293", "common-6", "donor-6")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when CountSampleSearch and the full SearchSamples run for hek r1, then the count equals the row-set size", func() {
			samples, err := client.SearchSamples(context.Background(), "hek r1", 1000, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1, 2, 3, 4, 5})

			count, countErr := client.CountSampleSearch(context.Background(), "hek r1")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 5})
			convey.So(count.Count, convey.ShouldEqual, len(samples))
		})

		convey.Convey("when SearchSamples pages a multi-word term (limit 2 offset 1), then it returns the second page in id order", func() {
			samples, err := client.SearchSamples(context.Background(), "hek r1", 2, 1)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{2, 3})
		})
	})
}

// TestSearchSamplesNonASCIITermSearchesItsAsciiTokens proves the new tokenised
// behaviour: a non-ASCII term is tokenised the same way stored values are, so
// "café" searches its token "caf" (matching a sample with a "caf*" word) while a
// term that tokenises to nothing ("ÿ", "___") returns empty without error and
// without ever fabricating an invalid-UTF-8 bound.
func TestSearchSamplesNonASCIITermSearchesItsAsciiTokens(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with a sample carrying a caf* word", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 1, "cafeteria-sample", "supplier-1", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "name-2", "supplier-2", "common-2", "donor-2")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when the term is café (token caf), then the sample with a caf* word matches", func() {
			samples, err := client.SearchSamples(context.Background(), "café", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})

			count, countErr := client.CountSampleSearch(context.Background(), "café")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})

		// A term whose runes are all separators or non-ASCII yields zero query
		// tokens, so there is nothing to query: the search returns empty without
		// error, and bytePrefixSuccessor is never asked to increment a non-ASCII
		// byte (no invalid-UTF-8 bound).
		for _, term := range []string{"ÿ", "ÿÿÿ", "___"} {
			convey.Convey("when the term "+term+" tokenises to nothing, then SearchSamples returns empty with no error", func() {
				samples, err := client.SearchSamples(context.Background(), term, 100, 0)
				convey.So(err, convey.ShouldBeNil)
				convey.So(samples, convey.ShouldBeEmpty)
			})

			convey.Convey("when the term "+term+" tokenises to nothing, then CountSampleSearch returns Count 0 with no error", func() {
				count, err := client.CountSampleSearch(context.Background(), term)
				convey.So(err, convey.ShouldBeNil)
				convey.So(count, convey.ShouldResemble, Count{})
			})
		}
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

func TestSearchSamplesMatchesWordPrefixNotMidWord(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose samples carry multi-word searchable fields", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// "Mus Musculus" tokenises to the words "mus" and "musculus".
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "specimen-1", "supplier-1", "Mus Musculus", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "specimen-2", "supplier-2", "Homo Sapiens", "donor-2")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when the term is a whole later word (musculus), then the Mus Musculus sample matches", func() {
			samples, err := client.SearchSamples(context.Background(), "musculus", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})
		})

		convey.Convey("when the term is a prefix of the first word (mus), then the Mus Musculus sample matches", func() {
			samples, err := client.SearchSamples(context.Background(), "mus", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})
		})

		convey.Convey("when the term is a mid-word substring (usculus), then it does not match (accepted word-prefix semantics)", func() {
			samples, err := client.SearchSamples(context.Background(), "usculus", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldBeEmpty)

			count, countErr := client.CountSampleSearch(context.Background(), "usculus")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{})
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

func TestSearchSamplesTreatsQueryWildcardsAndOperatorsAsTokenSeparatorsNotSQLWildcards(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose tokens are plain words and a query carrying LIKE/operator characters", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// supplier_name "abcXYZ" tokenises to the single word "abcxyz"; a second
		// sample carries an unrelated word so a wildcard char cannot be smuggled
		// through as a SQL wildcard matching everything.
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "specimen-1", "abcXYZ", "common-1", "donor-1")
		seedSampleMirrorSearchRow(t, cache.DB(), 2, "specimen-2", "zzzzz", "common-2", "donor-2")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when the term is a plain prefix (abc), then the word-prefix token matches", func() {
			samples, err := client.SearchSamples(context.Background(), "abc", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})
		})

		// '%' is a token separator, not a SQL LIKE wildcard: "abc%" tokenises to the
		// single word "abc", which prefix-matches "abcxyz" (and nothing else), so it
		// must not act as a wildcard matching every sample.
		convey.Convey("when the term embeds a percent (abc%), then it tokenises to abc and matches only the abc-prefixed sample", func() {
			samples, err := client.SearchSamples(context.Background(), "abc%", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})

			count, countErr := client.CountSampleSearch(context.Background(), "abc%")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})

		// Likewise the underscore is a token separator: "ab_" tokenises to the word
		// "ab", which word-prefix-matches "abcxyz" - it is NOT treated as the LIKE
		// single-character wildcard.
		convey.Convey("when the term embeds an underscore (ab_), then it tokenises to ab and matches the ab-prefixed sample", func() {
			samples, err := client.SearchSamples(context.Background(), "ab_", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})
		})
	})
}

func TestSearchSamplesPunctuationInTermSeparatesTokens(t *testing.T) {
	convey.Convey("Given a synced SQLite cache and a term containing a non-token punctuation byte", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// "abc" is an ordinary alphanumeric token of supplier_name "abc supplier".
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "specimen-1", "abc supplier", "common-1", "donor-1")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		// A punctuation byte that cannot appear in a [a-z0-9] token (here '!') is a
		// token separator, so "ab!" tokenises to the single word "ab", the query
		// stays well-formed (an index range seek, no LIKE), and it word-prefix-matches
		// the sample's "abc" word.
		convey.Convey("when the term embeds a non-token punctuation byte (ab!), then it tokenises to ab and matches the abc-prefixed sample", func() {
			samples, err := client.SearchSamples(context.Background(), "ab!", 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, []int64{1})

			count, countErr := client.CountSampleSearch(context.Background(), "ab!")
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})
	})
}

func TestSearchSamplesNonASCIITermReturnsEmptyWithoutBadBound(t *testing.T) {
	convey.Convey("Given a synced SQLite cache with an ordinary alphanumeric sample and a term with no usable [a-z0-9] tokens", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSampleMirrorSearchRow(t, cache.DB(), 1, "specimen-1", "abc supplier", "common-1", "donor-1")
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		// "ÿÿÿ" is all non-ASCII runes, so it tokenises to zero [a-z0-9] tokens:
		// there is nothing to query and the search short-circuits to an empty
		// result before any byte-prefix bound is computed. Incrementing the last
		// raw byte of such a term ("ÿ" -> C3 BF -> C3 C0) would otherwise produce
		// an invalid-UTF-8 upper bound that MySQL could reject; this proves no such
		// bound is generated and no error surfaces on either backend.
		const term = "ÿÿÿ"

		convey.Convey("when SearchSamples runs with the zero-token non-ASCII term "+term+", then it returns empty with no error", func() {
			samples, err := client.SearchSamples(context.Background(), term, 100, 0)
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldBeEmpty)
		})

		convey.Convey("when CountSampleSearch runs with the zero-token non-ASCII term "+term+", then it returns Count 0 with no error", func() {
			count, err := client.CountSampleSearch(context.Background(), term)
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{})
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

func TestCountSampleSearchBoundedByCap(t *testing.T) {
	convey.Convey("Given a synced SQLite cache where many distinct samples share a common token prefix", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// One sample below the cap proves the count is exact for normal sets.
		seedSampleMirrorSearchRow(t, cache.DB(), 1, "specimen-1", "ZEBRA-001", "common-1", "donor-1")

		// A block of distinct samples sharing the "homo" token, exceeding the
		// bounded count cap, proves the count stops at the cap (a floor) rather
		// than scanning every matching row.
		over := sampleSearchCountCap + 5
		for id := 2; id <= over+1; id++ {
			seedSampleMirrorSearchRow(t, cache.DB(), int64(id), "specimen-"+formatInt(int64(id)), "supplier-"+formatInt(int64(id)), "Homo sapiens", "donor-"+formatInt(int64(id)))
		}
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when the matching set is small, then CountSampleSearch is exact", func() {
			count, err := client.CountSampleSearch(context.Background(), "zebra")
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: 1})
		})

		convey.Convey("when the matching set exceeds the cap, then CountSampleSearch reports the cap as a floor (fast, not a full scan)", func() {
			count, err := client.CountSampleSearch(context.Background(), "homo")
			convey.So(err, convey.ShouldBeNil)
			convey.So(count, convey.ShouldResemble, Count{Count: sampleSearchCountCap})
		})
	})
}

func TestColdLoadTokenRebuildPagesMirrorAndKeepsEverySampleSearchable(t *testing.T) {
	convey.Convey("Given a synced SQLite cache whose sample_mirror spans many id-range pages of the cold-load token rebuild", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// A tiny read page size forces the rebuild to read sample_mirror in many
		// id-range pages, so the page cursor (lastID) and page boundaries are
		// exercised rather than a single full scan. This is the structure that
		// fails on MySQL at scale when a streaming result set is held open while
		// inserting on the same connection; here it pins paged correctness.
		withSampleSearchTokenReadPageSizeForTest(t, 2)

		// Non-contiguous, out-of-insertion-order ids (with gaps) prove the rebuild
		// orders by id_sample_tmp and advances strictly past the page's max id: a
		// naive contiguous-id or OFFSET assumption, or an off-by-one cursor, would
		// drop or double-process rows at a page boundary.
		ids := []int64{50, 3, 17, 4, 99, 5, 18, 100, 2, 64}

		// Each sample gets a globally unique, equal-length, collision-free word
		// token ("uniqa", "uniqb", ...) so a prefix search for one sample's token
		// matches that sample alone (no token is a prefix of another), plus a shared
		// "homo" token. uniqueToken pairs an id with its unique token.
		uniqueToken := func(id int64) string {
			for index, candidate := range ids {
				if candidate == id {
					return "uniq" + string(rune('a'+index))
				}
			}

			return ""
		}
		for _, id := range ids {
			seedSampleMirrorSearchRow(t, cache.DB(), id, "specimen-"+formatInt(id), uniqueToken(id), "Homo sapiens", "donor-"+formatInt(id))
		}
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when each sample is searched by its unique token, then every sample across every page is found", func() {
			missing := make([]int64, 0)
			for _, id := range ids {
				samples, err := client.SearchSamples(context.Background(), uniqueToken(id), 100, 0)
				convey.So(err, convey.ShouldBeNil)
				if len(samples) != 1 || samples[0].IDSampleTmp != id {
					missing = append(missing, id)
				}
			}
			convey.So(missing, convey.ShouldBeEmpty)
		})

		convey.Convey("when the shared token is searched, then all samples from all pages are returned in id order", func() {
			samples, err := client.SearchSamples(context.Background(), "homo", 100, 0)
			convey.So(err, convey.ShouldBeNil)

			want := append([]int64(nil), ids...)
			slices.Sort(want)
			convey.So(sampleTmpIDs(samples), convey.ShouldResemble, want)
		})

		convey.Convey("when the distinct owners in the token table are counted, then every page's samples are present", func() {
			// Counting the distinct id_sample_tmp owners proves no page was skipped
			// and no page boundary dropped a sample's tokens: each seeded sample must
			// own at least one token row.
			distinct := countRows(t, cache.DB(), `SELECT COUNT(DISTINCT id_sample_tmp) FROM `+sampleSearchTokenTable)
			convey.So(distinct, convey.ShouldEqual, len(ids))
		})
	})
}

// TestSampleSearchTokenQueriesSeekIndexRangeNotFullScan locks in the
// performance fix as an observable contract via SQLite's query planner: the
// sample token page query and the bounded count query must resolve the
// token-prefix predicate as an index RANGE SEARCH on sample_search_token_idx
// (detail carries "SEARCH", the index name, and the "token>?"/"token<?" range
// bounds), never the whole-index "SCAN" the old `token LIKE 'prefix%' ESCAPE
// '!'` predicate produced (SQLite's case-insensitive LIKE cannot use the
// BINARY-collated index, so it scanned the full covering index ~700-825ms on a
// 6M-token cache). Asserting the EXPLAIN QUERY PLAN is the legitimate
// behavioural proxy for "uses the index range, not a full scan" - the same kind
// of check as asserting a query uses a named index - because the wall-clock
// contract is exactly "index seek, not full scan". This test fails on the old
// LIKE SQL (a "SCAN ... USING COVERING INDEX" with no range bounds) and passes
// on the range SQL.
func TestSampleSearchTokenQueriesSeekIndexRangeNotFullScan(t *testing.T) {
	convey.Convey("Given a synced SQLite cache reachable through its read handle", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		// A spread of tokens so the planner has a populated index to reason about;
		// the chosen plan is independent of the bound values.
		for id := 1; id <= 64; id++ {
			seedSampleMirrorSearchRow(t, cache.DB(), int64(id), "specimen-"+formatInt(int64(id)), "ACME-"+formatInt(int64(id)), "Homo sapiens", "donor-"+formatInt(int64(id)))
		}
		rebuildSampleSearchIndexForTest(t, cache.DB())
		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		readDB := cacheReadDB(cache)
		convey.So(readDB, convey.ShouldNotBeNil)

		lower, upper, hasUpper := sampleTokenPrefixBounds("acme")
		convey.So(hasUpper, convey.ShouldBeTrue)

		convey.Convey("when the token page query is planned, then it is an index range SEARCH using sample_search_token_idx, not a full SCAN", func() {
			details := explainQueryPlanDetails(t, readDB, sampleSearchTokenPageSQL,
				lower, upper, 100*sampleSearchTokenPageMultiplier+sampleSearchTokenPageMargin, 0)

			convey.So(planUsesIndexRangeSearch(details), convey.ShouldBeTrue)
			convey.So(planHasFullTokenScan(details), convey.ShouldBeFalse)
		})

		convey.Convey("when the bounded count query is planned, then its inner token scan is an index range SEARCH using sample_search_token_idx, not a full SCAN", func() {
			details := explainQueryPlanDetails(t, readDB, sampleSearchCountSQL, lower, upper, sampleSearchCountCap)

			convey.So(planUsesIndexRangeSearch(details), convey.ShouldBeTrue)
			convey.So(planHasFullTokenScan(details), convey.ShouldBeFalse)
		})
	})
}

// seedSampleMirrorSearchRow inserts a sample_mirror row letting the caller set
// the four searchable fields (name, supplier_name, common_name, donor_id)
// independently, so word-prefix-search coverage can target each field. Callers
// must rebuildSampleSearchIndexForTest afterwards because sample_search_token is
// a derived prefix index that direct sample_mirror inserts do not populate.
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

// rebuildSampleSearchIndexForTest repopulates the SQLite sample_search_token
// prefix index from sample_mirror via the same code path the sample sync uses,
// so seeded sample_mirror rows become word-prefix searchable.
func rebuildSampleSearchIndexForTest(t *testing.T, db *sql.DB) {
	t.Helper()

	rebuildSampleSearchIndexForTestDialect(t, db, "sqlite")
}

// rebuildSampleSearchIndexForTestDialect rebuilds the sample_search_token prefix
// index for the given dialect, so both SQLite and MySQL parity fixtures become
// searchable after direct sample_mirror inserts.
func rebuildSampleSearchIndexForTestDialect(t *testing.T, db *sql.DB, dialect string) {
	t.Helper()

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("rebuildSampleSearchIndexForTest() begin: %v", err)
	}

	if err = rebuildSampleSearchTokenIndex(context.Background(), tx, dialect); err != nil {
		_ = tx.Rollback()
		t.Fatalf("rebuildSampleSearchIndexForTest(): %v", err)
	}

	if err = tx.Commit(); err != nil {
		t.Fatalf("rebuildSampleSearchIndexForTest() commit: %v", err)
	}
}

// explainQueryPlanDetails returns the detail column of every EXPLAIN QUERY PLAN
// row for query under the given bind args, the SQLite planner's description of
// how each table/index is accessed.
func explainQueryPlanDetails(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()

	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explainQueryPlanDetails(): %v", err)
	}
	defer func() { _ = rows.Close() }()

	details := make([]string, 0)
	for rows.Next() {
		var (
			id, parent, notUsed int
			detail              string
		)
		if scanErr := rows.Scan(&id, &parent, &notUsed, &detail); scanErr != nil {
			t.Fatalf("explainQueryPlanDetails() scan: %v", scanErr)
		}

		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("explainQueryPlanDetails() rows: %v", err)
	}

	return details
}

// planUsesIndexRangeSearch reports whether any plan row is an index range SEARCH
// on sample_search_token_idx bounded on both sides of token (SQLite renders the
// half-open `token >= ? AND token < ?` range as "token>? AND token<?").
func planUsesIndexRangeSearch(details []string) bool {
	for _, detail := range details {
		if strings.Contains(detail, "SEARCH") &&
			strings.Contains(detail, sampleSearchTokenIndex.Name) &&
			strings.Contains(detail, "token>?") &&
			strings.Contains(detail, "token<?") {
			return true
		}
	}

	return false
}

// planHasFullTokenScan reports whether any plan row is a full SCAN of
// sample_search_token (the old LIKE predicate's whole-covering-index scan),
// which the index range must avoid.
func planHasFullTokenScan(details []string) bool {
	for _, detail := range details {
		if strings.HasPrefix(detail, "SCAN "+sampleSearchTokenTable) {
			return true
		}
	}

	return false
}

func sampleTmpIDs(samples []Sample) []int64 {
	ids := make([]int64, len(samples))
	for index, sample := range samples {
		ids[index] = sample.IDSampleTmp
	}

	return ids
}
