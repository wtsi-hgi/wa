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
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const runDevProdRemoteMLWHURLForTest = "https://apps.example.test/mlwh-api"

const prodCtlCrashEvidenceForTest = "crash evidence from the previous run"

type prodCtlForTest struct {
	t        *testing.T
	repoRoot string
	binDir   string
	ports    runDevFeedbackPorts
	env      []string
}

// newProdCtlForTest returns a prod-ctl.sh harness whose stack runs stub
// services on free ports; overrides replace or add environment variables.
func newProdCtlForTest(t *testing.T, overrides map[string]string) *prodCtlForTest {
	t.Helper()

	repoRoot := runDevRepoRootForTest(t)
	ports := runDevFeedbackPortsForTest(t)
	binDir := t.TempDir()

	writeRunDevMLWHServeToolchainForTest(t, binDir, filepath.Join(t.TempDir(), "wa-invocations.log"))

	env := runDevEnvForTest(t, runDevUnsetProdRefusedEnvForTest())
	extra := runDevProdEnvForTest(t, binDir, ports, map[string]string{
		"WA_PROD_FRONTEND_PORT":       strconv.Itoa(ports.frontend),
		"WA_PROD_RESULTS_PORT":        strconv.Itoa(ports.results),
		"WA_PROD_SEQMETA_PORT":        strconv.Itoa(ports.seqmeta),
		"WA_RUN_DEV_FRONTEND_DEV_CMD": runDevFrontendStubCommandForTest(repoRoot, ports.frontend),
		"WA_RUN_DEV_ENV_SNAPSHOT":     filepath.Join(t.TempDir(), "frontend-env.json"),
	})
	maps.Copy(extra, overrides)
	for key, value := range extra {
		env = append(env, key+"="+value)
	}

	ctl := &prodCtlForTest{t: t, repoRoot: repoRoot, binDir: binDir, ports: ports, env: env}
	t.Cleanup(func() { ctl.run("stop") })

	return ctl
}

func (ctl *prodCtlForTest) run(action string) (string, string, int) {
	ctl.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	command := exec.CommandContext(ctx, "bash", filepath.Join(ctl.repoRoot, "prod-ctl.sh"), action)
	command.Dir = ctl.repoRoot
	command.Env = ctl.env
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr

	err := command.Run()

	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.String(), stderr.String(), 0
	case errors.As(err, &exitErr):
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	default:
		ctl.t.Fatalf("run prod-ctl.sh %s: %v\nstdout:\n%s\nstderr:\n%s", action, err, stdout, stderr)

		return "", "", -1
	}
}

func (ctl *prodCtlForTest) pidPath() string {
	return filepath.Join(runDevTmpDirForTest(ctl.t), "prod.pid")
}

func (ctl *prodCtlForTest) pid() int {
	ctl.t.Helper()

	body, err := os.ReadFile(ctl.pidPath())
	if err != nil {
		ctl.t.Fatalf("read prod pid file: %v", err)
	}

	fields := strings.Fields(string(body))
	if len(fields) == 0 {
		ctl.t.Fatalf("prod pid file is empty")
	}

	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		ctl.t.Fatalf("parse prod pid file %q: %v", body, err)
	}

	return pid
}

// holdLock holds prod-ctl.sh's lock file from another process, as a
// concurrent cron run would, until the returned release func runs.
func (ctl *prodCtlForTest) holdLock() func() {
	ctl.t.Helper()

	tmpDir := runDevTmpDirForTest(ctl.t)
	readyPath := filepath.Join(ctl.t.TempDir(), "locked")
	holder := exec.Command("flock", filepath.Join(tmpDir, "prod.lock"), "bash", "-c", `touch "$1"; sleep 60`, "--", readyPath)
	holder.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := holder.Start(); err != nil {
		ctl.t.Fatalf("start lock holder: %v", err)
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			signalRunDevProcessGroupForTest(holder, syscall.SIGKILL)
			_ = holder.Wait()
		})
	}
	ctl.t.Cleanup(release)

	if !runDevPathExistsWithinForTest(readyPath, 10*time.Second) {
		ctl.t.Fatal("lock holder did not take the lock")
	}

	return release
}

// crashFrontend kills the stack's frontend server, as a crash would, leaving
// run-dev.sh running.
func (ctl *prodCtlForTest) crashFrontend() {
	ctl.t.Helper()

	killed := exec.Command("pkill", "-f", fmt.Sprintf("run-dev-frontend-stub.mjs %d", ctl.ports.frontend))
	convey.So(killed.Run(), convey.ShouldBeNil)
	convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, 5*time.Second), convey.ShouldBeTrue)
}

// killLeader SIGKILLs the recorded run-dev.sh, leaving its services running,
// and returns its pid.
func (ctl *prodCtlForTest) killLeader() int {
	ctl.t.Helper()

	pid := ctl.pid()
	convey.So(syscall.Kill(pid, syscall.SIGKILL), convey.ShouldBeNil)
	convey.So(runDevPIDExitsWithinForTest(pid, 5*time.Second), convey.ShouldBeTrue)
	convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, time.Second), convey.ShouldBeFalse)

	return pid
}

func runDevPIDExitsWithinForTest(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !runDevPIDIsAliveForTest(pid) {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}

	return !runDevPIDIsAliveForTest(pid)
}

// writeFlakyCurl puts a curl in the stack's PATH that fails prod-ctl.sh's
// health probes while failuresPath holds a positive count, decrementing it on
// each failure, and otherwise runs the real curl.
func (ctl *prodCtlForTest) writeFlakyCurl(failuresPath string) {
	ctl.t.Helper()

	realCurl, err := exec.LookPath("curl")
	convey.So(err, convey.ShouldBeNil)

	stub := fmt.Sprintf(`#!/usr/bin/env bash
if [[ " $* " == *" wa-prod-ctl "* ]]; then
	left="$(cat %[1]q 2>/dev/null || printf 0)"
	if (( left > 0 )); then
		printf '%%s\n' "$((left - 1))" >%[1]q
		exit 7
	fi
fi
exec %[2]q "$@"
`, failuresPath, realCurl)

	convey.So(os.WriteFile(filepath.Join(ctl.binDir, "curl"), []byte(stub), 0o755), convey.ShouldBeNil)
}

// writePIDFile replaces prod-ctl.sh's pid file with body.
func (ctl *prodCtlForTest) writePIDFile(body string) {
	ctl.t.Helper()

	convey.So(os.WriteFile(ctl.pidPath(), []byte(body), 0o600), convey.ShouldBeNil)
}

func (ctl *prodCtlForTest) logPath() string {
	return filepath.Join(runDevTmpDirForTest(ctl.t), "prod.log")
}

// setEnv sets an environment variable for later runs.
func (ctl *prodCtlForTest) setEnv(key string, value string) {
	ctl.env = append(ctl.env, key+"="+value)
}

// interruptStart runs prod-ctl.sh start and interrupts it once the stack it
// launched is recorded, as Ctrl-C during an interactive start would, leaving
// the stack running. It returns the stack's pid.
func (ctl *prodCtlForTest) interruptStart() int {
	ctl.t.Helper()

	command := exec.Command("bash", filepath.Join(ctl.repoRoot, "prod-ctl.sh"), "start")
	command.Dir = ctl.repoRoot
	command.Env = ctl.env
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	convey.So(command.Start(), convey.ShouldBeNil)

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(ctl.pidPath())
		if err == nil && len(strings.Fields(string(body))) == 3 {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	signalRunDevProcessGroupForTest(command, syscall.SIGINT)
	_ = command.Wait()

	return ctl.pid()
}

func TestProdCtl(t *testing.T) {
	convey.Convey("prod-ctl.sh starts the prod stack detached, leaves a healthy stack alone, reports status, and stops it", t, func() {
		ctl := newProdCtlForTest(t, nil)

		stdout, stderr, code := ctl.run("start")
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "Started the wa production stack")
		pid := ctl.pid()
		convey.So(runDevPIDIsAliveForTest(pid), convey.ShouldBeTrue)

		stdout, stderr, code = ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldBeEmpty)
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(ctl.pid(), convey.ShouldEqual, pid)

		stdout, _, code = ctl.run("status")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Frontend: https://%s:%d", runDevHostnameForTest(t, "-f"), ctl.ports.frontend))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results: https://%s:%d", runDevHostnameForTest(t, "-f"), ctl.ports.results))
		convey.So(stdout, convey.ShouldContainSubstring, "MLWH: "+runDevProdRemoteMLWHURLForTest)

		_, stderr, code = ctl.run("stop")
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(runDevPIDExitsWithinForTest(pid, 5*time.Second), convey.ShouldBeTrue)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, 5*time.Second), convey.ShouldBeTrue)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.results, 5*time.Second), convey.ShouldBeTrue)

		_, _, code = ctl.run("status")
		convey.So(code, convey.ShouldNotEqual, 0)

		stdout, stderr, code = ctl.run("stop")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(stdout, convey.ShouldContainSubstring, "is not running")
	})

	convey.Convey("prod-ctl.sh start exits quietly without starting anything while another prod-ctl.sh holds the lock", t, func() {
		ctl := newProdCtlForTest(t, nil)
		release := ctl.holdLock()
		defer release()

		stdout, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldBeEmpty)
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
	})

	convey.Convey("prod-ctl.sh start run from a terminal says so when another prod-ctl.sh holds the lock", t, func() {
		ctl := newProdCtlForTest(t, nil)
		release := ctl.holdLock()
		defer release()

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		// script gives prod-ctl.sh a pseudo-terminal as its stdout.
		command := exec.CommandContext(ctx, "script", "-qec", "bash prod-ctl.sh start", "/dev/null")
		command.Dir = ctl.repoRoot
		command.Env = ctl.env
		output, err := command.CombinedOutput()
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(output), convey.ShouldContainSubstring, "Another prod-ctl.sh is already running")
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
	})

	convey.Convey("prod-ctl.sh stop and restart run from a terminal say they are waiting while another prod-ctl.sh holds the lock", t, func() {
		ctl := newProdCtlForTest(t, nil)

		for _, action := range []string{"stop", "restart"} {
			func() {
				release := ctl.holdLock()
				defer release()

				outputPath := filepath.Join(t.TempDir(), action+".out")
				output, err := os.Create(outputPath)
				convey.So(err, convey.ShouldBeNil)

				defer func() { _ = output.Close() }()

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()

				// script gives prod-ctl.sh a pseudo-terminal as its stdout.
				command := exec.CommandContext(ctx, "script", "-qec", "bash prod-ctl.sh "+action, "/dev/null")
				command.Dir = ctl.repoRoot
				command.Env = ctl.env
				command.Stdout = output
				command.Stderr = output
				convey.So(command.Start(), convey.ShouldBeNil)

				waiting := prodCtlFileContainsWithinForTest(outputPath, "Waiting for another prod-ctl.sh to finish", 10*time.Second)
				release()
				convey.So(command.Wait(), convey.ShouldBeNil)
				convey.So(waiting, convey.ShouldBeTrue)
			}()
		}
	})

	convey.Convey("prod-ctl.sh stop and restart wait silently for the lock when not run from a terminal", t, func() {
		ctl := newProdCtlForTest(t, nil)

		for _, action := range []string{"stop", "restart"} {
			release := ctl.holdLock()
			time.AfterFunc(2*time.Second, release)

			started := time.Now()
			stdout, stderr, code := ctl.run(action)
			convey.So(code, convey.ShouldEqual, 0)
			convey.So(time.Since(started), convey.ShouldBeGreaterThanOrEqualTo, 2*time.Second)
			convey.So(stdout, convey.ShouldNotContainSubstring, "Waiting")
			convey.So(stderr, convey.ShouldBeEmpty)
		}
	})

	convey.Convey("prod-ctl.sh start replaces a running but unhealthy stack, and restart replaces a healthy one", t, func() {
		ctl := newProdCtlForTest(t, nil)

		_, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		firstPID := ctl.pid()

		ctl.crashFrontend()

		_, _, code = ctl.run("status")
		convey.So(code, convey.ShouldNotEqual, 0)

		stdout, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "unhealthy")
		secondPID := ctl.pid()
		convey.So(secondPID, convey.ShouldNotEqual, firstPID)
		convey.So(runDevPIDExitsWithinForTest(firstPID, 5*time.Second), convey.ShouldBeTrue)

		_, _, code = ctl.run("restart")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(ctl.pid(), convey.ShouldNotEqual, secondPID)
		convey.So(runDevPIDExitsWithinForTest(secondPID, 5*time.Second), convey.ShouldBeTrue)

		_, _, code = ctl.run("status")
		convey.So(code, convey.ShouldEqual, 0)
	})
}

func runDevPIDIsAliveForTest(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))

	return err == nil && !strings.Contains(string(stat), ") Z ")
}

func prodCtlFileContainsWithinForTest(path string, substring string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), substring) {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}

	return false
}

func TestProdCtlRecovery(t *testing.T) {
	convey.Convey("prod-ctl.sh start keeps the crashed run's service logs when it restarts the stack", t, func() {
		ctl := newProdCtlForTest(t, nil)

		_, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)

		logDir := runDevLogDirForTest(t)
		serviceLogs := []string{filepath.Join(logDir, "results.log"), filepath.Join(logDir, "frontend.log")}
		for _, path := range serviceLogs {
			appendProdCtlLogLineForTest(path, prodCtlCrashEvidenceForTest)
		}

		ctl.crashFrontend()

		stdout, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "unhealthy")

		for _, path := range serviceLogs {
			convey.So(readProdCtlFileForTest(path+".1"), convey.ShouldContainSubstring, prodCtlCrashEvidenceForTest)
			convey.So(readProdCtlFileForTest(path), convey.ShouldNotContainSubstring, prodCtlCrashEvidenceForTest)
		}
	})

	convey.Convey("after run-dev.sh is SIGKILLed, prod-ctl.sh status reports its leftover services and stop stops them", t, func() {
		ctl := newProdCtlForTest(t, nil)

		_, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		pid := ctl.killLeader()

		_, stderr, code := ctl.run("status")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(stderr, convey.ShouldContainSubstring, fmt.Sprintf("run-dev.sh (pid %d) has exited", pid))
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeTrue)

		stdout, stderr, code := ctl.run("stop")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Stopped the wa production stack (pid %d)", pid))
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, 5*time.Second), convey.ShouldBeTrue)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.results, 5*time.Second), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)

		tmpDir := runDevTmpDirForTest(t)
		convey.So(runDevPathExistsForTest(filepath.Join(tmpDir, fmt.Sprintf("wa-run-dev-%d", pid))), convey.ShouldBeFalse)
		_, err := os.Lstat(filepath.Join(tmpDir, "wa"))
		convey.So(errors.Is(err, os.ErrNotExist), convey.ShouldBeTrue)
	})

	convey.Convey("after run-dev.sh is SIGKILLed, prod-ctl.sh start replaces its leftover services with a new stack", t, func() {
		ctl := newProdCtlForTest(t, nil)

		_, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		pid := ctl.killLeader()

		stdout, stderr, code := ctl.run("start")
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "restarting")
		convey.So(stdout, convey.ShouldContainSubstring, "Started the wa production stack")
		convey.So(ctl.pid(), convey.ShouldNotEqual, pid)

		_, _, code = ctl.run("status")
		convey.So(code, convey.ShouldEqual, 0)
	})

	convey.Convey("prod-ctl.sh start leaves a healthy stack alone when its health probes fail briefly", t, func() {
		ctl := newProdCtlForTest(t, map[string]string{"WA_PROD_HEALTH_RETRY_SECONDS": "1"})
		failuresPath := filepath.Join(t.TempDir(), "probe-failures")
		ctl.writeFlakyCurl(failuresPath)

		_, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		pid := ctl.pid()

		convey.So(os.WriteFile(failuresPath, []byte("2\n"), 0o600), convey.ShouldBeNil)

		stdout, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldBeEmpty)
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(ctl.pid(), convey.ShouldEqual, pid)
		convey.So(strings.TrimSpace(readProdCtlFileForTest(failuresPath)), convey.ShouldEqual, "0")
	})

	convey.Convey("prod-ctl.sh start fails, prints the log tail, and removes the pid file when the stack exits during startup", t, func() {
		ctl := newProdCtlForTest(t, map[string]string{
			"WA_RUN_DEV_FRONTEND_DEV_CMD": `printf 'stub frontend failed to start\n' >&2; exit 3`,
		})

		_, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(stderr, convey.ShouldContainSubstring, "exited during startup")
		convey.So(stderr, convey.ShouldContainSubstring, "stub frontend failed to start")
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.results, 5*time.Second), convey.ShouldBeTrue)
	})

	convey.Convey("prod-ctl.sh start fails without reporting a start when another healthy server holds the stack's ports", t, func() {
		ctl := newProdCtlForTest(t, nil)
		stub := filepath.Join(ctl.repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")
		env := append(os.Environ(), "WA_RUN_DEV_ENV_SNAPSHOT="+filepath.Join(t.TempDir(), "occupier-env.json"))
		startProdCtlForeignGroupForTest(t, t.TempDir(), env, `node "$1" "$2" & node "$1" "$3" & wait`,
			stub, strconv.Itoa(ctl.ports.frontend), strconv.Itoa(ctl.ports.results))
		waitForTCPPortForTest(t, ctl.ports.frontend)
		waitForTCPPortForTest(t, ctl.ports.results)

		stdout, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(stdout, convey.ShouldNotContainSubstring, "Started")
		convey.So(stderr, convey.ShouldContainSubstring, prodCtlUnrecordedPortMessageForTest(ctl.ports.frontend))
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, time.Second), convey.ShouldBeFalse)
	})

	convey.Convey("prod-ctl.sh start stops a stack that is not ready within WA_PROD_START_TIMEOUT_SECONDS", t, func() {
		ctl := newProdCtlForTest(t, map[string]string{
			"WA_PROD_START_TIMEOUT_SECONDS": "3",
			"WA_RUN_DEV_FRONTEND_DEV_CMD":   "sleep 600",
		})

		started := time.Now()
		_, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(time.Since(started), convey.ShouldBeLessThan, 30*time.Second)
		convey.So(stderr, convey.ShouldContainSubstring, "was not ready after 3s")
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.results, 5*time.Second), convey.ShouldBeTrue)
	})

	convey.Convey("prod-ctl.sh start fails at once when it cannot launch the stack", t, func() {
		ctl := newProdCtlForTest(t, nil)
		stub := "#!/usr/bin/env bash\nprintf 'stub setsid failed\\n' >&2\nexit 1\n"
		convey.So(os.WriteFile(filepath.Join(ctl.binDir, "setsid"), []byte(stub), 0o755), convey.ShouldBeNil)

		started := time.Now()
		_, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(time.Since(started), convey.ShouldBeLessThan, 30*time.Second)
		convey.So(stderr, convey.ShouldContainSubstring, "stub setsid failed")
		convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
	})
}

func appendProdCtlLogLineForTest(path string, line string) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	convey.So(err, convey.ShouldBeNil)

	_, err = fmt.Fprintln(file, line)
	convey.So(err, convey.ShouldBeNil)
	convey.So(file.Close(), convey.ShouldBeNil)
}

func readProdCtlFileForTest(path string) string {
	body, err := os.ReadFile(path)
	convey.So(err, convey.ShouldBeNil)

	return string(body)
}

// startProdCtlForeignGroupForTest starts a bash script as the leader of its
// own session in dir, as an unrelated process group would be, and kills its
// group when t ends.
func startProdCtlForeignGroupForTest(t *testing.T, dir string, env []string, script string, args ...string) *exec.Cmd {
	t.Helper()

	command := exec.Command("bash", append([]string{"-c", script, "--"}, args...)...)
	command.Dir = dir
	command.Env = env
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := command.Start(); err != nil {
		t.Fatalf("start foreign process group: %v", err)
	}

	t.Cleanup(func() {
		signalRunDevProcessGroupForTest(command, syscall.SIGKILL)
		_ = command.Wait()
	})

	return command
}

func prodCtlUnrecordedPortMessageForTest(port int) string {
	return fmt.Sprintf("port %d is in use by a process not recorded in", port)
}

func TestProdCtlStackIdentity(t *testing.T) {
	convey.Convey("prod-ctl.sh never signals a process group its pid file names unless the group carries the stack's identity", t, func() {
		ctl := newProdCtlForTest(t, nil)
		host, err := os.Hostname()
		convey.So(err, convey.ShouldBeNil)
		foreign := startProdCtlForeignGroupForTest(t, filepath.Join(ctl.repoRoot, "frontend"), os.Environ(), "exec sleep 600")
		pgid := foreign.Process.Pid

		for _, body := range []string{
			fmt.Sprintf("%d\n", pgid),
			fmt.Sprintf("%d %s 0123456789abcdef\n", pgid, host),
		} {
			ctl.writePIDFile(body)

			_, stderr, code := ctl.run("status")
			convey.So(code, convey.ShouldNotEqual, 0)
			convey.So(stderr, convey.ShouldContainSubstring, "is not running")

			stdout, _, code := ctl.run("stop")
			convey.So(code, convey.ShouldEqual, 0)
			convey.So(stdout, convey.ShouldContainSubstring, "is not running")
			convey.So(runDevPIDIsAliveForTest(pgid), convey.ShouldBeTrue)
			convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
		}

		ctl.writePIDFile(fmt.Sprintf("%d %s 0123456789abcdef\n", pgid, host))

		stdout, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "Started the wa production stack")
		convey.So(stdout, convey.ShouldNotContainSubstring, "restarting")
		convey.So(ctl.pid(), convey.ShouldNotEqual, pgid)

		_, _, code = ctl.run("stop")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, 5*time.Second), convey.ShouldBeTrue)
		convey.So(runDevPIDIsAliveForTest(pgid), convey.ShouldBeTrue)
	})

	convey.Convey("prod-ctl.sh refuses every action, keeping the pid file, when it records a stack on another host", t, func() {
		ctl := newProdCtlForTest(t, nil)
		foreign := startProdCtlForeignGroupForTest(t, filepath.Join(ctl.repoRoot, "frontend"), os.Environ(), "exec sleep 600")
		body := fmt.Sprintf("%d other-host.example 0123456789abcdef\n", foreign.Process.Pid)
		ctl.writePIDFile(body)

		for _, action := range []string{"start", "status", "stop", "restart"} {
			stdout, stderr, code := ctl.run(action)
			convey.So(code, convey.ShouldNotEqual, 0)
			convey.So(stdout, convey.ShouldBeEmpty)
			convey.So(stderr, convey.ShouldContainSubstring, "other-host.example")
			convey.So(readProdCtlFileForTest(ctl.pidPath()), convey.ShouldEqual, body)
		}

		convey.So(runDevPIDIsAliveForTest(foreign.Process.Pid), convey.ShouldBeTrue)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, time.Second), convey.ShouldBeTrue)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.results, time.Second), convey.ShouldBeTrue)

		// The cleanup stop is refused too, so let it find nothing.
		convey.So(os.Remove(ctl.pidPath()), convey.ShouldBeNil)
	})
}

func TestProdCtlUnrecordedStack(t *testing.T) {
	convey.Convey("with its pid file moved aside, prod-ctl.sh stop, start, and restart leave a running stack and its log alone", t, func() {
		ctl := newProdCtlForTest(t, nil)

		_, _, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		pid := ctl.pid()

		aside := ctl.pidPath() + ".aside"
		convey.So(os.Rename(ctl.pidPath(), aside), convey.ShouldBeNil)
		t.Cleanup(func() { _ = os.Rename(aside, ctl.pidPath()) })

		logBefore := readProdCtlFileForTest(ctl.logPath())
		convey.So(logBefore, convey.ShouldContainSubstring, "Development environment is ready.")

		stdout, stderr, code := ctl.run("stop")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "is not running")
		convey.So(stderr, convey.ShouldContainSubstring, prodCtlUnrecordedPortMessageForTest(ctl.ports.frontend))

		for _, action := range []string{"start", "restart"} {
			stdout, stderr, code = ctl.run(action)
			convey.So(code, convey.ShouldNotEqual, 0)
			convey.So(stdout, convey.ShouldNotContainSubstring, "Started")
			convey.So(stderr, convey.ShouldContainSubstring, prodCtlUnrecordedPortMessageForTest(ctl.ports.frontend))
			convey.So(runDevPathExistsForTest(ctl.logPath()+".1"), convey.ShouldBeFalse)
			convey.So(readProdCtlFileForTest(ctl.logPath()), convey.ShouldStartWith, logBefore)
			convey.So(runDevPathExistsForTest(ctl.pidPath()), convey.ShouldBeFalse)
		}

		convey.So(runDevPIDIsAliveForTest(pid), convey.ShouldBeTrue)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.results, time.Second), convey.ShouldBeFalse)
	})
}

func TestProdCtlUnrecordedResultsPort(t *testing.T) {
	convey.Convey("prod-ctl.sh stop reports, and start refuses before rotating its log, when an unrecorded process holds only the results port", t, func() {
		ctl := newProdCtlForTest(t, nil)
		stub := filepath.Join(ctl.repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")
		env := append(os.Environ(), "WA_RUN_DEV_ENV_SNAPSHOT="+filepath.Join(t.TempDir(), "occupier-env.json"))
		startProdCtlForeignGroupForTest(t, t.TempDir(), env, `exec node "$1" "$2"`, stub, strconv.Itoa(ctl.ports.results))
		waitForTCPPortForTest(t, ctl.ports.results)

		previousLog := "the previous run's log\n"
		convey.So(os.WriteFile(ctl.logPath(), []byte(previousLog), 0o600), convey.ShouldBeNil)

		stdout, stderr, code := ctl.run("stop")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "is not running")
		convey.So(stderr, convey.ShouldContainSubstring, prodCtlUnrecordedPortMessageForTest(ctl.ports.results))

		stdout, stderr, code = ctl.run("start")
		convey.So(code, convey.ShouldNotEqual, 0)
		convey.So(stdout, convey.ShouldNotContainSubstring, "Started")
		convey.So(stderr, convey.ShouldContainSubstring, prodCtlUnrecordedPortMessageForTest(ctl.ports.results))
		convey.So(readProdCtlFileForTest(ctl.logPath()), convey.ShouldEqual, previousLog)
		convey.So(runDevPathExistsForTest(ctl.logPath()+".1"), convey.ShouldBeFalse)
		convey.So(runDevTCPPortClosesWithinForTest(ctl.ports.frontend, time.Second), convey.ShouldBeTrue)
	})
}

func TestProdCtlStartingStack(t *testing.T) {
	convey.Convey("prod-ctl.sh start silently leaves a stack that is still starting alone, and replaces it once WA_PROD_START_TIMEOUT_SECONDS has passed", t, func() {
		ctl := newProdCtlForTest(t, map[string]string{"WA_RUN_DEV_FRONTEND_DEV_CMD": "sleep 600"})
		pid := ctl.interruptStart()
		launched := time.Now()
		convey.So(runDevPIDIsAliveForTest(pid), convey.ShouldBeTrue)
		convey.So(readProdCtlFileForTest(ctl.logPath()), convey.ShouldNotContainSubstring, "Development environment is ready.")

		stdout, stderr, code := ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldBeEmpty)
		convey.So(stderr, convey.ShouldBeEmpty)
		convey.So(time.Since(launched), convey.ShouldBeLessThan, 10*time.Second)
		convey.So(ctl.pid(), convey.ShouldEqual, pid)
		convey.So(runDevPIDIsAliveForTest(pid), convey.ShouldBeTrue)

		const startTimeoutSeconds = 15

		ctl.setEnv("WA_PROD_START_TIMEOUT_SECONDS", strconv.Itoa(startTimeoutSeconds))
		ctl.setEnv("WA_PROD_HEALTH_ATTEMPTS", "1")
		ctl.setEnv("WA_RUN_DEV_FRONTEND_DEV_CMD", runDevFrontendStubCommandForTest(ctl.repoRoot, ctl.ports.frontend))
		time.Sleep(time.Until(launched.Add((startTimeoutSeconds + 1) * time.Second)))

		stdout, _, code = ctl.run("start")
		convey.So(code, convey.ShouldEqual, 0)
		convey.So(stdout, convey.ShouldContainSubstring, "restarting")
		convey.So(stdout, convey.ShouldContainSubstring, "Started the wa production stack")
		convey.So(ctl.pid(), convey.ShouldNotEqual, pid)
		convey.So(runDevPIDExitsWithinForTest(pid, 5*time.Second), convey.ShouldBeTrue)
	})
}

func runDevFrontendStubCommandForTest(repoRoot string, port int) string {
	return fmt.Sprintf(`node %q %d`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"), port)
}

func TestRunDevProdFrontend(t *testing.T) {
	convey.Convey("run-dev.sh --mode prod builds the frontend, then serves the production build over HTTPS with the dev TLS cert and key", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		ports := runDevFeedbackPortsForTest(t)
		binDir := t.TempDir()
		pnpmLogPath := filepath.Join(t.TempDir(), "pnpm-args.txt")
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		writeRunDevMLWHServeToolchainForTest(t, binDir, filepath.Join(t.TempDir(), "wa-invocations.log"))
		writeRunDevProdPnpmStubForTest(t, repoRoot, binDir, pnpmLogPath, 0)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:         "prod",
			frontendPort: ports.frontend,
			resultsPort:  ports.results,
			seqmetaPort:  ports.seqmeta,
			unsetEnv:     append(runDevUnsetProdRefusedEnvForTest(), "WA_RUN_DEV_FRONTEND_DEV_CMD"),
			env: runDevProdEnvForTest(t, binDir, ports, map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT": snapshotPath,
			}),
		})

		waitForRunDevSnapshotForTest(t, process, snapshotPath)
		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		certPath, keyPath := runDevTLSCertPathsForTest(t)
		convey.So(waitForRunDevStepsForTest(t, pnpmLogPath, 2), convey.ShouldResemble, []string{
			"build",
			fmt.Sprintf("start:https --port %d --key %s --cert %s", ports.frontend, keyPath, certPath),
		})
		convey.So(process.stdout.String(), convey.ShouldContainSubstring,
			fmt.Sprintf("Starting frontend production server on https://127.0.0.1:%d", ports.frontend))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh --mode prod stops without starting the frontend when the production build fails", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		ports := runDevFeedbackPortsForTest(t)
		binDir := t.TempDir()
		pnpmLogPath := filepath.Join(t.TempDir(), "pnpm-args.txt")

		writeRunDevMLWHServeToolchainForTest(t, binDir, filepath.Join(t.TempDir(), "wa-invocations.log"))
		writeRunDevProdPnpmStubForTest(t, repoRoot, binDir, pnpmLogPath, 1)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:         "prod",
			frontendPort: ports.frontend,
			resultsPort:  ports.results,
			seqmetaPort:  ports.seqmeta,
			unsetEnv:     append(runDevUnsetProdRefusedEnvForTest(), "WA_RUN_DEV_FRONTEND_DEV_CMD"),
			env:          runDevProdEnvForTest(t, binDir, ports, nil),
		})

		convey.So(process.ExitedWithin(60*time.Second), convey.ShouldBeTrue)
		convey.So(process.Wait(), convey.ShouldNotBeNil)
		convey.So(process.Stderr(), convey.ShouldContainSubstring, "frontend production build failed")
		convey.So(process.Stderr(), convey.ShouldContainSubstring, "stub build failed")
		convey.So(waitForRunDevStepsForTest(t, pnpmLogPath, 1), convey.ShouldResemble, []string{"build"})
		convey.So(runDevTCPPortClosesWithinForTest(ports.results, 5*time.Second), convey.ShouldBeTrue)
	})
}

// writeRunDevProdPnpmStubForTest writes a pnpm that records its arguments,
// exits buildExit for "build", and serves the frontend stub for "start:https".
func writeRunDevProdPnpmStubForTest(t *testing.T, repoRoot string, binDir string, logPath string, buildExit int) {
	t.Helper()

	stub := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

printf '%%s\n' "$*" >> %q

case "${1:-}" in
	build)
		if [[ %d -ne 0 ]]; then
			printf 'stub build failed\n' >&2
		fi
		exit %d
		;;
	start:https)
		exec node %q "$3"
		;;
esac

printf 'unexpected pnpm args: %%s\n' "$*" >&2
exit 2
`, logPath, buildExit, buildExit, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"))

	convey.So(os.WriteFile(filepath.Join(binDir, "pnpm"), []byte(stub), 0o755), convey.ShouldBeNil)
}

func TestRunDevProdRemoteMLWH(t *testing.T) {
	convey.Convey("run-dev.sh --mode prod with a remote prefixed WA_MLWH_SERVER_URL starts no local MLWH and passes the URL to results serve and the frontend", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		ports := runDevFeedbackPortsForTest(t)
		binDir := t.TempDir()
		invocationsPath := filepath.Join(t.TempDir(), "wa-invocations.log")
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		writeRunDevMLWHServeToolchainForTest(t, binDir, invocationsPath)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:         "prod",
			frontendPort: ports.frontend,
			resultsPort:  ports.results,
			seqmetaPort:  ports.seqmeta,
			unsetEnv:     runDevUnsetProdRefusedEnvForTest(),
			env: runDevProdEnvForTest(t, binDir, ports, map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":     snapshotPath,
				"WA_RUN_DEV_FRONTEND_DEV_CMD": runDevFrontendStubCommandForTest(repoRoot, ports.frontend),
			}),
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)
		invocations := strings.Join(waitForRunDevStepsForTest(t, invocationsPath, 1), "\n")
		resultsMLWHURL, err := os.ReadFile(invocationsPath + ".results-mlwh-server-url")
		convey.So(err, convey.ShouldBeNil)

		convey.So(snapshot.MLWHBackendURL, convey.ShouldEqual, runDevProdRemoteMLWHURLForTest)
		convey.So(snapshot.MLWHCachePath, convey.ShouldBeEmpty)
		convey.So(strings.TrimSpace(string(resultsMLWHURL)), convey.ShouldEqual, runDevProdRemoteMLWHURLForTest)
		convey.So(invocations, convey.ShouldContainSubstring, "results serve")
		convey.So(invocations, convey.ShouldNotContainSubstring, "mlwh serve")
		convey.So(invocations, convey.ShouldNotContainSubstring, "mlwh sync")
		convey.So(process.stdout.String(), convey.ShouldContainSubstring, "MLWH: "+runDevProdRemoteMLWHURLForTest)
		convey.So(process.stdout.String(), convey.ShouldNotContainSubstring, "MLWH bind:")
		convey.So(runDevTCPPortClosesWithinForTest(ports.seqmeta, time.Second), convey.ShouldBeTrue)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func runDevProdEnvForTest(t *testing.T, binDir string, ports runDevFeedbackPorts, extra map[string]string) map[string]string {
	t.Helper()

	env := map[string]string{
		"PATH":                           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"WA_ENV":                         "production",
		"WA_RESULTS_DB_PATH":             filepath.Join(t.TempDir(), "results-prod.sqlite"),
		"WA_MLWH_SERVER_URL":             runDevProdRemoteMLWHURLForTest,
		"WA_RUN_DEV_RESULTS_HEALTH_URL":  fmt.Sprintf("http://127.0.0.1:%d/rest/v1/results/stats", ports.results),
		"WA_RUN_DEV_FRONTEND_HEALTH_URL": fmt.Sprintf("http://127.0.0.1:%d/api/health", ports.frontend),
	}

	for key, value := range extra {
		env[key] = value
	}

	return env
}
