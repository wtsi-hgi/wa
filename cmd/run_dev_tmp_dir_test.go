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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// runDevTmpDirsForTest maps each test to the directory its run-dev.sh runs use
// as WA_RUN_DEV_TMP_DIR, so a test's runs share state the way runs in one
// checkout do while never touching the checkout's own .tmp (and with it any
// verify-wa or make dev stack running there).
var runDevTmpDirsForTest sync.Map

func TestRunDevTmpDirOverride(t *testing.T) {
	convey.Convey("run-dev.sh --mode test keeps its dev TLS files, state, binaries, and DBs under WA_RUN_DEV_TMP_DIR", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		tmpDir := runDevTmpDirForTest(t)
		invocationsPath := filepath.Join(t.TempDir(), "wa-invocations.log")
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevTestModeForTest(t, repoRoot, invocationsPath, map[string]string{
			"WA_RUN_DEV_ENV_SNAPSHOT": snapshotPath,
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		serve := runDevMLWHServeInvocationForTest(t, invocationsPath)
		binaryPath := runDevBuiltBinaryPathForTest(t, process)
		stableBinaryPath := runDevStableBinaryPathForTest(t)
		certPath, keyPath := runDevTLSCertPathsForTest(t)

		convey.So(certPath, convey.ShouldEqual, filepath.Join(tmpDir, "wa-dev-cert.pem"))
		convey.So(keyPath, convey.ShouldEqual, filepath.Join(tmpDir, "wa-dev-key.pem"))
		convey.So(snapshot.ResultsServerCert, convey.ShouldEqual, certPath)
		convey.So(snapshot.ResultsServerKey, convey.ShouldEqual, keyPath)
		convey.So(snapshot.ResultsCACert, convey.ShouldEqual, certPath)
		convey.So(runDevPathExistsForTest(certPath), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(keyPath), convey.ShouldBeTrue)

		token, err := os.ReadFile(filepath.Join(tmpDir, "state-test", ".wa-results-server.token"))
		convey.So(err, convey.ShouldBeNil)
		convey.So(strings.TrimSpace(string(token)), convey.ShouldEqual, "stub-token")

		convey.So(filepath.Dir(binaryPath), convey.ShouldEqual, tmpDir)
		convey.So(stableBinaryPath, convey.ShouldEqual, filepath.Join(tmpDir, "wa"))
		convey.So(runDevStableBinarySymlinkTargetForTest(t, stableBinaryPath), convey.ShouldEqual, binaryPath)
		convey.So(runDevPathExistsForTest(filepath.Join(tmpDir, "wa.lock")), convey.ShouldBeTrue)

		convey.So(filepath.Dir(snapshot.ResultsDBPath), convey.ShouldEqual, tmpDir)
		convey.So(filepath.Dir(snapshot.MLWHCachePath), convey.ShouldEqual, tmpDir)
		convey.So(filepath.Dir(runDevFlagValueForTest(serve, "--feedback-db")), convey.ShouldEqual, tmpDir)
		convey.So(filepath.Dir(runDevFlagValueForTest(serve, "--usage-db")), convey.ShouldEqual, tmpDir)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
		convey.So(runDevPathExistsForTest(binaryPath), convey.ShouldBeFalse)
		convey.So(runDevLstatPathExistsForTest(stableBinaryPath), convey.ShouldBeFalse)
	})

	convey.Convey("run-dev.sh --mode test accepts a WA_MLWH_CACHE_PATH under WA_RUN_DEV_TMP_DIR", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		invocationsPath := filepath.Join(t.TempDir(), "wa-invocations.log")

		process := startRunDevTestModeForTest(t, repoRoot, invocationsPath, map[string]string{
			"WA_MLWH_CACHE_PATH": filepath.Join(runDevTmpDirForTest(t), "mlwh-cache.sqlite"),
		})

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func TestRunDevLogDirOverride(t *testing.T) {
	convey.Convey("run-dev.sh --mode test writes its service logs under WA_RUN_DEV_LOG_DIR, not the checkout's logs", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		logDir := runDevLogDirForTest(t)
		invocationsPath := filepath.Join(t.TempDir(), "wa-invocations.log")

		process := startRunDevTestModeForTest(t, repoRoot, invocationsPath, nil)

		convey.So(waitForRunDevStdoutForTest(t, process, "Logs: "+logDir+"\n"), convey.ShouldBeTrue)
		convey.So(process.stdout.String(), convey.ShouldNotContainSubstring, "Logs: "+filepath.Join(repoRoot, "logs"))

		for _, name := range []string{"results.log", "frontend.log", "mlwh.log"} {
			convey.So(runDevPathExistsForTest(filepath.Join(logDir, name)), convey.ShouldBeTrue)
		}

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

// runDevLogDirForTest returns the directory t's run-dev.sh runs use as
// WA_RUN_DEV_LOG_DIR, inside t's run-dev temp dir.
func runDevLogDirForTest(t *testing.T) string {
	t.Helper()

	return filepath.Join(runDevTmpDirForTest(t), "logs")
}

// runDevTmpDirForTest returns t's run-dev.sh temp directory, creating it on
// first use.
func runDevTmpDirForTest(t *testing.T) string {
	t.Helper()

	if dir, ok := runDevTmpDirsForTest.Load(t); ok {
		return dir.(string) //nolint:forcetypeassert
	}

	dir, _ := runDevTmpDirsForTest.LoadOrStore(t, t.TempDir())
	t.Cleanup(func() { runDevTmpDirsForTest.Delete(t) })

	return dir.(string) //nolint:forcetypeassert
}
