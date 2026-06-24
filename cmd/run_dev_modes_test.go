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
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// TestRunDevModeGuards locks in the cross-scenario isolation contract that
// `make test`, `make dev`, and `make prod` rely on. These are the regression
// tests for the bug where WA_TEST_*, WA_*_BACKEND_URL, WA_RESULTS_DB_PATH,
// and WA_MLWH_* values
// were mixed across scenarios so a stray test invocation could touch a
// configured dev/prod database.
func TestRunDevModeGuards(t *testing.T) {
	convey.Convey("run-dev.sh --mode dev refuses an occupied results port before building", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		convey.So(err, convey.ShouldBeNil)
		defer func() {
			convey.So(listener.Close(), convey.ShouldBeNil)
		}()

		resultsPort := listener.Addr().(*net.TCPAddr).Port
		stdout, stderr, err := runRunDevExpectingFailureWithinForTest(t, repoRoot, []string{
			"--mode", "dev",
			"--frontend-port", fmt.Sprintf("%d", frontendPort),
			"--results-port", fmt.Sprintf("%d", resultsPort),
			"--seqmeta-port", fmt.Sprintf("%d", seqmetaPort),
		}, map[string]string{
			"WA_ENV":                 "development",
			"WA_RESULTS_DB_PATH":     filepath.Join(t.TempDir(), "results-dev.sqlite"),
			"WA_RESULTS_LDAP_DN":     "uid=%s,ou=people,dc=example,dc=org",
			"WA_RESULTS_LDAP_SERVER": "ldap.example.org",
			"WA_MLWH_DSN":            "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
		}, nil, 5*time.Second)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout, convey.ShouldNotContainSubstring, "Building Go binary")
		convey.So(stderr, convey.ShouldContainSubstring, "results port")
		convey.So(stderr, convey.ShouldContainSubstring, fmt.Sprintf("%d", resultsPort))
		convey.So(stderr, convey.ShouldContainSubstring, "already in use")
		convey.So(stderr, convey.ShouldContainSubstring, "WA_DEV_RESULTS_PORT")
	})

	convey.Convey("run-dev.sh --mode prod refuses without WA_ENV=production", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, []string{"WA_ENV", "WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_ENV=production")
	})

	convey.Convey("run-dev.sh --mode dev refuses with WA_ENV=production inherited", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "dev", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":             "production",
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_ENV=production")
	})

	convey.Convey("run-dev.sh --mode dev refuses without WA_RESULTS_DB_PATH", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "dev", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, nil, []string{"WA_ENV", "WA_RESULTS_DB_PATH"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_RESULTS_DB_PATH")
	})

	convey.Convey("run-dev.sh --mode dev refuses without any MLWH query source", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "dev", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, []string{"WA_ENV", "WA_MLWH_SERVER_URL", "WA_MLWH_CACHE_PATH", "WA_MLWH_DSN", "WA_RUN_DEV_SEQMETA_CMD"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "MLWH query source")
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
	})

	convey.Convey("run-dev.sh --mode test refuses with WA_RESULTS_DB_PATH set", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "test", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, []string{"WA_ENV"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_RESULTS_DB_PATH")
	})

	convey.Convey("run-dev.sh --mode test refuses with WA_MLWH_CACHE_PATH set", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "test", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_MLWH_CACHE_PATH": "/var/lib/wa/mlwh.sqlite",
		}, []string{"WA_ENV"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
	})

	convey.Convey("run-dev.sh --mode test refuses with WA_MLWH_DSN set", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "test", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_MLWH_DSN": "mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse",
		}, []string{"WA_ENV"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_DSN")
	})

	convey.Convey("run-dev.sh --mode test refuses with WA_ENV=production inherited", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "test", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV": "production",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_ENV=production")
	})

	convey.Convey("run-dev.sh --mode prod rejects --fixtures", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--fixtures", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":             "production",
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, []string{"WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "--fixtures")
	})

	convey.Convey("run-dev.sh --mode prod rejects inherited WA_TEST_*_PORT", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":                "production",
			"WA_RESULTS_DB_PATH":    "/var/lib/wa/results.db",
			"WA_TEST_FRONTEND_PORT": "3000",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_TEST_FRONTEND_PORT")
	})

	convey.Convey("run-dev.sh --mode prod rejects inherited WA_DEV_*_PORT", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":              "production",
			"WA_RESULTS_DB_PATH":  "/var/lib/wa/results.db",
			"WA_DEV_RESULTS_PORT": "3672",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_DEV_RESULTS_PORT")
	})

	convey.Convey("run-dev.sh --mode prod rejects inherited WA_DEV_RESULTS_HOST", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":              "production",
			"WA_RESULTS_DB_PATH":  "/var/lib/wa/results.db",
			"WA_DEV_RESULTS_HOST": "0.0.0.0",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_DEV_RESULTS_HOST")
	})

	convey.Convey("run-dev.sh --mode prod rejects inherited WA_DEV_SEQMETA_HOST", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":              "production",
			"WA_RESULTS_DB_PATH":  "/var/lib/wa/results.db",
			"WA_DEV_SEQMETA_HOST": "0.0.0.0",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_DEV_SEQMETA_HOST")
	})

	convey.Convey("run-dev.sh --mode prod rejects test-shaped WA_MLWH_CACHE_PATH", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":             "production",
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
			"WA_MLWH_DSN":        "mlwh_prod@tcp(mlwh-db-ro:3435)/mlwarehouse",
			"WA_MLWH_CACHE_PATH": "/tmp/wa-test-mlwh.sqlite",
		}, []string{"WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
	})

	convey.Convey("run-dev.sh --mode prod rejects development or test-shaped WA_MLWH_DSN", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":             "production",
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
			"WA_MLWH_DSN":        "mlwh_test@tcp(localhost:3306)/mlwarehouse_test",
		}, []string{"WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_DSN")
	})

	convey.Convey("run-dev.sh --mode prod rejects inherited WA_MLWH_PASSWORD from development or test env", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "prod", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_ENV":             "production",
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
			"WA_MLWH_DSN":        "mlwh_prod@tcp(mlwh-db-ro:3435)/mlwarehouse",
			"WA_MLWH_PASSWORD":   "mlwh_humgen_is_secure",
		}, []string{"WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_PASSWORD")
	})
}

// runRunDevExpectingFailureForTest invokes run-dev.sh with the given args and
// environment overrides, expecting a non-zero exit. It returns stdout, stderr,
// and the exit error so the caller can assert on the diagnostic message.
func runRunDevExpectingFailureForTest(t *testing.T, repoRoot string, args []string, env map[string]string, unsetEnv []string) (string, string, error) {
	t.Helper()

	command := exec.Command("bash", append([]string{filepath.Join(repoRoot, "run-dev.sh")}, args...)...) //nolint:gosec
	command.Dir = repoRoot
	command.Env = applyTestEnvForTest(env, unsetEnv)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr

	err := command.Run()

	return stdout.String(), stderr.String(), err
}

func applyTestEnvForTest(overrides map[string]string, unset []string) []string {
	base := runDevEnvForTest(unset)
	for key, value := range overrides {
		base = append(base, key+"="+value)
	}

	// Strip any duplicate definitions so the override at the end wins
	// deterministically (exec.Cmd uses the last value for a given key on
	// most platforms but be explicit).
	seen := map[string]string{}
	for _, entry := range base {
		key, val, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		seen[key] = val
	}

	deduped := make([]string, 0, len(seen))
	for key, val := range seen {
		deduped = append(deduped, key+"="+val)
	}

	return deduped
}
