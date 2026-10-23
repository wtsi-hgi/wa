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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runDevGuardHelperPIDPathEnvForTest, when set, makes
// TestStartRunDevForTestGuardHelper start a stack and record its process
// group in the named file instead of skipping.
const runDevGuardHelperPIDPathEnvForTest = "WA_TEST_RUN_DEV_GUARD_HELPER_PID_PATH"

func TestStartRunDevForTestCleanupKillsGroupMembersLeftByExitedLeader(t *testing.T) {
	repoRoot := runDevRepoRootForTest(t)
	pidPath := filepath.Join(t.TempDir(), "child.pid")

	t.Run("run-dev.sh exits leaving a child in its process group", func(t *testing.T) {
		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:          "dev",
			omitPortFlags: true,
			command:       []string{"-c", `sleep 300 >/dev/null 2>&1 & echo "$!" > "$1"`, "--", pidPath},
		})

		if err := runDevWaitWithinForTest(process, 10*time.Second); err != nil {
			t.Fatalf("leader did not exit: %v", err)
		}
	})

	pid := runDevReadPIDsForTest(t, pidPath)[0]
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	if !runDevPIDExitsWithinForTest(pid, 10*time.Second) {
		t.Fatalf("process %d in the exited run-dev.sh's process group survived cleanup", pid)
	}
}

func TestStartRunDevForTestStackDiesWithTestBinary(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "stack.pid")

	helper := exec.Command(os.Args[0], "-test.run=^TestStartRunDevForTestGuardHelper$", "-test.count=1") //nolint:gosec
	helper.Env = append(os.Environ(), runDevGuardHelperPIDPathEnvForTest+"="+pidPath)
	helper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := helper.Start(); err != nil {
		t.Fatalf("start helper test binary: %v", err)
	}

	t.Cleanup(func() {
		signalRunDevProcessGroupForTest(helper, syscall.SIGKILL)
		_ = helper.Wait()
	})

	if !runDevPathExistsWithinForTest(pidPath, time.Minute) {
		t.Fatal("helper test binary did not start its stack")
	}

	pgid := runDevReadPIDsForTest(t, pidPath)[0]
	t.Cleanup(func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) })

	// A go test timeout, Ctrl-C or the harness killing the test binary skips
	// t.Cleanup; the idle stack must still go.
	if err := helper.Process.Kill(); err != nil {
		t.Fatalf("kill helper test binary: %v", err)
	}

	_ = helper.Wait()

	if !runDevProcessGroupGoneWithinForTest(pgid, 15*time.Second) {
		t.Fatalf("run-dev.sh process group %d survived its test binary", pgid)
	}
}

// TestStartRunDevForTestGuardHelper is run by
// TestStartRunDevForTestStackDiesWithTestBinary in a test binary it kills.
func TestStartRunDevForTestGuardHelper(t *testing.T) {
	pidPath := os.Getenv(runDevGuardHelperPIDPathEnvForTest)
	if pidPath == "" {
		t.Skip("only run by TestStartRunDevForTestStackDiesWithTestBinary")
	}

	// Like a ready run-dev.sh, the leader waits quietly on a child in its
	// process group. It records "<pgid> <child pid>".
	startRunDevForTest(t, runDevRepoRootForTest(t), runDevStartOptions{
		mode:          "dev",
		omitPortFlags: true,
		command:       []string{"-c", `sleep 300 >/dev/null 2>&1 & echo "$$ $!" > "$1.tmp"; mv "$1.tmp" "$1"; wait`, "--", pidPath},
	})

	time.Sleep(10 * time.Minute)
}

func runDevReadPIDsForTest(t *testing.T, path string) []int {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}

	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		t.Fatalf("pid file %s is empty", path)
	}

	pids := make([]int, 0, len(fields))
	for _, field := range fields {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 0 {
			t.Fatalf("parse pid file %s: %q", path, body)
		}

		pids = append(pids, pid)
	}

	return pids
}

// runDevProcessGroupGoneWithinForTest reports whether process group pgid has
// no members left within timeout.
func runDevProcessGroupGoneWithinForTest(pgid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for {
		if syscall.Kill(-pgid, 0) != nil {
			return true
		}

		if !time.Now().Before(deadline) {
			return false
		}

		time.Sleep(50 * time.Millisecond)
	}
}
