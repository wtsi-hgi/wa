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
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHSearchCommandRequiresTerm(t *testing.T) {
	convey.Convey("Given no term argument, when wa mlwh search runs, then it errors with usage information", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		original := openMLWHSearchClient
		t.Cleanup(func() { openMLWHSearchClient = original })

		openMLWHSearchClient = func(context.Context, mlwh.Config) (mlwhSearchClient, error) {
			t.Fatalf("client should not be opened when args are missing")

			return nil, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "term")
	})
}

func TestMLWHSearchHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh search --help renders documentation about env vars and an example", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_DSN")
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
		convey.So(output, convey.ShouldContainSubstring, "--env")
		convey.So(output, convey.ShouldContainSubstring, "Example")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh search")
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
	})
}

type stubMLWHSearchClient struct {
	searchStudies     func(ctx context.Context, term string, limit, offset int) ([]mlwh.Study, error)
	searchSamples     func(ctx context.Context, term string, limit, offset int) ([]mlwh.Sample, error)
	countStudySearch  func(ctx context.Context, term string) (mlwh.Count, error)
	countSampleSearch func(ctx context.Context, term string) (mlwh.Count, error)

	closed bool
}

func (s *stubMLWHSearchClient) SearchStudies(ctx context.Context, term string, limit, offset int) ([]mlwh.Study, error) {
	if s.searchStudies != nil {
		return s.searchStudies(ctx, term, limit, offset)
	}

	return nil, errors.New("SearchStudies not stubbed")
}

func (s *stubMLWHSearchClient) SearchSamples(ctx context.Context, term string, limit, offset int) ([]mlwh.Sample, error) {
	if s.searchSamples != nil {
		return s.searchSamples(ctx, term, limit, offset)
	}

	return nil, errors.New("SearchSamples not stubbed")
}

func (s *stubMLWHSearchClient) CountStudySearch(ctx context.Context, term string) (mlwh.Count, error) {
	if s.countStudySearch != nil {
		return s.countStudySearch(ctx, term)
	}

	return mlwh.Count{}, errors.New("CountStudySearch not stubbed")
}

func (s *stubMLWHSearchClient) CountSampleSearch(ctx context.Context, term string) (mlwh.Count, error) {
	if s.countSampleSearch != nil {
		return s.countSampleSearch(ctx, term)
	}

	return mlwh.Count{}, errors.New("CountSampleSearch not stubbed")
}

func (s *stubMLWHSearchClient) Close() error {
	s.closed = true

	return nil
}

func TestMLWHSearchCommandHumanReadable(t *testing.T) {
	convey.Convey("Given studies and samples match, when wa mlwh search runs, then text output lists both sections", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, term string, limit, offset int) ([]mlwh.Study, error) {
				convey.So(term, convey.ShouldEqual, "malaria")
				convey.So(limit, convey.ShouldEqual, 50)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Study{{
					IDStudyLims:     "6568",
					Name:            "Malaria genomics survey",
					StudyTitle:      "Malaria study title",
					AccessionNumber: "EGAS00001006568",
				}}, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 1}, nil
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{
					Name:            "DN1234",
					SupplierName:    "vendor-id-1",
					CommonName:      "Plasmodium falciparum",
					DonorID:         "donor-9",
					AccessionNumber: "SAMEA1234",
				}}, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 1}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Studies (1)")
		convey.So(output, convey.ShouldContainSubstring, "6568")
		convey.So(output, convey.ShouldContainSubstring, "Malaria study title")
		convey.So(output, convey.ShouldContainSubstring, "EGAS00001006568")
		convey.So(output, convey.ShouldContainSubstring, "Samples (1)")
		convey.So(output, convey.ShouldContainSubstring, "DN1234")
		convey.So(output, convey.ShouldContainSubstring, "vendor-id-1")
		convey.So(output, convey.ShouldContainSubstring, "Plasmodium falciparum")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHSearchCommandTypeStudyOnly(t *testing.T) {
	convey.Convey("Given --type study, when wa mlwh search runs, then only study search is called and no Samples section prints", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				return []mlwh.Study{{IDStudyLims: "6568", Name: "Malaria genomics survey"}}, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 1}, nil
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				t.Fatalf("SearchSamples must not be called for --type study")

				return nil, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				t.Fatalf("CountSampleSearch must not be called for --type study")

				return mlwh.Count{}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria", "--type", "study"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Studies (1)")
		convey.So(output, convey.ShouldContainSubstring, "6568")
		convey.So(output, convey.ShouldNotContainSubstring, "Samples")
	})
}

func TestMLWHSearchCommandTypeSampleOnly(t *testing.T) {
	convey.Convey("Given --type sample, when wa mlwh search runs, then only sample search is called and no Studies section prints", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				t.Fatalf("SearchStudies must not be called for --type sample")

				return nil, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				t.Fatalf("CountStudySearch must not be called for --type sample")

				return mlwh.Count{}, nil
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{Name: "DN1234", SupplierName: "vendor-id-1"}}, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 1}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "DN12", "--type", "sample"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Samples (1)")
		convey.So(output, convey.ShouldContainSubstring, "DN1234")
		convey.So(output, convey.ShouldNotContainSubstring, "Studies")
	})
}

func TestMLWHSearchCommandJSONOutput(t *testing.T) {
	convey.Convey("Given --json, when wa mlwh search runs, then stdout is a single JSON object with term, studies and samples", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				return []mlwh.Study{{IDStudyLims: "6568", Name: "Malaria genomics survey", StudyTitle: "Malaria study title"}}, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 3}, nil
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{Name: "DN1234", SupplierName: "vendor-id-1"}}, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 2}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria", "--json"})

		convey.So(err, convey.ShouldBeNil)

		decoded := map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded["term"], convey.ShouldEqual, "malaria")

		studies, ok := decoded["studies"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(studies["count"], convey.ShouldEqual, 3)
		studyResults, ok := studies["results"].([]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(studyResults, convey.ShouldHaveLength, 1)
		firstStudy := studyResults[0].(map[string]any)
		convey.So(firstStudy["id_study_lims"], convey.ShouldEqual, "6568")

		samples, ok := decoded["samples"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(samples["count"], convey.ShouldEqual, 2)
		sampleResults, ok := samples["results"].([]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(sampleResults, convey.ShouldHaveLength, 1)
	})
}

func TestMLWHSearchCommandJSONOmitsUnrequestedSection(t *testing.T) {
	convey.Convey("Given --type study --json, when wa mlwh search runs, then the JSON object omits the samples section", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				return []mlwh.Study{{IDStudyLims: "6568", Name: "Malaria genomics survey"}}, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 1}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria", "--type", "study", "--json"})

		convey.So(err, convey.ShouldBeNil)

		decoded := map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		_, hasStudies := decoded["studies"]
		convey.So(hasStudies, convey.ShouldBeTrue)
		_, hasSamples := decoded["samples"]
		convey.So(hasSamples, convey.ShouldBeFalse)
	})
}

func TestMLWHSearchCommandPassesLimitAndOffset(t *testing.T) {
	convey.Convey("Given --limit and --offset, when wa mlwh search runs, then they are passed through to the search calls", t, func() {
		var (
			gotStudyLimit, gotStudyOffset   int
			gotSampleLimit, gotSampleOffset int
		)

		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, limit, offset int) ([]mlwh.Study, error) {
				gotStudyLimit, gotStudyOffset = limit, offset

				return nil, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 0}, nil
			},
			searchSamples: func(_ context.Context, _ string, limit, offset int) ([]mlwh.Sample, error) {
				gotSampleLimit, gotSampleOffset = limit, offset

				return nil, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 0}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria", "--limit", "10", "--offset", "20"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(gotStudyLimit, convey.ShouldEqual, 10)
		convey.So(gotStudyOffset, convey.ShouldEqual, 20)
		convey.So(gotSampleLimit, convey.ShouldEqual, 10)
		convey.So(gotSampleOffset, convey.ShouldEqual, 20)
	})
}

func TestMLWHSearchCommandShortTerm(t *testing.T) {
	convey.Convey("Given a term shorter than three characters, when wa mlwh search runs, then it reports the minimum and never searches", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				t.Fatalf("SearchStudies must not be called for a short term")

				return nil, nil
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				t.Fatalf("CountStudySearch must not be called for a short term")

				return mlwh.Count{}, nil
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				t.Fatalf("SearchSamples must not be called for a short term")

				return nil, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				t.Fatalf("CountSampleSearch must not be called for a short term")

				return mlwh.Count{}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "ab"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "at least 3 characters")
	})
}

func TestMLWHSearchCommandNeverSyncedDoesNotMentionSync(t *testing.T) {
	convey.Convey("Given a never-synced cache, when wa mlwh search runs, then the output gives a neutral message with no sync instruction", t, func() {
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				return []mlwh.Study{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}

func TestMLWHSearchCommandNeverSyncedJSONOmitsSyncHint(t *testing.T) {
	convey.Convey("Given a never-synced cache and --json, when wa mlwh search runs, then the JSON object parses, mentions no sync and has empty (not null) result arrays", t, func() {
		// The real RemoteClient returns a nil slice alongside the never-synced
		// error (remoteCall returns the zero value of []Study/[]Sample on
		// error), so the stub mirrors that to exercise the actual path.
		stub := &stubMLWHSearchClient{
			searchStudies: func(_ context.Context, _ string, _, _ int) ([]mlwh.Study, error) {
				return nil, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
			countStudySearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return nil, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "malaria", "--json"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")

		// An empty cache must still serialise results as [] (an always-present
		// array), never null, matching the synced no-match success path.
		convey.So(output, convey.ShouldContainSubstring, "\"results\": []")
		convey.So(output, convey.ShouldNotContainSubstring, "\"results\": null")

		decoded := map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded["term"], convey.ShouldEqual, "malaria")

		studies := decoded["studies"].(map[string]any)
		convey.So(studies["count"], convey.ShouldEqual, 0)
		studyResults, ok := studies["results"].([]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(studyResults, convey.ShouldNotBeNil)
		convey.So(studyResults, convey.ShouldBeEmpty)

		samples := decoded["samples"].(map[string]any)
		convey.So(samples["count"], convey.ShouldEqual, 0)
		sampleResults, ok := samples["results"].([]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(sampleResults, convey.ShouldNotBeNil)
		convey.So(sampleResults, convey.ShouldBeEmpty)
	})
}

func TestMLWHSearchCommandSampleCountCapRendersAsFloor(t *testing.T) {
	convey.Convey("Given the sample count equals the cap, when wa mlwh search runs, then the count renders as a floor", t, func() {
		stub := &stubMLWHSearchClient{
			searchSamples: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{Name: "DN1234"}}, nil
			},
			countSampleSearch: func(_ context.Context, _ string) (mlwh.Count, error) {
				return mlwh.Count{Count: 10000}, nil
			},
		}

		withStubMLWHSearchClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "search", "mus", "--type", "sample"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Samples (10000+)")
	})
}

func withStubMLWHSearchClient(t *testing.T, stub *stubMLWHSearchClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHSearchClient
	t.Cleanup(func() { openMLWHSearchClient = original })

	openMLWHSearchClient = func(context.Context, mlwh.Config) (mlwhSearchClient, error) {
		return stub, nil
	}
}
