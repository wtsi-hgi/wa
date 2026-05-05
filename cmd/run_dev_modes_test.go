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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// TestRunDevModeGuards locks in the cross-scenario isolation contract that
// `make test`, `make dev`, and `make prod` rely on. These are the regression
// tests for the bug where WA_TEST_*, WA_*_BACKEND_URL, and WA_RESULTS_DB_PATH
// were mixed across scenarios so a stray test invocation could touch a
// configured dev/prod database.
func TestRunDevModeGuards(t *testing.T) {
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

	convey.Convey("run-dev.sh --mode test refuses with WA_RESULTS_DB_PATH set", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{"--mode", "test", "--frontend-port", "1", "--results-port", "1", "--seqmeta-port", "1"}, map[string]string{
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, []string{"WA_ENV"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_RESULTS_DB_PATH")
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

	convey.Convey("scripts/wa-env.sh test refuses with WA_RESULTS_DB_PATH inherited", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runWaEnvExpectingFailureForTest(t, repoRoot, []string{"test", "--", "true"}, map[string]string{
			"WA_RESULTS_DB_PATH": "/var/lib/wa/results.db",
		}, []string{"WA_ENV"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_RESULTS_DB_PATH")
	})

	convey.Convey("scripts/wa-env.sh prod refuses when .env.prod is missing", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runWaEnvExpectingFailureForTest(t, repoRoot, []string{"prod", "--", "true"}, nil, []string{"WA_ENV", "WA_RESULTS_DB_PATH", "WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT", "WA_DEV_FRONTEND_PORT", "WA_DEV_RESULTS_PORT", "WA_DEV_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, ".env.prod")
	})

	convey.Convey("scripts/wa-env.sh dev refuses with WA_ENV=production inherited", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runWaEnvExpectingFailureForTest(t, repoRoot, []string{"dev", "--", "true"}, map[string]string{
			"WA_ENV": "production",
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_ENV=production")
	})

	convey.Convey("scripts/wa-env.sh test does not inject a SAGA token without a developer token source", t, func() {
		repoRoot := createWaEnvRepoForTest(t, false, "")

		stdout, stderr, err := runWaEnvForTest(t, repoRoot, []string{"test", "--", "env"}, nil, []string{"WA_ENV", "SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RESULTS_DB_PATH"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr, convey.ShouldBeBlank)
		convey.So(stdout, convey.ShouldContainSubstring, "WA_ENV=test")
		convey.So(stdout, convey.ShouldNotContainSubstring, "SAGA_API_TOKEN=")
	})

	convey.Convey("scripts/wa-env.sh test exposes an optional .env.dev SAGA token to live integration tests", t, func() {
		repoRoot := createWaEnvRepoForTest(t, true, "WA_ENV=development\nSAGA_API_TOKEN=integration-token\n")

		stdout, stderr, err := runWaEnvForTest(t, repoRoot, []string{"test", "--", "env"}, nil, []string{"WA_ENV", "SAGA_API_TOKEN", "SAGA_TEST_API_TOKEN", "WA_RESULTS_DB_PATH"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr, convey.ShouldBeBlank)
		convey.So(stdout, convey.ShouldContainSubstring, "WA_ENV=test")
		convey.So(stdout, convey.ShouldContainSubstring, "SAGA_API_TOKEN=integration-token")
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

// runWaEnvExpectingFailureForTest invokes scripts/wa-env.sh with the given
// args and environment overrides, expecting a non-zero exit.
func runWaEnvExpectingFailureForTest(t *testing.T, repoRoot string, args []string, env map[string]string, unsetEnv []string) (string, string, error) {
	t.Helper()

	command := exec.Command("bash", append([]string{filepath.Join(repoRoot, "scripts", "wa-env.sh")}, args...)...) //nolint:gosec
	command.Dir = repoRoot
	command.Env = applyTestEnvForTest(env, unsetEnv)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr

	err := command.Run()

	return stdout.String(), stderr.String(), err
}

func createWaEnvRepoForTest(t *testing.T, includeDevEnv bool, devEnvContents string) string {
	t.Helper()

	actualRepoRoot := runDevRepoRootForTest(t)
	tempRepoRoot := t.TempDir()
	scriptsDir := filepath.Join(tempRepoRoot, "scripts")
	writeWaEnvFixtureFileForTest(t, filepath.Join(scriptsDir, "wa-env.sh"), mustReadFileForTest(t, filepath.Join(actualRepoRoot, "scripts", "wa-env.sh")), 0o755)
	writeWaEnvFixtureFileForTest(t, filepath.Join(tempRepoRoot, ".env.test"), []byte("WA_ENV=test\n"), 0o600)

	if includeDevEnv {
		writeWaEnvFixtureFileForTest(t, filepath.Join(tempRepoRoot, ".env.dev"), []byte(devEnvContents), 0o600)
	}

	return tempRepoRoot
}

func writeWaEnvFixtureFileForTest(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	err = os.WriteFile(path, contents, mode)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadFileForTest(t *testing.T, path string) []byte {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return contents
}

func runWaEnvForTest(t *testing.T, repoRoot string, args []string, env map[string]string, unsetEnv []string) (string, string, error) {
	t.Helper()

	command := exec.Command("bash", append([]string{filepath.Join(repoRoot, "scripts", "wa-env.sh")}, args...)...) //nolint:gosec
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
