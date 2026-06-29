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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	resolveName     func(ctx context.Context, raw string) (mlwh.Match, error)
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

	studyOverview          func(ctx context.Context, id string) (mlwh.StudyOverview, error)
	statusBreakdown        func(ctx context.Context, id string) (mlwh.StatusBreakdown, error)
	countSamplesWithData   func(ctx context.Context, id string) (mlwh.Count, error)
	countSamplesWithDataAt func(ctx context.Context, id, since, until string) (mlwh.Count, error)
	samplesWithoutData     func(ctx context.Context, id string, limit, offset int) ([]mlwh.SampleWithData, error)
	sampleProgress         func(ctx context.Context, name string) (mlwh.SampleProgress, error)
	runOverview            func(ctx context.Context, idRun string) (mlwh.RunOverview, error)
	runStatus              func(ctx context.Context, idRun string) (mlwh.RunStatusTimeline, error)

	closed bool
}

func (s *stubMLWHInfoClient) StudyOverview(ctx context.Context, id string) (mlwh.StudyOverview, error) {
	if s.studyOverview != nil {
		return s.studyOverview(ctx, id)
	}

	return mlwh.StudyOverview{IDStudyLims: id}, nil
}

func (s *stubMLWHInfoClient) StatusBreakdown(ctx context.Context, id string) (mlwh.StatusBreakdown, error) {
	if s.statusBreakdown != nil {
		return s.statusBreakdown(ctx, id)
	}

	return mlwh.StatusBreakdown{IDStudyLims: id}, nil
}

func (s *stubMLWHInfoClient) CountSamplesWithData(ctx context.Context, id string) (mlwh.Count, error) {
	if s.countSamplesWithData != nil {
		return s.countSamplesWithData(ctx, id)
	}

	return mlwh.Count{}, nil
}

func (s *stubMLWHInfoClient) CountSamplesWithDataSince(ctx context.Context, id, since, until string) (mlwh.Count, error) {
	if s.countSamplesWithDataAt != nil {
		return s.countSamplesWithDataAt(ctx, id, since, until)
	}

	return mlwh.Count{}, nil
}

func (s *stubMLWHInfoClient) SamplesWithoutData(ctx context.Context, id string, limit, offset int) ([]mlwh.SampleWithData, error) {
	if s.samplesWithoutData != nil {
		return s.samplesWithoutData(ctx, id, limit, offset)
	}

	return nil, nil
}

func (s *stubMLWHInfoClient) SampleProgress(ctx context.Context, name string) (mlwh.SampleProgress, error) {
	if s.sampleProgress != nil {
		return s.sampleProgress(ctx, name)
	}

	return mlwh.SampleProgress{}, nil
}

func (s *stubMLWHInfoClient) RunOverview(ctx context.Context, idRun string) (mlwh.RunOverview, error) {
	if s.runOverview != nil {
		return s.runOverview(ctx, idRun)
	}

	return mlwh.RunOverview{}, nil
}

func (s *stubMLWHInfoClient) RunStatus(ctx context.Context, idRun string) (mlwh.RunStatusTimeline, error) {
	if s.runStatus != nil {
		return s.runStatus(ctx, idRun)
	}

	return mlwh.RunStatusTimeline{}, nil
}

func (s *stubMLWHInfoClient) ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error) {
	if s.resolveName != nil {
		return s.resolveName(ctx, raw)
	}

	return mlwh.Match{}, mlwh.ErrNotFound
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

	return mlwh.Match{}, mlwh.ErrNotFound
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

func TestMLWHInfoCommandUsesConfiguredServerWithoutLocalCredentials(t *testing.T) {
	convey.Convey("Given only WA_MLWH_SERVER_URL is configured, when wa mlwh info runs, then it queries the MLWH server and does not require local DB or cache credentials", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch r.URL.Path {
			case "/resolve/study/DN1234":
				w.WriteHeader(http.StatusNotFound)
				writeMLWHInfoServerJSONForTest(w, map[string]string{
					"code":    "not_found",
					"message": "study not found",
				})
			case "/resolve/sample-name/DN1234":
				writeMLWHInfoServerJSONForTest(w, mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: "DN1234",
					Sample: &mlwh.Sample{
						Name:           "DN1234",
						IDSampleLims:   "8675309",
						SangerSampleID: "DN1234",
						SupplierName:   "remote-supplier",
						Studies:        []mlwh.Study{{IDStudyLims: "5901", Name: "Remote Study"}},
						Libraries:      []mlwh.Library{{PipelineIDLims: "Chromium", IDStudyLims: "5901"}},
					},
				})
			case "/sample/DN1234/lanes":
				writeMLWHInfoServerJSONForTest(w, []mlwh.Lane{{IDRun: 49001, Position: 2, TagIndex: 7}})
			case "/sample/DN1234/irods":
				writeMLWHInfoServerJSONForTest(w, []mlwh.IRODSPath{{
					IDProduct:  "product-remote",
					Collection: "/seq/remote",
					DataObject: "DN1234.cram",
					IRODSPath:  "/seq/remote/DN1234.cram",
				}})
			default:
				w.WriteHeader(http.StatusInternalServerError)
				writeMLWHInfoServerJSONForTest(w, map[string]string{
					"code":    "upstream_impaired",
					"message": "unexpected path " + r.URL.Path,
				})
			}
		}))
		defer server.Close()

		t.Setenv("WA_MLWH_SERVER_URL", server.URL)
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_PASSWORD", "")
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		t.Setenv("WA_MLWH_CACHE_PASSWORD", "")

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "DN1234"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Identifier: DN1234")
		convey.So(output, convey.ShouldContainSubstring, "remote-supplier")
		convey.So(output, convey.ShouldContainSubstring, "Remote Study")
		convey.So(output, convey.ShouldContainSubstring, "library: Chromium / 5901")
		convey.So(output, convey.ShouldContainSubstring, "/seq/remote/DN1234.cram")
	})
}

func writeMLWHInfoServerJSONForTest(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic("encode mlwh info test response: " + err.Error())
	}
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

func TestMLWHInfoCommandSampleIncludesLibraryIdentifiersAndIRODSPaths(t *testing.T) {
	convey.Convey("Given a sample resolves to a library with identifiers and an iRODS path, when wa mlwh info runs, then text and JSON output include those fields", t, func() {
		newStub := func() *stubMLWHInfoClient {
			return &stubMLWHInfoClient{
				classify: func(_ context.Context, raw string) (mlwh.Match, error) {
					convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

					return mlwh.Match{
						Kind:      mlwh.KindSangerSampleName,
						Canonical: "7607STDY14643771",
						Sample: &mlwh.Sample{
							Name:           "7607STDY14643771",
							IDSampleLims:   "9575305",
							SangerSampleID: "7607STDY14643771",
							Libraries: []mlwh.Library{{
								PipelineIDLims: "Custom",
								IDStudyLims:    "7607",
								LibraryID:      "71046409",
								IDLibraryLims:  "SQPP-47463-G:B1",
							}},
						},
					}, nil
				},
				studiesForSample: func(_ context.Context, _ string) ([]mlwh.Study, error) {
					return []mlwh.Study{{IDStudyLims: "7607", Name: "Target prioritisation"}}, nil
				},
				lanesForSample: func(_ context.Context, _ string, _, _ int) ([]mlwh.Lane, error) {
					return []mlwh.Lane{{IDRun: 48522, Position: 1, TagIndex: 1}}, nil
				},
				irodsPathsForSample: func(_ context.Context, _ string, _, _ int) ([]mlwh.IRODSPath, error) {
					return []mlwh.IRODSPath{{
						IDProduct:  "5c7e2518e6e4b9f0bff053374d43a2b1f9bbb84625f035148db857b9bb01bfc0",
						Collection: "/seq/illumina/runs/48/48522/plex1",
						DataObject: "48522#1.cram",
						IRODSPath:  "/seq/illumina/runs/48/48522/plex1/48522#1.cram",
					}}, nil
				},
			}
		}

		withStubMLWHInfoClient(t, newStub())
		textOutput, textErr := executeRootCommandForTest(t, []string{"mlwh", "info", "7607STDY14643771"})

		convey.So(textErr, convey.ShouldBeNil)
		convey.So(textOutput, convey.ShouldContainSubstring, "library: Custom / 7607 library_id=71046409 id_library_lims=SQPP-47463-G:B1")
		convey.So(textOutput, convey.ShouldContainSubstring, "/seq/illumina/runs/48/48522/plex1/48522#1.cram")

		withStubMLWHInfoClient(t, newStub())
		jsonOutput, jsonErr := executeRootCommandForTest(t, []string{"mlwh", "info", "7607STDY14643771", "--json"})

		convey.So(jsonErr, convey.ShouldBeNil)

		decoded := map[string]any{}
		convey.So(json.Unmarshal([]byte(jsonOutput), &decoded), convey.ShouldBeNil)
		sample := decoded["sample"].(map[string]any)
		libraries := sample["libraries"].([]any)
		library := libraries[0].(map[string]any)
		convey.So(library["library_id"], convey.ShouldEqual, "71046409")
		convey.So(library["id_library_lims"], convey.ShouldEqual, "SQPP-47463-G:B1")
		paths := decoded["irods_paths"].([]any)
		path := paths[0].(map[string]any)
		convey.So(path["irods_path"], convey.ShouldEqual, "/seq/illumina/runs/48/48522/plex1/48522#1.cram")
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

func TestMLWHInfoCommandUsesFastSampleNameResolver(t *testing.T) {
	convey.Convey("Given a canonical sample name, when wa mlwh info runs with and without --type sample, then it avoids the broader sample resolver cascade", t, func() {
		newStub := func() *stubMLWHInfoClient {
			return &stubMLWHInfoClient{
				classify: func(_ context.Context, _ string) (mlwh.Match, error) {
					t.Fatalf("ClassifyIdentifier must not be called after the sample-name fast path matches")

					return mlwh.Match{}, nil
				},
				resolveStudy: func(_ context.Context, raw string) (mlwh.Match, error) {
					convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

					return mlwh.Match{}, mlwh.ErrNotFound
				},
				resolveName: func(_ context.Context, raw string) (mlwh.Match, error) {
					convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

					return mlwh.Match{
						Kind:      mlwh.KindSangerSampleName,
						Canonical: raw,
						Sample: &mlwh.Sample{
							Name: raw,
							Libraries: []mlwh.Library{{
								PipelineIDLims: "Custom",
								IDStudyLims:    "7607",
							}},
							Studies: []mlwh.Study{{IDStudyLims: "7607"}},
						},
					}, nil
				},
				resolveSample: func(_ context.Context, _ string) (mlwh.Match, error) {
					t.Fatalf("ResolveSample must not be called after the sample-name fast path matches")

					return mlwh.Match{}, nil
				},
			}
		}

		withStubMLWHInfoClient(t, newStub())
		autoOutput, autoErr := executeRootCommandForTest(t, []string{"mlwh", "info", "7607STDY14643771"})

		convey.So(autoErr, convey.ShouldBeNil)
		convey.So(autoOutput, convey.ShouldContainSubstring, "7607STDY14643771")

		withStubMLWHInfoClient(t, newStub())
		typedOutput, typedErr := executeRootCommandForTest(t, []string{"mlwh", "info", "--type", "sample", "7607STDY14643771"})

		convey.So(typedErr, convey.ShouldBeNil)
		convey.So(typedOutput, convey.ShouldContainSubstring, "7607STDY14643771")
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
	convey.Convey("Given a never-synced cache error in local operator mode (WA_MLWH_DSN set, no server), when wa mlwh info runs, then stderr contains the actionable sync hint", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
		}

		// withStubMLWHInfoClient sets WA_MLWH_DSN and clears WA_MLWH_SERVER_URL,
		// i.e. local operator mode: the only mode where the user can sync, so the
		// sync hint is still correct here.
		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh sync")
	})
}

func TestMLWHInfoStudySinceRejectsNonPositiveDuration(t *testing.T) {
	convey.Convey("Given a --since duration that is not positive, when wa mlwh info runs for a study, "+
		"then it errors and never queries the recency window", t, func() {
		for _, value := range []string{"-1h", "-168h", "0s"} {
			stub := newStudyInfoStub()
			called := false
			stub.countSamplesWithDataAt = func(_ context.Context, _, _, _ string) (mlwh.Count, error) {
				called = true

				return mlwh.Count{}, nil
			}

			withStubMLWHInfoClient(t, stub)

			_, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901", "--since", value})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "duration must be positive")
			convey.So(called, convey.ShouldBeFalse)
		}
	})

	convey.Convey("Given a positive --since duration, when wa mlwh info runs for a study, "+
		"then it is accepted and resolved to ~now-duration", t, func() {
		var capturedSince string
		stub := newStudyInfoStub()
		stub.countSamplesWithDataAt = func(_ context.Context, _, since, _ string) (mlwh.Count, error) {
			capturedSince = since

			return mlwh.Count{Count: 1}, nil
		}

		withStubMLWHInfoClient(t, stub)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901", "--since", "1h"})

		convey.So(err, convey.ShouldBeNil)

		parsed, parseErr := time.Parse(time.RFC3339, capturedSince)
		convey.So(parseErr, convey.ShouldBeNil)

		want := time.Now().Add(-time.Hour)
		convey.So(parsed.Sub(want) < time.Minute && want.Sub(parsed) < time.Minute, convey.ShouldBeTrue)
	})
}

func withStubMLWHInfoClient(t *testing.T, stub *stubMLWHInfoClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHInfoClient
	t.Cleanup(func() { openMLWHInfoClient = original })

	openMLWHInfoClient = func(context.Context, mlwh.Config) (mlwhInfoClient, error) {
		return stub, nil
	}
}

func TestMLWHInfoCommandServerModeNeverSyncedDoesNotMentionSync(t *testing.T) {
	convey.Convey("Given a never-synced result via the MLWH server (WA_MLWH_SERVER_URL set, no WA_MLWH_DSN), when wa mlwh info runs, then it gives a neutral cache-unavailable message with no sync instruction", t, func() {
		stub := &stubMLWHInfoClient{
			resolveStudy: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrCacheNeverSynced
			},
		}

		withServerModeMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}

func TestMLWHInfoCommandServerModeNotFoundDoesNotMentionSync(t *testing.T) {
	convey.Convey("Given a not-found result via the MLWH server (WA_MLWH_SERVER_URL set, no WA_MLWH_DSN), when wa mlwh info runs, then it reports no match with no sync instruction", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
		}

		withServerModeMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "no-such-thing"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldContainSubstring, "no-such-thing")
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "no match")
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
	})
}

// withServerModeMLWHInfoClient wires the command to talk to an MLWH server (the
// normal end-user path): WA_MLWH_SERVER_URL is set and WA_MLWH_DSN is empty, so
// the user cannot sync. The remote-client opener is stubbed to return stub.
func withServerModeMLWHInfoClient(t *testing.T, stub *stubMLWHInfoClient) {
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

	original := openMLWHInfoRemoteClient
	t.Cleanup(func() { openMLWHInfoRemoteClient = original })

	openMLWHInfoRemoteClient = func(context.Context, mlwh.RemoteConfig) (mlwhInfoClient, error) {
		return stub, nil
	}
}

func newStudyInfoStub() *stubMLWHInfoClient {
	return &stubMLWHInfoClient{
		resolveStudy: func(_ context.Context, raw string) (mlwh.Match, error) {
			return mlwh.Match{
				Kind:      mlwh.KindStudyLimsID,
				Canonical: raw,
				Study:     &mlwh.Study{IDStudyLims: raw, Name: "Lung cancer GWAS"},
			}, nil
		},
		studyOverview: func(_ context.Context, id string) (mlwh.StudyOverview, error) {
			return mlwh.StudyOverview{
				IDStudyLims:     id,
				SamplesTotal:    100,
				SamplesWithData: 80,
				DataObjects:     1234,
				Runs:            5,
				Libraries:       12,
				AddedLast7Days:  3,
				NewestDataAdded: "2026-06-20T00:00:00Z",
				CacheSyncedAt:   "2026-06-27T00:00:00Z",
			}, nil
		},
		statusBreakdown: func(_ context.Context, id string) (mlwh.StatusBreakdown, error) {
			return mlwh.StatusBreakdown{
				IDStudyLims: id,
				Distinct:    mlwh.PhaseLadder{WithData: 80, SequencedNoData: 5, Registered: 15},
				PerPlatform: []mlwh.PlatformPhaseLadder{
					{Platform: "Illumina", Ladder: mlwh.PhaseLadder{WithData: 80}},
				},
				WithDetailedTimeline: 40,
			}, nil
		},
		countSamplesWithData: func(_ context.Context, _ string) (mlwh.Count, error) {
			return mlwh.Count{Count: 80}, nil
		},
		countSamplesWithDataAt: func(_ context.Context, _, _, _ string) (mlwh.Count, error) {
			return mlwh.Count{Count: 3}, nil
		},
		samplesWithoutData: func(_ context.Context, _ string, _, _ int) ([]mlwh.SampleWithData, error) {
			return []mlwh.SampleWithData{
				{Sample: mlwh.Sample{Name: "S1"}},
				{Sample: mlwh.Sample{Name: "S2"}},
			}, nil
		},
	}
}

func TestMLWHInfoStudyFeatureSections(t *testing.T) {
	convey.Convey("Given a study identifier, when wa mlwh info runs, then the new feature sections render in text and JSON", t, func() {
		convey.Convey("text output carries overview, status breakdown, data counts and without-data count", func() {
			withStubMLWHInfoClient(t, newStudyInfoStub())

			output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

			convey.So(err, convey.ShouldBeNil)
			convey.So(output, convey.ShouldContainSubstring, "Study overview:")
			convey.So(output, convey.ShouldContainSubstring, "samples_total: 100")
			convey.So(output, convey.ShouldContainSubstring, "added_last_7_days: 3")
			convey.So(output, convey.ShouldContainSubstring, "Status breakdown:")
			convey.So(output, convey.ShouldContainSubstring, "with_data: 80")
			convey.So(output, convey.ShouldContainSubstring, "Illumina")
			convey.So(output, convey.ShouldContainSubstring, "Samples with data:")
			convey.So(output, convey.ShouldContainSubstring, "all_time: 80")
			convey.So(output, convey.ShouldContainSubstring, "added_since")
			convey.So(output, convey.ShouldContainSubstring, "Samples without data:")
			convey.So(output, convey.ShouldContainSubstring, "count: 2")
		})

		convey.Convey("json output carries the structured feature data", func() {
			withStubMLWHInfoClient(t, newStudyInfoStub())

			output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901", "--json"})

			convey.So(err, convey.ShouldBeNil)

			decoded := map[string]any{}
			convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)

			overview := decoded["study_overview"].(map[string]any)
			convey.So(overview["samples_total"], convey.ShouldEqual, 100)

			breakdown := decoded["status_breakdown"].(map[string]any)
			distinct := breakdown["distinct"].(map[string]any)
			convey.So(distinct["with_data"], convey.ShouldEqual, 80)

			withData := decoded["samples_with_data_count"].(map[string]any)
			convey.So(withData["all_time"], convey.ShouldEqual, 80)
			convey.So(withData["added_since"], convey.ShouldEqual, 3)
			convey.So(withData["since"], convey.ShouldNotEqual, "")

			withoutData := decoded["samples_without_data_count"].(map[string]any)
			convey.So(withoutData["count"], convey.ShouldEqual, 2)
		})
	})
}

func TestMLWHInfoStudySinceDefaultsToSevenDaysAgo(t *testing.T) {
	convey.Convey("Given no --since, when wa mlwh info runs for a study, then the recency count uses ~now-7d", t, func() {
		var capturedSince string
		stub := newStudyInfoStub()
		stub.countSamplesWithDataAt = func(_ context.Context, _, since, _ string) (mlwh.Count, error) {
			capturedSince = since

			return mlwh.Count{Count: 3}, nil
		}

		withStubMLWHInfoClient(t, stub)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

		convey.So(err, convey.ShouldBeNil)

		parsed, parseErr := time.Parse(time.RFC3339, capturedSince)
		convey.So(parseErr, convey.ShouldBeNil)

		want := time.Now().Add(-7 * 24 * time.Hour)
		convey.So(parsed.Sub(want) < time.Minute && want.Sub(parsed) < time.Minute, convey.ShouldBeTrue)
	})
}

func TestMLWHInfoStudySinceOverride(t *testing.T) {
	convey.Convey("Given an explicit --since, when wa mlwh info runs for a study, then it overrides the default", t, func() {
		convey.Convey("an RFC3339 value is passed through verbatim", func() {
			var capturedSince string
			stub := newStudyInfoStub()
			stub.countSamplesWithDataAt = func(_ context.Context, _, since, _ string) (mlwh.Count, error) {
				capturedSince = since

				return mlwh.Count{Count: 7}, nil
			}

			withStubMLWHInfoClient(t, stub)

			_, err := executeRootCommandForTest(t,
				[]string{"mlwh", "info", "5901", "--since", "2026-01-02T03:04:05Z"})

			convey.So(err, convey.ShouldBeNil)
			convey.So(capturedSince, convey.ShouldEqual, "2026-01-02T03:04:05Z")
		})

		convey.Convey("a Go duration is converted to an RFC3339 timestamp ~now-duration", func() {
			var capturedSince string
			stub := newStudyInfoStub()
			stub.countSamplesWithDataAt = func(_ context.Context, _, since, _ string) (mlwh.Count, error) {
				capturedSince = since

				return mlwh.Count{Count: 7}, nil
			}

			withStubMLWHInfoClient(t, stub)

			_, err := executeRootCommandForTest(t,
				[]string{"mlwh", "info", "5901", "--since", "168h"})

			convey.So(err, convey.ShouldBeNil)

			parsed, parseErr := time.Parse(time.RFC3339, capturedSince)
			convey.So(parseErr, convey.ShouldBeNil)

			want := time.Now().Add(-168 * time.Hour)
			convey.So(parsed.Sub(want) < time.Minute && want.Sub(parsed) < time.Minute, convey.ShouldBeTrue)
		})
	})
}

func TestMLWHInfoStudyFeatureGracefulDegradation(t *testing.T) {
	convey.Convey("Given a study sub-endpoint errors, when wa mlwh info runs, then the command still succeeds and other sections render", t, func() {
		stub := newStudyInfoStub()
		stub.studyOverview = func(_ context.Context, _ string) (mlwh.StudyOverview, error) {
			return mlwh.StudyOverview{}, errors.New("overview boom")
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Status breakdown:")
		convey.So(output, convey.ShouldContainSubstring, "overview boom")
	})
}

func TestMLWHInfoSampleProgressSection(t *testing.T) {
	convey.Convey("Given a sample identifier, when wa mlwh info runs, then the sample progress section renders in text and JSON", t, func() {
		newStub := func() *stubMLWHInfoClient {
			return &stubMLWHInfoClient{
				classify: func(_ context.Context, raw string) (mlwh.Match, error) {
					return mlwh.Match{
						Kind:      mlwh.KindSangerSampleName,
						Canonical: raw,
						Sample:    &mlwh.Sample{Name: raw, IDSampleLims: "8675309"},
					}, nil
				},
				sampleProgress: func(_ context.Context, name string) (mlwh.SampleProgress, error) {
					return mlwh.SampleProgress{
						Sample:           mlwh.Sample{Name: name},
						Platforms:        []string{"Illumina"},
						BaselinePhase:    "delivered",
						QC:               "pass",
						DeliveredAt:      "2026-05-01T00:00:00Z",
						DetailedTimeline: true,
						Milestones: []mlwh.Milestone{
							{Name: "sequencing_done", ReachedAt: "2026-04-01T00:00:00Z"},
						},
						CurrentMilestone: "sequencing_done",
						Runs: []mlwh.RunStatusTimeline{
							{IDRun: 49001, Platform: "Illumina", Current: "qc complete"},
						},
						CacheSyncedAt: "2026-06-27T00:00:00Z",
					}, nil
				},
			}
		}

		convey.Convey("text output carries the progress section", func() {
			withStubMLWHInfoClient(t, newStub())

			output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "DN1234"})

			convey.So(err, convey.ShouldBeNil)
			convey.So(output, convey.ShouldContainSubstring, "Sample progress:")
			convey.So(output, convey.ShouldContainSubstring, "baseline_phase: delivered")
			convey.So(output, convey.ShouldContainSubstring, "qc: pass")
			convey.So(output, convey.ShouldContainSubstring, "sequencing_done")
			convey.So(output, convey.ShouldContainSubstring, "qc complete")
		})

		convey.Convey("json output carries the structured progress data", func() {
			withStubMLWHInfoClient(t, newStub())

			output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "DN1234", "--json"})

			convey.So(err, convey.ShouldBeNil)

			decoded := map[string]any{}
			convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)
			progress := decoded["sample_progress"].(map[string]any)
			convey.So(progress["baseline_phase"], convey.ShouldEqual, "delivered")
			convey.So(progress["qc"], convey.ShouldEqual, "pass")
		})
	})
}

func TestMLWHInfoSampleProgressNotTracked(t *testing.T) {
	convey.Convey("Given an ONT sample with not_tracked qc and empty runs, when wa mlwh info runs, then the progress section renders cleanly", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{
					Kind:      mlwh.KindSangerSampleName,
					Canonical: raw,
					Sample:    &mlwh.Sample{Name: raw},
				}, nil
			},
			sampleProgress: func(_ context.Context, name string) (mlwh.SampleProgress, error) {
				return mlwh.SampleProgress{
					Sample:           mlwh.Sample{Name: name},
					Platforms:        []string{"ONT"},
					BaselinePhase:    "registered",
					QC:               "not_tracked",
					DetailedTimeline: false,
					TimelineReason:   "not in tracking window",
				}, nil
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "ONT1"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Sample progress:")
		convey.So(output, convey.ShouldContainSubstring, "qc: not_tracked")
		convey.So(output, convey.ShouldContainSubstring, "runs: none")
	})
}

func TestMLWHInfoRunFeatureSections(t *testing.T) {
	convey.Convey("Given a run identifier, when wa mlwh info runs, then the run overview and status sections render in text and JSON", t, func() {
		newStub := func() *stubMLWHInfoClient {
			return &stubMLWHInfoClient{
				resolveRun: func(_ context.Context, _ string) (mlwh.Match, error) {
					return mlwh.Match{
						Kind:      mlwh.KindRunID,
						Canonical: "49001",
						Run:       &mlwh.Run{IDRun: 49001},
					}, nil
				},
				runOverview: func(_ context.Context, _ string) (mlwh.RunOverview, error) {
					return mlwh.RunOverview{
						IDRun:       49001,
						Samples:     96,
						Studies:     2,
						DataObjects: 480,
					}, nil
				},
				runStatus: func(_ context.Context, _ string) (mlwh.RunStatusTimeline, error) {
					return mlwh.RunStatusTimeline{
						IDRun:    49001,
						Platform: "Illumina",
						Current:  "qc complete",
						Events: []mlwh.RunStatusEvent{
							{Phase: "run pending", EnteredAt: "2026-04-01T00:00:00Z"},
						},
					}, nil
				},
			}
		}

		convey.Convey("text output carries both run sections", func() {
			withStubMLWHInfoClient(t, newStub())

			output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "49001", "--type", "run"})

			convey.So(err, convey.ShouldBeNil)
			convey.So(output, convey.ShouldContainSubstring, "Run overview:")
			convey.So(output, convey.ShouldContainSubstring, "samples: 96")
			convey.So(output, convey.ShouldContainSubstring, "Run status:")
			convey.So(output, convey.ShouldContainSubstring, "current: qc complete")
			convey.So(output, convey.ShouldContainSubstring, "run pending")
		})

		convey.Convey("json output carries the structured run data", func() {
			withStubMLWHInfoClient(t, newStub())

			output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "49001", "--type", "run", "--json"})

			convey.So(err, convey.ShouldBeNil)

			decoded := map[string]any{}
			convey.So(json.Unmarshal([]byte(output), &decoded), convey.ShouldBeNil)

			overview := decoded["run_overview"].(map[string]any)
			convey.So(overview["samples"], convey.ShouldEqual, 96)

			status := decoded["run_status"].(map[string]any)
			convey.So(status["current"], convey.ShouldEqual, "qc complete")
		})
	})
}

func TestMLWHInfoRunStatusNotTracked(t *testing.T) {
	convey.Convey("Given a non-Illumina run with no NPG status, when wa mlwh info runs, then the status section renders cleanly and the command succeeds", t, func() {
		stub := &stubMLWHInfoClient{
			resolveRun: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{
					Kind:      mlwh.KindRunID,
					Canonical: "70000",
					Run:       &mlwh.Run{IDRun: 70000},
				}, nil
			},
			runOverview: func(_ context.Context, _ string) (mlwh.RunOverview, error) {
				return mlwh.RunOverview{IDRun: 70000, Samples: 1}, nil
			},
			runStatus: func(_ context.Context, _ string) (mlwh.RunStatusTimeline, error) {
				return mlwh.RunStatusTimeline{}, mlwh.ErrNotFound
			},
		}

		withStubMLWHInfoClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "70000", "--type", "run"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Run overview:")
		convey.So(output, convey.ShouldContainSubstring, "Run status:")
		convey.So(output, convey.ShouldContainSubstring, "none")
	})
}

func TestMLWHInfoSinceFlagDocumented(t *testing.T) {
	convey.Convey("wa mlwh info --help documents the --since flag", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "--since")
	})
}

func TestMLWHInfoCommandCacheOnlyNeverSyncedDoesNotMentionSync(t *testing.T) {
	convey.Convey("Given a never-synced cache in local cache-only mode (WA_MLWH_CACHE_PATH set, no WA_MLWH_DSN, no server), when wa mlwh info runs, then it gives a neutral cache-unavailable message with no sync instruction", t, func() {
		stub := &stubMLWHInfoClient{
			classify: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
		}

		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_BACKEND_URL", "")
		t.Setenv("WA_MLWH_CACHE_PATH", "/tmp/does-not-matter-cache.sqlite")
		t.Setenv("WA_ENV", "")
		t.Setenv("WA_TEST_SEQMETA_PORT", "")
		t.Setenv("WA_DEV_SEQMETA_PORT", "")
		t.Setenv("WA_PROD_SEQMETA_PORT", "")

		original := openMLWHInfoClient
		t.Cleanup(func() { openMLWHInfoClient = original })

		openMLWHInfoClient = func(context.Context, mlwh.Config) (mlwhInfoClient, error) {
			return stub, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "info", "5901"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(output, convey.ShouldNotContainSubstring, "wa mlwh sync")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not available")
	})
}
