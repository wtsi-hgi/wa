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
	"os"
	"path/filepath"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/results"

	_ "modernc.org/sqlite"
)

func TestResultsRescanCommand(t *testing.T) {
	convey.Convey("G5.1: Given a registered result set and a t.TempDir() with 3 files (1 new since registration), when rescan <id> <dir> is run, then the server's file list for that ID has 3 output files", t, func() {
		store := newResultsRescanStoreForTest(t)
		dir := t.TempDir()

		initialFiles := []results.FileEntry{
			createResultsRescanFileForTest(t, dir, "a.txt", "alpha"),
			createResultsRescanFileForTest(t, dir, "nested/b.txt", "beta"),
		}

		registration := &results.Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			Operator:           "bob",
			Command:            "nextflow run pipe",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    dir,
			Files:              initialFiles,
			Metadata:           map[string]string{"library": "exon"},
		}

		stored, err := store.Upsert(context.Background(), registration)
		convey.So(err, convey.ShouldBeNil)

		createResultsRescanFileForTest(t, dir, "nested/c.txt", "gamma")

		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		_, err = executeRootCommandForTest(t, []string{"results", "rescan", "--server", server.URL, stored.ID, dir})

		convey.So(err, convey.ShouldBeNil)

		files, getErr := store.GetFiles(context.Background(), stored.ID)
		convey.So(getErr, convey.ShouldBeNil)
		convey.So(files, convey.ShouldHaveLength, 3)

		filesByBase := map[string]results.FileEntry{}
		for _, file := range files {
			filesByBase[filepath.Base(file.Path)] = file
			convey.So(file.Kind, convey.ShouldEqual, "output")
		}

		convey.So(filesByBase["a.txt"].Path, convey.ShouldEqual, filepath.Join(dir, "a.txt"))
		convey.So(filesByBase["b.txt"].Path, convey.ShouldEqual, filepath.Join(dir, "nested", "b.txt"))
		convey.So(filesByBase["c.txt"].Path, convey.ShouldEqual, filepath.Join(dir, "nested", "c.txt"))
	})

	convey.Convey("G5.2: Given non-existent ID, then exit code is non-zero", t, func() {
		dir := t.TempDir()
		createResultsRescanFileForTest(t, dir, "a.txt", "alpha")

		store := newResultsRescanStoreForTest(t)
		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		_, err := executeRootCommandForTest(t, []string{"results", "rescan", "--server", server.URL, "missing-id", dir})

		convey.So(err, convey.ShouldNotBeNil)
	})

	convey.Convey("rescan rejects a directory that does not match the registered output directory", t, func() {
		store := newResultsRescanStoreForTest(t)
		registeredDir := t.TempDir()
		wrongDir := t.TempDir()

		originalFile := createResultsRescanFileForTest(t, registeredDir, "a.txt", "alpha")
		createResultsRescanFileForTest(t, wrongDir, "other.txt", "beta")

		stored, err := store.Upsert(context.Background(), &results.Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			Operator:           "bob",
			Command:            "nextflow run pipe",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    registeredDir,
			Files:              []results.FileEntry{originalFile},
		})
		convey.So(err, convey.ShouldBeNil)

		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		_, err = executeRootCommandForTest(t, []string{"results", "rescan", "--server", server.URL, stored.ID, wrongDir})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "does not match registered output directory")

		files, getErr := store.GetFiles(context.Background(), stored.ID)
		convey.So(getErr, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []results.FileEntry{originalFile})
	})

	convey.Convey("rescan reports scan warnings on stderr when alias paths are skipped", t, func() {
		store := newResultsRescanStoreForTest(t)
		dir := t.TempDir()
		realDir := filepath.Join(dir, "real")
		aliasDir := filepath.Join(dir, "alias")

		originalFile := createResultsRescanFileForTest(t, realDir, "a.txt", "alpha")
		convey.So(os.Symlink(realDir, aliasDir), convey.ShouldBeNil)

		stored, err := store.Upsert(context.Background(), &results.Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			Operator:           "bob",
			Command:            "nextflow run pipe",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    dir,
			Files:              []results.FileEntry{originalFile},
		})
		convey.So(err, convey.ShouldBeNil)

		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		command := NewRootCommand()
		command.SetOut(stdout)
		command.SetErr(stderr)
		command.SetArgs([]string{"results", "rescan", "--server", server.URL, stored.ID, dir})

		err = command.Execute()

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "warning: skipped")
	})

	convey.Convey("rescan rejects directory symlinks that resolve outside the requested output directory before sending a request", t, func() {
		dir := t.TempDir()
		externalDir := t.TempDir()
		createResultsRescanFileForTest(t, dir, "a.txt", "alpha")
		createResultsRescanFileForTest(t, externalDir, "outside.txt", "beta")
		convey.So(os.Symlink(externalDir, filepath.Join(dir, "escape")), convey.ShouldBeNil)

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(results.ResultSet{
				ID:              "ignored-id",
				OutputDirectory: dir,
			})
		}))
		defer server.Close()

		_, err := executeRootCommandForTest(t, []string{"results", "rescan", "--server", server.URL, "ignored-id", dir})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "resolves outside")
		convey.So(requestCount, convey.ShouldEqual, 1)
	})

	convey.Convey("rescan rejects alias directories that are not the registered output directory", t, func() {
		store := newResultsRescanStoreForTest(t)
		registeredDir := t.TempDir()
		aliasRoot := t.TempDir()
		convey.So(os.Symlink(registeredDir, filepath.Join(aliasRoot, "linked-output")), convey.ShouldBeNil)
		createResultsRescanFileForTest(t, registeredDir, "a.txt", "alpha")

		stored, err := store.Upsert(context.Background(), &results.Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			Operator:           "bob",
			Command:            "nextflow run pipe",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    registeredDir,
			Files:              []results.FileEntry{createResultsRescanFileForTest(t, registeredDir, "b.txt", "beta")},
		})
		convey.So(err, convey.ShouldBeNil)

		server := httptest.NewServer(results.NewServer(store, nil, nil).Handler())
		defer server.Close()

		_, err = executeRootCommandForTest(t, []string{"results", "rescan", "--server", server.URL, stored.ID, aliasRoot})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "does not match registered output directory")
	})
}

func newResultsRescanStoreForTest(t *testing.T) *results.Store {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

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

func createResultsRescanFileForTest(t *testing.T, rootDir, relativePath, content string) results.FileEntry {
	t.Helper()

	absPath := filepath.Join(rootDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", absPath, err)
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", absPath, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		t.Fatalf("stat %s: %v", absPath, err)
	}

	return results.FileEntry{
		Path:  absPath,
		Mtime: info.ModTime().UTC(),
		Size:  info.Size(),
		Kind:  "output",
	}
}
