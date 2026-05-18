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
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/results"
)

func TestResultsRegisterCommand(t *testing.T) {
	convey.Convey("G1.1: Given a results server and an output directory with 2 files, when register is run, then stdout is valid JSON with an id and the server receives 2 output files", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "a.txt"), "alpha")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "sub", "b.txt"), "beta")
		writeRegisterCommandTestFile(t, workflowPath, "nextflow.enable.dsl=2\n")

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				handlerErrCh <- errors.New("unexpected request method")

				return
			}

			if r.URL.Path != "/results" {
				handlerErrCh <- errors.New("unexpected request path")

				return
			}

			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration

			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "result-123"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		stdout, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--operator", "bob",
			"--runid", "48522",
			"--additional-unique", "exon",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		var result results.ResultSet
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.ID, convey.ShouldEqual, "result-123")

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Requester, convey.ShouldEqual, "alice")
		convey.So(registration.Operator, convey.ShouldEqual, "bob")
		convey.So(registration.RunKey, convey.ShouldEqual, "runid=48522&unique=exon")
		convey.So(registration.OutputDirectory, convey.ShouldEqual, outputDir)
		convey.So(countRegisterCommandFilesByKind(registration.Files, "output"), convey.ShouldEqual, 2)
		convey.So(countRegisterCommandFilesByKind(registration.Files, "pipeline"), convey.ShouldEqual, 1)
	})

	convey.Convey("G1.2: Given --json, when registration JSON is piped to stdin, then it is sent as-is to the server without scanning", t, func() {
		payload := registerCommandRegistrationForTest()
		payload.OutputDirectory = "/does/not/need/to/exist"
		payload.Files = []results.FileEntry{{
			Path:  "/does/not/need/to/exist/from-json.txt",
			Mtime: time.Date(2026, time.April, 16, 12, 34, 56, 0, time.UTC),
			Size:  99,
			Kind:  "output",
		}}

		body, err := json.Marshal(payload)
		convey.So(err, convey.ShouldBeNil)

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration

			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "json-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		stdout, stderr, runErr := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--json",
		}, body)

		convey.So(runErr, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(<-registrationCh, convey.ShouldResemble, *payload)
		convey.So(<-handlerErrCh, convey.ShouldBeNil)

		var result results.ResultSet
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.ID, convey.ShouldEqual, "json-result")
	})

	convey.Convey("register --json rejects trailing JSON input", t, func() {
		payload := append(mustRegisterCommandJSONBody(t, registerCommandRegistrationForTest()), []byte("\n{}")...)

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register", "--json",
		}, payload)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "trailing JSON")
	})

	convey.Convey("register --json rejects registrations missing requester", t, func() {
		payload := registerCommandRegistrationForTest()
		payload.Requester = ""

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register", "--json",
		}, mustRegisterCommandJSONBody(t, payload))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "requester is required")
	})

	convey.Convey("register --json rejects relative output paths", t, func() {
		payload := registerCommandRegistrationForTest()
		payload.OutputDirectory = "relative/run"
		payload.Files[0].Path = "relative/run/out.txt"

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register", "--json",
		}, mustRegisterCommandJSONBody(t, payload))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "must be absolute")
	})

	convey.Convey("register --json rejects duplicate tracked files", t, func() {
		payload := registerCommandRegistrationForTest()
		payload.Files = []results.FileEntry{
			payload.Files[0],
			payload.Files[0],
		}

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register", "--json",
		}, mustRegisterCommandJSONBody(t, payload))

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "duplicate file path")
	})

	convey.Convey("G1.3: Given --input-file, then the registration includes an input entry with the correct size", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		inputPath := filepath.Join(t.TempDir(), "sample_sheet.tsv")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")
		writeRegisterCommandTestFile(t, inputPath, "col1\tcol2\n")

		inputFileCh := make(chan results.FileEntry, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			inputFile, findErr := findRegisterCommandFileByKind(registration.Files, "input")
			if findErr != nil {
				handlerErrCh <- findErr

				return
			}

			inputFileCh <- inputFile

			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "input-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--input-file", inputPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		inputFile := <-inputFileCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(inputFile.Path, convey.ShouldEqual, inputPath)
		convey.So(inputFile.Size, convey.ShouldEqual, int64(len("col1\tcol2\n")))
	})

	convey.Convey("register reports scan warnings on stderr when directory aliases are skipped", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		realDir := filepath.Join(outputDir, "real")
		aliasDir := filepath.Join(outputDir, "alias")
		writeRegisterCommandTestFile(t, filepath.Join(realDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")
		convey.So(os.Symlink(realDir, aliasDir), convey.ShouldBeNil)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "warn-result"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "warning: skipped")
	})

	convey.Convey("register rejects directory symlinks that resolve outside the output directory before sending a request", t, func() {
		outputDir := t.TempDir()
		externalDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, filepath.Join(externalDir, "external.txt"), "outside")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")
		convey.So(os.Symlink(externalDir, filepath.Join(outputDir, "escape")), convey.ShouldBeNil)

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "resolves outside")
		convey.So(requestCount, convey.ShouldEqual, 0)
	})

	convey.Convey("register deduplicates workflow files that also appear in the scanned output directory", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(outputDir, "main.nf")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		registrationCh := make(chan results.Registration, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			_ = json.NewDecoder(r.Body).Decode(&registration)
			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "dedupe-result"})
		}))
		defer server.Close()

		_, _, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		registration := <-registrationCh
		convey.So(countRegisterCommandFilesByKind(registration.Files, "pipeline"), convey.ShouldEqual, 1)
		convey.So(countRegisterCommandFilesByKind(registration.Files, "output"), convey.ShouldEqual, 0)
	})

	convey.Convey("register accepts a 200 upsert response from the server", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")
		handlerErrCh := make(chan error, 1)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			handlerErrCh <- json.NewEncoder(w).Encode(results.ResultSet{ID: "updated-result"})
		}))
		defer server.Close()

		stdout, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(<-handlerErrCh, convey.ShouldBeNil)

		var result results.ResultSet
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.ID, convey.ShouldEqual, "updated-result")
	})

	convey.Convey("Given --run and a warm MLWH cache, register resolves from the cache without opening the upstream source DSN", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
		seedResultsRegisterRunCacheForTest(t, cachePath, 48522)
		t.Setenv("WA_MLWH_CACHE_PATH", cachePath)
		t.Setenv("WA_MLWH_DSN", "mlwh_humgen:secret@tcp(127.0.0.1:1)/mlwarehouse")

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "cache-only-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--run", "48522",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata, convey.ShouldResemble, map[string]string{
			"seqmeta_runid": "48522",
		})
	})

	convey.Convey("E1.4: Given --study/--run/--library/--sample together, when all mlwh resolvers succeed, then register stores canonical seqmeta metadata entries", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			runFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{Canonical: raw, Run: &mlwh.Run{IDRun: 12345}}, nil
			},
			studyFn: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{Canonical: "6568", Study: &mlwh.Study{IDStudyLims: "6568"}}, nil
			},
			sampleFn: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{Canonical: "7607STDY14643771", Sample: &mlwh.Sample{Name: "7607STDY14643771"}}, nil
			},
			libraryFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{Canonical: raw, Library: &mlwh.Library{PipelineIDLims: raw}}, nil
			},
		})

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "seqmeta-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--run", "12345",
			"--study", "EGAS00001005445",
			"--sample", "7607STDY14643771",
			"--library", "Standard",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata, convey.ShouldResemble, map[string]string{
			"seqmeta_runid":       "12345",
			"seqmeta_studyid":     "6568",
			"seqmeta_sampleid":    "7607STDY14643771",
			"seqmeta_librarytype": "Standard",
		})
	})

	convey.Convey("Bug 2: Given --library is a library_id, when mlwh resolves it, then register stores the library ID metadata key and canonical library type context", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			libraryFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "71046409")

				return mlwh.Match{
					Kind:      mlwh.KindLibraryType,
					Canonical: "Custom",
					Library: &mlwh.Library{
						PipelineIDLims: "Custom",
						IDStudyLims:    "7607",
						LibraryID:      "71046409",
						IDLibraryLims:  "SQPP-47463-G:B1",
					},
				}, nil
			},
		})

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "library-id-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--library", "71046409",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata, convey.ShouldResemble, map[string]string{
			"seqmeta_libraryid":   "71046409",
			"seqmeta_librarytype": "Custom",
		})
	})

	convey.Convey("Bug 2: Given a realistic register command with a library_id, when exact library lookup is available, then register does not wait on broad library resolution", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		var broadLibraryCalls int
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			runFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{Kind: mlwh.KindRunID, Canonical: raw, Run: &mlwh.Run{IDRun: 48522}}, nil
			},
			studyFn: func(_ context.Context, _ string) (mlwh.Match, error) {
				return mlwh.Match{Kind: mlwh.KindStudyLimsID, Canonical: "7607", Study: &mlwh.Study{IDStudyLims: "7607"}}, nil
			},
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{Kind: mlwh.KindSangerSampleName, Canonical: raw, Sample: &mlwh.Sample{Name: raw}}, nil
			},
			libraryIdentifierFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "71046409")

				return mlwh.Match{
					Kind:      mlwh.KindLibraryID,
					Canonical: "71046409",
					Library: &mlwh.Library{
						PipelineIDLims: "Custom",
						IDStudyLims:    "7607",
						LibraryID:      "71046409",
					},
				}, nil
			},
			libraryFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				broadLibraryCalls++
				time.Sleep(1100 * time.Millisecond)

				return mlwh.Match{Kind: mlwh.KindLibraryType, Canonical: raw, Library: &mlwh.Library{PipelineIDLims: raw}}, nil
			},
		})

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "library-id-fast-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		start := time.Now()
		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--operator", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--run", "48522",
			"--study", "7607",
			"--sample", "7607STDY14643771",
			"--library", "71046409",
			outputDir,
		}, nil)
		elapsed := time.Since(start)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(elapsed, convey.ShouldBeLessThan, time.Second)
		convey.So(broadLibraryCalls, convey.ShouldEqual, 0)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata["seqmeta_libraryid"], convey.ShouldEqual, "71046409")
		convey.So(registration.Metadata["seqmeta_librarytype"], convey.ShouldEqual, "Custom")
	})

	convey.Convey("E1.1: Given --sample 7607STDY14643771, when ResolveSample returns a canonical match, then register stores seqmeta_sampleid", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{Canonical: raw, Sample: &mlwh.Sample{Name: raw}}, nil
			},
		})

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}
			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "sample-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--sample", "7607STDY14643771",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So((<-registrationCh).Metadata["seqmeta_sampleid"], convey.ShouldEqual, "7607STDY14643771")
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})

	convey.Convey("Given --sample is a canonical Sanger sample name, register uses the exact sample-name resolver before the broad sample cascade", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "7607STDY14643771")

				return mlwh.Match{Canonical: raw, Sample: &mlwh.Sample{Name: raw}}, nil
			},
			sampleFn: func(context.Context, string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.New("broad sample resolver should not be called for canonical sample names")
			},
		})

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}
			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "sample-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--sample", "7607STDY14643771",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So((<-registrationCh).Metadata["seqmeta_sampleid"], convey.ShouldEqual, "7607STDY14643771")
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})

	convey.Convey("E1.2: Given --sample SQSCP, when ResolveSample rejects a LIMS provider constant, then the command fails before registering", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{}, fmt.Errorf("%w: %q looks like a LIMS provider constant", mlwh.ErrUnsupportedIdentifier, raw)
			},
		})

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--sample", "SQSCP",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "--sample")
		convey.So(stderr.String(), convey.ShouldContainSubstring, "SQSCP")
		convey.So(stderr.String(), convey.ShouldContainSubstring, "LIMS provider constant")
		convey.So(requestCount, convey.ShouldEqual, 0)
	})

	convey.Convey("E1.3: Given --sample missing-id, when ResolveSample returns ErrNotFound, then stderr names the flag, value, and not found", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleFn: func(context.Context, string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
		})

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			"--sample", "missing-id",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, `--sample "missing-id"`)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "not found")
		convey.So(requestCount, convey.ShouldEqual, 0)
	})

	convey.Convey("E1.5: Given register help, when printed, then it lists the mlwh input forms", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "register", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "Sanger name")
		convey.So(output, convey.ShouldContainSubstring, "supplier name")
		convey.So(output, convey.ShouldContainSubstring, "id_sample_lims")
		convey.So(output, convey.ShouldContainSubstring, "sample UUID")
		convey.So(output, convey.ShouldContainSubstring, "donor ID")
		convey.So(output, convey.ShouldContainSubstring, "LIMS ID")
		convey.So(output, convey.ShouldContainSubstring, "accession")
		convey.So(output, convey.ShouldContainSubstring, "UUID")
		convey.So(output, convey.ShouldContainSubstring, "name")
		convey.So(output, convey.ShouldContainSubstring, "numeric")
		convey.So(output, convey.ShouldContainSubstring, "exact")
	})

	convey.Convey("E1.6: Given register help, when printed, then --library states that the MLWH cache must already be synced", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "register", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "requires a previously synced MLWH cache")
	})

	convey.Convey("Given register help, when printed, then it explains when a registration replaces an existing result set", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "register", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "same pipeline identity and run key")
		convey.So(output, convey.ShouldContainSubstring, "--nextflow-workflow")
		convey.So(output, convey.ShouldContainSubstring, "--runid and --additional-unique")
		convey.So(output, convey.ShouldContainSubstring, "multiple independently registered outputs")
		convey.So(output, convey.ShouldContainSubstring, "short, stable, human-readable label")
		convey.So(output, convey.ShouldContainSubstring, "timestamp, random value, or output path")
	})

	convey.Convey("G1.4: Given missing --user, then the command returns an error and stderr contains the error", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--runid", "48522",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "user")
	})
}

func registerCommandRegistrationForTest() *results.Registration {
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

func executeRootCommandWithInputForRegisterTest(t *testing.T, args []string, stdin []byte) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command := NewRootCommand()
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetArgs(args)

	reader := io.Reader(bytes.NewReader(stdin))
	if stdin == nil {
		reader = bytes.NewReader(nil)
	}

	command.SetIn(reader)

	err := command.Execute()

	return stdout, stderr, err
}

func writeRegisterCommandTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func seedResultsRegisterRunCacheForTest(t *testing.T, cachePath string, runID int) {
	t.Helper()

	cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
	if err != nil {
		t.Fatalf("open mlwh cache: %v", err)
	}
	defer func() {
		if err := cache.Close(); err != nil {
			t.Fatalf("close mlwh cache: %v", err)
		}
	}()

	_, err = cache.DB().Exec(
		`INSERT INTO iseq_product_metrics_mirror(id_iseq_product, id_iseq_flowcell_tmp, id_run, position, tag_index, id_sample_tmp, id_study_lims, qc, qc_lib, qc_seq, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1001,
		1,
		runID,
		1,
		0,
		1,
		"7607",
		1,
		1,
		1,
		"2026-05-14T12:00:00Z",
	)
	if err != nil {
		t.Fatalf("seed run cache: %v", err)
	}
}

func countRegisterCommandFilesByKind(files []results.FileEntry, kind string) int {
	count := 0

	for _, file := range files {
		if file.Kind == kind {
			count++
		}
	}

	return count
}

func findRegisterCommandFileByKind(files []results.FileEntry, kind string) (results.FileEntry, error) {
	for _, file := range files {
		if file.Kind == kind {
			return file, nil
		}
	}

	return results.FileEntry{}, errors.New("file kind not found")
}

func mustRegisterCommandJSONBody(t *testing.T, value any) []byte {
	t.Helper()

	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal register command JSON: %v", err)
	}

	return body
}

type fakeResultsRegisterResolver struct {
	sampleNameFn        func(context.Context, string) (mlwh.Match, error)
	sampleFn            func(context.Context, string) (mlwh.Match, error)
	studyFn             func(context.Context, string) (mlwh.Match, error)
	runFn               func(context.Context, string) (mlwh.Match, error)
	libraryFn           func(context.Context, string) (mlwh.Match, error)
	libraryIdentifierFn func(context.Context, string) (mlwh.Match, error)
	closeFn             func() error
}

func (f *fakeResultsRegisterResolver) ResolveSampleName(ctx context.Context, raw string) (mlwh.Match, error) {
	if f.sampleNameFn == nil {
		return mlwh.Match{}, mlwh.ErrNotFound
	}

	return f.sampleNameFn(ctx, raw)
}

func (f *fakeResultsRegisterResolver) ResolveSample(ctx context.Context, raw string) (mlwh.Match, error) {
	if f.sampleFn == nil {
		return mlwh.Match{}, errors.New("unexpected ResolveSample call")
	}

	return f.sampleFn(ctx, raw)
}

func (f *fakeResultsRegisterResolver) ResolveStudy(ctx context.Context, raw string) (mlwh.Match, error) {
	if f.studyFn == nil {
		return mlwh.Match{}, errors.New("unexpected ResolveStudy call")
	}

	return f.studyFn(ctx, raw)
}

func (f *fakeResultsRegisterResolver) ResolveRun(ctx context.Context, raw string) (mlwh.Match, error) {
	if f.runFn == nil {
		return mlwh.Match{}, errors.New("unexpected ResolveRun call")
	}

	return f.runFn(ctx, raw)
}

func (f *fakeResultsRegisterResolver) ResolveLibrary(ctx context.Context, raw string) (mlwh.Match, error) {
	if f.libraryFn == nil {
		return mlwh.Match{}, errors.New("unexpected ResolveLibrary call")
	}

	return f.libraryFn(ctx, raw)
}

func (f *fakeResultsRegisterResolver) ResolveLibraryIdentifier(ctx context.Context, raw string) (mlwh.Match, error) {
	if f.libraryIdentifierFn == nil {
		return mlwh.Match{}, mlwh.ErrNotFound
	}

	return f.libraryIdentifierFn(ctx, raw)
}

func (f *fakeResultsRegisterResolver) Close() error {
	if f.closeFn != nil {
		return f.closeFn()
	}

	return nil
}

func stubResultsRegisterResolverOpener(t *testing.T, resolver *fakeResultsRegisterResolver) {
	t.Helper()

	original := resultsRegisterResolverOpener
	resultsRegisterResolverOpener = func(context.Context) (resultsRegisterResolver, error) {
		return resolver, nil
	}

	t.Cleanup(func() {
		resultsRegisterResolverOpener = original
	})
}
