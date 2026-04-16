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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
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
