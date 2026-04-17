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
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/results"
)

func TestResultsSearchCommand(t *testing.T) {
	convey.Convey("G2.1: Given a server with 2 result sets, when search --user alice is run, then stdout is a valid JSON array with matching results", t, func() {
		store := newResultsSearchStoreForTest(t)
		seedResultsSearchRegistrationForTest(t, store, func(reg *results.Registration) {
			reg.RunKey = "runid=alice"
			reg.Requester = "alice"
		})
		seedResultsSearchRegistrationForTest(t, store, func(reg *results.Registration) {
			reg.PipelineIdentifier = "pipe-b"
			reg.RunKey = "runid=bob"
			reg.Requester = "bob"
		})

		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "search", "--server", server.URL, "--user", "alice"})

		convey.So(err, convey.ShouldBeNil)

		var got []results.ResultSet
		err = json.Unmarshal([]byte(output), &got)
		convey.So(err, convey.ShouldBeNil)
		convey.So(got, convey.ShouldHaveLength, 1)
		convey.So(got[0].Requester, convey.ShouldEqual, "alice")
	})

	convey.Convey("G2.2: Given no matches, then stdout is []", t, func() {
		store := newResultsSearchStoreForTest(t)
		seedResultsSearchRegistrationForTest(t, store, func(reg *results.Registration) {
			reg.RunKey = "runid=alice"
			reg.Requester = "alice"
		})

		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "search", "--server", server.URL, "--user", "nobody"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldEqual, "[]")
	})

	convey.Convey("search forwards pipeline-version, pipeline-identifier, and run-key filters", t, func() {
		query, err := buildResultsSearchQuery(
			"alice",
			"bob",
			"nf-pipe",
			"1.2.3",
			"pipe-a",
			"runid=48522",
			"/tmp/results",
			[]string{"library=exon"},
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(query.Get("user"), convey.ShouldEqual, "alice")
		convey.So(query.Get("operator"), convey.ShouldEqual, "bob")
		convey.So(query.Get("pipeline_name"), convey.ShouldEqual, "nf-pipe")
		convey.So(query.Get("pipeline_version"), convey.ShouldEqual, "1.2.3")
		convey.So(query.Get("pipeline_identifier"), convey.ShouldEqual, "pipe-a")
		convey.So(query.Get("run_key"), convey.ShouldEqual, "runid=48522")
		convey.So(query.Get("output_dir_prefix"), convey.ShouldEqual, "/tmp/results")
		convey.So(query.Get("meta_library"), convey.ShouldEqual, "exon")
	})

	convey.Convey("results endpoints preserve a server path prefix", t, func() {
		endpoint, err := resultsEndpointURL("http://example.test/wa-api", "/results/abc/files")

		convey.So(err, convey.ShouldBeNil)
		convey.So(endpoint.String(), convey.ShouldEqual, "http://example.test/wa-api/results/abc/files")
	})
}

func newResultsSearchStoreForTest(t *testing.T) *results.Store {
	t.Helper()

	db, err := openResultsDB(":memory:")
	if err != nil {
		t.Fatalf("open results db: %v", err)
	}

	store, err := results.NewStore(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create results store: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func seedResultsSearchRegistrationForTest(t *testing.T, store *results.Store, mutate func(*results.Registration)) {
	t.Helper()

	registration := &results.Registration{
		PipelineIdentifier: "pipe-a",
		RunKey:             "runid=48522",
		Requester:          "alice",
		Operator:           "bob",
		Command:            "nextflow run pipe",
		PipelineName:       "nf-pipe",
		PipelineVersion:    "1.2.3",
		OutputDirectory:    "/tmp/results/run",
		Files: []results.FileEntry{{
			Path:  "/tmp/results/run/out.txt",
			Mtime: time.Date(2026, time.April, 16, 12, 0, 0, 0, time.UTC),
			Size:  123,
			Kind:  "output",
		}},
		Metadata: map[string]string{"library": "exon"},
	}

	if mutate != nil {
		mutate(registration)
	}

	if _, err := store.Upsert(context.Background(), registration); err != nil {
		t.Fatalf("seed result set: %v", err)
	}
}
