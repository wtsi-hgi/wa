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
	gas "github.com/wtsi-hgi/go-authserver"
)

func TestServerGetFile(t *testing.T) {
	convey.Convey("C1.3: Given no JWT and an existing result, when GET /rest/v1/results/<id>/file is called, then status is 403 and no file bytes are returned", t, func() {
		payload := []byte("<html>secret</html>")
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 9, 0, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, gas.EndPointREST+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		convey.So(response.Body.String(), convey.ShouldNotContainSubstring, string(payload))
		assertLockedResponseForTest(t, response, resultID)
	})

	convey.Convey("C1.7: Given user bob lacks access, when GET /rest/v1/auth/results/<id>/file is called, then status is 403 and no file bytes are returned", t, func() {
		payload := []byte("<html>secret</html>")
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 9, 1, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})
		handler := newResultsGinHandlerForTest(t, server, &CurrentUser{
			Username: "bob",
			User:     authUserForTest{gids: []string{"100"}},
		})

		response := performResultsRequestForTest(t, handler, http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusForbidden)
		convey.So(response.Body.String(), convey.ShouldNotContainSubstring, string(payload))
		assertLockedResponseForTest(t, response, resultID)
	})

	convey.Convey("C1.8: Given user alice has access, when auth file content is requested, then existing 200 headers and body are preserved", t, func() {
		payload := []byte("<html>hello</html>")
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 9, 2, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "text/html; charset=utf-8")
		convey.So(response.Body.Bytes(), convey.ShouldResemble, payload)
	})

	convey.Convey("A1.1: Given a registered html file on disk, when GET /results/{id}/file is called, then status 200, Content-Type is text/html; charset=utf-8, and body matches", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), []byte("<html>hello</html>"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 0, 0, 0, time.UTC), Size: 18, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "text/html; charset=utf-8")
		convey.So(response.Body.String(), convey.ShouldEqual, "<html>hello</html>")
	})

	convey.Convey("A1.2: Given a registered csv file, when requested, then Content-Type is text/csv", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "data.csv"), []byte("a,b\n1,2\n"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 1, 0, 0, time.UTC), Size: 8, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "text/csv")
	})

	convey.Convey("A1.3: Given a registered file with an unknown extension, when requested, then Content-Type falls back to application/octet-stream", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "unknown.xyz"), []byte("opaque"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 2, 0, 0, time.UTC), Size: 6, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/octet-stream")
	})

	convey.Convey("A1.4: Given an existing file that is not registered, when requested, then status is 403", t, func() {
		server, resultID, _ := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			registeredPath := writeTestFileForServer(t, filepath.Join(root, "registered.txt"), []byte("registered"))

			return []FileEntry{{Path: registeredPath, Mtime: time.Date(2026, time.April, 16, 10, 3, 0, 0, time.UTC), Size: 10, Kind: "output"}}
		})
		missingRegistrationPath := writeTestFileForServer(t, filepath.Join(t.TempDir(), "not-registered.txt"), []byte("not registered"))

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(missingRegistrationPath), nil)

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

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(missingPath), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusGone)
		convey.So(errorResponseBodyForTest(t, response), convey.ShouldContainSubstring, "file not found on disk")
	})

	convey.Convey("A1.6: Given a missing result set ID, when requested, then status is 404", t, func() {
		store := newSQLiteStoreForTest(t)
		server := NewServer(store, nil, nil)
		path := writeTestFileForServer(t, filepath.Join(t.TempDir(), "report.html"), []byte("<html>hello</html>"))

		response := performResultsRequestForTest(t, server.Handler(), http.MethodGet, gas.EndPointREST+"/results/missing-id/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusNotFound)
	})

	convey.Convey("A1.7: Given no path query parameter, when requested, then status is 400", t, func() {
		server, resultID, _ := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.html"), []byte("<html>hello</html>"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 5, 0, 0, time.UTC), Size: 18, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusBadRequest)
	})

	convey.Convey("A1.8: Given a registered gzipped csv file, when previewed, then status 200, Content-Type is text/csv, and body is decompressed", t, func() {
		compressed, raw := gzipFileBytesForTest(t, "data.csv", []byte("a,b\n1,2\n"))
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "data.csv.gz"), compressed)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 6, 0, 0, time.UTC), Size: int64(len(compressed)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

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

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&download=true", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "application/gzip")
		convey.So(response.Header().Get("Content-Disposition"), convey.ShouldContainSubstring, "filename=\"data.csv.gz\"")
		convey.So(response.Body.Bytes(), convey.ShouldResemble, compressed)
	})

	convey.Convey("A1.10: Given a preview size limit smaller than a non-readable registered file, when previewed, then status is 413 with X-File-Size", t, func() {
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(100)}, func(root string) []FileEntry {
			payload := bytes.Repeat([]byte("x"), 200)
			path := writeTestFileForServer(t, filepath.Join(root, "large.bin"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 8, 0, 0, time.UTC), Size: 200, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusRequestEntityTooLarge)
		convey.So(response.Header().Get("X-File-Size"), convey.ShouldEqual, "200")
	})

	convey.Convey("A1.11: Given the same oversized non-readable file with download=true, then status is 200 and the full file is streamed", t, func() {
		payload := bytes.Repeat([]byte("x"), 200)
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(100)}, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "large.bin"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 9, 0, 0, time.UTC), Size: 200, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&download=true", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.Bytes(), convey.ShouldResemble, payload)
	})

	convey.Convey("A1.12: Given a registered png file, when requested, then Content-Type is image/png", t, func() {
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "image.png"), []byte("png"))

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 10, 0, 0, time.UTC), Size: 3, Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Header().Get("Content-Type"), convey.ShouldEqual, "image/png")
	})

	convey.Convey("A1.13: Given a gzipped file whose decompressed content exceeds maxPreviewBytes, when previewed, then the response body is truncated to the limit", t, func() {
		largeContent := bytes.Repeat([]byte("x"), 200)
		compressed, _ := gzipFileBytesForTest(t, "data.csv", largeContent)
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(50)}, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "data.csv.gz"), compressed)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 11, 0, 0, time.UTC), Size: int64(len(compressed)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.Len(), convey.ShouldBeLessThanOrEqualTo, 50)
		convey.So(response.Body.Bytes(), convey.ShouldResemble, largeContent[:50])
	})

	convey.Convey("A1.14: Given an oversized registered TSV file, when previewed, then status is 200 with as many full lines as fit under the preview-size limit and truncation metadata", t, func() {
		payload := []byte("h\n1111\n2222\n3333\n4444\n")
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(12)}, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.tsv"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 12, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path), nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "h\n1111\n2222\n")
		convey.So(response.Header().Get("X-Preview-Truncated"), convey.ShouldEqual, "true")
	})

	convey.Convey("A1.15: Given a readable TSV preview with line_limit, when short rows stay under the byte cap, then the response still respects the requested line limit and truncation metadata", t, func() {
		payload := []byte("sample\tstatus\nalpha\tready\nbeta\tready\ngamma\tready\n")
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.tsv"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 13, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&line_limit=2", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.String(), convey.ShouldEqual, "sample\tstatus\nalpha\tready\n")
		convey.So(response.Header().Get("X-Preview-Truncated"), convey.ShouldEqual, "true")
	})

	convey.Convey("A1.16: Given a large readable TSV file, when previewed in inline mode, then the response is capped at MaxInlinePreviewLines lines regardless of underlying file size", t, func() {
		var payloadBuffer bytes.Buffer
		payloadBuffer.WriteString("col_a\tcol_b\n")
		for i := 0; i < 5_000; i++ {
			payloadBuffer.WriteString("alpha\tready\n")
		}
		payload := payloadBuffer.Bytes()
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.tsv"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 14, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&mode=inline", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		lineCount := bytes.Count(response.Body.Bytes(), []byte("\n"))
		convey.So(lineCount, convey.ShouldBeLessThanOrEqualTo, MaxInlinePreviewLines)
		convey.So(response.Header().Get("X-Preview-Truncated"), convey.ShouldEqual, "true")
	})

	convey.Convey("A1.17: Given a request in inline mode, when client supplies a larger line_limit query, then the backend still caps the response at MaxInlinePreviewLines", t, func() {
		var payloadBuffer bytes.Buffer
		for i := 0; i < 200; i++ {
			payloadBuffer.WriteString("row\n")
		}
		payload := payloadBuffer.Bytes()
		server, resultID, path := newFileServerScenarioForTest(t, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.tsv"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 15, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&mode=inline&line_limit=9999", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		lineCount := bytes.Count(response.Body.Bytes(), []byte("\n"))
		convey.So(lineCount, convey.ShouldEqual, MaxInlinePreviewLines)
		convey.So(response.Header().Get("X-Preview-Truncated"), convey.ShouldEqual, "true")
	})

	convey.Convey("A1.18: Given a large readable TSV file, when previewed in enlarged mode, then the response honours the byte cap with no per-line cap", t, func() {
		var payloadBuffer bytes.Buffer
		payloadBuffer.WriteString("col_a\tcol_b\n")
		for i := 0; i < 5_000; i++ {
			payloadBuffer.WriteString("alpha\tready\n")
		}
		payload := payloadBuffer.Bytes()
		byteCap := int64(2048)
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(byteCap)}, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.tsv"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 16, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&mode=enlarged", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(int64(response.Body.Len()), convey.ShouldBeLessThanOrEqualTo, byteCap)
		lineCount := bytes.Count(response.Body.Bytes(), []byte("\n"))
		convey.So(lineCount, convey.ShouldBeGreaterThan, MaxInlinePreviewLines)
		convey.So(response.Header().Get("X-Preview-Truncated"), convey.ShouldEqual, "true")
	})

	convey.Convey("A1.19: Given a large readable TSV file, when downloaded in download mode, then the entire file is delivered without preview caps", t, func() {
		var payloadBuffer bytes.Buffer
		payloadBuffer.WriteString("col_a\tcol_b\n")
		for i := 0; i < 5_000; i++ {
			payloadBuffer.WriteString("alpha\tready\n")
		}
		payload := payloadBuffer.Bytes()
		server, resultID, path := newFileServerScenarioForTestWithOptions(t, []ServerOption{WithMaxPreviewBytes(2048)}, func(root string) []FileEntry {
			path := writeTestFileForServer(t, filepath.Join(root, "report.tsv"), payload)

			return []FileEntry{{Path: path, Mtime: time.Date(2026, time.April, 16, 10, 17, 0, 0, time.UTC), Size: int64(len(payload)), Kind: "output"}}
		})

		response := performResultsRequestForTest(t, fileServerHandlerForTest(t, server), http.MethodGet, gas.EndPointAuth+"/results/"+resultID+"/file?path="+url.QueryEscape(path)+"&mode=download", nil)

		convey.So(response.Code, convey.ShouldEqual, http.StatusOK)
		convey.So(response.Body.Len(), convey.ShouldEqual, len(payload))
		convey.So(response.Body.Bytes(), convey.ShouldResemble, payload)
		convey.So(response.Header().Get("Content-Disposition"), convey.ShouldContainSubstring, "attachment;")
		convey.So(response.Header().Get("X-Preview-Truncated"), convey.ShouldEqual, "")
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

func fileServerHandlerForTest(t *testing.T, server *Server) http.Handler {
	t.Helper()

	return newResultsGinHandlerForTest(t, server, &CurrentUser{
		Username: "alice",
		User:     authUserForTest{},
	})
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
	reg.OutputDirectoryGID = gidForTest(200)
	reg.Operator = "carol"
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
