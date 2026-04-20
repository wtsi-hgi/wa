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
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/results"
)

type runDevEnvSnapshot struct {
	ResultsBackendURL string `json:"WA_RESULTS_BACKEND_URL"`
	SeqmetaBackendURL string `json:"WA_SEQMETA_BACKEND_URL"`
	ResultsDBPath     string `json:"WA_RESULTS_DB_PATH"`
}

func TestRunDevScript(t *testing.T) {
	convey.Convey("R1.1/R1.2/R1.3/R1.5: run-dev.sh builds wa, seeds three fixtures, skips seqmeta without a token, and cleans up on SIGINT", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `printf '%s\n' 'app/results/page.tsx' 'package.json'`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'lint:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'format:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'test:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)
		steps := waitForRunDevStepsForTest(t, snapshotPath+".steps", 3)
		resultsList := waitForSeededResultsForTest(t, resultsPort)

		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, ".tmp", "wa")), convey.ShouldBeTrue)
		convey.So(resultsList, convey.ShouldHaveLength, 3)
		convey.So(steps, convey.ShouldResemble, []string{
			`lint:["app/results/page.tsx"]`,
			`format:["app/results/page.tsx","package.json"]`,
			`test:[]`,
		})
		convey.So(snapshot.ResultsBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", resultsPort))
		convey.So(strings.TrimSpace(snapshot.SeqmetaBackendURL), convey.ShouldEqual, "")
		convey.So(snapshot.ResultsDBPath, convey.ShouldNotBeBlank)
		convey.So(runDevPathExistsForTest(snapshot.ResultsDBPath), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, "logs", "results.log")), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, "logs", "frontend.log")), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, "logs", "seqmeta.log")), convey.ShouldBeTrue)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
		convey.So(runDevPathExistsForTest(snapshot.ResultsDBPath), convey.ShouldBeFalse)
	})

	convey.Convey("R1.4: run-dev.sh starts seqmeta and exports WA_SEQMETA_BACKEND_URL when SAGA_API_TOKEN is set", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			seqmetaPort:  seqmetaPort,
			env: map[string]string{
				"SAGA_API_TOKEN":                        "test-token",
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)
		waitForTCPPortForTest(t, seqmetaPort)

		convey.So(snapshot.SeqmetaBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", seqmetaPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh waits for seqmeta readiness without printing transient curl probe errors", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		healthPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		startDelayedHTTPServerForTest(t, healthPort, 1500*time.Millisecond)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			seqmetaPort:  seqmetaPort,
			env: map[string]string{
				"SAGA_API_TOKEN":                        "test-token",
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/studies", healthPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "curl:")
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "Failed to connect")
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "Operation timed out")
	})

	convey.Convey("R1.7: run-dev.sh skips frontend lint and format checks when no frontend files have changed", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'lint\n')"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'format\n')"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'test:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)
		steps := waitForRunDevStepsForTest(t, snapshotPath+".steps", 1)

		convey.So(steps, convey.ShouldResemble, []string{
			`test:[]`,
		})

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("R1.8: run-dev.sh ignores generated frontend artifacts when selecting changed files for lint and format", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `printf '%s\n' 'node_modules/react/index.js' '.next/server/app.js' 'test-results/results.spec.ts' 'tsconfig.tsbuildinfo' 'app/results/page.tsx' 'package.json'`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'lint:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'format:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'test:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)
		steps := waitForRunDevStepsForTest(t, snapshotPath+".steps", 3)

		convey.So(steps, convey.ShouldResemble, []string{
			`lint:["app/results/page.tsx"]`,
			`format:["app/results/page.tsx","package.json"]`,
			`test:[]`,
		})

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh builds wa from the repository root even when started from frontend/", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			startDir:     filepath.Join(repoRoot, "frontend"),
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)

		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, ".tmp", "wa")), convey.ShouldBeTrue)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh passes the frontend port to the default pnpm dev command without an extra separator", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		binDir := t.TempDir()
		pnpmLogPath := filepath.Join(t.TempDir(), "pnpm-dev-args.txt")
		pnpmPath := filepath.Join(binDir, "pnpm")

		stub := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

printf '%%s\n' "$*" >> %q

if [[ "$1" == "dev" ]]; then
	if [[ "$2" != "--port" || "$3" != %q ]]; then
		printf 'unexpected pnpm dev args: %%s\n' "$*" >&2
		exit 1
	fi
	exec node %q "$3"
fi

exit 0
`, pnpmLogPath, fmt.Sprintf("%d", frontendPort), filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"))

		convey.So(os.WriteFile(pnpmPath, []byte(stub), 0o755), convey.ShouldBeNil)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			env: map[string]string{
				"PATH":                                  binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)
		loggedArgs := waitForRunDevStepsForTest(t, pnpmLogPath, 1)

		convey.So(loggedArgs, convey.ShouldResemble, []string{
			fmt.Sprintf("dev --port %d", frontendPort),
		})

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh surfaces frontend test failures instead of exiting silently", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			env: map[string]string{
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "console.error('frontend tests blew up'); process.exit(17)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		err := process.Wait()

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(process.Stderr(), convey.ShouldContainSubstring, "Frontend test step failed")
		convey.So(process.Stderr(), convey.ShouldContainSubstring, "frontend tests blew up")
		convey.So(process.Stderr(), convey.ShouldContainSubstring, filepath.Join(repoRoot, "logs", "frontend.log"))
	})
}

func startDelayedHTTPServerForTest(t *testing.T, port int, delay time.Duration) {
	t.Helper()

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("[]"))
		}),
	}

	errCh := make(chan error, 1)
	go func() {
		time.Sleep(delay)

		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			errCh <- err
			return
		}

		errCh <- server.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = server.Close()

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("delayed HTTP server failed: %v", err)
			}
		default:
		}
	})
}

type runDevProcess struct {
	Command *exec.Cmd
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
}

func (process *runDevProcess) Wait() error {
	err := process.Command.Wait()
	if err != nil {
		return fmt.Errorf("%w\nstdout:\n%s\nstderr:\n%s", err, process.stdout.String(), process.stderr.String())
	}

	return nil
}

func (process *runDevProcess) Stderr() string {
	return process.stderr.String()
}

type runDevStartOptions struct {
	frontendPort int
	resultsPort  int
	seqmetaPort  int
	startDir     string
	env          map[string]string
}

func startRunDevForTest(t *testing.T, repoRoot string, options runDevStartOptions) *runDevProcess {
	t.Helper()

	args := []string{
		filepath.Join(repoRoot, "run-dev.sh"),
		"--frontend-port", fmt.Sprintf("%d", options.frontendPort),
		"--results-port", fmt.Sprintf("%d", options.resultsPort),
	}

	if options.seqmetaPort != 0 {
		args = append(args, "--seqmeta-port", fmt.Sprintf("%d", options.seqmetaPort))
	}

	command := exec.Command("bash", args...)
	command.Dir = repoRoot
	if options.startDir != "" {
		command.Dir = options.startDir
	}
	command.Env = append(os.Environ(), "CI=1")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr
	for key, value := range options.env {
		command.Env = append(command.Env, key+"="+value)
	}

	if err := command.Start(); err != nil {
		t.Fatalf("start run-dev.sh: %v", err)
	}

	if command.Process == nil {
		t.Fatalf("run-dev.sh did not start a process")
	}

	t.Cleanup(func() {
		if command.ProcessState == nil || !command.ProcessState.Exited() {
			_ = command.Process.Signal(syscall.SIGTERM)
		}
	})

	return &runDevProcess{
		Command: command,
		stdout:  stdout,
		stderr:  stderr,
	}
}

func runDevRepoRootForTest(t *testing.T) string {
	t.Helper()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	return filepath.Dir(workingDir)
}

func runDevFreePortForTest(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	defer func() { _ = listener.Close() }()

	return listener.Addr().(*net.TCPAddr).Port
}

func waitForRunDevSnapshotForTest(t *testing.T, snapshotPath string) runDevEnvSnapshot {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(snapshotPath)
		if err == nil {
			var snapshot runDevEnvSnapshot
			if unmarshalErr := json.Unmarshal(body, &snapshot); unmarshalErr != nil {
				t.Fatalf("decode frontend snapshot: %v", unmarshalErr)
			}

			return snapshot
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for frontend snapshot %s", snapshotPath)

	return runDevEnvSnapshot{}
}

func waitForSeededResultsForTest(t *testing.T, resultsPort int) []results.ResultSet {
	t.Helper()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/results", resultsPort)
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		response, err := http.Get(endpoint)
		if err == nil {
			var stored []results.ResultSet
			func() {
				defer func() { _ = response.Body.Close() }()
				if response.StatusCode != http.StatusOK {
					return
				}

				if decodeErr := json.NewDecoder(response.Body).Decode(&stored); decodeErr != nil {
					t.Fatalf("decode results response: %v", decodeErr)
				}
			}()

			if len(stored) == 3 {
				resultsCopy := make([]results.ResultSet, len(stored))
				copy(resultsCopy, stored)

				return resultsCopy
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for seeded results at %s", endpoint)

	return nil
}

func waitForRunDevStepsForTest(t *testing.T, stepsPath string, expectedCount int) []string {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(stepsPath)
		if err == nil {
			trimmed := strings.TrimSpace(string(body))
			if trimmed == "" {
				if expectedCount == 0 {
					return nil
				}
			} else {
				steps := strings.Split(trimmed, "\n")
				if len(steps) >= expectedCount {
					return steps
				}
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for frontend steps %s", stepsPath)

	return nil
}

func runDevPathExistsForTest(path string) bool {
	_, err := os.Stat(path)

	return err == nil || !errors.Is(err, fs.ErrNotExist)
}

func waitForTCPPortForTest(t *testing.T, port int) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)

	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			_ = connection.Close()

			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for TCP listener on %s", address)
}
