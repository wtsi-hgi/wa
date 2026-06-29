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
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// E1 acceptance test 3: a whitespace-only :name over HTTP is a 400 bad_request,
// raised in the handler BEFORE the queryer (proven with a panic-on-call fake).
func TestStudiesForFacultySponsorWhitespaceNameIsBadRequestE1(t *testing.T) {
	convey.Convey("Given a server over a fake Queryer that panics if its faculty-sponsor methods are reached", t, func() {
		queryer := &serverFakeQueryer{}

		convey.Convey("when GET /studies/faculty-sponsor/%20 (whitespace-only) is served, then 400 bad_request and the queryer is not reached", func() {
			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/studies/faculty-sponsor/%20")

			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})

		convey.Convey("when GET /studies/faculty-sponsor/%20/count (whitespace-only) is served, then 400 bad_request and the queryer is not reached", func() {
			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/studies/faculty-sponsor/%20/count")

			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})
}

// E2 acceptance test 5: a whitespace-only :person over HTTP is a 400 bad_request,
// raised in the handler BEFORE the queryer (proven with a panic-on-call fake).
func TestStudiesForUserWhitespacePersonIsBadRequestE2(t *testing.T) {
	convey.Convey("Given a server over a fake Queryer that panics if its user methods are reached", t, func() {
		queryer := &serverFakeQueryer{}

		convey.Convey("when GET /studies/user/%20 (whitespace-only) is served, then 400 bad_request and the queryer is not reached", func() {
			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/studies/user/%20")

			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})

		convey.Convey("when GET /studies/user/%20/count (whitespace-only) is served, then 400 bad_request and the queryer is not reached", func() {
			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/studies/user/%20/count")

			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})
}

// E3 acceptance test 3: two distinct sponsors "Carl Anderson" and "Carla Anders"
// both match "carl" (disambiguation), so both faculty_sponsor candidates appear,
// and CountResolvePerson("carl") counts EVERY distinct candidate.
func TestResolvePersonDisambiguatesMultipleSponsorsE3(t *testing.T) {
	convey.Convey("Given two distinct faculty_sponsors Carl Anderson (x2) and Carla Anders (x1)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "Carl Anderson")
		seedStudyMirrorSearchRow(t, cache.DB(), 2, "5002", "study-b", "Title B", "Programme B", "Carl Anderson")
		seedStudyMirrorSearchRow(t, cache.DB(), 3, "5003", "study-c", "Title C", "Programme C", "Carla Anders")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableStudyUsers, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson(\"carl\", all) runs, then both faculty_sponsor candidates appear with their distinct study counts", func() {
			candidates, err := client.ResolvePerson(context.Background(), "carl", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(candidates, convey.ShouldResemble, []PersonCandidate{
				{Source: "faculty_sponsor", Name: "Carl Anderson", StudyCount: 2},
				{Source: "faculty_sponsor", Name: "Carla Anders", StudyCount: 1},
			})
		})

		convey.Convey("when CountResolvePerson(\"carl\") runs, then it counts every distinct candidate (2) == len(list)", func() {
			count, err := client.CountResolvePerson(context.Background(), "carl")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 2)
		})
	})
}

// E3 acceptance test 4: a whitespace-only :term over HTTP is a 400 bad_request,
// raised in the handler BEFORE the queryer (proven with a panic-on-call fake).
func TestResolvePersonWhitespaceTermIsBadRequestE3(t *testing.T) {
	convey.Convey("Given a server over a fake Queryer that panics if its resolve-person methods are reached", t, func() {
		queryer := &serverFakeQueryer{}

		convey.Convey("when GET /resolve-person/%20 (whitespace-only) is served, then 400 bad_request and the queryer is not reached", func() {
			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/resolve-person/%20")

			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})

		convey.Convey("when GET /resolve-person/%20/count (whitespace-only) is served, then 400 bad_request and the queryer is not reached", func() {
			response := performMLWHRequestForTest(t, queryer, http.MethodGet, "/resolve-person/%20/count")

			assertMLWHErrorEnvelopeForTest(t, response, http.StatusBadRequest, "bad_request")
		})
	})
}

// E1 acceptance test 1: a "carl" term matches the three Carl studies
// case-insensitively (substring), each PersonStudy carries the FULL Study and an
// empty Role, and CountStudiesForFacultySponsor("carl") is 3 (count == len(list)).
func TestStudiesForFacultySponsorMatchesCaseInsensitiveSubstringE1(t *testing.T) {
	convey.Convey("Given studies with faculty_sponsor Carl Anderson (x2), carl anderson (x1) and Jane Doe (x1)", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedFacultySponsorStudies(t, cache)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForFacultySponsor(\"carl\", all) runs, then the 3 Carl studies are returned with full Study and empty Role", func() {
			rows, err := client.StudiesForFacultySponsor(context.Background(), "carl", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002", "5003"})

			// Each row carries the FULL study row (not just an id) and an empty Role.
			convey.So(rows[0].Study.Name, convey.ShouldEqual, "study-a")
			convey.So(rows[0].Study.FacultySponsor, convey.ShouldEqual, "Carl Anderson")
			convey.So(rows[0].Study.StudyTitle, convey.ShouldEqual, "Title A")
			convey.So(rows[2].Study.FacultySponsor, convey.ShouldEqual, "carl anderson")
			for _, row := range rows {
				convey.So(row.Role, convey.ShouldBeEmpty)
			}
		})

		convey.Convey("when CountStudiesForFacultySponsor(\"carl\") runs, then it is 3 (count == len(list))", func() {
			count, err := client.CountStudiesForFacultySponsor(context.Background(), "carl")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 3)
		})
	})
}

// E1 acceptance test 2: over HTTP, limit=2&offset=0 with 3 matches returns 2 rows
// with X-Total-Count: 3 and X-Next-Offset: 2.
func TestStudiesForFacultySponsorHTTPPaginationHeadersE1(t *testing.T) {
	convey.Convey("Given a server over a Client whose cache has 3 Carl-sponsor studies", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedFacultySponsorStudies(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when GET /studies/faculty-sponsor/carl?limit=2&offset=0 is served, then 2 rows with X-Total-Count 3 and X-Next-Offset 2", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/studies/faculty-sponsor/carl?limit=2&offset=0")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "3")
			convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "2")

			var rows []PersonStudy
			decodeMLWHJSONResponseForTest(t, response, &rows)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002"})
		})
	})
}

// E1 acceptance test 3 (Client defensive re-validation): the Client method itself
// rejects a whitespace-only name with ErrUnsupportedIdentifier so a direct caller
// is not silently wrong.
func TestStudiesForFacultySponsorClientRevalidatesWhitespaceE1(t *testing.T) {
	convey.Convey("Given a synced cache and a Client", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedFacultySponsorStudies(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForFacultySponsor is called with a whitespace-only name, then ErrUnsupportedIdentifier", func() {
			_, err := client.StudiesForFacultySponsor(context.Background(), "   ", 100, 0)
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})

		convey.Convey("when CountStudiesForFacultySponsor is called with a whitespace-only name, then ErrUnsupportedIdentifier", func() {
			_, err := client.CountStudiesForFacultySponsor(context.Background(), "   ")
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})
	})
}

// E1 acceptance test 4: a never-synced cache returns an EMPTY list plus an error
// satisfying both ErrCacheNeverSynced and ErrNotFound; a synced cache with no
// match returns an empty list and NO error (the people-endpoint cascade).
func TestStudiesForFacultySponsorPeopleCascadeE1(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForFacultySponsor runs, then an empty list and both sentinels", func() {
			rows, err := client.StudiesForFacultySponsor(context.Background(), "carl", 100, 0)

			convey.So(rows, convey.ShouldResemble, []PersonStudy{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})

		convey.Convey("when CountStudiesForFacultySponsor runs, then Count{} and both sentinels", func() {
			count, err := client.CountStudiesForFacultySponsor(context.Background(), "carl")

			convey.So(count, convey.ShouldResemble, Count{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a synced SQLite cache whose studies do not match the term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedFacultySponsorStudies(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForFacultySponsor runs for a non-matching term, then an empty list and NO error", func() {
			rows, err := client.StudiesForFacultySponsor(context.Background(), "zzz-nobody", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, []PersonStudy{})
		})

		convey.Convey("when CountStudiesForFacultySponsor runs for a non-matching term, then 0 and NO error", func() {
			count, err := client.CountStudiesForFacultySponsor(context.Background(), "zzz-nobody")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 0)
		})
	})
}

// seedFacultySponsorStudies seeds the four-study faculty-sponsor fixture used by
// E1's acceptance tests: three studies whose faculty_sponsor contains "Carl"
// (two "Carl Anderson", one lower-case "carl anderson") plus a "Jane Doe"
// non-match, then marks the study table synced.
func seedFacultySponsorStudies(t *testing.T, cache Cache) {
	t.Helper()

	seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "Carl Anderson")
	seedStudyMirrorSearchRow(t, cache.DB(), 2, "5002", "study-b", "Title B", "Programme B", "Carl Anderson")
	seedStudyMirrorSearchRow(t, cache.DB(), 3, "5003", "study-c", "Title C", "Programme C", "carl anderson")
	seedStudyMirrorSearchRow(t, cache.DB(), 4, "5004", "study-d", "Title D", "Programme D", "Jane Doe")
	seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
}

// E2 acceptance test 1: a login "ca3" with the default role set returns the
// studies the person owns or manages (X,Y owner, Z manager) but NOT the one they
// merely follow (W), each PersonStudy carrying its matched Role, and
// CountStudiesForUser("ca3","") is 3 (count == len(list)).
func TestStudiesForUserDefaultRolesExcludeFollowerE2(t *testing.T) {
	convey.Convey("Given study_users_mirror rows for ca3 (owner of X,Y; manager of Z; follower of W) and study_mirror rows for X,Y,Z,W", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUserStudies(t, cache)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser(\"ca3\", \"\", all) runs, then X,Y,Z are returned (owner/manager) but NOT W (follower), each with its matched Role", func() {
			rows, err := client.StudiesForUser(context.Background(), "ca3", "", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002", "5003"})
			convey.So(peopleStudyRoles(rows), convey.ShouldResemble, []string{"owner", "owner", "manager"})

			// Each row carries the FULL study row, not just an id.
			convey.So(rows[0].Study.Name, convey.ShouldEqual, "study-a")
			convey.So(rows[0].Study.StudyTitle, convey.ShouldEqual, "Title A")
		})

		convey.Convey("when CountStudiesForUser(\"ca3\", \"\") runs, then it is 3 (count == len(list))", func() {
			count, err := client.CountStudiesForUser(context.Background(), "ca3", "")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 3)
		})
	})
}

// E2 acceptance test 2: the same person resolves by email and by name substring,
// not just by login; both return the same three default-role studies.
func TestStudiesForUserMatchesAcrossLoginEmailNameE2(t *testing.T) {
	convey.Convey("Given the ca3 user fixture", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUserStudies(t, cache)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser is called by email \"ca3@sanger.ac.uk\", then the same 3 default-role studies are returned", func() {
			rows, err := client.StudiesForUser(context.Background(), "ca3@sanger.ac.uk", "", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002", "5003"})
		})

		convey.Convey("when StudiesForUser is called by name substring \"anderson\", then the same 3 default-role studies are returned", func() {
			rows, err := client.StudiesForUser(context.Background(), "anderson", "", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002", "5003"})
		})
	})
}

// E2 acceptance test 3: a non-empty role= overrides (replaces) the default set,
// so role=follower returns only the followed study W; the count honours it too.
func TestStudiesForUserRoleOverrideReplacesDefaultSetE2(t *testing.T) {
	convey.Convey("Given the ca3 user fixture", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUserStudies(t, cache)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser(\"ca3\", \"follower\", all) runs, then only W is returned (override replaces the default set), with Role=follower", func() {
			rows, err := client.StudiesForUser(context.Background(), "ca3", "follower", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5004"})
			convey.So(peopleStudyRoles(rows), convey.ShouldResemble, []string{"follower"})
		})

		convey.Convey("when CountStudiesForUser(\"ca3\", \"follower\") runs, then it is 1 (count == len(list))", func() {
			count, err := client.CountStudiesForUser(context.Background(), "ca3", "follower")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 1)
		})

		convey.Convey("when StudiesForUser is called with a mixed-case, spaced override \"Owner, Manager\", then it matches owner+manager case-insensitively (X,Y,Z)", func() {
			rows, err := client.StudiesForUser(context.Background(), "ca3", "Owner, Manager", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002", "5003"})
		})
	})
}

// E2 acceptance test 4: a person who is BOTH owner and data_access_contact of the
// same study X appears TWICE under the default roles (once per matched role), and
// the count is 2 (distinct (study, role)).
func TestStudiesForUserSameStudyMultipleRolesDeduplicatesE2(t *testing.T) {
	convey.Convey("Given a person who is both owner and data_access_contact of study X", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "")
		seedStudyUsersMirrorRow(t, cache.DB(), 8001, 1, "owner", "dc9", "dc9@sanger.ac.uk", "Dee Contact")
		seedStudyUsersMirrorRow(t, cache.DB(), 8002, 1, "data_access_contact", "dc9", "dc9@sanger.ac.uk", "Dee Contact")
		// A duplicate (same study, same role) must collapse to one row.
		seedStudyUsersMirrorRow(t, cache.DB(), 8003, 1, "owner", "dc9", "dc9@sanger.ac.uk", "Dee Contact")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableStudyUsers, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser(\"dc9\", \"\", all) runs, then X appears twice (role=owner and role=data_access_contact), ordered by (id_study_lims, role)", func() {
			rows, err := client.StudiesForUser(context.Background(), "dc9", "", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5001"})
			convey.So(peopleStudyRoles(rows), convey.ShouldResemble, []string{"data_access_contact", "owner"})
		})

		convey.Convey("when CountStudiesForUser(\"dc9\", \"\") runs, then it is 2 (distinct (study, role))", func() {
			count, err := client.CountStudiesForUser(context.Background(), "dc9", "")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 2)
		})
	})
}

// E2 acceptance test 5 (Client defensive re-validation): the Client method itself
// rejects a whitespace-only person with ErrUnsupportedIdentifier so a direct
// caller is not silently wrong.
func TestStudiesForUserClientRevalidatesWhitespaceE2(t *testing.T) {
	convey.Convey("Given a synced cache and a Client", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUserStudies(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser is called with a whitespace-only person, then ErrUnsupportedIdentifier", func() {
			_, err := client.StudiesForUser(context.Background(), "   ", "", 100, 0)
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})

		convey.Convey("when CountStudiesForUser is called with a whitespace-only person, then ErrUnsupportedIdentifier", func() {
			_, err := client.CountStudiesForUser(context.Background(), "   ", "")
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})
	})
}

// E2 acceptance test 2 / 3 over HTTP: the list endpoint passes the raw role query
// param through to the queryer and sets the X-Total-Count / X-Next-Offset sizing
// headers from the matching count, so a role= override is honoured end-to-end.
func TestStudiesForUserHTTPRoleParamAndSizingHeadersE2(t *testing.T) {
	convey.Convey("Given a server over a Client whose cache has the ca3 user fixture", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUserStudies(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when GET /studies/user/ca3?limit=2&offset=0 is served (default roles), then 2 rows with X-Total-Count 3 and X-Next-Offset 2", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/studies/user/ca3?limit=2&offset=0")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "3")
			convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "2")

			var rows []PersonStudy
			decodeMLWHJSONResponseForTest(t, response, &rows)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5001", "5002"})
		})

		convey.Convey("when GET /studies/user/ca3?role=follower is served, then only W is returned with X-Total-Count 1", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/studies/user/ca3?role=follower")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "1")

			var rows []PersonStudy
			decodeMLWHJSONResponseForTest(t, response, &rows)
			convey.So(peopleStudyLimsIDs(rows), convey.ShouldResemble, []string{"5004"})
			convey.So(peopleStudyRoles(rows), convey.ShouldResemble, []string{"follower"})
		})
	})
}

// E2 regression: study_users role-membership results are not complete unless the
// study_users mirror has synced. A cache with study rows synced but no
// study_users sync-state row must degrade as never-synced instead of serving
// empty or partial role-membership results.
func TestStudiesForUserRequiresStudyUsersSyncStateE2(t *testing.T) {
	convey.Convey("Given study and study_users rows for ca3 but only the study table has sync state", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "")
		seedStudyUsersMirrorRow(t, cache.DB(), 9001, 1, "owner", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser runs, then it returns an empty list and the never-synced sentinels", func() {
			rows, err := client.StudiesForUser(context.Background(), "ca3", "", 100, 0)

			convey.So(rows, convey.ShouldResemble, []PersonStudy{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})

		convey.Convey("when CountStudiesForUser runs, then it returns Count{} and the never-synced sentinels", func() {
			count, err := client.CountStudiesForUser(context.Background(), "ca3", "")

			convey.So(count, convey.ShouldResemble, Count{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

// E2 acceptance test 6: a never-synced cache returns an EMPTY list plus an error
// satisfying both ErrCacheNeverSynced and ErrNotFound; a synced cache with no
// match returns an empty list and NO error (the people-endpoint cascade).
func TestStudiesForUserPeopleCascadeE2(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser runs, then an empty list and both sentinels", func() {
			rows, err := client.StudiesForUser(context.Background(), "ca3", "", 100, 0)

			convey.So(rows, convey.ShouldResemble, []PersonStudy{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})

		convey.Convey("when CountStudiesForUser runs, then Count{} and both sentinels", func() {
			count, err := client.CountStudiesForUser(context.Background(), "ca3", "")

			convey.So(count, convey.ShouldResemble, Count{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a synced SQLite cache whose study_users rows do not match the person", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUserStudies(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when StudiesForUser runs for a non-matching person, then an empty list and NO error", func() {
			rows, err := client.StudiesForUser(context.Background(), "zzz-nobody", "", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, []PersonStudy{})
		})

		convey.Convey("when CountStudiesForUser runs for a non-matching person, then 0 and NO error", func() {
			count, err := client.CountStudiesForUser(context.Background(), "zzz-nobody", "")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 0)
		})
	})
}

// seedUserStudies seeds the ca3 user fixture used by E2's acceptance tests: four
// SQSCP studies (X=5001, Y=5002, Z=5003, W=5004) and study_users_mirror rows for
// person login "ca3" / email "ca3@sanger.ac.uk" / name "Carl Anderson" -- owner
// of X,Y; manager of Z; follower of W -- then marks the study and study_users
// tables synced.
func seedUserStudies(t *testing.T, cache Cache) {
	t.Helper()

	seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "")
	seedStudyMirrorSearchRow(t, cache.DB(), 2, "5002", "study-b", "Title B", "Programme B", "")
	seedStudyMirrorSearchRow(t, cache.DB(), 3, "5003", "study-c", "Title C", "Programme C", "")
	seedStudyMirrorSearchRow(t, cache.DB(), 4, "5004", "study-d", "Title D", "Programme D", "")

	seedStudyUsersMirrorRow(t, cache.DB(), 9001, 1, "owner", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
	seedStudyUsersMirrorRow(t, cache.DB(), 9002, 2, "owner", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
	seedStudyUsersMirrorRow(t, cache.DB(), 9003, 3, "manager", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
	seedStudyUsersMirrorRow(t, cache.DB(), 9004, 4, "follower", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")

	seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
	seedSyncState(t, cache.DB(), syncTableStudyUsers, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
}

// E3 acceptance test 1: a "carl" term returns BOTH a faculty_sponsor candidate
// (Name="Carl Anderson", StudyCount=91 = distinct SQSCP studies for the sponsor)
// AND a study_users candidate (Name="Carl Anderson", Login="ca3",
// Email="ca3@sanger.ac.uk", Role="owner", StudyCount=59 = distinct studies for the
// (login, role)). The two StudyCount bases differ (91 vs 59), proving Note 2: the
// candidate is grouped by (name, login, email, role) but the study_users count is
// per (login, role).
func TestResolvePersonReturnsBothSourcesWithDistinctCountsE3(t *testing.T) {
	convey.Convey("Given faculty_sponsor Carl Anderson on 91 SQSCP studies and a study_users owner row ca3 on 59 of them", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedCarlResolveFixture(t, cache)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson(\"carl\", all) runs, then a faculty_sponsor candidate (91) and a study_users candidate (59) are returned", func() {
			candidates, err := client.ResolvePerson(context.Background(), "carl", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(candidates, convey.ShouldResemble, []PersonCandidate{
				{Source: "faculty_sponsor", Name: "Carl Anderson", StudyCount: 91},
				{
					Source: "study_users", Name: "Carl Anderson", Login: "ca3",
					Email: "ca3@sanger.ac.uk", Role: "owner", StudyCount: 59,
				},
			})
		})

		convey.Convey("when CountResolvePerson(\"carl\") runs, then it is 2 (count == len(list))", func() {
			count, err := client.CountResolvePerson(context.Background(), "carl")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 2)
		})
	})
}

// E3 acceptance test 2: a "ca3" login fragment returns the study_users candidate
// (match across login), enabling translation from a login to the stored name.
func TestResolvePersonMatchesByLoginFragmentE3(t *testing.T) {
	convey.Convey("Given the Carl resolve fixture", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedCarlResolveFixture(t, cache)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson(\"ca3\", all) runs, then the study_users candidate is returned (the login resolves to the stored name)", func() {
			candidates, err := client.ResolvePerson(context.Background(), "ca3", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(candidates, convey.ShouldResemble, []PersonCandidate{
				{
					Source: "study_users", Name: "Carl Anderson", Login: "ca3",
					Email: "ca3@sanger.ac.uk", Role: "owner", StudyCount: 59,
				},
			})
		})
	})
}

// E3 acceptance test 4 (Client defensive re-validation): the Client method itself
// rejects a whitespace-only term with ErrUnsupportedIdentifier so a direct caller
// is not silently wrong.
func TestResolvePersonClientRevalidatesWhitespaceE3(t *testing.T) {
	convey.Convey("Given a synced cache and a Client", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedCarlResolveFixture(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson is called with a whitespace-only term, then ErrUnsupportedIdentifier", func() {
			_, err := client.ResolvePerson(context.Background(), "   ", 100, 0)
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})

		convey.Convey("when CountResolvePerson is called with a whitespace-only term, then ErrUnsupportedIdentifier", func() {
			_, err := client.CountResolvePerson(context.Background(), "   ")
			convey.So(errors.Is(err, ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		})
	})
}

// E3 acceptance test 1/3 over HTTP: the list endpoint pages the combined candidate
// set and sets the X-Total-Count / X-Next-Offset sizing headers from the matching
// count, so the count and list cannot drift.
func TestResolvePersonHTTPPaginationHeadersE3(t *testing.T) {
	convey.Convey("Given a server over a Client whose cache has two faculty_sponsor candidates and a study_users candidate", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "Carl Anderson")
		seedStudyMirrorSearchRow(t, cache.DB(), 2, "5002", "study-b", "Title B", "Programme B", "Carla Anders")
		seedStudyUsersMirrorRow(t, cache.DB(), 9001, 1, "owner", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
		seedSyncState(t, cache.DB(), syncTableStudyUsers, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when GET /resolve-person/carl?limit=2&offset=0 is served, then 2 rows with X-Total-Count 3 and X-Next-Offset 2", func() {
			response := performMLWHRequestForTest(t, client, http.MethodGet, "/resolve-person/carl?limit=2&offset=0")

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
			convey.So(response.Header().Get("X-Total-Count"), convey.ShouldEqual, "3")
			convey.So(response.Header().Get("X-Next-Offset"), convey.ShouldEqual, "2")

			var candidates []PersonCandidate
			decodeMLWHJSONResponseForTest(t, response, &candidates)
			convey.So(personCandidateNames(candidates), convey.ShouldResemble, []string{"Carl Anderson", "Carla Anders"})
		})
	})
}

// E3 regression: resolve-person combines faculty_sponsor and study_users sources,
// so it must not serve a partial candidate set when study is synced but
// study_users has never synced.
func TestResolvePersonRequiresStudyUsersSyncStateE3(t *testing.T) {
	convey.Convey("Given matching faculty_sponsor and study_users rows but only the study table has sync state", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedStudyMirrorSearchRow(t, cache.DB(), 1, "5001", "study-a", "Title A", "Programme A", "Carl Anderson")
		seedStudyUsersMirrorRow(t, cache.DB(), 9001, 1, "owner", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
		seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson runs, then it returns an empty list and the never-synced sentinels", func() {
			candidates, err := client.ResolvePerson(context.Background(), "carl", 100, 0)

			convey.So(candidates, convey.ShouldResemble, []PersonCandidate{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})

		convey.Convey("when CountResolvePerson runs, then it returns Count{} and the never-synced sentinels", func() {
			count, err := client.CountResolvePerson(context.Background(), "carl")

			convey.So(count, convey.ShouldResemble, Count{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

// E3 acceptance test 5: a never-synced cache returns an EMPTY list plus an error
// satisfying both ErrCacheNeverSynced and ErrNotFound; a synced cache with no
// match returns an empty list and NO error (the people-endpoint cascade).
func TestResolvePersonPeopleCascadeE3(t *testing.T) {
	convey.Convey("Given a never-synced SQLite cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson runs, then an empty list and both sentinels", func() {
			candidates, err := client.ResolvePerson(context.Background(), "carl", 100, 0)

			convey.So(candidates, convey.ShouldResemble, []PersonCandidate{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})

		convey.Convey("when CountResolvePerson runs, then Count{} and both sentinels", func() {
			count, err := client.CountResolvePerson(context.Background(), "carl")

			convey.So(count, convey.ShouldResemble, Count{})
			convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a synced SQLite cache whose people do not match the term", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedCarlResolveFixture(t, cache)
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		convey.Convey("when ResolvePerson runs for a non-matching term, then an empty list and NO error", func() {
			candidates, err := client.ResolvePerson(context.Background(), "zzz-nobody", 100, 0)

			convey.So(err, convey.ShouldBeNil)
			convey.So(candidates, convey.ShouldResemble, []PersonCandidate{})
		})

		convey.Convey("when CountResolvePerson runs for a non-matching term, then 0 and NO error", func() {
			count, err := client.CountResolvePerson(context.Background(), "zzz-nobody")

			convey.So(err, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, 0)
		})
	})
}

// seedCarlResolveFixture seeds the Carl resolve-person fixture used by E3's
// acceptance tests: 91 SQSCP studies whose faculty_sponsor is "Carl Anderson",
// with a study_users owner row for login "ca3" / email "ca3@sanger.ac.uk" / name
// "Carl Anderson" on 59 of them, then marks the study and study_users tables
// synced. The two StudyCount bases differ on purpose (91 distinct studies for the
// sponsor; 59 distinct studies for the (login, role)), so the test proves Note 2's
// two keys.
func seedCarlResolveFixture(t *testing.T, cache Cache) {
	t.Helper()

	const (
		sponsorStudies = 91
		ownerStudies   = 59
	)

	for i := range sponsorStudies {
		idStudyTmp := int64(i + 1)
		idStudyLims := strconv.Itoa(6000 + i)
		seedStudyMirrorSearchRow(t, cache.DB(), idStudyTmp, idStudyLims,
			"study-"+idStudyLims, "Title "+idStudyLims, "Programme", "Carl Anderson")

		if i < ownerStudies {
			seedStudyUsersMirrorRow(t, cache.DB(), 9000+idStudyTmp, idStudyTmp,
				"owner", "ca3", "ca3@sanger.ac.uk", "Carl Anderson")
		}
	}

	seedSyncState(t, cache.DB(), syncTableStudy, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
	seedSyncState(t, cache.DB(), syncTableStudyUsers, time.Date(2026, time.May, 6, 17, 0, 0, 0, time.UTC))
}

// seedStudyUsersMirrorRow inserts one study_users_mirror row linking a person
// (role/login/email/name) to a study by its id_study_tmp, so the user-endpoint
// role-membership coverage can target each field independently.
func seedStudyUsersMirrorRow(t *testing.T, db *sql.DB, idStudyUsersTmp, idStudyTmp int64, role, login, email, name string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO study_users_mirror(id_study_users_tmp, id_study_tmp, role, login, email, name, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		idStudyUsersTmp,
		idStudyTmp,
		role,
		login,
		email,
		name,
		formatSyncTime(time.Date(2026, time.May, 6, 16, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedStudyUsersMirrorRow(): %v", err)
	}
}

// personCandidateNames returns the Name of each PersonCandidate in order, so a
// page of candidates can be asserted compactly.
func personCandidateNames(candidates []PersonCandidate) []string {
	names := make([]string, len(candidates))
	for index, candidate := range candidates {
		names[index] = candidate.Name
	}

	return names
}

// peopleStudyLimsIDs returns the id_study_lims of each PersonStudy row in order,
// so a result set can be asserted compactly.
func peopleStudyLimsIDs(rows []PersonStudy) []string {
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.Study.IDStudyLims
	}

	return ids
}

// peopleStudyRoles returns the matched Role of each PersonStudy row in order, so a
// result set's roles can be asserted compactly alongside peopleStudyLimsIDs.
func peopleStudyRoles(rows []PersonStudy) []string {
	roles := make([]string, len(rows))
	for index, row := range rows {
		roles[index] = row.Role
	}

	return roles
}
