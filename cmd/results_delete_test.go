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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/results"
)

func TestResultsDeleteCommand(t *testing.T) {
	convey.Convey("G4.1: Given a valid ID, when delete <id> is run, then exit code 0 and subsequent GET returns error", t, func() {
		store := newResultsDeleteStoreForTest(t)
		stored, err := store.Upsert(t.Context(), resultsDeleteRegistrationForTest())
		convey.So(err, convey.ShouldBeNil)

		server := httptest.NewServer(results.NewServer(store, nil).Handler())
		defer server.Close()

		output, err := executeRootCommandForTest(t, []string{"results", "delete", "--server", server.URL, stored.ID})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldBeBlank)

		response, err := http.Get(server.URL + "/results/" + stored.ID)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = response.Body.Close() }()
		convey.So(response.StatusCode, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("G4.2: Given non-existent ID, then exit code is non-zero", t, func() {
		store := newResultsDeleteStoreForTest(t)
		server := httptest.NewServer(results.NewServer(store, nil).Handler())
		defer server.Close()

		_, err := executeRootCommandForTest(t, []string{"results", "delete", "--server", server.URL, "missing-id"})

		convey.So(err, convey.ShouldNotBeNil)
	})
}

func newResultsDeleteStoreForTest(t *testing.T) *results.Store {
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

func resultsDeleteRegistrationForTest() *results.Registration {
	return &results.Registration{
		PipelineIdentifier: "pipe",
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
}
