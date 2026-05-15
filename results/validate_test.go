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
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

type seqmetaResponseForTest struct {
	status int
	body   string
}

func TestSeqmetaValidatorValidateMetadata(t *testing.T) {
	convey.Convey("D1.1: ValidateMetadata accepts matching seqmeta identifier types", t, func() {
		server := newSeqmetaServerForTest(map[string]seqmetaResponseForTest{
			"48522": {status: http.StatusOK, body: `{"identifier":"48522","type":"run_id","object":{}}`},
		})
		defer server.Close()

		validator := NewSeqmetaValidator(server.URL, time.Second)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_runid": "48522"})

		convey.So(err, convey.ShouldBeNil)
	})

	convey.Convey("D1.2: ValidateMetadata rejects mismatched seqmeta identifier types", t, func() {
		server := newSeqmetaServerForTest(map[string]seqmetaResponseForTest{
			"48522": {status: http.StatusOK, body: `{"identifier":"48522","type":"sanger_sample_id","object":{}}`},
		})
		defer server.Close()

		validator := NewSeqmetaValidator(server.URL, time.Second)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_runid": "48522"})

		convey.So(errors.Is(err, ErrSeqmetaRejected), convey.ShouldBeTrue)
	})

	convey.Convey("Bug 2: ValidateMetadata accepts seqmeta_library when seqmeta resolves it as a library type", t, func() {
		server := newSeqmetaServerForTest(map[string]seqmetaResponseForTest{
			"71046409": {status: http.StatusOK, body: `{"identifier":"Custom","type":"library_type","object":{}}`},
		})
		defer server.Close()

		validator := NewSeqmetaValidator(server.URL, time.Second)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_library": "71046409"})

		convey.So(err, convey.ShouldBeNil)
	})

	convey.Convey("D1.3: ValidateMetadata rejects unknown seqmeta metadata suffixes", t, func() {
		validator := NewSeqmetaValidator("http://example.test", time.Second)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_unknown": "val"})

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
	})

	convey.Convey("D1.4: ValidateMetadata wraps unreachable seqmeta service failures", t, func() {
		validator := NewSeqmetaValidator("http://127.0.0.1:1", 50*time.Millisecond)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_runid": "48522"})

		convey.So(errors.Is(err, ErrSeqmetaFailed), convey.ShouldBeTrue)
	})

	convey.Convey("D1.5: ValidateMetadata skips validation for a nil validator", t, func() {
		var validator *SeqmetaValidator

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_runid": "48522"})

		convey.So(err, convey.ShouldBeNil)
	})

	convey.Convey("D1.6: ValidateMetadata skips metadata without seqmeta keys", t, func() {
		validator := NewSeqmetaValidator("http://127.0.0.1:1", time.Second)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"library": "exon"})

		convey.So(err, convey.ShouldBeNil)
	})

	convey.Convey("D1.7: ValidateMetadata treats missing seqmeta identifiers as rejected", t, func() {
		server := newSeqmetaServerForTest(map[string]seqmetaResponseForTest{
			"48522": {status: http.StatusNotFound, body: `{"error":"not found"}`},
		})
		defer server.Close()

		validator := NewSeqmetaValidator(server.URL, time.Second)

		err := validator.ValidateMetadata(context.Background(), map[string]string{"seqmeta_runid": "48522"})

		convey.So(errors.Is(err, ErrSeqmetaRejected), convey.ShouldBeTrue)
	})
}

func TestValidateRegistrationRejectsDirectorySymlinkEscapes(t *testing.T) {
	convey.Convey("ValidateRegistration rejects output files reached via a directory symlink that exits the output directory", t, func() {
		outputDir := t.TempDir()
		externalDir := t.TempDir()
		externalFile := filepath.Join(externalDir, "external.txt")

		convey.So(os.WriteFile(externalFile, []byte("data"), 0o644), convey.ShouldBeNil)
		convey.So(os.Symlink(externalDir, filepath.Join(outputDir, "escape")), convey.ShouldBeNil)

		reg := &Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    outputDir,
			Files: []FileEntry{{
				Path:  filepath.Join(outputDir, "escape", "external.txt"),
				Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC),
				Size:  4,
				Kind:  "output",
			}},
		}

		err := ValidateRegistration(reg)

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "outside output directory")
	})
}

func TestValidateRegistrationRejectsFileSymlinkEscapes(t *testing.T) {
	convey.Convey("ValidateRegistration rejects output file symlinks whose resolved targets are outside the output directory", t, func() {
		outputDir := t.TempDir()
		externalDir := t.TempDir()
		externalFile := filepath.Join(externalDir, "external.txt")
		linkPath := filepath.Join(outputDir, "link.txt")

		convey.So(os.WriteFile(externalFile, []byte("data"), 0o644), convey.ShouldBeNil)
		convey.So(os.Symlink(externalFile, linkPath), convey.ShouldBeNil)

		reg := &Registration{
			PipelineIdentifier: "pipe",
			RunKey:             "runid=48522",
			Requester:          "alice",
			PipelineName:       "nf-pipe",
			PipelineVersion:    "1.2.3",
			OutputDirectory:    outputDir,
			Files: []FileEntry{{
				Path:  linkPath,
				Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC),
				Size:  4,
				Kind:  "output",
			}},
		}

		err := ValidateRegistration(reg)

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "outside output directory")
	})
}

func newSeqmetaServerForTest(responses map[string]seqmetaResponseForTest) *httptest.Server {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response, ok := responses[r.PathValue("identifier")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(response.status)
		_, _ = fmt.Fprint(w, response.body)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /validate/{identifier}", handler)

	return httptest.NewServer(mux)
}
