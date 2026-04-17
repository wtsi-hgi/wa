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

package results

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestServerGetFile(t *testing.T) {
	convey.Convey("A1.1: Given a registered html file on disk, when GET /results/{id}/file is called, then status 200, Content-Type is text/html; charset=utf-8, and body matches", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), []byte("<html>hello</html>"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 0, 0, 0, time.UTC), Size: 18, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "text/html; charset=utf-8")
		convey.So(response.Body.String(), convey.ShouldEqual, "<html>hello</html>")
	})

	convey.Convey("A1.2: Given a registered csv file, when requested, then Content-Type is text/csv", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "data.csv"), []byte("a,b\n1,2\n"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 1, 0, 0, time.UTC), Size: 8, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "text/csv")
	})

	convey.Convey("A1.3: Given a registered file with an unknown extension, when requested, then Content-Type falls back to application/octet-stream", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "unknown.xyz"), []byte("opaque"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 2, 0, 0, time.UTC), Size: 6, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/octet-stream")
	})

	convey.Convey("A1.4: Given an existing file that is not registered, when requested, then status is 403", t, func() {
		server, resultID, _ := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			registeredPath := writeTestFileForServer(t, filepath.Join(root, "registered.txt"), []byte("registered"))

			return []FileEntry{{Path: registeredPath, Mtime: time.Date(2026, time.April, 16, 10, 3, 0, 0, time.UTC), Size: 10, Kind: "output"}}
		})
		missingRegistrationPath := writeTestFileForServer(t, filepath.Join(t.TempDir(), "not-registered.txt"), []byte("not registered"))

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(missingRegistrationPath), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
	})

	convey.Convey("A1.5: Given a registered file path missing on disk, when requested, then status is 410 with file not found on disk", t, func() {
		root := t.TempDir()
		missingPath := filepath.Join(root, "gone.txt")
		server, resultID := newFileServerWithFilesForTest(t, []FileEntry{{
			Path:  missingPath,
			Mtime: time.Date(2026, time.April, 16, 10, 4, 0, 0, time.UTC),
			Size:  4,
			Kind:  "output",
		}})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(missingPath), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusGone)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "file not found on disk")
	})

	convey.Convey("A1.6: Given a missing result set ID, when requested, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)
		path := writeTestFileForServer(t, filepath.Join(t.TempDir(), "report.html"), []byte("<html>hello</html>"))

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/missing-id/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("A1.7: Given no path query parameter, when requested, then status is 400", t, func() {
		server, resultID, _ := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), []byte("<html>hello</html>"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 5, 0, 0, time.UTC), Size: 18, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})

	convey.Convey("A1.8: Given a registered gzipped csv file, when previewed, then status 200, Content-Type is text/csv, and body is decompressed", t, func() {
		compressed, raw := gzipFileBytesForTest(t, "data.csv", []byte("a,b\n1,2\n"))
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "data.csv.gz"), compressed)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 6, 0, 0, time.UTC), Size: int64(len(compressed)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "text/csv")
		convey.So(response.Body.Bytes(), convey.ShouldResemble, raw)
	})

	convey.Convey("A1.9: Given the same gzipped csv file with download=true, then status 200, application/gzip, attachment filename, and compressed bytes", t, func() {
		compressed, _ := gzipFileBytesForTest(t, "data.csv", []byte("a,b\n1,2\n"))
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "data.csv.gz"), compressed)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 7, 0, 0, time.UTC), Size: int64(len(compressed)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&download=true", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/gzip")
		convey.So(response.Header().Get("Content-Disposition"), convey.ShouldContainSubstring, "filename=\"data.csv.gz\"")
		convey.So(response.Body.Bytes(), convey.ShouldResemble, compressed)
	})

	convey.Convey("A1.10: Given a preview size limit smaller than the registered file, when previewed, then status is 413 with X-File-Size", t, func() {
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(100)}, func(root string) []FileEntry {
			payload := bytes.Repeat([]byte("x"), 200)
			path := writeTestFileForServer(t, filepath.Join(root, "large.txt"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 8, 0, 0, time.UTC), Size: 200, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusRequestEntityTooLarge)
		convey.So(response.Header().Get("X-File-Size"), convey.ShouldEqual, "200")
	})

	convey.Convey("A1.11: Given the same oversized file with download=true, then status is 200 and the full file is streamed", t, func() {
		payload := bytes.Repeat([]byte("x"), 200)
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(100)}, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "large.txt"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 9, 0, 0, time.UTC), Size: 200, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&download=true", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.Bytes(), convey.ShouldResemble, payload)
	})

	convey.Convey("A1.12: Given a registered png file, when requested, then Content-Type is image/png", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "image.png"), []byte("png"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 10, 0, 0, time.UTC), Size: 3, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, "/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "image/png")
	})
}

func newFileServerScenarioForTest(t *testing.T, files func(root string) []FileEntry) (*Server, string, string) {
	t.Helper()

	return newFileServerScenarioForTestWithOptions(t, nil, files)
}

func writeTestFileForServer(t *testing.T, path string, body []byte) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create test file dir: %v", err)
	}

	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	return path
}

func outputDirectoryForFilesForTest(t *testing.T, files []FileEntry) string {
	t.Helper()

	for _, file := range files {
		if file.Kind == "output" {
			return filepath.Dir(file.Path)
		}
	}

	return t.TempDir()
}

func gzipFileBytesForTest(t *testing.T, name string, body []byte) ([]byte, []byte) {
	t.Helper()

	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	gzipWriter.Name = name
	_, err := gzipWriter.Write(body)
	if err != nil {
		t.Fatalf("write gzip body: %v", err)
	}

	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	return buffer.Bytes(), body
}

func newFileServerScenarioForTestWithOptions(t *testing.T, opts []ServerOption, files func(root string) []FileEntry) (*Server, string, string) {
	t.Helper()

	root := t.TempDir()
	registeredFiles := files(root)
	server, resultID := newFileServerWithFilesForTest(t, registeredFiles, opts...)

	return server, resultID, registeredFiles[0].Path
}

func newFileServerWithFilesForTest(t *testing.T, files []FileEntry, opts ...ServerOption) (*Server, string) {
	t.Helper()

	store := newSQLiteStoreForTest(t)
	reg := testRegistration()
	reg.OutputDirectory = outputDirectoryForFilesForTest(t, files)
	reg.Files = files
	for i := range reg.Files {
		if reg.Files[i].Kind == "output" && reg.Files[i].Mtime.IsZero() {
			reg.Files[i].Mtime = time.Date(2026, time.April, 16, 10, 0, 0, 0, time.UTC)
		}
		if reg.Files[i].Kind == "output" && reg.Files[i].Size == 0 {
			info, err := os.Stat(reg.Files[i].Path)
			if err == nil {
				reg.Files[i].Size = info.Size()
			}
		}
	}

	result, err := store.Upsert(context.Background(), reg)
	if err != nil {
		t.Fatalf("seed result set for file server: %v", err)
	}

	return NewServer(store, nil, nil, opts...), result.ID
}
