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
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	AllowedDevOrigins string `json:"WA_DEV_ALLOWED_ORIGINS"`
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
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `printf '%s\n' 'app/results/page.tsx' 'package.json'`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'lint:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'format:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "require('node:fs').appendFileSync(process.env.WA_RUN_DEV_ENV_SNAPSHOT + '.steps', 'test:' + JSON.stringify(process.argv.slice(1)) + '\n')"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)
		resultsList := waitForSeededResultsForTest(t, resultsPort)
		fixtureSummary := summarizeRunDevFixturesForTest(t, repoRoot, resultsPort, resultsList)

		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, ".tmp", "wa")), convey.ShouldBeTrue)
		convey.So(resultsList, convey.ShouldHaveLength, 3)
		convey.So(fixtureSummary.nestedDirectoryCount, convey.ShouldBeGreaterThanOrEqualTo, 3)
		convey.So(fixtureSummary.hasSiblingDirectories, convey.ShouldBeTrue)
		convey.So(fixtureSummary.hasRepeatedFileTypes, convey.ShouldBeTrue)
		convey.So(fixtureSummary.fileKinds, convey.ShouldContain, "input")
		convey.So(fixtureSummary.fileKinds, convey.ShouldContain, "output")
		convey.So(fixtureSummary.fileKinds, convey.ShouldContain, "pipeline")
		convey.So(fixtureSummary.maxPreviewableImagesInDirectory, convey.ShouldBeGreaterThan, 100)
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
		healthPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		startDelayedHTTPServerForTest(t, healthPort, 0)

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
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/studies", healthPort),
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
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
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

	convey.Convey("run-dev.sh waits for seqmeta studies readiness before starting the frontend by default", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		binDir := t.TempDir()
		curlLogPath := filepath.Join(t.TempDir(), "seqmeta-curl.log")
		curlStatePath := filepath.Join(t.TempDir(), "seqmeta-curl.state")
		curlPath := filepath.Join(binDir, "curl")
		realCurlPath, err := exec.LookPath("curl")
		convey.So(err, convey.ShouldBeNil)

		stub := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

url="${!#}"

if [[ "$url" == %q ]]; then
	if [[ ! -f %q ]]; then
		printf '%%s\n' "$url" > %q
	fi

	count=0
	if [[ -f %q ]]; then
		count="$(cat %q)"
	fi

	count=$((count + 1))
	printf '%%s' "$count" > %q

		if (( count < 8 )); then
		exit 22
	fi

	exit 0
fi

if [[ "$url" == %q ]]; then
	if [[ ! -f %q ]]; then
		printf '%%s\n' "$url" > %q
	fi

		exit 22
fi

exec %q "$@"
`,
			fmt.Sprintf("http://127.0.0.1:%d/studies", seqmetaPort),
			curlLogPath,
			curlLogPath,
			curlStatePath,
			curlStatePath,
			curlStatePath,
			fmt.Sprintf("http://127.0.0.1:%d/validate/SANG001", seqmetaPort),
			curlLogPath,
			curlLogPath,
			realCurlPath,
		)

		convey.So(os.WriteFile(curlPath, []byte(stub), 0o755), convey.ShouldBeNil)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			seqmetaPort:  seqmetaPort,
			env: map[string]string{
				"PATH":                                  binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"SAGA_API_TOKEN":                        "test-token",
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		loggedURL := waitForRunDevStepsForTest(t, curlLogPath, 1)
		convey.So(loggedURL, convey.ShouldResemble, []string{fmt.Sprintf("http://127.0.0.1:%d/studies", seqmetaPort)})

		convey.So(runDevPathExistsWithinForTest(snapshotPath, 1400*time.Millisecond), convey.ShouldBeFalse)

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)

		convey.So(snapshot.SeqmetaBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", seqmetaPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh tests can explicitly disable an inherited SAGA token without changing product behavior", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		t.Setenv("SAGA_API_TOKEN", "inherited-token")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)

		convey.So(strings.TrimSpace(snapshot.SeqmetaBackendURL), convey.ShouldEqual, "")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("R1.7: run-dev.sh skips frontend lint and format checks when no frontend files have changed", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":        snapshotPath,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":    fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL": fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)

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
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
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
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
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

	convey.Convey("run-dev.sh can extend the frontend health wait budget for slow e2e frontend startup", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		binDir := t.TempDir()
		curlStatePath := filepath.Join(t.TempDir(), "frontend-health-curl.state")
		curlPath := filepath.Join(binDir, "curl")
		sleepPath := filepath.Join(binDir, "sleep")
		realCurlPath, err := exec.LookPath("curl")
		convey.So(err, convey.ShouldBeNil)

		curlStub := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

url="${!#}"

if [[ "$url" == %q ]]; then
	count=0
	if [[ -f %q ]]; then
		count="$(cat %q)"
	fi

	count=$((count + 1))
	printf '%%s' "$count" > %q

	if (( count <= 200 )); then
		exit 22
	fi
fi

exec %q "$@"
`,
			fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			curlStatePath,
			curlStatePath,
			curlStatePath,
			realCurlPath,
		)

		sleepStub := "#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"

		convey.So(os.WriteFile(curlPath, []byte(curlStub), 0o755), convey.ShouldBeNil)
		convey.So(os.WriteFile(sleepPath, []byte(sleepStub), 0o755), convey.ShouldBeNil)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"PATH":                                    binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"WA_RUN_DEV_ENV_SNAPSHOT":                 snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD":   `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":            `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":            `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":             fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":          fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS": "240",
			},
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)
		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)
		convey.So(snapshot.ResultsBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", resultsPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh exports WA_DEV_ALLOWED_ORIGINS to the frontend including the current hostname so Next.js dev permits cross-origin access", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL", "WA_DEV_ALLOWED_ORIGINS"},
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)

		origins := splitRunDevOriginsForTest(snapshot.AllowedDevOrigins)

		convey.So(origins, convey.ShouldContain, "localhost")
		convey.So(origins, convey.ShouldContain, "127.0.0.1")

		fqdn := runDevHostnameForTest(t, "-f")
		short := runDevHostnameForTest(t, "-s")

		if fqdn != "" {
			convey.So(origins, convey.ShouldContain, fqdn)
		}

		if short != "" && short != fqdn {
			convey.So(origins, convey.ShouldContain, short)
		}

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh appends user-supplied WA_DEV_ALLOWED_ORIGINS values to the defaults", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"WA_DEV_ALLOWED_ORIGINS":                "extra-host.example.com, another.example.com",
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, snapshotPath)
		origins := splitRunDevOriginsForTest(snapshot.AllowedDevOrigins)

		convey.So(origins, convey.ShouldContain, "localhost")
		convey.So(origins, convey.ShouldContain, "extra-host.example.com")
		convey.So(origins, convey.ShouldContain, "another.example.com")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

}

func runDevEnvForTest(unsetKeys []string) []string {
	blocked := make(map[string]struct{}, len(unsetKeys))
	for _, key := range unsetKeys {
		blocked[key] = struct{}{}
	}

	filtered := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}

		if _, skip := blocked[key]; skip {
			continue
		}

		filtered = append(filtered, entry)
	}

	return filtered
}

func terminateRunDevCommandForTest(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(done)
	}()

	signalRunDevProcessGroupForTest(command, syscall.SIGTERM)

	select {
	case <-done:
		return
	case <-time.After(5 * time.Second):
	}

	signalRunDevProcessGroupForTest(command, syscall.SIGKILL)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func signalRunDevProcessGroupForTest(command *exec.Cmd, signal syscall.Signal) {
	if command == nil || command.Process == nil || command.Process.Pid <= 0 {
		return
	}

	err := syscall.Kill(-command.Process.Pid, signal)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return
	}

	_ = command.Process.Signal(signal)
}

func summarizeRunDevFixturesForTest(t *testing.T, repoRoot string, resultsPort int, resultSets []results.ResultSet) runDevFixtureSummary {
	t.Helper()

	previewableImageExts := map[string]struct{}{
		".apng": {},
		".avif": {},
		".gif":  {},
		".jpeg": {},
		".jpg":  {},
		".png":  {},
		".svg":  {},
		".webp": {},
	}

	directoryDepths := map[string]int{}
	parentToChildren := map[string]map[string]struct{}{}
	fileTypeCounts := map[string]int{}
	previewableByDirectory := map[string]int{}
	kindSet := map[string]struct{}{}

	for _, resultSet := range resultSets {
		files := listRunDevResultFilesForTest(t, resultsPort, resultSet.ID)

		for _, file := range files {
			kindSet[file.Kind] = struct{}{}

			relPath, err := filepath.Rel(resultSet.OutputDirectory, file.Path)
			if err != nil {
				t.Fatalf("compute relative path for %s: %v", file.Path, err)
			}

			relPath = filepath.ToSlash(relPath)
			directory := filepath.ToSlash(filepath.Dir(relPath))
			if directory == "." {
				directory = ""
			}

			depth := 0
			if directory != "" {
				depth = strings.Count(directory, "/") + 1
				directoryDepths[directory] = depth

				parent := filepath.ToSlash(filepath.Dir(directory))
				if parent == "." {
					parent = ""
				}

				children := parentToChildren[parent]
				if children == nil {
					children = map[string]struct{}{}
					parentToChildren[parent] = children
				}

				children[directory] = struct{}{}
			}

			ext := strings.ToLower(filepath.Ext(file.Path))
			if ext != "" {
				fileTypeCounts[ext]++
			}

			if _, ok := previewableImageExts[ext]; ok {
				previewableByDirectory[directory]++
			}
		}

		convey.So(filepath.IsAbs(resultSet.OutputDirectory), convey.ShouldBeTrue)
		convey.So(strings.HasPrefix(resultSet.OutputDirectory, repoRoot), convey.ShouldBeTrue)
	}

	nestedDirectoryCount := 0
	for _, depth := range directoryDepths {
		if depth >= 2 {
			nestedDirectoryCount++
		}
	}

	hasSiblingDirectories := false
	for _, children := range parentToChildren {
		if len(children) >= 2 {
			hasSiblingDirectories = true
			break
		}
	}

	hasRepeatedFileTypes := false
	for _, count := range fileTypeCounts {
		if count >= 2 {
			hasRepeatedFileTypes = true
			break
		}
	}

	maxPreviewableImagesInDirectory := 0
	for _, count := range previewableByDirectory {
		if count > maxPreviewableImagesInDirectory {
			maxPreviewableImagesInDirectory = count
		}
	}

	kinds := make([]string, 0, len(kindSet))
	for kind := range kindSet {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)

	return runDevFixtureSummary{
		nestedDirectoryCount:            nestedDirectoryCount,
		hasSiblingDirectories:           hasSiblingDirectories,
		hasRepeatedFileTypes:            hasRepeatedFileTypes,
		maxPreviewableImagesInDirectory: maxPreviewableImagesInDirectory,
		fileKinds:                       kinds,
	}
}

func listRunDevResultFilesForTest(t *testing.T, resultsPort int, resultID string) []results.FileEntry {
	t.Helper()
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/results/%s/files", resultsPort, resultID)
	response, err := http.Get(endpoint)
	if err != nil {
		t.Fatalf("get result files from %s: %v", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("get result files from %s returned %d and unreadable body: %v", endpoint, response.StatusCode, readErr)
		}

		t.Fatalf("get result files from %s returned %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body)))
	}

	var files []results.FileEntry
	if err := json.NewDecoder(response.Body).Decode(&files); err != nil {
		t.Fatalf("decode result files from %s: %v", endpoint, err)
	}

	return files
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

func runDevPathExistsWithinForTest(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if runDevPathExistsForTest(path) {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}

	return runDevPathExistsForTest(path)
}

func splitRunDevOriginsForTest(raw string) []string {
	parts := strings.Split(raw, ",")
	trimmed := make([]string, 0, len(parts))

	for _, part := range parts {
		entry := strings.TrimSpace(part)
		if entry != "" {
			trimmed = append(trimmed, entry)
		}
	}

	return trimmed
}

func runDevHostnameForTest(t *testing.T, flag string) string {
	t.Helper()

	output, err := exec.Command("hostname", flag).Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func TestStartRunDevForTestAutoCleanup(t *testing.T) {
	repoRoot := runDevRepoRootForTest(t)
	frontendPort := runDevFreePortForTest(t)
	resultsPort := runDevFreePortForTest(t)
	snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

	passed := t.Run("cleanup on subtest teardown closes child listeners", func(t *testing.T) {
		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     []string{"SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RUN_DEV_SEQMETA_HEALTH_URL"},
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, snapshotPath)
		_ = waitForSeededResultsForTest(t, resultsPort)
		if process.Command.Process == nil {
			t.Fatal("run-dev.sh did not start a process")
		}
	})

	if !passed {
		t.Fatal("run-dev cleanup regression subtest failed")
	}
	waitForTCPPortToCloseForTest(t, frontendPort)
	waitForTCPPortToCloseForTest(t, resultsPort)
}

func waitForTCPPortToCloseForTest(t *testing.T, port int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	address := fmt.Sprintf("127.0.0.1:%d", port)

	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err != nil {
			return
		}

		_ = connection.Close()
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for TCP listener on %s to stop", address)
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
	unsetEnv     []string
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
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = append(runDevEnvForTest(options.unsetEnv), "CI=1")
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
			terminateRunDevCommandForTest(command)
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

func waitForRunDevStdoutForTest(t *testing.T, process *runDevProcess, substring string) bool {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(process.stdout.String(), substring) {
			return true
		}

		if process.Command.ProcessState != nil && process.Command.ProcessState.Exited() {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	return strings.Contains(process.stdout.String(), substring)
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

type runDevFixtureSummary struct {
	nestedDirectoryCount            int
	hasSiblingDirectories           bool
	hasRepeatedFileTypes            bool
	maxPreviewableImagesInDirectory int
	fileKinds                       []string
}
