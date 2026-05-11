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

func TestMLWHInfoCommandRequiresIdentifier(t *testing.T) {
	convey.Convey("Given no identifier argument, when wa mlwh info runs, then it errors with usage information", t, func() {
		t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

		original := openMLWHInfoClient
		t.Cleanup(func() { openMLWHInfoClient = original })

		openMLWHInfoClient = func(context.Context, mlwh.Config) (mlwhInfoClient, error) {
			t.Fatalf("client should not be opened when args are missing")

			return nil, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "identifier")
	})
}

func TestMLWHInfoHelpRendersConfigurationDetails(t *testing.T) {
	convey.Convey("wa mlwh info --help renders documentation about env vars and an example", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_DSN")
		convey.So(output, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
		convey.So(output, convey.ShouldContainSubstring, "--env")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh info")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh sync")
	})
}

type stubMLWHInfoClient struct {
	classify        func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveSample   func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveStudy    func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveRun      func(ctx context.Context, raw string) (mlwh.Match, error)
	resolveLibrary  func(ctx context.Context, raw string) (mlwh.Match, error)
	findBySangerID  func(ctx context.Context, sangerID string) ([]mlwh.Sample, error)
	findByLimsID    func(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error)
	findByAccession func(ctx context.Context, accession string) ([]mlwh.Sample, error)

	studiesForSample    func(ctx context.Context, name string) ([]mlwh.Study, error)
	lanesForSample      func(ctx context.Context, name string, limit, offset int) ([]mlwh.Lane, error)
	irodsPathsForSample func(ctx context.Context, name string, limit, offset int) ([]mlwh.IRODSPath, error)
	librariesForStudy   func(ctx context.Context, id string, limit, offset int) ([]mlwh.Library, error)
	runsForStudy        func(ctx context.Context, id string, limit, offset int) ([]mlwh.Run, error)
	samplesForStudy     func(ctx context.Context, id string, limit, offset int) ([]mlwh.Sample, error)
	samplesForRun       func(ctx context.Context, id string, limit, offset int) ([]mlwh.Sample, error)
	samplesForLibrary   func(ctx context.Context, pipelineID, studyLimsID string, limit, offset int) ([]mlwh.Sample, error)

	closed bool
}

func (s *stubMLWHInfoClient) ClassifyIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if s.classify != nil {
		return s.classify(ctx, raw)
	}

	return mlwh.Match{}, errors.New("classify not stubbed")
}

func (s *stubMLWHInfoClient) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	if s.resolveSample != nil {
		return s.resolveSample(ctx, raw)
	}

	return mlwh.Match{}, errors.New("resolveSample not stubbed")
}

func (s *stubMLWHInfoClient) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	if s.resolveStudy != nil {
		return s.resolveStudy(ctx, raw)
	}

	return mlwh.Match{}, errors.New("resolveStudy not stubbed")
}

func (s *stubMLWHInfoClient) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	if s.resolveRun != nil {
		return s.resolveRun(ctx, raw)
	}

	return mlwh.Match{}, errors.New("resolveRun not stubbed")
}

func (s *stubMLWHInfoClient) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	if s.resolveLibrary != nil {
		return s.resolveLibrary(ctx, raw)
	}

	return mlwh.Match{}, errors.New("resolveLibrary not stubbed")
}

func (s *stubMLWHInfoClient) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]mlwh.Sample, error) {
	if s.findBySangerID != nil {
		return s.findBySangerID(ctx, sangerID)
	}

	return nil, mlwh.ErrNotFound
}

func (s *stubMLWHInfoClient) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]mlwh.Sample, error) {
	if s.findByLimsID != nil {
		return s.findByLimsID(ctx, idSampleLims)
	}

	return nil, mlwh.ErrNotFound
}

func (s *stubMLWHInfoClient) FindSamplesByAccessionNumber(ctx context.Context, accession string) ([]mlwh.Sample, error) {
	if s.findByAccession != nil {
		return s.findByAccession(ctx, accession)
	}

	return nil, mlwh.ErrNotFound
}

func (s *stubMLWHInfoClient) StudiesForSample(ctx context.Context, name string) ([]mlwh.Study, error) {
	if s.studiesForSample != nil {
		return s.studiesForSample(ctx, name)
	}

	return nil, mlwh.ErrNotFound
}

func (s *stubMLWHInfoClient) LanesForSample(ctx context.Context, name string, limit, offset int) ([]mlwh.Lane, error) {
	if s.lanesForSample != nil {
		return s.lanesForSample(ctx, name, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) IRODSPathsForSample(ctx context.Context, name string, limit, offset int) ([]mlwh.IRODSPath, error) {
	if s.irodsPathsForSample != nil {
		return s.irodsPathsForSample(ctx, name, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) LibrariesForStudy(ctx context.Context, id string, limit, offset int) ([]mlwh.Library, error) {
	if s.librariesForStudy != nil {
		return s.librariesForStudy(ctx, id, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) RunsForStudy(ctx context.Context, id string, limit, offset int) ([]mlwh.Run, error) {
	if s.runsForStudy != nil {
		return s.runsForStudy(ctx, id, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) SamplesForStudy(ctx context.Context, id string, limit, offset int) ([]mlwh.Sample, error) {
	if s.samplesForStudy != nil {
		return s.samplesForStudy(ctx, id, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) SamplesForRun(ctx context.Context, id string, limit, offset int) ([]mlwh.Sample, error) {
	if s.samplesForRun != nil {
		return s.samplesForRun(ctx, id, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) SamplesForLibrary(ctx context.Context, pipelineID, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
	if s.samplesForLibrary != nil {
		return s.samplesForLibrary(ctx, pipelineID, studyLimsID, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) Close() error {
	s.closed = true

	return nil
}

func TestMLWHInfoCommandHumanReadableSample(t *testing.T) {
	convey.Convey("Given a sample identifier resolves to a sample with two library-study pairings, when wa mlwh info runs, then stdout contains one library line per pairing", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "DN1234")

				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: "DN1234",
					Sample: &mlwh.Sample{
						Name:            "DN1234",
						IDSampleLims:    "8675309",
						SangerSampleID:  "DN1234",
						SupplierName:    "vendor-id-1",
						AccessionNumber: "EGAS00001005678",
						Libraries: []mlwh.Library{
							{PipelineIDLims: "Standard", IDStudyLims: "5901"},
							{PipelineIDLims: "Chromium", IDStudyLims: "5902"},
						},
					},
				}, nil
			},
			studiesForSample: func(_ context.Context, name string) ([]mlwh.Study, error) {
				convey.So(name, convey.ShouldEqual, "DN1234")

				return []mlwh.Study{{
					IDStudyLims:     "5901",
					Name:            "Lung cancer GWAS",
					AccessionNumber: "EGAS00001005678",
				}, {
					IDStudyLims:     "5902",
					Name:            "Lung cancer scRNA",
					AccessionNumber: "EGAS00001005679",
				}}, nil
			},
			lanesForSample: func(_ context.Context, _ string, _, _ int) ([]mlwh.Lane, error) {
				return []mlwh.Lane{{IDRun: 49001, Position: 2, TagIndex: 7}}, nil
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "DN1234"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Identifier: DN1234")
		convey.So(output, convey.ShouldContainSubstring, "sanger_sample_name")
		convey.So(output, convey.ShouldContainSubstring, "DN1234")
		convey.So(output, convey.ShouldContainSubstring, "8675309")
		convey.So(output, convey.ShouldContainSubstring, "vendor-id-1")
		convey.So(strings.Count(output, "library:"), convey.ShouldEqual, 2)
		convey.So(output, convey.ShouldContainSubstring, "library: Standard / 5901")
		convey.So(output, convey.ShouldContainSubstring, "library: Chromium / 5902")
		convey.So(output, convey.ShouldContainSubstring, "49001")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHInfoCommandJSONOutput(t *testing.T) {
	convey.Convey("Given --json, when wa mlwh info runs, then stdout is a single JSON object containing the resolved data", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{
					Kind:      mlwh.KindStudyLimsID,
					Canonical: "5901",
					Study: &mlwh.Study{
						IDStudyLims: "5901",
						Name:        "Lung cancer GWAS",
					},
				}, nil
			},
			librariesForStudy: func(_ context.Context, id string, _, _ int) ([]mlwh.Library, error) {
				convey.So(id, convey.ShouldEqual, "5901")

				return []mlwh.Library{{PipelineIDLims: "lib-A", IDStudyLims: "5901"}}, nil
			},
			samplesForStudy: func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return []mlwh.Sample{{Name: "DN1234", IDSampleLims: "8675309"}}, nil
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901", "--json"})

		convey.So(err, convey.ShouldBeNil)

		decoded := map[string]any{}
		convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
		convey.So(decoded["identifier"], convey.ShouldEqual, "5901")
		convey.So(decoded["kind"], convey.ShouldEqual, string(mlwh.KindStudyLimsID))

		study, ok := decoded["study"].(map[string]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(study["id_study_lims"], convey.ShouldEqual, "5901")

		libraries, ok := decoded["libraries"].([]any)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(libraries, convey.ShouldHaveLength, 1)
	})
}

func TestMLWHInfoCommandTypeOverride(t *testing.T) {
	convey.Convey("Given --type sample, when wa mlwh info runs, then ResolveSample is used and ClassifyIdentifier is not called", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				t.Fatalf("ClassifyIdentifier must not be called when --type is set")

				return mlwh.Match{}, nil
			},
			resolveSample: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "DN1234")

				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: "DN1234",
					Sample:    &mlwh.Sample{Name: "DN1234"},
				}, nil
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "DN1234", "--type", "sample"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "DN1234")
	})
}

func TestMLWHInfoCommandNotFound(t *testing.T) {
	convey.Convey("Given the identifier does not match anything, when wa mlwh info runs, then it exits non-zero with a clear message", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "no-such-thing"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "no-such-thing")
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "no match")
	})
}

func TestMLWHInfoCommandSurfacesEmptyCacheHint(t *testing.T) {
	convey.Convey("Given a non-NotFound resolver error (e.g. cache empty / upstream unavailable), when wa mlwh info runs, then the error suggests running wa mlwh sync", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.New("mlwh: cache reader not configured")
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh sync")
	})
}

func withStubMLWHInfoClient(t *testing.T, stub *stubMLWHInfoClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")

	original := openMLWHInfoClient
	t.Cleanup(func() { openMLWHInfoClient = original })

	openMLWHInfoClient = func(context.Context, mlwh.Config) (mlwhInfoClient, error) {
		return stub, nil
	}
}
