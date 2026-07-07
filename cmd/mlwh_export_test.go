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

func TestMLWHExportIRODSStudyPrintsCramTSVAndPageStatusD1b(t *testing.T) {
	convey.Convey("D1b.1/D1b.3: Given an iRODS study export, when the command runs without --all, then it prints cram TSV and bounded-page status", t, func() {
		var capturedRel mlwh.ExportRelationship
		var capturedParent string
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedRel = rel
				capturedParent = parentID
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns:    []string{"supplier_name", "study_accession_number", "sanger_sample_id", "manual_qc", "irods_path"},
					Rows:       [][]string{{"supplier-a", "ENA5901", "SANGER-1", "pass", "/seq/5901/5901_1#1.cram"}},
					Total:      2,
					NextCursor: "cursor-2",
					Format:     "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "irods", "study", "5901", "--file-type", "cram"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "supplier_name\tstudy_accession_number\tsanger_sample_id\tmanual_qc\tirods_path")
		convey.So(output, convey.ShouldContainSubstring, "supplier-a\tENA5901\tSANGER-1\tpass\t/seq/5901/5901_1#1.cram")
		convey.So(output, convey.ShouldContainSubstring, "bounded page")
		convey.So(output, convey.ShouldContainSubstring, "total=2")
		convey.So(output, convey.ShouldContainSubstring, "next_cursor=cursor-2")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "irods", ParentKind: "study"})
		convey.So(capturedParent, convey.ShouldEqual, "5901")
		convey.So(capturedOptions.FileType, convey.ShouldEqual, "cram")
		convey.So(capturedOptions.Format, convey.ShouldEqual, "tsv")
		convey.So(capturedOptions.Limit, convey.ShouldEqual, 50)
		convey.So(capturedOptions.All, convey.ShouldBeFalse)
		convey.So(stub.closed, convey.ShouldBeTrue)
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
		convey.So(output, convey.ShouldContainSubstring, "total=732")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "sample-crams", ParentKind: "study"})
		convey.So(capturedParent, convey.ShouldEqual, "7568")
		convey.So(capturedOptions.FileType, convey.ShouldEqual, "cram")
		convey.So(capturedOptions.DeliverablesOnly, convey.ShouldBeNil)
		convey.So(capturedOptions.Format, convey.ShouldEqual, "tsv")
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func TestMLWHExportAllStatesCompleteSetD1b(t *testing.T) {
	convey.Convey("D1b.3: Given --all, when the export command runs, then it states the complete set was emitted", t, func() {
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, _ mlwh.ExportRelationship, _ string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedOptions = opts

				return mlwh.ExportResult{
					Columns:  []string{"irods_path"},
					Rows:     [][]string{{"/seq/a.cram"}, {"/seq/b.cram"}},
					Total:    -1,
					Complete: true,
					Format:   "tsv",
				}, nil
			},
		}
		withStubMLWHExportClient(t, stub)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "irods", "study", "5901", "--all"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "irods_path")
		convey.So(output, convey.ShouldContainSubstring, "complete set")
		convey.So(output, convey.ShouldContainSubstring, "rows=2")
		convey.So(capturedOptions.All, convey.ShouldBeTrue)
	})
}

func TestMLWHExportAllServerPagesBoundedRequestsP1(t *testing.T) {
	convey.Convey("P1/server-export-all: Given server-mode --all over a multi-page iRODS export", t, func() {
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
			"mlwh", "export", "irods", "study", "5901", "--all", "--limit", "2", "--columns", "irods_path", "--server", server.URL,
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "irods_path")
		convey.So(output, convey.ShouldContainSubstring, "/seq/a.cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/b.cram")
		convey.So(output, convey.ShouldContainSubstring, "/seq/c.cram")
		convey.So(output, convey.ShouldContainSubstring, "complete set emitted: rows=3")

		requests := drainMLWHExportRequestURIs(requestURIs)
		convey.So(requests, convey.ShouldHaveLength, 2)
		convey.So(mlwhExportRequestQueryValue(t, requests[0], "all"), convey.ShouldEqual, "")
		convey.So(mlwhExportRequestQueryValue(t, requests[1], "all"), convey.ShouldEqual, "")
		convey.So(mlwhExportRequestQueryValue(t, requests[0], "limit"), convey.ShouldEqual, "2")
		convey.So(mlwhExportRequestQueryValue(t, requests[1], "limit"), convey.ShouldEqual, "2")
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
