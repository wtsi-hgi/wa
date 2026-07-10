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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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
		convey.So(output, convey.ShouldContainSubstring, "  products: product-grained rows, one per distinct id_run/lane/tag, including products with no iRODS object; parent-kind: study")
		convey.So(strings.Index(output, "  sample-crams:"), convey.ShouldBeLessThan, strings.Index(output, "  irods (alias: files)"))
		convey.So(output, convey.ShouldContainSubstring, "  sample-crams")
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
		convey.So(output, convey.ShouldContainSubstring, "products:")
		convey.So(output, convey.ShouldContainSubstring, "default: name, supplier_name, accession_number, sanger_sample_id, id_run, lane, tag_index, manual_qc")
		convey.So(output, convey.ShouldContainSubstring, "available: name, supplier_name (alias: supplier_sample_name), accession_number, sanger_sample_id, id_run, lane (alias: position), tag_index, manual_qc, irods_path, irods_unmatched, reason, id_study_lims, study_accession_number")
		convey.So(output, convey.ShouldContainSubstring, "irods/files")
		convey.So(output, convey.ShouldContainSubstring, "supplier_name")
		convey.So(output, convey.ShouldContainSubstring, "supplier_sample_name")
		convey.So(output, convey.ShouldContainSubstring, "irods_path")
		convey.So(output, convey.ShouldContainSubstring, "Product exports:")
		convey.So(output, convey.ShouldContainSubstring, "Products are product-grained")
		convey.So(output, convey.ShouldContainSubstring, "--file-type only")
		convey.So(output, convey.ShouldContainSubstring, "restricts the attached irods_path")
		convey.So(output, convey.ShouldContainSubstring, "blank irods_path is expected")
		convey.So(output, convey.ShouldContainSubstring, "irods_unmatched")
		convey.So(output, convey.ShouldContainSubstring, "reason")
		convey.So(output, convey.ShouldContainSubstring, "sample-crams")
		convey.So(output, convey.ShouldContainSubstring, "accession_number")
		convey.So(output, convey.ShouldNotContainSubstring, "ega_id")
		convey.So(output, convey.ShouldNotContainSubstring, "irods_cram_path")
		convey.So(output, convey.ShouldContainSubstring, "Deliverables:")
		convey.So(output, convey.ShouldContainSubstring, "iseq_flowcell.entity_type IN ('library','library_indexed')")
		convey.So(output, convey.ShouldContainSubstring, "approximates iRODS target=1")
		convey.So(output, convey.ShouldContainSubstring, "not is_spiked")
		convey.So(output, convey.ShouldContainSubstring, "--sort created-desc is the only supported explicit sort")
		convey.So(output, convey.ShouldContainSubstring, "By default, exports include every matching row")
		convey.So(output, convey.ShouldContainSubstring, "append no success message")
		convey.So(output, convey.ShouldContainSubstring, "Use --limit to emit one bounded page")
		convey.So(output, convey.ShouldContainSubstring, "Omit --limit to export")
		convey.So(output, convey.ShouldNotContainSubstring, "user-facing paging")
		convey.So(output, convey.ShouldNotContainSubstring, "internal pages")
		convey.So(output, convey.ShouldContainSubstring, "see `wa mlwh -h`")
		convey.So(output, convey.ShouldNotContainSubstring, "before resolving:")
		convey.So(output, convey.ShouldNotContainSubstring, "Normal CLI users")
		convey.So(output, convey.ShouldContainSubstring, "RFC3339 timestamp such as")
		convey.So(output, convey.ShouldContainSubstring, "2026-07-01T00:00:00Z")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh export sample-crams study 5901")
		convey.So(output, convey.ShouldContainSubstring, "wa mlwh export products study 7568 --file-type cram --columns name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc,irods_path,irods_unmatched,reason")
		convey.So(output, convey.ShouldNotContainSubstring, "--all")
		convey.So(output, convey.ShouldContainSubstring, "--limit")
		convey.So(output, convey.ShouldContainSubstring, "--cursor")
		convey.So(output, convey.ShouldNotContainSubstring, "--offset")
	})
}

func TestMLWHExportUnsupportedPagingFlagsAreNotUserFacing(t *testing.T) {
	for _, flag := range []string{"--all", "--offset"} {
		flag := flag
		convey.Convey("Given "+flag+" is supplied to wa mlwh export, then cobra rejects the removed paging flag", t, func() {
			args := []string{"mlwh", "export", "irods", "study", "5901", flag}
			if flag == "--offset" {
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

func TestMLWHExportProductsStudyRendersTSVThroughRenderPathI1(t *testing.T) {
	convey.Convey("I1.1/I1.4: Given products of study S1, when default TSV export runs, then stdout is rectangular data and no bounded status is written", t, func() {
		var capturedRel mlwh.ExportRelationship
		var capturedParent string
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, rel mlwh.ExportRelationship, parentID string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedRel = rel
				capturedParent = parentID
				capturedOptions = opts

				return mlwhProductExportResultForTest(opts.Format), nil
			},
		}
		withStubMLWHExportClient(t, stub)

		stdout, stderr, err := executeRootCommandStreamsForTest(t, []string{
			"mlwh", "export", "products", "study", "S1",
			"--columns", strings.Join(mlwhProductExportColumnsForTest(), ","),
			"--file-type", "cram",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.TrimSpace(stdout), convey.ShouldEqual, strings.Join([]string{
			"name\tsupplier_name\tid_run\tlane\ttag_index\tmanual_qc\tirods_path\tirods_unmatched\treason",
			"S1-A\tsupplier-a\t61010\t1\t1\tpass\t/seq/S1-A.cram\tfalse\t",
			"S1-B\tsupplier-b\t61011\t2\t0\tpending\t\ttrue\tmerged_cram_gap",
		}, "\n"))
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(stdout, convey.ShouldNotContainSubstring, "study_manifest")
		convey.So(stdout, convey.ShouldNotContainSubstring, "success")
		convey.So(capturedRel, convey.ShouldResemble, mlwh.ExportRelationship{Children: "products", ParentKind: "study"})
		convey.So(capturedParent, convey.ShouldEqual, "S1")
		convey.So(capturedOptions.Columns, convey.ShouldResemble, mlwhProductExportColumnsForTest())
		convey.So(capturedOptions.FileType, convey.ShouldEqual, "cram")
		convey.So(capturedOptions.Format, convey.ShouldEqual, "tsv")
		convey.So(capturedOptions.All, convey.ShouldBeTrue)
		convey.So(capturedOptions.Limit, convey.ShouldEqual, 0)
		convey.So(capturedOptions.Cursor, convey.ShouldBeEmpty)
		convey.So(stub.closed, convey.ShouldBeTrue)
	})
}

func mlwhProductExportResultForTest(format string) mlwh.ExportResult {
	return mlwh.ExportResult{
		Columns: mlwhProductExportColumnsForTest(),
		Rows: [][]string{
			{"S1-A", "supplier-a", "61010", "1", "1", "pass", "/seq/S1-A.cram", "false", ""},
			{"S1-B", "supplier-b", "61011", "2", "0", "pending", "", "true", "merged_cram_gap"},
		},
		Total:    2,
		Complete: true,
		Format:   format,
	}
}

func mlwhProductExportColumnsForTest() []string {
	return []string{
		"name",
		"supplier_name",
		"id_run",
		"lane",
		"tag_index",
		"manual_qc",
		"irods_path",
		"irods_unmatched",
		"reason",
	}
}

func executeRootCommandStreamsForTest(t *testing.T, args []string) (string, string, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command := NewRootCommand()
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs(args)

	err := command.Execute()

	return stdout.String(), stderr.String(), err
}

func TestMLWHExportProductsStudyRendersCSVAndJSONI1(t *testing.T) {
	convey.Convey("I1.2: Given products of study S1, when CSV export runs, then stdout is the normal comma-delimited export", t, func() {
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, _ mlwh.ExportRelationship, _ string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedOptions = opts

				return mlwhProductExportResultForTest(opts.Format), nil
			},
		}
		withStubMLWHExportClient(t, stub)

		stdout, stderr, err := executeRootCommandStreamsForTest(t, []string{
			"mlwh", "export", "products", "study", "S1",
			"--columns", strings.Join(mlwhProductExportColumnsForTest(), ","),
			"--file-type", "cram",
			"--format", "csv",
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.TrimSpace(stdout), convey.ShouldEqual, strings.Join([]string{
			"name,supplier_name,id_run,lane,tag_index,manual_qc,irods_path,irods_unmatched,reason",
			"S1-A,supplier-a,61010,1,1,pass,/seq/S1-A.cram,false,",
			"S1-B,supplier-b,61011,2,0,pending,,true,merged_cram_gap",
		}, "\n"))
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(capturedOptions.Format, convey.ShouldEqual, "csv")
	})

	convey.Convey("I1.2: Given products of study S1, when JSON export runs, then stdout is the normal row-array export", t, func() {
		var capturedOptions mlwh.ExportOptions
		stub := &stubMLWHExportClient{
			export: func(_ context.Context, _ mlwh.ExportRelationship, _ string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
				capturedOptions = opts

				return mlwhProductExportResultForTest(opts.Format), nil
			},
		}
		withStubMLWHExportClient(t, stub)

		stdout, stderr, err := executeRootCommandStreamsForTest(t, []string{
			"mlwh", "export", "products", "study", "S1",
			"--columns", strings.Join(mlwhProductExportColumnsForTest(), ","),
			"--file-type", "cram",
			"--json",
		})

		var rows []map[string]string
		decodeErr := json.Unmarshal([]byte(stdout), &rows)

		convey.So(err, convey.ShouldBeNil)
		convey.So(decodeErr, convey.ShouldBeNil)
		convey.So(rows, convey.ShouldHaveLength, 2)
		convey.So(rows[0]["name"], convey.ShouldEqual, "S1-A")
		convey.So(rows[0]["irods_path"], convey.ShouldEqual, "/seq/S1-A.cram")
		convey.So(rows[1]["irods_path"], convey.ShouldBeEmpty)
		convey.So(rows[1]["irods_unmatched"], convey.ShouldEqual, "true")
		convey.So(rows[1]["reason"], convey.ShouldEqual, "merged_cram_gap")
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(capturedOptions.Format, convey.ShouldEqual, "json")
	})
}

func TestMLWHExportBoundedProductsPageWritesStatusI1(t *testing.T) {
	testCases := []struct {
		name               string
		nextCursor         string
		complete           bool
		args               []string
		wantCursorHint     string
		wantFinalStatement bool
		wantCapturedCursor string
	}{
		{
			name:               "with cursor continuation",
			nextCursor:         "next-products-cursor",
			args:               []string{"--cursor", "current-products-cursor"},
			wantCursorHint:     "--cursor next-products-cursor",
			wantCapturedCursor: "current-products-cursor",
		},
		{
			name:               "final page",
			complete:           true,
			wantFinalStatement: true,
		},
		{
			name: "without cursor continuation",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		convey.Convey("I1.3: Given a bounded products page "+testCase.name+", when --limit is used, then stderr states bounded-page mode", t, func() {
			var capturedOptions mlwh.ExportOptions
			stub := &stubMLWHExportClient{
				export: func(_ context.Context, _ mlwh.ExportRelationship, _ string, opts mlwh.ExportOptions) (mlwh.ExportResult, error) {
					capturedOptions = opts
					result := mlwhProductExportResultForTest(opts.Format)
					result.Rows = result.Rows[:1]
					result.NextCursor = testCase.nextCursor
					result.Complete = testCase.complete

					return result, nil
				},
			}
			withStubMLWHExportClient(t, stub)

			args := []string{
				"mlwh", "export", "products", "study", "S1",
				"--columns", strings.Join(mlwhProductExportColumnsForTest(), ","),
				"--limit", "50",
			}
			args = append(args, testCase.args...)

			stdout, stderr, err := executeRootCommandStreamsForTest(t, args)

			convey.So(err, convey.ShouldBeNil)
			convey.So(strings.TrimSpace(stdout), convey.ShouldEqual, strings.Join([]string{
				"name\tsupplier_name\tid_run\tlane\ttag_index\tmanual_qc\tirods_path\tirods_unmatched\treason",
				"S1-A\tsupplier-a\t61010\t1\t1\tpass\t/seq/S1-A.cram\tfalse",
			}, "\n"))
			convey.So(stderr, convey.ShouldContainSubstring, "bounded page emitted")
			convey.So(stderr, convey.ShouldContainSubstring, "omit --limit to export everything")
			convey.So(strings.Count(stderr, "\n"), convey.ShouldEqual, 1)
			convey.So(capturedOptions.All, convey.ShouldBeFalse)
			convey.So(capturedOptions.Limit, convey.ShouldEqual, 50)
			convey.So(capturedOptions.Cursor, convey.ShouldEqual, testCase.wantCapturedCursor)
			if testCase.wantCursorHint != "" {
				convey.So(stderr, convey.ShouldContainSubstring, testCase.wantCursorHint)
			} else {
				convey.So(stderr, convey.ShouldNotContainSubstring, "--cursor")
			}
			if testCase.wantFinalStatement {
				convey.So(stderr, convey.ShouldContainSubstring, "final page")
			} else {
				convey.So(stderr, convey.ShouldNotContainSubstring, "final page")
			}
		})
	}
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
					Columns: []string{"name", "accession_number", "irods_path", "merged"},
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
		convey.So(output, convey.ShouldContainSubstring, "name\taccession_number\tirods_path\tmerged")
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

func startMLWHExportServerForCachePathForTest(t *testing.T, cachePath string) string {
	t.Helper()

	client, err := mlwh.OpenCacheOnly(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open MLWH export test cache: %v", err)
	}
	t.Cleanup(func() {
		if err = client.Close(); err != nil {
			t.Fatalf("close MLWH export test cache: %v", err)
		}
	})

	gin.SetMode(gin.TestMode)
	router := gin.New()
	configureMLWHServeRouter(router)
	mlwh.NewServer(client).RegisterRoutes(router, nil)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server.URL
}

func withFailingOpenMLWHExportClient(t *testing.T) *bool {
	t.Helper()
	t.Setenv("WA_MLWH_DSN", "mlwh_user@tcp(localhost:3306)/mlwarehouse")
	t.Setenv("WA_MLWH_SERVER_URL", "")
	t.Setenv("WA_MLWH_BACKEND_URL", "")
	t.Setenv("WA_ENV", "")
	t.Setenv("WA_TEST_SEQMETA_PORT", "")
	t.Setenv("WA_DEV_SEQMETA_PORT", "")
	t.Setenv("WA_PROD_SEQMETA_PORT", "")

	opened := false
	original := openMLWHExportClient
	t.Cleanup(func() { openMLWHExportClient = original })
	openMLWHExportClient = func(context.Context, mlwh.Config) (mlwhExportClient, error) {
		opened = true

		return &stubMLWHExportClient{
			export: func(context.Context, mlwh.ExportRelationship, string, mlwh.ExportOptions) (mlwh.ExportResult, error) {
				return mlwh.ExportResult{}, errors.New("export client should not be used")
			},
		}, nil
	}

	return &opened
}

func TestMLWHExportProductsAllRenderTimeNeverSyncedIsClean(t *testing.T) {
	for _, testCase := range []struct {
		name string
		args func(string) []string
	}{
		{
			name: "cache-only mode",
			args: func(cachePath string) []string {
				configureMLWHNeverSyncedCommandEnvForTest(t, cachePath)

				return []string{"mlwh", "export", "products", "study", "6568"}
			},
		},
		{
			name: "server mode",
			args: func(cachePath string) []string {
				configureMLWHNeverSyncedCommandEnvForTest(t, "")

				return []string{
					"mlwh", "export", "products", "study", "6568",
					"--server", startMLWHExportServerForCachePathForTest(t, cachePath),
				}
			},
		},
	} {
		testCase := testCase
		convey.Convey("Given a default products export reaches never-synced state while rendering in "+testCase.name, t, func() {
			cachePath := prepareMLWHServeCacheForTest(t, true)

			stdout, stderr, err := executeRootCommandStreamsForTest(t, testCase.args(cachePath))

			convey.So(err, convey.ShouldBeNil)
			convey.So(stdout, convey.ShouldBeEmpty)
			convey.So(stderr, convey.ShouldContainSubstring, mlwhCacheUnavailableMessage)
			convey.So(stderr, convey.ShouldNotContainSubstring, "render export")
			convey.So(stderr, convey.ShouldNotContainSubstring, mlwh.ErrCacheNeverSynced.Error())
			convey.So(stderr, convey.ShouldNotContainSubstring, mlwh.ErrNotFound.Error())
			convey.So(stderr, convey.ShouldNotContainSubstring, "wa mlwh sync")
			convey.So(stderr, convey.ShouldNotContainSubstring, "name\tsupplier_name")
		})
	}
}

func TestMLWHExportRejectsInvalidBoundedFlagsBeforeOpeningClient(t *testing.T) {
	convey.Convey("Given products of study S1, when --limit is negative, then export rejects it before opening the client", t, func() {
		opened := withFailingOpenMLWHExportClient(t)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "products", "study", "S1", "--limit", "-1"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(*opened, convey.ShouldBeFalse)
		convey.So(output, convey.ShouldContainSubstring, "--limit")
		convey.So(output, convey.ShouldContainSubstring, "non-negative")
		convey.So(output, convey.ShouldNotContainSubstring, "open mlwh client")
	})

	convey.Convey("Given products of study S1, when --cursor is supplied without --limit, then export asks for an explicit bounded page size before opening the client", t, func() {
		opened := withFailingOpenMLWHExportClient(t)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "products", "study", "S1", "--cursor", "cursor-1"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(*opened, convey.ShouldBeFalse)
		convey.So(output, convey.ShouldContainSubstring, "--cursor")
		convey.So(output, convey.ShouldContainSubstring, "--limit")
		convey.So(output, convey.ShouldContainSubstring, "positive")
		convey.So(output, convey.ShouldNotContainSubstring, "open mlwh client")
	})

	convey.Convey("Given products of study S1, when --cursor is supplied with a zero --limit, then export still asks for a positive bounded page size before opening the client", t, func() {
		opened := withFailingOpenMLWHExportClient(t)

		output, err := executeRootCommandForTest(t, []string{"mlwh", "export", "products", "study", "S1", "--cursor", "cursor-1", "--limit", "0"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(*opened, convey.ShouldBeFalse)
		convey.So(output, convey.ShouldContainSubstring, "--cursor")
		convey.So(output, convey.ShouldContainSubstring, "--limit")
		convey.So(output, convey.ShouldContainSubstring, "positive")
		convey.So(output, convey.ShouldNotContainSubstring, "open mlwh client")
	})
}
