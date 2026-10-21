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
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

type searchDataRowForTest struct {
	IDStudyLims          string `json:"id_study_lims"`
	StudyAccessionNumber string `json:"study_accession_number"`
	Platform             string `json:"platform"`
	IDRun                string `json:"id_run"`
	Lane                 string `json:"lane"`
	TagIndex             string `json:"tag_index"`
	ManualQC             string `json:"manual_qc"`
	IRODSPath            string `json:"irods_path"`
}

func TestMLWHSearchWithData(t *testing.T) {
	convey.Convey("Given a supplier name shared by four samples, one sequenced in two studies on two platforms and two never sequenced", t, func() {
		clearMLWHClientEnvForTest(t)
		serverURL := startMLWHExportServerForCachePathForTest(t, newSearchDataCachePathForTest(t))
		searchArgs := []string{"mlwh", "search", "--type", "sample", multiSampleSupplierForTest, "--server", serverURL}

		convey.Convey("when searching with --with-data --json, then each sample carries its data rows, empty for samples without data", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, append(searchArgs, "--with-data", "--json"))

			convey.So(err, convey.ShouldBeNil)
			var report searchWithDataJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Samples.Results, convey.ShouldHaveLength, 4)

			rows := map[string][]searchDataRowForTest{}
			for _, sample := range report.Samples.Results {
				convey.So(sample.Data, convey.ShouldNotBeNil)
				rows[sample.Name] = *sample.Data
			}
			convey.So(rows["5534STDY0000101"], convey.ShouldBeEmpty)
			convey.So(rows["5534STDY0000103"], convey.ShouldBeEmpty)
			convey.So(rows["5534STDY0000102"], convey.ShouldResemble, []searchDataRowForTest{{
				IDStudyLims: "5534", StudyAccessionNumber: "EGAS00005534", Platform: "illumina",
				IDRun: "29284", Lane: "6", TagIndex: "6", ManualQC: "pass", IRODSPath: "/seq/29284/29284_6#6.cram",
			}})
			convey.So(rows["5534STDY0000104"], convey.ShouldHaveLength, 5)
			convey.So(rows["5534STDY0000104"], convey.ShouldContain, searchDataRowForTest{
				IDStudyLims: "7000", StudyAccessionNumber: "EGAS00007000", Platform: "pacbio",
				IDRun: "48503", Lane: "1", TagIndex: "2", ManualQC: "pass", IRODSPath: "/seq/48503/48503_1#2.cram",
			})
		})

		convey.Convey("when searching with --with-data as text, then each sample is followed by its indented data rows", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, append(searchArgs, "--with-data"))

			convey.So(err, convey.ShouldBeNil)
			convey.So(stdout, convey.ShouldContainSubstring, "  name=5534STDY0000101 supplier_name=PD42487a common_name=Homo sapiens donor_id=PD42487a accession_number=EGAN0000101\n    no data\n")
			convey.So(stdout, convey.ShouldContainSubstring, "accession_number=EGAN0000102\n    5534 illumina 29284_6#6 pass /seq/29284/29284_6#6.cram\n")
			convey.So(stdout, convey.ShouldContainSubstring, "    7000 pacbio 48503_1#2 pass /seq/48503/48503_1#2.cram\n")
		})

		convey.Convey("when searching without --with-data, then the output carries no data", func() {
			jsonOut, _, err := executeRootCommandSplitForTest(t, append(searchArgs, "--json"))
			convey.So(err, convey.ShouldBeNil)
			convey.So(jsonOut, convey.ShouldNotContainSubstring, `"data"`)

			text, _, err := executeRootCommandSplitForTest(t, searchArgs)
			convey.So(err, convey.ShouldBeNil)
			convey.So(text, convey.ShouldNotContainSubstring, "/seq/")
			convey.So(text, convey.ShouldNotContainSubstring, "no data")
		})

		convey.Convey("when --limit bounds the results, then only the returned samples carry data", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, append(searchArgs, "--with-data", "--json", "--limit", "2", "--offset", "1"))

			convey.So(err, convey.ShouldBeNil)
			var report searchWithDataJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Samples.Results, convey.ShouldHaveLength, 2)
			convey.So(report.Samples.Results[0].Name, convey.ShouldEqual, "5534STDY0000102")
			convey.So(*report.Samples.Results[0].Data, convey.ShouldHaveLength, 1)
		})

		convey.Convey("when --with-data is given for a study-only search, then it is a usage error", func() {
			_, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "search", "--type", "study", "Study", "--server", serverURL, "--with-data"})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(strings.Contains(err.Error(), "--with-data"), convey.ShouldBeTrue)
		})
	})
}

// newSearchDataCachePathForTest extends the multi-sample cache so sample 104
// also has a PacBio product in study 7000, besides its Illumina products in
// study 5534, and sample 102's product also has a BAM in iRODS besides its
// CRAM; samples 101 and 103 have no data.
func newSearchDataCachePathForTest(t *testing.T) string {
	t.Helper()

	cachePath := newMultiSampleCachePathForTest(t)
	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err = cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	seedMultiSampleStudyProductForTest(t, cache.DB(), multiSampleProductForTest{
		idLocation: 20, idSample: 104, idStudy: 901, studyLims: "7000", platform: "pacbio",
		idRun: 48503, position: 1, tagIndex: 2,
	})
	execMultiSampleSQLForTest(t, cache.DB(),
		`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (21, 'product-29284_6#6', '/seq', '29284_6#6.bam', '/seq/29284', '29284_6#6.bam', 102, '5534', ?, ?, 'illumina', 29284, 6, 6, 1, 1, 0)`,
		multiSampleSyncedAtForTest, multiSampleSyncedAtForTest)

	return cachePath
}

// exportStubbedSearchClientForTest is a real search client whose per-sample
// exports run export, which may delegate to the real client's Export.
type exportStubbedSearchClientForTest struct {
	mlwhSearchClient

	export func(ctx context.Context, real mlwhSampleDataExporter, sangerName string) (mlwh.ExportResult, error)
}

func newExportStubbedSearchClientForTest(t *testing.T, serverURL string, export func(context.Context, mlwhSampleDataExporter, string) (mlwh.ExportResult, error)) exportStubbedSearchClientForTest {
	t.Helper()

	client, err := openMLWHSearchConfiguredClient(context.Background(), serverURL)
	if err != nil {
		t.Fatalf("open search client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	return exportStubbedSearchClientForTest{mlwhSearchClient: client, export: export}
}

func (c exportStubbedSearchClientForTest) Export(ctx context.Context, _ mlwh.ExportRelationship, parentID string, _ mlwh.ExportOptions) (mlwh.ExportResult, error) {
	real, _ := c.mlwhSearchClient.(mlwhSampleDataExporter)

	return c.export(ctx, real, parentID)
}

func TestMLWHSearchWithDataExportFailures(t *testing.T) {
	convey.Convey("Given a supplier name shared by four samples behind wa mlwh serve, and a client whose exports can be made to fail", t, func() {
		clearMLWHClientEnvForTest(t)
		serverURL := startMLWHExportServerForCachePathForTest(t, newSearchDataCachePathForTest(t))
		page := searchPage{limit: 50, withData: true}

		convey.Convey("when one sample's export fails, then that sample carries data_error, the others their data, and the search succeeds", func() {
			client := newExportStubbedSearchClientForTest(t, serverURL, func(ctx context.Context, real mlwhSampleDataExporter, sangerName string) (mlwh.ExportResult, error) {
				if sangerName == "5534STDY0000102" {
					return mlwh.ExportResult{}, errors.New("export refused for test")
				}

				return real.Export(ctx, mlwh.ExportRelationship{Children: "irods", ParentKind: "sample"}, sangerName, mlwh.ExportOptions{Columns: searchDataColumns, All: true})
			})

			jsonOut := &bytes.Buffer{}
			err := runMLWHSearch(context.Background(), client, jsonOut, multiSampleSupplierForTest, "sample", mlwh.SampleSearchOptions{}, page, true)
			convey.So(err, convey.ShouldBeNil)
			report := decodeSearchWithDataForTest(jsonOut.String())
			convey.So(report.TimedOut, convey.ShouldBeFalse)
			convey.So(report.sample("5534STDY0000102").DataError, convey.ShouldEqual, "export refused for test")
			convey.So(report.sample("5534STDY0000102").Data, convey.ShouldBeNil)
			convey.So(report.sample("5534STDY0000104").DataError, convey.ShouldBeEmpty)
			convey.So(*report.sample("5534STDY0000104").Data, convey.ShouldHaveLength, 5)

			text := &bytes.Buffer{}
			err = runMLWHSearch(context.Background(), client, text, multiSampleSupplierForTest, "sample", mlwh.SampleSearchOptions{}, page, false)
			convey.So(err, convey.ShouldBeNil)
			convey.So(text.String(), convey.ShouldContainSubstring, "accession_number=EGAN0000102\n    data unavailable: export refused for test\n")
		})

		convey.Convey("when the exports outlast the search deadline, then the report is marked timed out and no sample blames the upstream database", func() {
			client := newExportStubbedSearchClientForTest(t, serverURL, func(ctx context.Context, _ mlwhSampleDataExporter, _ string) (mlwh.ExportResult, error) {
				<-ctx.Done()

				return mlwh.ExportResult{}, fmt.Errorf("%w: %w", mlwh.ErrUpstreamImpaired, ctx.Err())
			})

			for _, jsonOut := range []bool{true, false} {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				out := &bytes.Buffer{}
				err := runMLWHSearch(ctx, client, out, multiSampleSupplierForTest, "sample", mlwh.SampleSearchOptions{}, page, jsonOut)
				cancel()

				convey.So(err, convey.ShouldBeNil)
				convey.So(out.String(), convey.ShouldNotContainSubstring, "upstream database impaired")
				convey.So(out.String(), convey.ShouldNotContainSubstring, "deadline exceeded")

				if !jsonOut {
					convey.So(out.String(), convey.ShouldContainSubstring, "    data unavailable: timed out\n")
					convey.So(out.String(), convey.ShouldEndWith, "\n"+mlwhSearchDataTimedOutMessage+"\n")

					continue
				}

				report := decodeSearchWithDataForTest(out.String())
				convey.So(report.TimedOut, convey.ShouldBeTrue)
				convey.So(report.Samples.Results, convey.ShouldHaveLength, 4)
				for _, sample := range report.Samples.Results {
					convey.So(sample.DataError, convey.ShouldEqual, "timed out")
				}
			}
		})

		convey.Convey("when the search is interrupted during the exports, then it fails with the cancellation instead of printing results", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := newExportStubbedSearchClientForTest(t, serverURL, func(ctx context.Context, _ mlwhSampleDataExporter, _ string) (mlwh.ExportResult, error) {
				cancel()

				return mlwh.ExportResult{}, fmt.Errorf("%w: %w", mlwh.ErrUpstreamImpaired, ctx.Err())
			})

			out := &bytes.Buffer{}
			err := runMLWHSearch(ctx, client, out, multiSampleSupplierForTest, "sample", mlwh.SampleSearchOptions{}, page, true)

			convey.So(errors.Is(err, context.Canceled), convey.ShouldBeTrue)
			convey.So(ExitCode(err), convey.ShouldEqual, ExitCodeError)
			convey.So(out.String(), convey.ShouldBeEmpty)
		})
	})
}

func decodeSearchWithDataForTest(out string) searchWithDataJSONForTest {
	var report searchWithDataJSONForTest
	convey.So(json.Unmarshal([]byte(out), &report), convey.ShouldBeNil)

	return report
}

type searchDataSampleForTest struct {
	Name      string                  `json:"name"`
	Data      *[]searchDataRowForTest `json:"data"`
	DataError string                  `json:"data_error"`
}

type searchWithDataJSONForTest struct {
	TimedOut bool `json:"timed_out"`
	Samples  struct {
		Count   int                       `json:"count"`
		Results []searchDataSampleForTest `json:"results"`
	} `json:"samples"`
}

func (r searchWithDataJSONForTest) sample(name string) searchDataSampleForTest {
	for _, sample := range r.Samples.Results {
		if sample.Name == name {
			return sample
		}
	}

	return searchDataSampleForTest{}
}
