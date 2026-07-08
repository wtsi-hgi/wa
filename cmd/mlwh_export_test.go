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
	"net/url"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestMLWHExportReplacesIRODSSurfaceD1b(t *testing.T) {
	convey.Convey("D1b: Given wa mlwh help, then export is listed and the removed irods command is not", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "export")
		convey.So(output, convey.ShouldNotContainSubstring, "    irods")
	})

	convey.Convey("D1b: Given the removed wa mlwh irods command, when invoked, then cobra reports it as unknown", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "irods", "study", "5901"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "unknown command")
	})
}

func TestMLWHExportHelpDocumentsGrammarVocabularyAndFilters(t *testing.T) {
	convey.Convey("Given wa mlwh export -h, then the rendered help explains the grammar, vocabulary, and MLWH-specific filters", t, func() {
		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "-h"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh export <children> <parent-kind> <parent-id>")
		convey.So(output, convey.ShouldNotContainSubstring, "Relationships include")
		convey.So(output, convey.ShouldContainSubstring, "Children:")
		convey.So(output, convey.ShouldContainSubstring, "  irods (alias: files)")
		convey.So(output, convey.ShouldContainSubstring, "  samples")
		convey.So(output, convey.ShouldContainSubstring, "  runs")
		convey.So(output, convey.ShouldContainSubstring, "  libraries")
		convey.So(output, convey.ShouldContainSubstring, "  lanes")
		convey.So(output, convey.ShouldContainSubstring, "  studies")
		convey.So(output, convey.ShouldContainSubstring, "  users")
		convey.So(output, convey.ShouldContainSubstring, "  sample-crams")
		convey.So(output, convey.ShouldContainSubstring, "Parent kinds and parent-id values:")
		convey.So(output, convey.ShouldContainSubstring, "study: a study LIMS id, study UUID, accession number or study name")
		convey.So(output, convey.ShouldContainSubstring, "sample: a sample UUID, LIMS id, Sanger sample name/id, supplier name, accession or donor id")
		convey.So(output, convey.ShouldContainSubstring, "run: an Illumina NPG id_run")
		convey.So(output, convey.ShouldContainSubstring, "library: a pipeline_id_lims, library_id, id_library_lims or library type")
		convey.So(output, convey.ShouldContainSubstring, "faculty-sponsor: a case-insensitive faculty_sponsor substring")
		convey.So(output, convey.ShouldContainSubstring, "user: a study_users name, login or email substring")
		convey.So(output, convey.ShouldContainSubstring, "programme: an exact programme value")
		convey.So(output, convey.ShouldContainSubstring, "Columns:")
		convey.So(output, convey.ShouldContainSubstring, "irods/files")
		convey.So(output, convey.ShouldContainSubstring, "supplier_name")
		convey.So(output, convey.ShouldContainSubstring, "supplier_sample_name")
		convey.So(output, convey.ShouldContainSubstring, "irods_path")
		convey.So(output, convey.ShouldContainSubstring, "sample-crams")
		convey.So(output, convey.ShouldContainSubstring, "irods_cram_path")
		convey.So(output, convey.ShouldContainSubstring, "Deliverables:")
		convey.So(output, convey.ShouldContainSubstring, "iseq_flowcell.entity_type IN ('library','library_indexed')")
		convey.So(output, convey.ShouldContainSubstring, "approximates iRODS target=1")
		convey.So(output, convey.ShouldContainSubstring, "not is_spiked")
		convey.So(output, convey.ShouldContainSubstring, "--sort created-desc is the only supported explicit sort")
		convey.So(output, convey.ShouldContainSubstring, "Exports always emit the complete matching set")
		convey.So(output, convey.ShouldContainSubstring, "RFC3339 timestamp such as")
		convey.So(output, convey.ShouldContainSubstring, "2026-07-01T00:00:00Z")
		convey.So(output, convey.ShouldNotContainSubstring, "--all")
		convey.So(output, convey.ShouldNotContainSubstring, "--limit")
		convey.So(output, convey.ShouldNotContainSubstring, "--cursor")
		convey.So(output, convey.ShouldNotContainSubstring, "--offset")
	})
}

func TestMLWHExportPagingFlagsAreNotUserFacing(t *testing.T) {
	for _, flag := range []string{"--all", "--limit", "--cursor", "--offset"} {
		flag := flag
		convey.Convey("Given "+flag+" is supplied to wa mlwh export, then cobra rejects the removed paging flag", t, func() {
			args := []string{"mlwh", "export", "irods", "study", "5901", flag}
			if flag == "--limit" || flag == "--cursor" || flag == "--offset" {
				args = append(args, "1")
			}

			output, err := executeRootCommandForTest(t, args)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(output, convey.ShouldContainSubstring, "unknown flag: "+flag)
		})
	}
}

type stubMLWHExportClient struct {
	export func(context.Context, mlwh.ExportRelationship, string, mlwh.ExportOptions) (mlwh.ExportResult, error)
	closed bool
}

func (s *stubMLWHExportClient) Export(ctx context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
	if s.export != nil {
		return s.export(ctx, rel, parentID, opts)
	}

	return mlwh.ExportResult{}, errors.New("export not stubbed")
}

func (s *stubMLWHExportClient) Close() error {
	s.closed = true

	return nil
}

func TestMLWHExportIRODSStudyPrintsCompleteCramTSVOnlyD1b(t *testing.T) {
	convey.Convey("D1b.1/D1b.3: Given an iRODS study export, when the command runs, then it prints complete cram TSV without status text", t, func() {
		var capturedRel mlwh.ExportRelationship
		var capturedParent string
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedRel = rel
				capturedParent = parentID
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns:  []string{"supplier_name", "sanger_sample_id", "manual_qc", "irods_path"},
					Rows:     [][]string{{"supplier-a", "SANGER-1", "pass", "/seq/5901/5901_1#1.cram"}},
					Total:    -1,
					Complete: true,
					Format:   "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "irods", "study", "5901", "--file-type", "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldEqual, "supplier_name\tsanger_sample_id\tmanual_qc\tirods_path\nsupplier-a\tSANGER-1\tpass\t/seq/5901/5901_1#1.cram")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "irods", ParentKind: "study"})
		convey.So(capturedParent, convey.ShouldEqual, "5901")
		convey.So(capturedOptions.FileType, convey.ShouldEqual, "cram")
		convey.So(capturedOptions.Format, convey.ShouldEqual, "tsv")
		convey.So(capturedOptions.Limit, convey.ShouldEqual, 0)
		convey.So(capturedOptions.Offset, convey.ShouldEqual, 0)
		convey.So(capturedOptions.Cursor, convey.ShouldBeEmpty)
		convey.So(capturedOptions.All, convey.ShouldBeTrue)
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHExportIRODSRunPrintsMergedCompositeH2(t *testing.T) {
	convey.Convey("H2: Given an iRODS run export includes a merged composite, when the command runs, then it prints the run-scoped rows", t, func() {
		var capturedRel mlwh.ExportRelationship
		var capturedParent string
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedRel = rel
				capturedParent = parentID
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns: []string{"name", "id_run", "merged", "irods_path"},
					Rows: [][]string{
						{"sample-49348", "0", "true", "/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram"},
						{"sample-49348", "49348", "false", "/seq/illumina/runs/49/49348/lane1/plex1/49348_1#1.cram"},
					},
					Total:    2,
					Complete: true,
					Format:   "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "export", "irods", "run", "49348",
			"--columns", "name,id_run,merged,irods_path",
			"--file-type", "cram",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "name\tid_run\tmerged\tirods_path")
		convey.So(output, convey.ShouldContainSubstring, "sample-49348\t0\ttrue\t/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram")
		convey.So(output, convey.ShouldContainSubstring, "sample-49348\t49348\tfalse\t/seq/illumina/runs/49/49348/lane1/plex1/49348_1#1.cram")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "irods", ParentKind: "run"})
		convey.So(capturedParent, convey.ShouldEqual, "49348")
		convey.So(capturedOptions.Columns, convey.ShouldResemble, []string{"name", "id_run", "merged", "irods_path"})
		convey.So(capturedOptions.FileType, convey.ShouldEqual, "cram")
	})
}

func TestMLWHExportRunsSampleHonoursSelectedColumnsD1b(t *testing.T) {
	convey.Convey("D1b.2: Given a runs-of-sample export with selected columns, when the command runs, then it prints exactly those columns", t, func() {
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				convey.So(rel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "runs", ParentKind: "sample"})
				convey.So(parentID, convey.ShouldEqual, "DN1234")
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns:  []string{"id_run", "platform", "run_date"},
					Rows:     [][]string{{"61010", "Illumina", "2026-06-03"}},
					Total:    1,
					Complete: true,
					Format:   "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "runs", "sample", "DN1234", "--columns", "id_run,platform,run_date"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "id_run\tplatform\trun_date")
		convey.So(output, convey.ShouldContainSubstring, "61010\tIllumina\t2026-06-03")
		convey.So(capturedOptions.Columns, convey.ShouldResemble, []string{"id_run", "platform", "run_date"})
	})
}

func TestMLWHExportUsersStudyHonoursRoleFilterG3(t *testing.T) {
	convey.Convey("G3 acceptance 3: Given users of study 7568, when --role owner,manager,follower is supplied, then it prints role,name,login,email rows", t, func() {
		var capturedRel mlwh.ExportRelationship
		var capturedParent string
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedRel = rel
				capturedParent = parentID
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns: []string{"role", "name", "login", "email"},
					Rows: [][]string{
						{"follower", "Fran Follower", "ff1", "ff1@sanger.ac.uk"},
						{"manager", "Maya Manager", "mm1", "mm1@sanger.ac.uk"},
						{"owner", "Olive Owner", "oo1", "oo1@sanger.ac.uk"},
					},
					Total:    3,
					Complete: true,
					Format:   "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "export", "users", "study", "7568", "--role", "owner,manager,follower",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "role\tname\tlogin\temail")
		convey.So(output, convey.ShouldContainSubstring, "follower\tFran Follower\tff1\tff1@sanger.ac.uk")
		convey.So(output, convey.ShouldContainSubstring, "manager\tMaya Manager\tmm1\tmm1@sanger.ac.uk")
		convey.So(output, convey.ShouldContainSubstring, "owner\tOlive Owner\too1\too1@sanger.ac.uk")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "users", ParentKind: "study"})
		convey.So(capturedParent, convey.ShouldEqual, "7568")
		convey.So(capturedOptions.Role, convey.ShouldEqual, "owner,manager,follower")
	})
}

func TestMLWHExportSampleCramsStudy7568H3(t *testing.T) {
	convey.Convey("H3: Given sample-crams of study 7568, when the export command runs, then it requests the sample-crams relationship and prints merged-aware CRAM rows", t, func() {
		var capturedRel mlwh.ExportRelationship
		var capturedParent string
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedRel = rel
				capturedParent = parentID
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns: []string{"name", "ega_id", "irods_cram_path", "merged"},
					Rows: [][]string{
						{"7568STDYCONTROL", "ERS7568CTRL", "/seq/illumina/runs/49/52554/lane1/plex1/52554_1#1.cram", "false"},
						{"7568STDY9419243", "ERS7568001", "/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram", "true"},
					},
					Total:  732,
					Format: "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "sample-crams", "study", "7568"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "name\tega_id\tirods_cram_path\tmerged")
		convey.So(output, convey.ShouldContainSubstring, "7568STDYCONTROL\tERS7568CTRL\t/seq/illumina/runs/49/52554/lane1/plex1/52554_1#1.cram\tfalse")
		convey.So(output, convey.ShouldContainSubstring, "7568STDY9419243\tERS7568001\t/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram\ttrue")
		convey.So(output, convey.ShouldNotContainSubstring, "total=732")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "sample-crams", ParentKind: "study"})
		convey.So(capturedParent, convey.ShouldEqual, "7568")
		convey.So(capturedOptions.FileType, convey.ShouldEqual, "cram")
		convey.So(capturedOptions.DeliverablesOnly, convey.ShouldBeNil)
		convey.So(capturedOptions.Format, convey.ShouldEqual, "tsv")
		convey.So(capturedOptions.All, convey.ShouldBeTrue)
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHExportSampleCramsIncludeControlsD1(t *testing.T) {
	convey.Convey("D1: Given sample-crams export controls are requested, when the export command runs, then it passes an explicit include-controls override", t, func() {
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, _ mlwh.ExportRelationship, _ string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedOptions = opts

				return mlwh.ExportResult{Columns: []string{"name"}, Rows: [][]string{}, Total: 0, Format: "tsv"}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "export", "sample-crams", "study", "7568", "--include-controls"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(capturedOptions.DeliverablesOnly, convey.ShouldNotBeNil)
		convey.So(*capturedOptions.DeliverablesOnly, convey.ShouldBeFalse)
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHExportRunsSampleRequestsCompleteSetD1b(t *testing.T) {
	convey.Convey("D1b.3: Given a non-iRODS export, when the command runs, then it requests the complete set without status text", t, func() {
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, _ mlwh.ExportRelationship, _ string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns:  []string{"id_run"},
					Rows:     [][]string{{"61010"}, {"61011"}},
					Total:    -1,
					Complete: true,
					Format:   "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "runs", "sample", "DN1234"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldEqual, "id_run\n61010\n61011")
		convey.So(capturedOptions.All, convey.ShouldBeTrue)
		convey.So(capturedOptions.Limit, convey.ShouldEqual, 0)
	})
}

func TestMLWHExportAllServerPagesBoundedRequestsP1(t *testing.T) {
	convey.Convey("P1/server-export-all: Given server-mode export over a multi-page iRODS export", t, func() {
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_BACKEND_URL", "")
		t.Setenv("WA_ENV", "")

		requestURIs := make(chan string, 4)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestURIs <- r.URL.RequestURI()
			if r.URL.Path != "/export/irods/study/5901" {
				http.NotFound(w, r)

				return
			}
			w.Header().Set("Content-Type", "application/json")

			result := mlwh.ExportResult{
				Columns: []string{"irods_path"},
				Total:   3,
				Format:  "tsv",
			}
			switch {
			case r.URL.Query().Get("all") == "true":
				result.Rows = [][]string{{"/seq/a.cram"}, {"/seq/b.cram"}, {"/seq/c.cram"}}
				result.Total = -1
				result.Complete = true
			case r.URL.Query().Get("cursor") == "cursor-2":
				result.Rows = [][]string{{"/seq/c.cram"}}
				result.Complete = true
			default:
				result.Rows = [][]string{{"/seq/a.cram"}, {"/seq/b.cram"}}
				result.NextCursor = "cursor-2"
			}

			_ = json.NewEncoder(w).Encode(result)
		}))
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{
			"mlwh", "export", "irods", "study", "5901", "--columns", "irods_path", "--server", server.URL,
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "irods_path")
		convey.So(output, convey.ShouldContainSubstring, "/seq/a.cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/b.cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/c.cram")
		convey.So(output, convey.ShouldNotContainSubstring, "complete set emitted")

		requests := drainMLWHExportRequestURIs(requestURIs)
		convey.So(requests, convey.ShouldHaveLength, 2)
		convey.So(mlwhExportRequestQueryValue(t, requests[0], "all"), convey.ShouldEqual, "")
		convey.So(mlwhExportRequestQueryValue(t, requests[1], "all"), convey.ShouldEqual, "")
		convey.So(mlwhExportRequestQueryValue(t, requests[0], "limit"), convey.ShouldEqual, "1000")
		convey.So(mlwhExportRequestQueryValue(t, requests[1], "limit"), convey.ShouldEqual, "1000")
		convey.So(mlwhExportRequestQueryValue(t, requests[0], "cursor"), convey.ShouldEqual, "")
		convey.So(mlwhExportRequestQueryValue(t, requests[1], "cursor"), convey.ShouldEqual, "cursor-2")
	})
}

func drainMLWHExportRequestURIs(requestURIs <-chan string) []string {
	requests := make([]string, 0, len(requestURIs))
	for len(requestURIs) > 0 {
		requests = append(requests, <-requestURIs)
	}

	return requests
}

func mlwhExportRequestQueryValue(t *testing.T, rawURI, key string) string {
	t.Helper()

	parsed, err := url.ParseRequestURI(rawURI)
	convey.So(err, convey.ShouldBeNil)

	return parsed.Query().Get(key)
}

func TestMLWHExportUnknownParentIDIsCleanNotFoundD1b(t *testing.T) {
	convey.Convey("D1b.4: Given an unknown parent id, when export runs, then it renders clean not-found and exits 0", t, func() {
		stub := &stubMLWHExportClient{
			export: func(context.Context, mlwh.ExportRelationship, string, mlwh.ExportOptions) (mlwh.ExportResult, error) {
				return mlwh.ExportResult{}, mlwh.ErrNotFound
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "runs", "sample", "missing-sample"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.ToLower(output), convey.ShouldContainSubstring, "not found")
		convey.So(output, convey.ShouldContainSubstring, "sample")
		convey.So(output, convey.ShouldContainSubstring, "missing-sample")
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrNotFound.Error())
	})
}

func TestMLWHExportNeverSyncedCacheIsCleanD1b(t *testing.T) {
	convey.Convey("D1b reviewer: Given the export client reports a never-synced cache, when export runs, then it prints a neutral message and exits 0", t, func() {
		stub := &stubMLWHExportClient{
			export: func(context.Context, mlwh.ExportRelationship, string, mlwh.ExportOptions) (mlwh.ExportResult, error) {
				return mlwh.ExportResult{}, errors.Join(mlwh.ErrNotFound, mlwh.ErrCacheNeverSynced)
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "irods", "study", "5901"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, mlwhCacheUnavailableMessage)
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
		convey.So(output, convey.ShouldNotContainSubstring, mlwh.ErrNotFound.Error())
	})
}

func withStubMLWHExportClient(t *testing.T, stub *stubMLWHExportClient) {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	original := openMLWHExportClient
	t.Cleanup(func() { openMLWHExportClient = original })
	openMLWHExportClient = func(context.Context, mlwh.Config) (mlwhExportClient, error) {
		return stub, nil
	}
}

func TestMLWHExportServerFlagUsesRemoteClientD1b(t *testing.T) {
	convey.Convey("D1b: Given --server, when export runs, then it opens the remote export client", t, func() {
		t.Setenv("WA_MLWH_DSN", "")
		t.Setenv("WA_MLWH_SERVER_URL", "")
		t.Setenv("WA_MLWH_BACKEND_URL", "")
		t.Setenv("WA_ENV", "")

		stub := &stubMLWHExportClient{
			export: func(context.Context, mlwh.ExportRelationship, string, mlwh.ExportOptions) (mlwh.ExportResult, error) {
				return mlwh.ExportResult{Columns: []string{"id_run"}, Rows: [][]string{{"61010"}}, Total: 1, Format: "tsv"}, nil
			},
		}

		var capturedBaseURL string
		original := openMLWHExportRemoteClient
		t.Cleanup(func() { openMLWHExportRemoteClient = original })
		openMLWHExportRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhExportClient, error) {
			capturedBaseURL = cfg.BaseURL

			return stub, nil
		}

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "runs", "sample", "DN1234", "--server", "http://mlwh.example:8091"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "61010")
		convey.So(capturedBaseURL, convey.ShouldEqual, "http://mlwh.example:8091")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}
