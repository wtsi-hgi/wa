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

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHStudiesHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh studies --help renders documentation about env vars and the mode flags", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(output, convey.ShouldContainSubstring, "--faculty-sponsor")
		convey.So(output, convey.ShouldContainSubstring, "--user")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh studies")
	})
}

func TestMLWHPeopleHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh people --help renders documentation about env vars and an example", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "people", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh people")
	})
}

// H4 AT3: neither flag -> a clear usage error and a non-zero exit, and the client is
// never opened.
func TestMLWHStudiesNeitherFlagIsUsageError(t *testing.T) {
	convey.Convey("Given neither --faculty-sponsor nor --user, when wa mlwh studies runs, then it errors with usage and never opens a client (H4 acceptance 3)", t, func() {
		original := openMLWHStudiesClient
		t.Cleanup(func() { openMLWHStudiesClient = original })
		openMLWHStudiesClient = func(context.Context, mlwh.Config) (mlwhStudiesClient, error) {
			t.Fatalf("client should not be opened when no mode flag is given")

			return nil, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "exactly one")
	})
}

// H4 AT3: both flags -> a clear usage error and a non-zero exit, and the client is
// never opened.
func TestMLWHStudiesBothFlagsIsUsageError(t *testing.T) {
	convey.Convey("Given both --faculty-sponsor and --user, when wa mlwh studies runs, then it errors with usage and never opens a client (H4 acceptance 3)", t, func() {
		original := openMLWHStudiesClient
		t.Cleanup(func() { openMLWHStudiesClient = original })
		openMLWHStudiesClient = func(context.Context, mlwh.Config) (mlwhStudiesClient, error) {
			t.Fatalf("client should not be opened when both mode flags are given")

			return nil, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--faculty-sponsor", "carl", "--user", "ca3"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "exactly one")
	})
}

func TestMLWHPeopleRequiresTerm(t *testing.T) {
	convey.Convey("Given a missing term, when wa mlwh people runs, then it errors with usage and never opens a client", t, func() {
		original := openMLWHStudiesClient
		t.Cleanup(func() { openMLWHStudiesClient = original })
		openMLWHStudiesClient = func(context.Context, mlwh.Config) (mlwhStudiesClient, error) {
			t.Fatalf("client should not be opened when the term is missing")

			return nil, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "people"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "usage")
	})
}

// stubMLWHStudiesClient is a fake mlwhStudiesClient for the `wa mlwh studies` and
// `wa mlwh people` tests. Each query dispatches to its own func field so a test can
// assert the command routed to the right method and observe the raw role / paging
// arguments passed through.
type stubMLWHStudiesClient struct {
	facultySponsor      func(ctx context.Context, name string, limit, offset int) ([]mlwh.PersonStudy, error)
	countFacultySponsor func(ctx context.Context, name string) (mlwh.Count, error)
	user                func(ctx context.Context, person, role string, limit, offset int) ([]mlwh.PersonStudy, error)
	countUser           func(ctx context.Context, person, role string) (mlwh.Count, error)
	resolvePerson       func(ctx context.Context, term string, limit, offset int) ([]mlwh.PersonCandidate, error)

	lastFacultySponsorPageLen int
	lastUserPageLen           int
	closed                    bool
}

func (s *stubMLWHStudiesClient) StudiesForFacultySponsor(ctx context.Context, name string, limit, offset int) ([]mlwh.PersonStudy, error) {
	if s.facultySponsor != nil {
		studies, err := s.facultySponsor(ctx, name, limit, offset)
		s.lastFacultySponsorPageLen = len(studies)

		return studies, err
	}

	return nil, errors.New("faculty sponsor not stubbed")
}

func (s *stubMLWHStudiesClient) CountStudiesForFacultySponsor(ctx context.Context, name string) (mlwh.Count, error) {
	if s.countFacultySponsor != nil {
		return s.countFacultySponsor(ctx, name)
	}

	return mlwh.Count{Count: s.lastFacultySponsorPageLen}, nil
}

func (s *stubMLWHStudiesClient) StudiesForUser(ctx context.Context, person, role string, limit, offset int) ([]mlwh.PersonStudy, error) {
	if s.user != nil {
		studies, err := s.user(ctx, person, role, limit, offset)
		s.lastUserPageLen = len(studies)

		return studies, err
	}

	return nil, errors.New("user not stubbed")
}

func (s *stubMLWHStudiesClient) CountStudiesForUser(ctx context.Context, person, role string) (mlwh.Count, error) {
	if s.countUser != nil {
		return s.countUser(ctx, person, role)
	}

	return mlwh.Count{Count: s.lastUserPageLen}, nil
}

func (s *stubMLWHStudiesClient) ResolvePerson(ctx context.Context, term string, limit, offset int) ([]mlwh.PersonCandidate, error) {
	if s.resolvePerson != nil {
		return s.resolvePerson(ctx, term, limit, offset)
	}

	return nil, errors.New("resolve person not stubbed")
}

func (s *stubMLWHStudiesClient) Close() error {
	s.closed = true

	return nil
}

// H4 AT1: a fake client where StudiesForFacultySponsor("carl") returns 3 studies
// prints 3 study lines with the total (3) and faculty_sponsor, exit 0.
func TestMLWHStudiesFacultySponsorPrintsStudies(t *testing.T) {
	convey.Convey("Given StudiesForFacultySponsor(\"carl\") returns 3 studies, when wa mlwh studies --faculty-sponsor carl runs, then 3 study lines print with the total (3) and faculty_sponsor, exit 0 (H4 acceptance 1)", t, func() {
		var capturedName string
		stub := &stubMLWHStudiesClient{
			facultySponsor: func(_ context.Context, name string, _, _ int) ([]mlwh.PersonStudy, error) {
				capturedName = name

				return []mlwh.PersonStudy{
					{Study: mlwh.Study{IDStudyLims: "5901", Name: "Carl Study One", FacultySponsor: "Carl"}},
					{Study: mlwh.Study{IDStudyLims: "5902", Name: "Carl Study Two", FacultySponsor: "Carl"}},
					{Study: mlwh.Study{IDStudyLims: "5903", Name: "Carl Study Three", FacultySponsor: "Carl"}},
				}, nil
			},
			user: func(context.Context, string, string, int, int) ([]mlwh.PersonStudy, error) {
				t.Fatalf("user dispatch must not be used for --faculty-sponsor")

				return nil, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--faculty-sponsor", "carl"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedName, convey.ShouldEqual, "carl")
		convey.So(output, convey.ShouldContainSubstring, "5901")
		convey.So(output, convey.ShouldContainSubstring, "5902")
		convey.So(output, convey.ShouldContainSubstring, "5903")
		convey.So(output, convey.ShouldContainSubstring, "Carl Study One")
		convey.So(output, convey.ShouldContainSubstring, "faculty_sponsor")
		convey.So(output, convey.ShouldContainSubstring, "Carl")
		convey.So(output, convey.ShouldContainSubstring, "3")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHStudiesPaginationPrintsShownRangeAndTotal(t *testing.T) {
	convey.Convey("Given total sponsor matches exceed the returned page, when wa mlwh studies runs, then the text output shows the current page range and total count", t, func() {
		var capturedListName, capturedCountName string
		var capturedLimit, capturedOffset int
		page := make([]mlwh.PersonStudy, 50)
		for i := range 50 {
			page[i] = mlwh.PersonStudy{
				Study: mlwh.Study{
					IDStudyLims:    strconv.Itoa(1000 + i),
					Name:           "Carl Page Study " + strconv.Itoa(i+1),
					FacultySponsor: "Carl",
				},
			}
		}

		stub := &stubMLWHStudiesClient{
			facultySponsor: func(_ context.Context, name string, limit, offset int) ([]mlwh.PersonStudy, error) {
				capturedListName = name
				capturedLimit = limit
				capturedOffset = offset

				return page, nil
			},
			countFacultySponsor: func(_ context.Context, name string) (mlwh.Count, error) {
				capturedCountName = name

				return mlwh.Count{Count: 91}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--faculty-sponsor", "carl"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedListName, convey.ShouldEqual, "carl")
		convey.So(capturedCountName, convey.ShouldEqual, "carl")
		convey.So(capturedLimit, convey.ShouldEqual, 50)
		convey.So(capturedOffset, convey.ShouldEqual, 0)
		convey.So(output, convey.ShouldContainSubstring, "Studies (showing 1-50 of 91 total):")
		convey.So(output, convey.ShouldNotContainSubstring, "Studies (50):")
	})
}

// H4 AT2: StudiesForUser("ca3", "owner,manager") returns 3 rows; wa mlwh studies
// --user ca3 --role owner,manager prints 3 lines each with its role, exit 0. The
// raw --role string is passed through verbatim to StudiesForUser.
func TestMLWHStudiesUserPassesRawRoleAndPrintsPerRowRole(t *testing.T) {
	convey.Convey("Given StudiesForUser(\"ca3\", \"owner,manager\") returns 3 rows, when wa mlwh studies --user ca3 --role owner,manager runs, then 3 lines print each with its role and the raw role is passed through, exit 0 (H4 acceptance 2)", t, func() {
		var capturedPerson, capturedRole string
		stub := &stubMLWHStudiesClient{
			user: func(_ context.Context, person, role string, _, _ int) ([]mlwh.PersonStudy, error) {
				capturedPerson = person
				capturedRole = role

				return []mlwh.PersonStudy{
					{Study: mlwh.Study{IDStudyLims: "X", Name: "Study X", FacultySponsor: "Carl"}, Role: "owner"},
					{Study: mlwh.Study{IDStudyLims: "Y", Name: "Study Y", FacultySponsor: "Carl"}, Role: "manager"},
					{Study: mlwh.Study{IDStudyLims: "Z", Name: "Study Z", FacultySponsor: "Carl"}, Role: "owner"},
				}, nil
			},
			facultySponsor: func(context.Context, string, int, int) ([]mlwh.PersonStudy, error) {
				t.Fatalf("faculty sponsor dispatch must not be used for --user")

				return nil, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--user", "ca3", "--role", "owner,manager"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedPerson, convey.ShouldEqual, "ca3")
		convey.So(capturedRole, convey.ShouldEqual, "owner,manager")
		convey.So(output, convey.ShouldContainSubstring, "Study X")
		convey.So(output, convey.ShouldContainSubstring, "Study Y")
		convey.So(output, convey.ShouldContainSubstring, "Study Z")
		convey.So(output, convey.ShouldContainSubstring, "role=owner")
		convey.So(output, convey.ShouldContainSubstring, "role=manager")
	})
}

// H4 AT2 (default role): with no --role, the raw role passed through is the empty
// string (the default role set).
func TestMLWHStudiesUserDefaultRoleIsEmpty(t *testing.T) {
	convey.Convey("Given no --role, when wa mlwh studies --user ca3 runs, then the empty default role is passed through to StudiesForUser", t, func() {
		var capturedRole = "unset"
		stub := &stubMLWHStudiesClient{
			user: func(_ context.Context, _, role string, _, _ int) ([]mlwh.PersonStudy, error) {
				capturedRole = role

				return []mlwh.PersonStudy{
					{Study: mlwh.Study{IDStudyLims: "X", Name: "Study X"}, Role: "owner"},
				}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--user", "ca3"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedRole, convey.ShouldEqual, "")
	})
}

// H4 studies --json: a single JSON array of PersonStudy.
func TestMLWHStudiesJSONArrayOutput(t *testing.T) {
	convey.Convey("Given --json, when wa mlwh studies runs, then stdout is a single JSON array of PersonStudy", t, func() {
		stub := &stubMLWHStudiesClient{
			user: func(context.Context, string, string, int, int) ([]mlwh.PersonStudy, error) {
				return []mlwh.PersonStudy{
					{Study: mlwh.Study{IDStudyLims: "X", Name: "Study X"}, Role: "owner"},
				}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--user", "ca3", "--json"})

		convey.So(err, convey.ShouldBeNil)

		decoded := []map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded, convey.ShouldHaveLength, 1)
		convey.So(decoded[0]["role"], convey.ShouldEqual, "owner")

		study, ok := decoded[0]["study"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(study["id_study_lims"], convey.ShouldEqual, "X")
	})
}

// H4 studies empty: a synced cache returning no studies prints "no matches", exit 0.
func TestMLWHStudiesEmptyPrintsNoMatches(t *testing.T) {
	convey.Convey("Given a synced cache returning no studies, when wa mlwh studies --faculty-sponsor nobody runs, then it prints \"no matches\" and exits 0", t, func() {
		stub := &stubMLWHStudiesClient{
			facultySponsor: func(context.Context, string, int, int) ([]mlwh.PersonStudy, error) {
				return []mlwh.PersonStudy{}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--faculty-sponsor", "nobody"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "no matches")
	})
}

func TestMLWHStudiesEmptyJSONIsArray(t *testing.T) {
	convey.Convey("Given --json with an empty result, when wa mlwh studies runs, then stdout is an empty JSON array (not null)", t, func() {
		stub := &stubMLWHStudiesClient{
			facultySponsor: func(context.Context, string, int, int) ([]mlwh.PersonStudy, error) {
				return []mlwh.PersonStudy{}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--faculty-sponsor", "nobody", "--json"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.TrimSpace(output), convey.ShouldEqual, "[]")
	})
}

// H4 AT4: ResolvePerson("carl") returns a faculty_sponsor candidate AND a
// study_users candidate; wa mlwh people carl prints both candidate lines with their
// source / stored form / study_count, exit 0.
func TestMLWHPeoplePrintsCandidates(t *testing.T) {
	convey.Convey("Given ResolvePerson(\"carl\") returns a faculty_sponsor and a study_users candidate, when wa mlwh people carl runs, then both candidate lines print with their source / stored form / study_count, exit 0 (H4 acceptance 4)", t, func() {
		var capturedTerm string
		stub := &stubMLWHStudiesClient{
			resolvePerson: func(_ context.Context, term string, _, _ int) ([]mlwh.PersonCandidate, error) {
				capturedTerm = term

				return []mlwh.PersonCandidate{
					{Source: "faculty_sponsor", Name: "Carl Anderson", StudyCount: 3},
					{Source: "study_users", Name: "Carl Anderson", Login: "ca3", Email: "ca3@sanger.ac.uk", Role: "owner", StudyCount: 5},
				}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "people", "carl"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedTerm, convey.ShouldEqual, "carl")
		convey.So(output, convey.ShouldContainSubstring, "faculty_sponsor")
		convey.So(output, convey.ShouldContainSubstring, "study_users")
		convey.So(output, convey.ShouldContainSubstring, "Carl Anderson")
		convey.So(output, convey.ShouldContainSubstring, "ca3")
		convey.So(output, convey.ShouldContainSubstring, "ca3@sanger.ac.uk")
		convey.So(output, convey.ShouldContainSubstring, "owner")
		convey.So(output, convey.ShouldContainSubstring, "study_count=3")
		convey.So(output, convey.ShouldContainSubstring, "study_count=5")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

// H4 people --json: a single JSON array of PersonCandidate.
func TestMLWHPeopleJSONArrayOutput(t *testing.T) {
	convey.Convey("Given --json, when wa mlwh people runs, then stdout is a single JSON array of PersonCandidate", t, func() {
		stub := &stubMLWHStudiesClient{
			resolvePerson: func(context.Context, string, int, int) ([]mlwh.PersonCandidate, error) {
				return []mlwh.PersonCandidate{
					{Source: "study_users", Name: "Carl Anderson", Login: "ca3", Email: "ca3@sanger.ac.uk", Role: "owner", StudyCount: 5},
				}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "people", "carl", "--json"})

		convey.So(err, convey.ShouldBeNil)

		decoded := []map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded, convey.ShouldHaveLength, 1)
		convey.So(decoded[0]["source"], convey.ShouldEqual, "study_users")
		convey.So(decoded[0]["login"], convey.ShouldEqual, "ca3")
		convey.So(decoded[0]["study_count"], convey.ShouldEqual, 5)
	})
}

// H4 people empty: a synced cache returning no candidates prints "no matches",
// exit 0.
func TestMLWHPeopleEmptyPrintsNoMatches(t *testing.T) {
	convey.Convey("Given a synced cache returning no candidates, when wa mlwh people nobody runs, then it prints \"no matches\" and exits 0", t, func() {
		stub := &stubMLWHStudiesClient{
			resolvePerson: func(context.Context, string, int, int) ([]mlwh.PersonCandidate, error) {
				return []mlwh.PersonCandidate{}, nil
			},
		}

		withStubMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "people", "nobody"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "no matches")
	})
}

// withStubMLWHStudiesClient wires `wa mlwh studies`/`people` to a local cache that
// returns stub, mirroring withStubMLWHIRODSClient: WA_MLWH_DSN is set and
// WA_MLWH_SERVER_URL is cleared so openMLWHStudiesConfiguredClient takes the local
// path.
func withStubMLWHStudiesClient(t *testing.T, stub *stubMLWHStudiesClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHStudiesClient
	t.Cleanup(func() { openMLWHStudiesClient = original })

	openMLWHStudiesClient = func(context.Context, mlwh.Config) (mlwhStudiesClient, error) {
		return stub, nil
	}
}

// H4 AT5 (studies): a never-synced cache via --server degrades gracefully: a neutral
// cache-unavailable message, no sync hint, exit 0.
func TestMLWHStudiesServerModeNeverSyncedDegradesGracefully(t *testing.T) {
	convey.Convey("Given a never-synced cache reached via the MLWH server (no WA_MLWH_DSN), when wa mlwh studies runs, then it degrades gracefully: a neutral cache-unavailable message, no sync hint, exit 0 (H4 acceptance 5)", t, func() {
		stub := &stubMLWHStudiesClient{
			facultySponsor: func(context.Context, string, int, int) ([]mlwh.PersonStudy, error) {
				return []mlwh.PersonStudy{}, mlwh.ErrCacheNeverSynced
			},
		}

		withServerModeMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "studies", "--faculty-sponsor", "carl"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}

// H4 AT5 (people): a never-synced cache via --server degrades gracefully: a neutral
// cache-unavailable message, no sync hint, exit 0.
func TestMLWHPeopleServerModeNeverSyncedDegradesGracefully(t *testing.T) {
	convey.Convey("Given a never-synced cache reached via the MLWH server (no WA_MLWH_DSN), when wa mlwh people runs, then it degrades gracefully: a neutral cache-unavailable message, no sync hint, exit 0 (H4 acceptance 5)", t, func() {
		stub := &stubMLWHStudiesClient{
			resolvePerson: func(context.Context, string, int, int) ([]mlwh.PersonCandidate, error) {
				return []mlwh.PersonCandidate{}, mlwh.ErrCacheNeverSynced
			},
		}

		withServerModeMLWHStudiesClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "people", "carl"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}

// withServerModeMLWHStudiesClient wires `wa mlwh studies`/`people` to talk to an
// MLWH server (the normal end-user path): WA_MLWH_SERVER_URL is set and WA_MLWH_DSN
// is empty, so the user cannot sync. The remote-client opener is stubbed to return
// stub.
func withServerModeMLWHStudiesClient(t *testing.T, stub *stubMLWHStudiesClient) {
	t.Helper()
	t.Setenv("WA_MLWH_SERVER_URL", "http://mlwh.example:8091")
	t.Setenv("WA_MLWH_DSN", "")
	t.Setenv("WA_MLWH_PASSWORD", "")
	t.Setenv("WA_MLWH_CACHE_PATH", "")
	t.Setenv("WA_MLWH_CACHE_PASSWORD", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHStudiesRemoteClient
	t.Cleanup(func() { openMLWHStudiesRemoteClient = original })

	openMLWHStudiesRemoteClient = func(context.Context, mlwh.RemoteConfig) (mlwhStudiesClient, error) {
		return stub, nil
	}
}
