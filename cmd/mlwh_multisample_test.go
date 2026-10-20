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
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

const (
	multiSampleSupplierForTest = "PD42487a"
	multiSampleSyncedAtForTest = "2026-07-01T05:00:00Z"
)

const multiSampleLanesTSVForTest = "supplier_name\tsanger_sample_id\tid_run\tlane\ttag_index\n" +
	"PD42487a\t5534STDY0000102\t29284\t6\t6\n" +
	"PD42487a\t5534STDY0000104\t31360\t1\t9\n" +
	"PD42487a\t5534STDY0000104\t31360\t2\t9\n" +
	"PD42487a\t5534STDY0000104\t31360\t3\t9\n" +
	"PD42487a\t5534STDY0000104\t31360\t4\t9\n"

const (
	infoQuotedSupplierForTest                 = "PD 42487a's"
	infoSupplierAndDuplicatedAccessionForTest = "DUPACC-SUP"
)

type multiSampleInfoJSONForTest struct {
	Identifier    string `json:"identifier"`
	Kind          string `json:"kind"`
	CacheSyncedAt string `json:"cache_synced_at"`
	Candidates    []struct {
		Name               string `json:"name"`
		SangerSampleID     string `json:"sanger_sample_id"`
		SupplierName       string `json:"supplier_name"`
		HasDeliverableData bool   `json:"has_deliverable_data"`
		Studies            []struct {
			IDStudyLims string `json:"id_study_lims"`
			Name        string `json:"name"`
		} `json:"studies"`
	} `json:"candidates"`
	NextCommands []string `json:"next_commands"`
}

func TestMLWHInfoSupplierNameSharedBySeveralSamples(t *testing.T) {
	convey.Convey("Given a supplier name shared by four samples behind wa mlwh serve", t, func() {
		clearMLWHClientEnvForTest(t)
		serverURL := startMLWHExportServerForCachePathForTest(t, newMultiSampleCachePathForTest(t))

		convey.Convey("when wa mlwh info runs, then it lists the candidates, marks which have data, suggests next commands and exits non-zero", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", multiSampleSupplierForTest, "--server", serverURL})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "matches 4 samples")
			convey.So(stdout, convey.ShouldContainSubstring, "4 samples")
			for _, name := range []string{"5534STDY0000101", "5534STDY0000102", "5534STDY0000103", "5534STDY0000104"} {
				convey.So(stdout, convey.ShouldContainSubstring, name)
			}
			convey.So(stdout, convey.ShouldNotContainSubstring, "5534STDY0000200")
			convey.So(stdout, convey.ShouldContainSubstring, "5534 Study 5534")
			convey.So(stdout, convey.ShouldContainSubstring, "wa mlwh export irods sample PD42487a")
			convey.So(stdout, convey.ShouldContainSubstring, "wa mlwh search --type sample PD42487a --deliverables-only")
			convey.So(stdout, convey.ShouldContainSubstring, "Cache synced")
		})

		convey.Convey("when wa mlwh info --json runs, then stdout is a structured candidates object with the cache sync time", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", multiSampleSupplierForTest, "--server", serverURL, "--json"})

			convey.So(err, convey.ShouldNotBeNil)
			var report multiSampleInfoJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Identifier, convey.ShouldEqual, multiSampleSupplierForTest)
			convey.So(report.Kind, convey.ShouldEqual, "ambiguous_sample")
			convey.So(report.CacheSyncedAt, convey.ShouldEqual, multiSampleSyncedAtForTest)
			convey.So(report.Candidates, convey.ShouldHaveLength, 4)

			withData := []string{}
			for _, candidate := range report.Candidates {
				if candidate.HasDeliverableData {
					withData = append(withData, candidate.Name)
				}
			}
			convey.So(withData, convey.ShouldResemble, []string{"5534STDY0000102", "5534STDY0000104"})
			convey.So(report.Candidates[3].Studies, convey.ShouldHaveLength, 2)
			convey.So(report.NextCommands, convey.ShouldResemble, []string{
				"wa mlwh export irods sample PD42487a",
				"wa mlwh search --type sample PD42487a --deliverables-only",
			})
		})

		convey.Convey("when one of the samples is looked up by Sanger name with --json, then the report carries cache_synced_at", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", "5534STDY0000102", "--server", serverURL, "--json"})

			convey.So(err, convey.ShouldBeNil)
			var report multiSampleInfoJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Kind, convey.ShouldNotEqual, "ambiguous_sample")
			// A sample report keeps its progress timeline's as-of time: the oldest
			// sync of the tables progress reads, of which only
			// iseq_product_metrics is seeded here.
			convey.So(report.CacheSyncedAt, convey.ShouldEqual, "2026-07-01T05:30:00Z")
		})

		convey.Convey("when wa mlwh search --json runs, then the report carries cache_synced_at", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "search", "--type", "sample", multiSampleSupplierForTest, "--server", serverURL, "--json"})

			convey.So(err, convey.ShouldBeNil)
			var report struct {
				CacheSyncedAt string `json:"cache_synced_at"`
			}
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.CacheSyncedAt, convey.ShouldEqual, multiSampleSyncedAtForTest)
		})
	})
}

func clearMLWHClientEnvForTest(t *testing.T) {
	t.Helper()

	for _, name := range []string{"WA_MLWH_DSN", "WA_MLWH_SERVER_URL", "WA_MLWH_BACKEND_URL", "WA_MLWH_CACHE_PATH", "WA_ENV", "WA_TEST_SEQMETA_PORT", "WA_DEV_SEQMETA_PORT", "WA_PROD_SEQMETA_PORT"} {
		t.Setenv(name, "")
	}
}

// newMultiSampleCachePathForTest builds a cache where supplier name PD42487a
// names four samples: 101 and 103 are registered only, 102 has one CRAM on run
// 29284 and 104 is a top-up with CRAMs on four lanes of run 31360 in a second
// study too. Sample 200 has data under another supplier name.
func newMultiSampleCachePathForTest(t *testing.T) string {
	t.Helper()

	cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err = cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	db := cache.DB()
	seedMultiSampleStudyForTest(t, db, 900, "5534")
	seedMultiSampleStudyForTest(t, db, 901, "7000")
	for _, id := range []int64{101, 102, 103, 104} {
		seedMultiSampleSampleForTest(t, db, id, multiSampleSupplierForTest)
		execMultiSampleSQLForTest(t, db, `INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES ('Standard', ?, '5534')`, id)
	}
	execMultiSampleSQLForTest(t, db, `INSERT INTO library_samples(pipeline_id_lims, id_sample_tmp, id_study_lims) VALUES ('Standard', 104, '7000')`)
	seedMultiSampleSampleForTest(t, db, 200, "OTHER")

	seedMultiSampleProductForTest(t, db, 1, 102, 29284, 6, 6)
	for lane := 1; lane <= 4; lane++ {
		seedMultiSampleProductForTest(t, db, int64(1+lane), 104, 31360, lane, 9)
	}
	seedMultiSampleProductForTest(t, db, 9, 200, 29284, 6, 7)

	// The tables last synced at different times; cache_synced_at must report the
	// oldest, multiSampleSyncedAtForTest, which is neither first nor last.
	for table, lastRun := range map[string]string{
		"study":                       "2026-07-01T05:20:00Z",
		"sample":                      "2026-07-01T05:10:00Z",
		"iseq_flowcell":               multiSampleSyncedAtForTest,
		"iseq_product_metrics":        "2026-07-01T05:30:00Z",
		"seq_product_irods_locations": "2026-07-01T05:40:00Z",
	} {
		execMultiSampleSQLForTest(t, db, `INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`, table, multiSampleSyncedAtForTest, lastRun)
	}

	return cachePath
}

func seedMultiSampleStudyForTest(t *testing.T, db *sql.DB, id int64, idStudyLims string) {
	t.Helper()

	execMultiSampleSQLForTest(t, db,
		`INSERT INTO study_mirror(id_study_tmp, id_lims, id_study_lims, uuid_study_lims, name, accession_number, study_title, faculty_sponsor, state, data_release_strategy, data_access_group, programme, reference_genome, ethically_approved, study_type, contains_human_dna, contaminated_human_dna, study_visibility, ega_dac_accession_number, ega_policy_accession_number, data_release_timing, last_updated) VALUES (?, 'SQSCP', ?, ?, ?, ?, '', '', 'active', '', '', '', '', 1, '', 0, 0, '', '', '', '', ?)`,
		id, idStudyLims, "study-uuid-"+idStudyLims, "Study "+idStudyLims, "EGAS0000"+idStudyLims, multiSampleSyncedAtForTest)
}

func execMultiSampleSQLForTest(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("seed multi-sample cache: %v", err)
	}
}

func seedMultiSampleSampleForTest(t *testing.T, db *sql.DB, id int64, supplier string) {
	t.Helper()

	name := "5534STDY0000" + formatMultiSampleIntForTest(id)
	execMultiSampleSQLForTest(t, db,
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, 'SQSCP', ?, ?, ?, ?, ?, ?, ?, 9606, 'Homo sapiens', '', ?)`,
		id, "lims-"+formatMultiSampleIntForTest(id), "uuid-"+formatMultiSampleIntForTest(id), name, name, supplier, "EGAN0000"+formatMultiSampleIntForTest(id), supplier, multiSampleSyncedAtForTest)
	for _, token := range []string{strings.ToLower(name), strings.ToLower(supplier), "homo", "sapiens"} {
		execMultiSampleSQLForTest(t, db, `INSERT INTO sample_search_token(token, id_sample_tmp) VALUES (?, ?)`, token, id)
	}
}

func formatMultiSampleIntForTest(value int64) string {
	return strconv.FormatInt(value, 10)
}

func seedMultiSampleProductForTest(t *testing.T, db *sql.DB, idLocation, idSample int64, idRun, position, tagIndex int) {
	t.Helper()

	product := formatMultiSampleIntForTest(int64(idRun)) + "_" + formatMultiSampleIntForTest(int64(position)) + "#" + formatMultiSampleIntForTest(int64(tagIndex))
	execMultiSampleSQLForTest(t, db,
		`INSERT INTO iseq_flowcell_mirror(id_iseq_flowcell_tmp, entity_type, pipeline_id_lims, id_sample_tmp, id_study_tmp) VALUES (?, 'library_indexed', 'Standard', ?, 900)`,
		idLocation, idSample)
	execMultiSampleSQLForTest(t, db,
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, '5534', 1, 1, 1, ?)`,
		"product-"+product, idLocation, idRun, position, tagIndex, idSample, multiSampleSyncedAtForTest)
	execMultiSampleSQLForTest(t, db,
		`INSERT INTO seq_product_irods_locations_mirror(id_seq_product_irods_locations_tmp, id_iseq_product, irods_root_collection, irods_data_relative_path, irods_collection, irods_file_name, id_sample_tmp, id_study_lims, last_updated, created, platform, id_run, position, tag_index, qc, is_deliverable, merged) VALUES (?, ?, '/seq', ?, ?, ?, ?, '5534', ?, ?, 'illumina', ?, ?, ?, 1, 1, 0)`,
		idLocation, "product-"+product, product+".cram", "/seq/"+formatMultiSampleIntForTest(int64(idRun)), product+".cram", idSample,
		multiSampleSyncedAtForTest, multiSampleSyncedAtForTest, idRun, position, tagIndex)
}

// executeRootCommandSplitForTest runs the root command and keeps stdout and
// stderr apart so data-only stdout contracts can be asserted.
func executeRootCommandSplitForTest(t *testing.T, args []string) (string, string, error) {
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

func TestMLWHInfoAmbiguitySuggestionsFollowMultiSampleRule(t *testing.T) {
	convey.Convey("Given identifiers whose ambiguity a multi-sample export does or does not cover", t, func() {
		clearMLWHClientEnvForTest(t)
		cachePath := newMultiSampleCachePathForTest(t)
		addInfoCandidateSamplesForTest(t, cachePath)
		serverURL := startMLWHExportServerForCachePathForTest(t, cachePath)

		convey.Convey("when info names a value that is one sample's accession and others' supplier name, then it suggests exporting each sample by Sanger name", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", "SHARED-ACC", "--server", serverURL, "--json"})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, "matches 3 samples; use one Sanger sample name")
			convey.So(err.Error(), convey.ShouldNotContainSubstring, "cover them all")
			var report multiSampleInfoJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.Candidates, convey.ShouldHaveLength, 3)
			convey.So(report.NextCommands, convey.ShouldHaveLength, 4)
			convey.So(report.NextCommands, convey.ShouldResemble, []string{
				"wa mlwh export irods sample 5534STDY0000300",
				"wa mlwh export irods sample 5534STDY0000301",
				"wa mlwh export irods sample 5534STDY0000302",
				"wa mlwh search --type sample SHARED-ACC --deliverables-only",
			})
		})

		convey.Convey("when info prints the same ambiguity as text, then the export heading speaks of one sample", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", "SHARED-ACC", "--server", serverURL})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(stdout, convey.ShouldContainSubstring, "Export the files of one sample")
			convey.So(stdout, convey.ShouldNotContainSubstring, "every matching sample")
			convey.So(stdout, convey.ShouldContainSubstring, "wa mlwh export irods sample 5534STDY0000301")
		})

		convey.Convey("when info names a value that is two samples' shared supplier name and two other samples' duplicated accession, then it suggests exporting each sample by Sanger name", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", infoSupplierAndDuplicatedAccessionForTest, "--server", serverURL, "--json"})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldNotContainSubstring, "cover them all")
			var report multiSampleInfoJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.NextCommands, convey.ShouldResemble, []string{
				"wa mlwh export irods sample 5534STDY0000501",
				"wa mlwh export irods sample 5534STDY0000502",
				"wa mlwh search --type sample " + infoSupplierAndDuplicatedAccessionForTest + " --deliverables-only",
			})
		})

		convey.Convey("when info names a shared supplier name holding shell metacharacters, then the suggested commands quote it", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "info", infoQuotedSupplierForTest, "--server", serverURL, "--json"})

			convey.So(err, convey.ShouldNotBeNil)
			var report multiSampleInfoJSONForTest
			convey.So(json.Unmarshal([]byte(stdout), &report), convey.ShouldBeNil)
			convey.So(report.NextCommands, convey.ShouldResemble, []string{
				`wa mlwh export irods sample 'PD 42487a'\''s'`,
				`wa mlwh search --type sample 'PD 42487a'\''s' --deliverables-only`,
			})
		})
	})
}

// addInfoCandidateSamplesForTest adds SHARED-ACC, the accession of sample 300
// and the supplier name of samples 301 and 302; a supplier name with a space
// and a quote shared by samples 401 and 402; and DUPACC-SUP, the supplier name
// of samples 501 and 502 and the accession of both 503 and 504, so an accession
// lookup of it is itself ambiguous and lists no samples.
func addInfoCandidateSamplesForTest(t *testing.T, cachePath string) {
	t.Helper()

	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err = cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	db := cache.DB()
	seedInfoCandidateSampleForTest(t, db, 300, "5534STDY0000300", "supplier-300", "5534stdy0000300", "shared", "acc")
	execMultiSampleSQLForTest(t, db, `UPDATE sample_mirror SET accession_number = 'SHARED-ACC' WHERE id_sample_tmp = 300`)
	seedInfoCandidateSampleForTest(t, db, 301, "5534STDY0000301", "SHARED-ACC", "5534stdy0000301", "shared", "acc")
	seedInfoCandidateSampleForTest(t, db, 302, "5534STDY0000302", "SHARED-ACC", "5534stdy0000302", "shared", "acc")
	seedInfoCandidateSampleForTest(t, db, 401, "5534STDY0000401", infoQuotedSupplierForTest, "5534stdy0000401", "pd", "42487a", "s")
	seedInfoCandidateSampleForTest(t, db, 402, "5534STDY0000402", infoQuotedSupplierForTest, "5534stdy0000402", "pd", "42487a", "s")
	seedInfoCandidateSampleForTest(t, db, 501, "5534STDY0000501", infoSupplierAndDuplicatedAccessionForTest, "5534stdy0000501", "dupacc", "sup")
	seedInfoCandidateSampleForTest(t, db, 502, "5534STDY0000502", infoSupplierAndDuplicatedAccessionForTest, "5534stdy0000502", "dupacc", "sup")
	seedInfoCandidateSampleForTest(t, db, 503, "5534STDY0000503", "supplier-503", "5534stdy0000503")
	seedInfoCandidateSampleForTest(t, db, 504, "5534STDY0000504", "supplier-504", "5534stdy0000504")
	execMultiSampleSQLForTest(t, db, `UPDATE sample_mirror SET accession_number = ? WHERE id_sample_tmp IN (503, 504)`, infoSupplierAndDuplicatedAccessionForTest)
}

func seedInfoCandidateSampleForTest(t *testing.T, db *sql.DB, id int64, name, supplier string, tokens ...string) {
	t.Helper()

	idText := formatMultiSampleIntForTest(id)
	execMultiSampleSQLForTest(t, db,
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, 'SQSCP', ?, ?, ?, ?, ?, ?, ?, 9606, 'Homo sapiens', '', ?)`,
		id, "lims-"+idText, "uuid-"+idText, name, name, supplier, "EGAN0000"+idText, "donor-"+idText, multiSampleSyncedAtForTest)
	for _, token := range tokens {
		execMultiSampleSQLForTest(t, db, `INSERT INTO sample_search_token(token, id_sample_tmp) VALUES (?, ?)`, token, id)
	}
}

func TestMLWHExportSampleSupplierNameSharedBySeveralSamples(t *testing.T) {
	convey.Convey("Given a supplier name shared by four samples of which two were sequenced", t, func() {
		clearMLWHClientEnvForTest(t)
		cachePath := newMultiSampleCachePathForTest(t)
		serverURL := startMLWHExportServerForCachePathForTest(t, cachePath)

		convey.Convey("when lanes are exported through wa mlwh serve, then every sequenced sample's lanes are listed with its identity", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "export", "lanes", "sample", multiSampleSupplierForTest, "--server", serverURL})

			convey.So(err, convey.ShouldBeNil)
			convey.So(stdout, convey.ShouldEqual, multiSampleLanesTSVForTest)
		})

		convey.Convey("when iRODS CRAMs are exported through wa mlwh serve, then only the sequenced samples' CRAMs are listed", func() {
			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "export", "irods", "sample", multiSampleSupplierForTest, "--server", serverURL, "--columns", "sanger_sample_id,irods_path"})

			convey.So(err, convey.ShouldBeNil)
			convey.So(stdout, convey.ShouldEqual, "sanger_sample_id\tirods_path\n"+
				"5534STDY0000102\t/seq/29284/29284_6#6.cram\n"+
				"5534STDY0000104\t/seq/31360/31360_1#9.cram\n"+
				"5534STDY0000104\t/seq/31360/31360_2#9.cram\n"+
				"5534STDY0000104\t/seq/31360/31360_3#9.cram\n"+
				"5534STDY0000104\t/seq/31360/31360_4#9.cram\n")
		})

		convey.Convey("when lanes are exported from the local cache, then the rows match the server path", func() {
			t.Setenv("WA_MLWH_CACHE_PATH", cachePath)

			stdout, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "export", "lanes", "sample", multiSampleSupplierForTest, "--server", ""})

			convey.So(err, convey.ShouldBeNil)
			convey.So(stdout, convey.ShouldEqual, multiSampleLanesTSVForTest)
		})
	})
}

func TestMLWHExportSampleSupplierNameOnOldServer(t *testing.T) {
	convey.Convey("Given a server older than API 1.11.0 that reports a shared supplier name as ambiguous", t, func() {
		clearMLWHClientEnvForTest(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/openapi.json" {
				_, _ = w.Write([]byte(`{"info":{"version":"1.10.0"},"paths":{"/export/{children}/{parent_kind}/{parent_id}":{"get":{"parameters":[{"name":"all"}]}}}}`))

				return
			}
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"ambiguous","message":"mlwh: identifier matches multiple records: \"PD42478a\" matches samples 4018284 and 4032041"}`))
		}))
		defer server.Close()

		convey.Convey("when a sample export runs, then it fails with a message that the server is too old and how to proceed", func() {
			stdout, stderr, err := executeRootCommandSplitForTest(t, []string{"mlwh", "export", "irods", "sample", "PD42478a", "--server", server.URL})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(stdout, convey.ShouldBeEmpty)
			message := err.Error()
			convey.So(message, convey.ShouldContainSubstring, server.URL)
			convey.So(message, convey.ShouldContainSubstring, "API 1.10.0")
			convey.So(message, convey.ShouldContainSubstring, "1.11.0")
			convey.So(message, convey.ShouldContainSubstring, "wa mlwh search --type sample PD42478a")
			convey.So(message, convey.ShouldNotContainSubstring, "4018284")
			convey.So(message, convey.ShouldContainSubstring, "if it is a supplier name or donor id the samples share, a server with API 1.11.0 or later can export them together; otherwise")
			convey.So(stderr, convey.ShouldContainSubstring, "too old")
		})

		convey.Convey("when the identifier holds shell metacharacters, then the suggested search command quotes it", func() {
			_, _, err := executeRootCommandSplitForTest(t, []string{"mlwh", "export", "irods", "sample", infoQuotedSupplierForTest, "--server", server.URL})

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldEndWith, "listing them with:\n  wa mlwh search --type sample 'PD 42487a'\\''s'")
		})
	})
}
