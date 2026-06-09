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
	osuser "os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/mlwh"
	"github.com/wtsi-hgi/wa/results"
)

var resultsRegisterServerResolverForTest results.RegistrationResolver

func TestResultsRegisterWorkflowFiles(t *testing.T) {
	convey.Convey("register does not stat non-local workflow identities as local pipeline files", t, func() {
		for _, identity := range []results.WorkflowIdentity{
			{
				Identifier: "https://github.com/nf-core/sarek::main.nf",
				Name:       "nf-core/sarek",
				Version:    "abc123",
			},
			{
				Identifier: "manual-qc-v1",
				Name:       "manual-qc-v1",
				Version:    "manual-qc-v1",
			},
		} {
			files, err := resultsRegisterWorkflowFiles(identity)

			convey.So(err, convey.ShouldBeNil)
			convey.So(files, convey.ShouldBeEmpty)
		}
	})
}

func TestResultsRegisterCommand(t *testing.T) {
	installPassthroughResultsAuthClientForTest(t)

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

			if r.URL.Path != gas.EndPointAuth+"/results" {
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
			"--workflow", workflowPath,
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

	convey.Convey("Given --operator is omitted, when register is run, then the registration operator defaults to the current user", t, func() {
		currentUser, err := osuser.Current()
		convey.So(err, convey.ShouldBeNil)

		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "default-operator-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "default-operator",
			"--workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Requester, convey.ShouldEqual, "alice")
		convey.So(registration.Operator, convey.ShouldEqual, currentUser.Username)
	})

	convey.Convey("Given --unique, when register is run, then the registration uses the stable unique key", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

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

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "result-unique"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--operator", "bob",
			"--unique", "48522-random-exon",
			"--workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.RunKey, convey.ShouldEqual, "runid=48522-random-exon")
	})

	convey.Convey("Given a generic --workflow string, when register is run, then it is used as the deterministic workflow identity without tracking a pipeline file", t, func() {
		outputDir := t.TempDir()
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")

		var registration results.Registration
		var handlerErr error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErr = err

				return
			}

			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "generic-workflow"}); err != nil {
				handlerErr = err

				return
			}
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "generic-001",
			"--workflow", "manual-qc-v1",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(handlerErr, convey.ShouldBeNil)
		convey.So(registration.PipelineIdentifier, convey.ShouldEqual, "manual-qc-v1")
		convey.So(registration.PipelineName, convey.ShouldEqual, "manual-qc-v1")
		convey.So(registration.PipelineVersion, convey.ShouldEqual, "manual-qc-v1")
		convey.So(registration.RunKey, convey.ShouldEqual, "runid=generic-001")
		convey.So(countRegisterCommandFilesByKind(registration.Files, "pipeline"), convey.ShouldEqual, 0)

		resultID := results.CompositeKeyID(registration.PipelineIdentifier, registration.RunKey)
		convey.So(resultID, convey.ShouldEqual, results.CompositeKeyID("manual-qc-v1", "runid=generic-001"))
		convey.So(resultID, convey.ShouldNotEqual, results.CompositeKeyID("manual-qc-v2", "runid=generic-001"))
	})

	convey.Convey("Given deprecated --nextflow-workflow, when register is run, then it remains a hidden alias for --workflow", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		var registration results.Registration
		var handlerErr error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErr = err

				return
			}

			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "deprecated-workflow"}); err != nil {
				handlerErr = err

				return
			}
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "deprecated-001",
			"--nextflow-workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(handlerErr, convey.ShouldBeNil)
		convey.So(registration.PipelineIdentifier, convey.ShouldContainSubstring, filepath.Base(workflowPath))
	})

	convey.Convey("Given repeated --match globs, register only sends output files matching any glob", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "reports", "summary.html"), "html")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "reports", "summary.txt"), "text")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "metrics", "qc.json"), "{}")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "metrics", "qc.tsv"), "col\n")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		registrationCh := make(chan results.Registration, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "matched-result"}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "matched",
			"--workflow", workflowPath,
			"--match", "reports/*.html",
			"--match", "metrics/*.json",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registerCommandFileRelPathsByKind(t, outputDir, registration.Files, "output"), convey.ShouldResemble, map[string]bool{
			filepath.Join("metrics", "qc.json"):      true,
			filepath.Join("reports", "summary.html"): true,
		})
		convey.So(countRegisterCommandFilesByKind(registration.Files, "pipeline"), convey.ShouldEqual, 1)
	})

	convey.Convey("Given --match filters out every scanned output, register fails locally without sending a request", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "reports", "summary.html"), "html")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "metrics", "qc.json"), "{}")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "no-matches",
			"--workflow", workflowPath,
			"--match", "vcfs/*.vcf.gz",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "no output files")
		convey.So(requestCount, convey.ShouldEqual, 0)
	})

	convey.Convey("Given --match filters out every scanned output, register fails before MLWH or workflow identity resolution", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "missing.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "reports", "summary.html"), "html")

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "filtered-before-resolvers",
			"--workflow", workflowPath,
			"--match", "vcfs/*.vcf.gz",
			"--sample", "sample-1",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "no output files")
		convey.So(requestCount, convey.ShouldEqual, 0)
	})

	convey.Convey("Bug 260609-2: Given remote register with --sample and no local MLWH cache, the CLI sends raw lookup values without opening a local resolver", t, func() {
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		type lookupPayload struct {
			Sample []string `json:"sample"`
		}
		type registrationPayload struct {
			Metadata     map[string]string `json:"metadata"`
			LookupValues lookupPayload     `json:"lookup_values"`
		}

		registrationCh := make(chan registrationPayload, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration registrationPayload
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "server-resolved-result"}); err != nil {
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
			"--unique", "remote-sample",
			"--workflow", workflowPath,
			"--sample", "7607STDY14643771",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.LookupValues.Sample, convey.ShouldResemble, []string{"7607STDY14643771"})
		convey.So(registration.Metadata[results.SeqmetaSampleNameKey], convey.ShouldBeBlank)
	})

	convey.Convey("Given an empty output directory, register fails locally without sending a request", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "empty-dir",
			"--workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "no output files")
		convey.So(requestCount, convey.ShouldEqual, 0)
	})

	convey.Convey("Given an empty output directory, register fails before metadata parsing or workflow identity resolution", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "missing.nf")

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", server.URL,
			"register",
			"--user", "alice",
			"--unique", "empty-before-parsing",
			"--workflow", workflowPath,
			"--meta", "not-key-value",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "no output files")
		convey.So(requestCount, convey.ShouldEqual, 0)
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

	convey.Convey("C2.4: Given LDAP user bob and --json registration with operator carol, when CLI register posts it, then backend response contains operator bob", t, func() {
		payload := registerCommandRegistrationForTest()
		payload.Operator = "carol"

		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != gas.EndPointAuth+"/results" {
				handlerErrCh <- fmt.Errorf("unexpected request path: %s", r.URL.Path)
				http.NotFound(w, r)

				return
			}

			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			if registration.Requester != "alice" || registration.Operator != "carol" {
				handlerErrCh <- fmt.Errorf("unexpected registration actors: requester=%s operator=%s", registration.Requester, registration.Operator)

				return
			}

			w.WriteHeader(http.StatusCreated)
			if err := json.NewEncoder(w).Encode(results.ResultSet{
				ID:        "ldap-result",
				Requester: "alice",
				Operator:  "bob",
			}); err != nil {
				handlerErrCh <- err

				return
			}

			handlerErrCh <- nil
		}))
		defer server.Close()

		stdout, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--json",
		}, mustRegisterCommandJSONBody(t, payload))

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(<-handlerErrCh, convey.ShouldBeNil)

		var result results.ResultSet
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.Operator, convey.ShouldEqual, "bob")
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
			"--workflow", workflowPath,
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
			"--workflow", workflowPath,
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
			"--workflow", workflowPath,
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
			"--workflow", workflowPath,
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
			"--workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(<-handlerErrCh, convey.ShouldBeNil)

		var result results.ResultSet
		convey.So(json.Unmarshal(stdout.Bytes(), &result), convey.ShouldBeNil)
		convey.So(result.ID, convey.ShouldEqual, "updated-result")
	})

	convey.Convey("Given --run and no local MLWH cache, register sends raw run lookup values to the server", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")
		t.Setenv("WA_MLWH_CACHE_PATH", "")
		t.Setenv("WA_MLWH_DSN", "")

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
			"--workflow", workflowPath,
			"--run", "48522",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.LookupValues.Run, convey.ShouldResemble, []string{"48522"})
		convey.So(registration.Metadata[results.SeqmetaIDRunKey], convey.ShouldBeBlank)
	})

	convey.Convey("E1.4: Given --study/--run/--library/--sample together, when all mlwh resolvers succeed, then register stores MLWH-named seqmeta metadata entries", t, func() {
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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
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
			"--workflow", workflowPath,
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
			"seqmeta_id_run":           "12345",
			"seqmeta_id_study_lims":    "6568",
			"seqmeta_name":             "7607STDY14643771",
			"seqmeta_pipeline_id_lims": "Standard",
		})
	})

	convey.Convey("Bug 2: Given --library is a library_id, when mlwh resolves it, then register stores the library ID metadata key and canonical library type context", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			libraryFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw != "71046409" {
					return mlwh.Match{}, fmt.Errorf("unexpected library lookup %q", raw)
				}

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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
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
			"--workflow", workflowPath,
			"--library", "71046409",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata, convey.ShouldResemble, map[string]string{
			"seqmeta_library_id":       "71046409",
			"seqmeta_pipeline_id_lims": "Custom",
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
				if raw != "71046409" {
					return mlwh.Match{}, fmt.Errorf("unexpected library identifier lookup %q", raw)
				}

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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
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
			"--workflow", workflowPath,
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
		convey.So(registration.Metadata["seqmeta_library_id"], convey.ShouldEqual, "71046409")
		convey.So(registration.Metadata["seqmeta_pipeline_id_lims"], convey.ShouldEqual, "Custom")
	})

	convey.Convey("E1.1: Given --sample 7607STDY14643771, when ResolveSample returns a canonical match, then register stores seqmeta_name", t, func() {
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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
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
			"--workflow", workflowPath,
			"--sample", "7607STDY14643771",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So((<-registrationCh).Metadata["seqmeta_name"], convey.ShouldEqual, "7607STDY14643771")
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})

	convey.Convey("Given repeated --sample flags, register sends every resolved sample metadata value", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		sampleCalls := []string{}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				sampleCalls = append(sampleCalls, raw)

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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "multi-sample-result"}); err != nil {
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
			"--workflow", workflowPath,
			"--sample", "7607STDY14643771",
			"--sample", "7607STDY14643772",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(sampleCalls, convey.ShouldResemble, []string{"7607STDY14643771", "7607STDY14643772"})

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata[results.SeqmetaSampleNameKey], convey.ShouldEqual, "7607STDY14643771")
		convey.So(registration.MetadataValues[results.SeqmetaSampleNameKey], convey.ShouldResemble, []string{"7607STDY14643771", "7607STDY14643772"})
	})

	convey.Convey("Bug item 4: Given repeated --sample supplier names resolve to the same canonical sample, register preserves raw sample values and canonical seqmeta metadata", t, func() {
		sampleCalls := []string{}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				sampleCalls = append(sampleCalls, raw)

				return mlwh.Match{
					Kind:      mlwh.KindSupplierName,
					Canonical: "7607STDY14643771",
					Sample: &mlwh.Sample{
						Name:         "7607STDY14643771",
						SupplierName: raw,
					},
				}, nil
			},
		})

		registration, stderr, err := runResultsRegisterAndCaptureRegistrationForTest(t,
			"--sample", "Hek_R1",
			"--sample", "Hek_R2",
			"--meta", "foo=bar",
			"--meta", "foo=baz",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(sampleCalls, convey.ShouldResemble, []string{"Hek_R1", "Hek_R2"})
		convey.So(registration.Metadata["sample"], convey.ShouldEqual, "Hek_R1")
		convey.So(registration.MetadataValues["sample"], convey.ShouldResemble, []string{"Hek_R1", "Hek_R2"})
		convey.So(registration.Metadata[results.SeqmetaSupplierNameKey], convey.ShouldEqual, "Hek_R1")
		convey.So(registration.MetadataValues[results.SeqmetaSupplierNameKey], convey.ShouldResemble, []string{"Hek_R1", "Hek_R2"})
		convey.So(registration.Metadata[results.SeqmetaSampleNameKey], convey.ShouldEqual, "7607STDY14643771")
		convey.So(registration.MetadataValues[results.SeqmetaSampleNameKey], convey.ShouldResemble, []string{"7607STDY14643771"})
		convey.So(registration.Metadata["foo"], convey.ShouldEqual, "bar")
		convey.So(registration.MetadataValues["foo"], convey.ShouldResemble, []string{"bar", "baz"})
	})

	convey.Convey("Given alternate --sample source kinds, register preserves the MLWH column-specific metadata keys", t, func() {
		sampleMatches := map[string]mlwh.Match{
			"SANGER_SOURCE_3": {
				Kind:      mlwh.KindSangerSampleID,
				Canonical: "CANONICAL_SAMPLE_3",
				Sample: &mlwh.Sample{
					Name:           "CANONICAL_SAMPLE_3",
					SangerSampleID: "SANGER_SOURCE_3",
				},
			},
			"6050954": {
				Kind:      mlwh.KindSampleLimsID,
				Canonical: "7607STDY14643771",
				Sample: &mlwh.Sample{
					Name:         "7607STDY14643771",
					IDSampleLims: "6050954",
				},
			},
			"SAMEA76070": {
				Kind:      mlwh.KindSampleAccession,
				Canonical: "7607STDY14643771",
				Sample: &mlwh.Sample{
					Name:            "7607STDY14643771",
					AccessionNumber: "SAMEA76070",
				},
			},
			"22222222-2222-3333-4444-555555557601": {
				Kind:      mlwh.KindSampleUUID,
				Canonical: "7607STDY14643771",
				Sample: &mlwh.Sample{
					Name:           "7607STDY14643771",
					UUIDSampleLims: "22222222-2222-3333-4444-555555557601",
				},
			},
			"DONOR_HEK1": {
				Kind:      mlwh.KindDonorID,
				Canonical: "7607STDY14643771",
				Sample: &mlwh.Sample{
					Name:    "7607STDY14643771",
					DonorID: "DONOR_HEK1",
				},
			},
		}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(context.Context, string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return sampleMatches[raw], nil
			},
		})

		registration, stderr, err := runResultsRegisterAndCaptureRegistrationForTest(t,
			"--sample", "SANGER_SOURCE_3",
			"--sample", "6050954",
			"--sample", "SAMEA76070",
			"--sample", "22222222-2222-3333-4444-555555557601",
			"--sample", "DONOR_HEK1",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(registration.MetadataValues["sample"], convey.ShouldResemble, []string{
			"SANGER_SOURCE_3",
			"6050954",
			"SAMEA76070",
			"22222222-2222-3333-4444-555555557601",
			"DONOR_HEK1",
		})
		convey.So(registration.MetadataValues[results.SeqmetaSampleNameKey], convey.ShouldResemble, []string{
			"CANONICAL_SAMPLE_3",
			"7607STDY14643771",
		})
		convey.So(registration.MetadataValues[results.SeqmetaSangerSampleIDKey], convey.ShouldResemble, []string{"SANGER_SOURCE_3"})
		convey.So(registration.MetadataValues[results.SeqmetaIDSampleLimsKey], convey.ShouldResemble, []string{"6050954"})
		convey.So(registration.MetadataValues[results.SeqmetaAccessionNumberKey], convey.ShouldResemble, []string{"SAMEA76070"})
		convey.So(registration.MetadataValues["seqmeta_uuid_sample_lims"], convey.ShouldResemble, []string{"22222222-2222-3333-4444-555555557601"})
		convey.So(registration.MetadataValues["seqmeta_donor_id"], convey.ShouldResemble, []string{"DONOR_HEK1"})
	})

	convey.Convey("Given repeated --run flags, register sends every resolved run metadata value", t, func() {
		runCalls := []string{}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			runFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				runCalls = append(runCalls, raw)

				return mlwh.Match{Canonical: raw, Run: &mlwh.Run{IDRun: 48522}}, nil
			},
		})

		registration, stderr, err := runResultsRegisterAndCaptureRegistrationForTest(t,
			"--run", "48522",
			"--run", "48523",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(runCalls, convey.ShouldResemble, []string{"48522", "48523"})
		convey.So(registration.Metadata[results.SeqmetaIDRunKey], convey.ShouldEqual, "48522")
		convey.So(registration.MetadataValues[results.SeqmetaIDRunKey], convey.ShouldResemble, []string{"48522", "48523"})
	})

	convey.Convey("Given repeated --study flags, register sends every resolved study metadata value", t, func() {
		studyCalls := []string{}
		studyIDs := map[string]string{
			"EGAS00001005445": "6568",
			"EGAS00001005446": "6569",
		}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			studyFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				studyCalls = append(studyCalls, raw)

				return mlwh.Match{Canonical: studyIDs[raw], Study: &mlwh.Study{IDStudyLims: studyIDs[raw]}}, nil
			},
		})

		registration, stderr, err := runResultsRegisterAndCaptureRegistrationForTest(t,
			"--study", "EGAS00001005445",
			"--study", "EGAS00001005446",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(studyCalls, convey.ShouldResemble, []string{"EGAS00001005445", "EGAS00001005446"})
		convey.So(registration.Metadata[results.SeqmetaIDStudyLimsKey], convey.ShouldEqual, "6568")
		convey.So(registration.MetadataValues[results.SeqmetaIDStudyLimsKey], convey.ShouldResemble, []string{"6568", "6569"})
	})

	convey.Convey("Given alternate --study source kinds, register preserves the MLWH column-specific metadata keys", t, func() {
		studyMatches := map[string]mlwh.Match{
			"ERP7607": {
				Kind:      mlwh.KindStudyAccession,
				Canonical: "7607",
				Study: &mlwh.Study{
					IDStudyLims:     "7607",
					AccessionNumber: "ERP7607",
				},
			},
			"11111111-2222-3333-4444-555555557607": {
				Kind:      mlwh.KindStudyUUID,
				Canonical: "7608",
				Study: &mlwh.Study{
					IDStudyLims:   "7608",
					UUIDStudyLims: "11111111-2222-3333-4444-555555557607",
				},
			},
			"Study 7609 Name": {
				Kind:      mlwh.KindStudyName,
				Canonical: "7609",
				Study: &mlwh.Study{
					IDStudyLims: "7609",
					Name:        "Study 7609 Name",
				},
			},
		}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			studyFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				return studyMatches[raw], nil
			},
		})

		registration, stderr, err := runResultsRegisterAndCaptureRegistrationForTest(t,
			"--study", "ERP7607",
			"--study", "11111111-2222-3333-4444-555555557607",
			"--study", "Study 7609 Name",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(registration.MetadataValues["study"], convey.ShouldResemble, []string{
			"ERP7607",
			"11111111-2222-3333-4444-555555557607",
			"Study 7609 Name",
		})
		convey.So(registration.MetadataValues[results.SeqmetaIDStudyLimsKey], convey.ShouldResemble, []string{"7607", "7608", "7609"})
		convey.So(registration.MetadataValues["seqmeta_study_accession"], convey.ShouldResemble, []string{"ERP7607"})
		convey.So(registration.MetadataValues["seqmeta_uuid_study_lims"], convey.ShouldResemble, []string{"11111111-2222-3333-4444-555555557607"})
		convey.So(registration.MetadataValues["seqmeta_study_name"], convey.ShouldResemble, []string{"Study 7609 Name"})
	})

	convey.Convey("Given repeated --library flags, register sends every resolved library metadata value across all library keys", t, func() {
		libraryCalls := []string{}
		libraryMatches := map[string]mlwh.Match{
			"71046409": {
				Kind:      mlwh.KindLibraryID,
				Canonical: "71046409",
				Library: &mlwh.Library{
					PipelineIDLims: "PCR-free",
					LibraryID:      "71046409",
				},
			},
			"71046410": {
				Kind:      mlwh.KindLibraryID,
				Canonical: "71046410",
				Library: &mlwh.Library{
					PipelineIDLims: "Custom",
					LibraryID:      "71046410",
				},
			},
			"SQPP-47463-G:B1": {
				Kind:      mlwh.KindLibraryLimsID,
				Canonical: "SQPP-47463-G:B1",
				Library: &mlwh.Library{
					PipelineIDLims: "Standard",
					IDLibraryLims:  "SQPP-47463-G:B1",
				},
			},
		}
		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			libraryFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				libraryCalls = append(libraryCalls, raw)

				return libraryMatches[raw], nil
			},
		})

		registration, stderr, err := runResultsRegisterAndCaptureRegistrationForTest(t,
			"--library", "71046409",
			"--library", "71046410",
			"--library", "SQPP-47463-G:B1",
		)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(libraryCalls, convey.ShouldResemble, []string{"71046409", "71046410", "SQPP-47463-G:B1"})
		convey.So(registration.Metadata[results.SeqmetaPipelineIDLimsKey], convey.ShouldEqual, "PCR-free")
		convey.So(registration.Metadata[results.SeqmetaLibraryIDKey], convey.ShouldEqual, "71046409")
		convey.So(registration.Metadata[results.SeqmetaIDLibraryLimsKey], convey.ShouldEqual, "SQPP-47463-G:B1")
		convey.So(registration.MetadataValues[results.SeqmetaPipelineIDLimsKey], convey.ShouldResemble, []string{"PCR-free", "Custom", "Standard"})
		convey.So(registration.MetadataValues[results.SeqmetaLibraryIDKey], convey.ShouldResemble, []string{"71046409", "71046410"})
		convey.So(registration.MetadataValues[results.SeqmetaIDLibraryLimsKey], convey.ShouldResemble, []string{"SQPP-47463-G:B1"})
	})

	convey.Convey("Given repeated --meta keys, register sends every metadata value while preserving the single-value metadata object", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		type registrationPayload struct {
			Metadata       map[string]string   `json:"metadata"`
			MetadataValues map[string][]string `json:"metadata_values"`
		}

		registrationCh := make(chan registrationPayload, 1)
		handlerErrCh := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var registration registrationPayload
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				handlerErrCh <- err

				return
			}

			registrationCh <- registration
			w.WriteHeader(http.StatusCreated)

			if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "multi-meta-result"}); err != nil {
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
			"--workflow", workflowPath,
			"--meta", "assay=RNA",
			"--meta", "assay=WGS",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)

		registration := <-registrationCh
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
		convey.So(registration.Metadata["assay"], convey.ShouldEqual, "RNA")
		convey.So(registration.MetadataValues["assay"], convey.ShouldResemble, []string{"RNA", "WGS"})
	})

	convey.Convey("Given --sample is a canonical Sanger sample name, register uses the exact sample-name resolver before the broad sample cascade", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw != "7607STDY14643771" {
					return mlwh.Match{}, fmt.Errorf("unexpected sample name lookup %q", raw)
				}

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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
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
			"--workflow", workflowPath,
			"--sample", "7607STDY14643771",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So((<-registrationCh).Metadata["seqmeta_name"], convey.ShouldEqual, "7607STDY14643771")
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})

	convey.Convey("Given --sample is a fixture-shaped alternate identifier, register falls back to the broad sample resolver", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.foo"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw != "gallery-beta" {
					return mlwh.Match{}, fmt.Errorf("unexpected sample name lookup %q", raw)
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw != "gallery-beta" {
					return mlwh.Match{}, fmt.Errorf("unexpected sample lookup %q", raw)
				}

				return mlwh.Match{Canonical: "7607STDY14643771", Sample: &mlwh.Sample{Name: "7607STDY14643771"}}, nil
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
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
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
			"--workflow", "one-off",
			"--unique", "test",
			"--match", "*.foo",
			"--sample", "gallery-beta",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So((<-registrationCh).Metadata["seqmeta_name"], convey.ShouldEqual, "7607STDY14643771")
		convey.So(<-handlerErrCh, convey.ShouldBeNil)
	})

	convey.Convey("Given --sample is an invalid fixture-like local slug, register fails before creating a result", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.foo"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		stubResultsRegisterResolverOpener(t, &fakeResultsRegisterResolver{
			sampleNameFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw != "gallery-beta" {
					return mlwh.Match{}, fmt.Errorf("unexpected sample name lookup %q", raw)
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
			sampleFn: func(_ context.Context, raw string) (mlwh.Match, error) {
				if raw != "gallery-beta" {
					return mlwh.Match{}, fmt.Errorf("unexpected sample lookup %q", raw)
				}

				return mlwh.Match{}, mlwh.ErrNotFound
			},
		})

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				writeResultsRegisterServerErrorForTest(t, w, err)

				return
			}
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
				writeResultsRegisterServerErrorForTest(t, w, err)

				return
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		start := time.Now()
		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--workflow", "one-off",
			"--unique", "test",
			"--match", "*.foo",
			"--sample", "gallery-beta",
			"--sample", "gallery-alpha",
			outputDir,
		}, nil)
		elapsed := time.Since(start)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, `--sample "gallery-beta"`)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "not found")
		convey.So(elapsed, convey.ShouldBeLessThan, time.Second)
		convey.So(requestCount, convey.ShouldEqual, 1)
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
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				writeResultsRegisterServerErrorForTest(t, w, err)

				return
			}
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
				writeResultsRegisterServerErrorForTest(t, w, err)

				return
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--workflow", workflowPath,
			"--sample", "SQSCP",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "--sample")
		convey.So(stderr.String(), convey.ShouldContainSubstring, "SQSCP")
		convey.So(stderr.String(), convey.ShouldContainSubstring, "LIMS provider constant")
		convey.So(requestCount, convey.ShouldEqual, 1)
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
			var registration results.Registration
			if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
				writeResultsRegisterServerErrorForTest(t, w, err)

				return
			}
			if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
				writeResultsRegisterServerErrorForTest(t, w, err)

				return
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--runid", "48522",
			"--workflow", workflowPath,
			"--sample", "missing-id",
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, `--sample "missing-id"`)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "not found")
		convey.So(requestCount, convey.ShouldEqual, 1)
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

	convey.Convey("E1.6: Given register help, when printed, then it says MLWH lookup happens on the results server", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "register", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "sent to the results server")
		convey.So(output, convey.ShouldContainSubstring, "Normal CLI users do not need WA_MLWH_CACHE_PATH")
		convey.So(output, convey.ShouldNotContainSubstring, "requires a previously synced MLWH cache")
	})

	convey.Convey("Given register help, when printed, then it explains when a registration replaces an existing result set", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "register", "--help"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(output, convey.ShouldContainSubstring, "same workflow identity and unique key")
		convey.So(output, convey.ShouldContainSubstring, "--workflow")
		convey.So(output, convey.ShouldNotContainSubstring, "--nextflow-workflow")
		convey.So(output, convey.ShouldContainSubstring, "--unique")
		convey.So(output, convey.ShouldContainSubstring, "single stable, human-readable label")
		convey.So(output, convey.ShouldContainSubstring, "timestamp, random value, or output path")
		convey.So(output, convey.ShouldNotContainSubstring, "--runid")
		convey.So(output, convey.ShouldNotContainSubstring, "--additional-unique")
	})

	convey.Convey("Given register help, when printed, then it presents required identity, file, metadata, and server options in priority order", t, func() {
		output, err := executeRootCommandForTest(t, []string{"results", "register", "--help"})

		convey.So(err, convey.ShouldBeNil)
		assertHelpMarkersInOrder(output,
			"Identity:",
			"output-dir (required)",
			"--user (required)",
			"--operator (optional)",
			"--command (optional)",
			"--workflow (required)",
			"--unique (required)",
			"Files:",
			"--input-file",
			"--match",
			"--include-hidden",
			"Metadata:",
			"--run",
			"--study",
			"--sample",
			"--library",
			"--meta",
			"Server:",
			"--server",
			"--cert",
		)

		_, flagsOutput, foundFlags := strings.Cut(output, "\nFlags:")
		convey.So(foundFlags, convey.ShouldBeTrue)
		localFlags, globalFlags, foundGlobalFlags := strings.Cut(flagsOutput, "\nGlobal Flags:")
		convey.So(foundGlobalFlags, convey.ShouldBeTrue)
		assertHelpMarkersInOrder(localFlags,
			"--user string",
			"--operator string",
			"--command string",
			"--workflow string",
			"--unique string",
			"--input-file stringArray",
			"--match stringArray",
			"--include-hidden",
			"--run stringArray",
			"--study stringArray",
			"--sample stringArray",
			"--library stringArray",
			"--meta stringArray",
		)
		convey.So(globalFlags, convey.ShouldContainSubstring, "--server string")
		convey.So(globalFlags, convey.ShouldContainSubstring, "--cert string")
		convey.So(output, convey.ShouldNotContainSubstring, "--nextflow-workflow")
	})

	convey.Convey("G1.4: Given missing --user, then the command returns an error and stderr contains the error", t, func() {
		outputDir := t.TempDir()
		workflowPath := filepath.Join(t.TempDir(), "main.nf")
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
		writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--unique", "48522",
			"--workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "user")
	})
}

func applyResultsRegisterLookupsForTest(t *testing.T, r *http.Request, registration *results.Registration) error {
	t.Helper()

	return results.ApplyRegistrationLookups(r.Context(), registration, resultsRegisterServerResolverForTest)
}

func registerCommandFileRelPathsByKind(t *testing.T, root string, files []results.FileEntry, kind string) map[string]bool {
	t.Helper()

	paths := make(map[string]bool)
	for _, file := range files {
		if file.Kind != kind {
			continue
		}

		relPath, err := filepath.Rel(root, file.Path)
		if err != nil {
			t.Fatalf("relative path for %s: %v", file.Path, err)
		}

		paths[relPath] = true
	}

	return paths
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

func writeResultsRegisterServerErrorForTest(t *testing.T, w http.ResponseWriter, err error) {
	t.Helper()

	status := http.StatusBadRequest
	if errors.Is(err, results.ErrSeqmetaFailed) {
		status = http.StatusBadGateway
	}

	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(map[string]string{"error": err.Error()}); encodeErr != nil {
		t.Fatalf("encode register error response: %v", encodeErr)
	}
}

func assertHelpMarkersInOrder(output string, markers ...string) {
	previousIndex := -1

	for _, marker := range markers {
		index := strings.Index(output, marker)

		convey.So(index, convey.ShouldBeGreaterThan, -1)
		convey.So(index, convey.ShouldBeGreaterThan, previousIndex)

		previousIndex = index
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

	original := resultsRegisterServerResolverForTest
	resultsRegisterServerResolverForTest = resolver

	t.Cleanup(func() {
		resultsRegisterServerResolverForTest = original
	})
}

func runResultsRegisterAndCaptureRegistrationForTest(t *testing.T, extraArgs ...string) (results.Registration, *bytes.Buffer, error) {
	t.Helper()

	outputDir := t.TempDir()
	workflowPath := filepath.Join(t.TempDir(), "main.nf")
	writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
	writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

	registrationCh := make(chan results.Registration, 1)
	handlerErrCh := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var registration results.Registration
		if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
			handlerErrCh <- err

			return
		}
		if err := applyResultsRegisterLookupsForTest(t, r, &registration); err != nil {
			writeResultsRegisterServerErrorForTest(t, w, err)
			handlerErrCh <- nil

			return
		}

		registrationCh <- registration
		w.WriteHeader(http.StatusCreated)

		if err := json.NewEncoder(w).Encode(results.ResultSet{ID: "captured-result"}); err != nil {
			handlerErrCh <- err

			return
		}

		handlerErrCh <- nil
	}))
	defer server.Close()

	args := []string{
		"results", "register",
		"--server", server.URL,
		"--user", "alice",
		"--runid", "48522",
		"--workflow", workflowPath,
	}
	args = append(args, extraArgs...)
	args = append(args, outputDir)

	_, stderr, err := executeRootCommandWithInputForRegisterTest(t, args, nil)
	if err != nil {
		return results.Registration{}, stderr, err
	}

	var registration results.Registration
	select {
	case registration = <-registrationCh:
	case <-time.After(time.Second):
		t.Fatal("results register test server did not receive a registration")
	}

	select {
	case handlerErr := <-handlerErrCh:
		if handlerErr != nil {
			t.Fatalf("handle results register request: %v", handlerErr)
		}
	case <-time.After(time.Second):
		t.Fatal("results register test server did not finish handling the registration")
	}

	return registration, stderr, nil
}

func TestResultsRegisterInvalidSampleWithSparseMLWHCache(t *testing.T) {
	installPassthroughResultsAuthClientForTest(t)

	convey.Convey("Given an invalid fixture-like --sample and a legacy sparse MLWH sample cache, register returns a fast not-found error", t, func() {
		cachePath := filepath.Join(t.TempDir(), "mlwh.sqlite")
		cache, err := mlwh.OpenCache(context.Background(), mlwh.CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)

		for _, indexName := range []string{
			"sample_mirror_id_sample_lims_idx",
			"sample_mirror_uuid_sample_lims_idx",
			"sample_mirror_sanger_sample_id_idx",
			"sample_mirror_supplier_name_idx",
			"sample_mirror_accession_number_idx",
			"sample_mirror_donor_id_idx",
			"sample_mirror_last_updated_idx",
		} {
			_, err = cache.DB().Exec(`DROP INDEX IF EXISTS ` + indexName)
			convey.So(err, convey.ShouldBeNil)
		}
		_, err = cache.DB().Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, ?) ON CONFLICT(table_name) DO UPDATE SET high_water = excluded.high_water, last_run = excluded.last_run, resume_cursor = excluded.resume_cursor, indexes_dropped = excluded.indexes_dropped`,
			"sample",
			"2026-05-13T11:24:59Z",
			"2026-05-13T11:25:00Z",
			nil,
			1,
		)
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)
		t.Setenv("WA_MLWH_CACHE_PATH", "")

		cacheClient, err := mlwh.OpenCacheOnly(context.Background(), mlwh.CacheConfig{Path: cachePath})
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			convey.So(cacheClient.Close(), convey.ShouldBeNil)
		}()
		resolver := results.NewMLWHSearchResolver(cacheClient)

		outputDir := t.TempDir()
		writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.foo"), "result")

		requestCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestCount++
			var registration results.Registration
			if decodeErr := json.NewDecoder(r.Body).Decode(&registration); decodeErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": decodeErr.Error()})

				return
			}
			if lookupErr := results.ApplyRegistrationLookups(r.Context(), &registration, resolver); lookupErr != nil {
				writeResultsRegisterServerErrorForTest(t, w, lookupErr)

				return
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "unexpected"})
		}))
		defer server.Close()

		start := time.Now()
		_, stderr, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results", "register",
			"--server", server.URL,
			"--user", "alice",
			"--workflow", "one-off",
			"--unique", "test",
			"--match", "*.foo",
			"--sample", "gallery-beta",
			"--sample", "gallery-alpha",
			outputDir,
		}, nil)
		elapsed := time.Since(start)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stderr.String(), convey.ShouldContainSubstring, `--sample "gallery-beta"`)
		convey.So(stderr.String(), convey.ShouldContainSubstring, "not found")
		convey.So(elapsed, convey.ShouldBeLessThan, time.Second)
		convey.So(requestCount, convey.ShouldEqual, 1)
	})
}
