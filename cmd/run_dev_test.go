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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/results"
)

const (
	runDevDefaultFrontendHealthAttemptsForTest = 120
	runDevDefaultSeqmetaHealthAttemptsForTest  = 1200
	runDevHealthPollIntervalForTest            = 250 * time.Millisecond
	runDevSnapshotStartupGraceForTest          = 45 * time.Second
)

type runDevEnvSnapshot struct {
	ResultsBackendURL string `json:"WA_RESULTS_BACKEND_URL"`
	ResultsCACert     string `json:"WA_RESULTS_BACKEND_CA_CERT"`
	ResultsServerCert string `json:"WA_RESULTS_SERVER_CERT"`
	ResultsServerKey  string `json:"WA_RESULTS_SERVER_KEY"`
	MLWHBackendURL    string `json:"WA_MLWH_BACKEND_URL"`
	ResultsDBPath     string `json:"WA_RESULTS_DB_PATH"`
	MLWHCachePath     string `json:"WA_MLWH_CACHE_PATH"`
	AllowedDevOrigins string `json:"WA_DEV_ALLOWED_ORIGINS"`
}

func TestRunDevAutoManagedMLWHBackendUsesMLWHServe(t *testing.T) {
	convey.Convey("run-dev.sh starts mlwh serve behind WA_MLWH_BACKEND_URL when it auto-manages the backend", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		invocationsPath := filepath.Join(t.TempDir(), "wa-invocations.log")
		binDir := t.TempDir()

		writeRunDevMLWHServeToolchainForTest(t, binDir, invocationsPath)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:         "dev",
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			seqmetaPort:  seqmetaPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
			env: map[string]string{
				"PATH":                                  binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"WA_ENV":                                "development",
				"WA_RESULTS_DB_PATH":                    filepath.Join(t.TempDir(), "results-dev.sqlite"),
				"WA_MLWH_DSN":                           "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
				"WA_RESULTS_LDAP_SERVER":                "ldap.example.org",
				"WA_RESULTS_LDAP_DN":                    "uid=%s,ou=people,dc=example,dc=org",
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_RESULTS_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/rest/v1/results/stats", resultsPort),
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q %d`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"), frontendPort),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		invocations := strings.Join(waitForRunDevStepsForTest(t, invocationsPath, 2), "\n")

		convey.So(snapshot.MLWHBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", seqmetaPort))
		convey.So(invocations, convey.ShouldContainSubstring, fmt.Sprintf("mlwh serve --port %d", seqmetaPort))
		convey.So(invocations, convey.ShouldNotContainSubstring, "mlwhdiff serve")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func writeRunDevMLWHServeToolchainForTest(t *testing.T, binDir string, invocationsPath string) {
	t.Helper()

	fakeGo := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "build" && "${2:-}" == "-o" && -n "${3:-}" ]]; then
	output="$3"
	mkdir -p "$(dirname "$output")"
	cat >"$output" <<'WAEOF'
#!/usr/bin/env bash
set -euo pipefail

invocations_path=%q

port_arg() {
	local previous=""
	local arg

	for arg in "$@"; do
		if [[ "$previous" == "--port" ]]; then
			printf '%%s' "$arg"
			return
		fi

		case "$arg" in
			--port=*)
				printf '%%s' "${arg#*=}"
				return
				;;
		esac

		previous="$arg"
	done

	printf '0'
}

printf '%%s\n' "$*" >> "$invocations_path"

case "${1:-} ${2:-}" in
	"results serve")
		port="$(port_arg "$@")"
		exec node -e 'const http = require("node:http"); const port = Number(process.argv[1]); const server = http.createServer((_, response) => { response.writeHead(200, {"content-type":"application/json"}); response.end("{}"); }); const shutdown = () => server.close(() => process.exit(0)); process.on("SIGINT", shutdown); process.on("SIGTERM", shutdown); server.listen(port, "127.0.0.1");' "$port"
		;;
	"mlwh serve")
		port="$(port_arg "$@")"
		exec node -e 'const http = require("node:http"); const port = Number(process.argv[1]); const server = http.createServer((request, response) => { if (request.url === "/studies") { response.writeHead(200, {"content-type":"application/json"}); response.end("[]"); return; } response.writeHead(404); response.end(); }); const shutdown = () => server.close(() => process.exit(0)); process.on("SIGINT", shutdown); process.on("SIGTERM", shutdown); server.listen(port, "127.0.0.1");' "$port"
		;;
esac

printf 'unexpected fake wa args: %%s\n' "$*" >&2
exit 2
WAEOF
	chmod +x "$output"
	exit 0
fi

printf 'unexpected fake go args: %%s\n' "$*" >&2
exit 2
`, invocationsPath)

	convey.So(os.WriteFile(filepath.Join(binDir, "go"), []byte(fakeGo), 0o755), convey.ShouldBeNil)
}

func runDevUnsetSeqmetaEnvForTest() []string {
	return []string{"WA_RUN_DEV_SEQMETA_CMD", "WA_RUN_DEV_SEQMETA_HEALTH_URL"}
}

func TestRunDevHelpDocumentsProdRefusedEnvironment(t *testing.T) {
	convey.Convey("run-dev.sh --help documents the prod-mode refused environment variables", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		command := exec.Command("bash", filepath.Join(repoRoot, "run-dev.sh"), "--help") //nolint:gosec
		command.Dir = repoRoot
		command.Env = runDevEnvForTest(nil)
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		command.Stdout = stdout
		command.Stderr = stderr

		err := command.Run()
		output := stdout.String()

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldEqual, "")
		convey.So(output, convey.ShouldContainSubstring, "Refuses these inherited environment variables")

		for _, variable := range []string{
			"WA_TEST_FRONTEND_PORT",
			"WA_TEST_RESULTS_PORT",
			"WA_TEST_SEQMETA_PORT",
			"WA_TEST_RESULTS_HOST",
			"WA_DEV_FRONTEND_PORT",
			"WA_DEV_RESULTS_PORT",
			"WA_DEV_SEQMETA_PORT",
			"WA_DEV_RESULTS_HOST",
		} {
			convey.So(output, convey.ShouldContainSubstring, variable)
		}
	})
}

func TestRunDevSnapshotWaitAllowsSlowFrontendStartup(t *testing.T) {
	convey.Convey("run-dev.sh test harness waits for a frontend snapshot after slow startup prerequisites", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		frontendScriptPath := filepath.Join(t.TempDir(), "delayed-frontend")

		frontendScript := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

sleep 21
exec node %q "$WA_TEST_FRONTEND_PORT"
`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"))
		convey.So(os.WriteFile(frontendScriptPath, []byte(frontendScript), 0o755), convey.ShouldBeNil)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":                 snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD":   `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":            `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":            `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":             fmt.Sprintf("bash %q", frontendScriptPath),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":          fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS": "180",
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)

		convey.So(snapshot.ResultsBackendURL, convey.ShouldEqual, fmt.Sprintf("https://127.0.0.1:%d", resultsPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func runDevEnsureTLSCertificateForTest(t *testing.T, repoRoot string) (string, string) {
	t.Helper()

	certPath, keyPath := runDevTLSCertPathsForTest(repoRoot)
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	hosts = append(hosts, runDevHostnameForTest(t, "-f"), runDevHostnameForTest(t, "-s"))
	runDevWriteTLSCertificateForHostnamesForTest(t, certPath, keyPath, hosts)

	return certPath, keyPath
}

func runDevTLSCertPathsForTest(repoRoot string) (string, string) {
	return filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"),
		filepath.Join(repoRoot, ".tmp", "wa-dev-key.pem")
}

func TestRunDevScriptF1DevCertificates(t *testing.T) {
	convey.Convey("F1.1: Given run-dev.sh --mode dev with LDAP configured, then Next receives an HTTPS backend URL and existing CA PEM", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:         "dev",
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			seqmetaPort:  seqmetaPort,
			env: map[string]string{
				"WA_ENV":                                "development",
				"WA_RESULTS_DB_PATH":                    filepath.Join(t.TempDir(), "results-dev.sqlite"),
				"WA_MLWH_DSN":                           "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
				"WA_RESULTS_LDAP_SERVER":                "ldap.example.org",
				"WA_RESULTS_LDAP_DN":                    "uid=%s,ou=people,dc=example,dc=org",
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q %d`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"), frontendPort),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_SEQMETA_CMD":                fmt.Sprintf(`node -e "require('node:http').createServer((_, response) => { response.writeHead(200, {'content-type':'application/json'}); response.end('[]'); }).listen(%d, '127.0.0.1')"`, seqmetaPort),
				"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/studies", seqmetaPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)

		convey.So(snapshot.ResultsBackendURL, convey.ShouldEqual, fmt.Sprintf("https://127.0.0.1:%d", resultsPort))
		convey.So(snapshot.ResultsCACert, convey.ShouldEqual, filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"))
		convey.So(runDevPathExistsForTest(snapshot.ResultsCACert), convey.ShouldBeTrue)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("F1.2: Given test mode, results serve gets explicit test-only LDAP flags and no LDAP bind is needed for startup", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     append(runDevUnsetSeqmetaEnvForTest(), "WA_RESULTS_LDAP_SERVER", "WA_RESULTS_LDAP_DN"),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		cmdline := waitForRunDevChildCommandLineForTest(t, process.Command.Process.Pid, " results serve ")

		convey.So(snapshot.ResultsServerCert, convey.ShouldEqual, filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"))
		convey.So(snapshot.ResultsServerKey, convey.ShouldEqual, filepath.Join(repoRoot, ".tmp", "wa-dev-key.pem"))
		convey.So(cmdline, convey.ShouldNotContainSubstring, " --cert ")
		convey.So(cmdline, convey.ShouldNotContainSubstring, " --key ")
		convey.So(cmdline, convey.ShouldContainSubstring, " --mlwh-cache ")
		convey.So(cmdline, convey.ShouldContainSubstring, snapshot.MLWHCachePath)
		convey.So(cmdline, convey.ShouldContainSubstring, " --ldap_server wa-test-ldap.invalid ")
		convey.So(cmdline, convey.ShouldContainSubstring, " --ldap_dn uid=%s,ou=people,dc=example,dc=org ")
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "ldap")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("F1.3: Given development mode without LDAP env, results serve exits with the LDAP requirement", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)

		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{
			"--mode", "dev",
			"--frontend-port", fmt.Sprintf("%d", frontendPort),
			"--results-port", fmt.Sprintf("%d", resultsPort),
			"--seqmeta-port", fmt.Sprintf("%d", seqmetaPort),
		}, map[string]string{
			"WA_ENV":                                "development",
			"WA_RESULTS_DB_PATH":                    filepath.Join(t.TempDir(), "results-dev.sqlite"),
			"WA_RESULTS_LDAP_DN":                    "",
			"WA_RESULTS_LDAP_SERVER":                "",
			"WA_MLWH_DSN":                           "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
			"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
			"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
			"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
			"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
			"WA_RUN_DEV_SEQMETA_CMD":                runDevSeqmetaStubCommandForTest(),
		}, []string{"WA_RESULTS_LDAP_SERVER", "WA_RESULTS_LDAP_DN"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout, convey.ShouldContainSubstring, "Starting results server")
		convey.So(stderr, convey.ShouldContainSubstring, "--ldap_server and --ldap_dn are required")
	})
}

func runDevSeqmetaStubCommandForTest() string {
	return `node -e "require('node:http').createServer((_, response) => { response.writeHead(200, {'content-type':'application/json'}); response.end('[]'); }).listen(Number(process.env.WA_TEST_SEQMETA_PORT), '127.0.0.1')"`
}

func runDevWriteTLSCertificateForHostnamesForTest(t *testing.T, certPath string, keyPath string, hosts []string) {
	t.Helper()

	convey.So(os.MkdirAll(filepath.Dir(certPath), 0o755), convey.ShouldBeNil)
	convey.So(os.MkdirAll(filepath.Dir(keyPath), 0o755), convey.ShouldBeNil)

	configPath := filepath.Join(t.TempDir(), "openssl.cnf")
	convey.So(os.WriteFile(configPath, []byte(runDevOpenSSLConfigForHostnamesForTest(hosts)), 0o600), convey.ShouldBeNil)

	command := exec.Command(
		"openssl",
		"req",
		"-x509",
		"-newkey",
		"rsa:2048",
		"-nodes",
		"-days",
		"7",
		"-keyout",
		keyPath,
		"-out",
		certPath,
		"-config",
		configPath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate TLS certificate: %v\n%s", err, output)
	}

	convey.So(os.Chmod(keyPath, 0o600), convey.ShouldBeNil)
	convey.So(os.Chmod(certPath, 0o644), convey.ShouldBeNil)
}

func runDevOpenSSLConfigForHostnamesForTest(hosts []string) string {
	var builder strings.Builder
	seen := make(map[string]struct{}, len(hosts))
	dnsCount := 0
	ipCount := 0

	builder.WriteString("[req]\n")
	builder.WriteString("distinguished_name = req_distinguished_name\n")
	builder.WriteString("x509_extensions = v3_req\n")
	builder.WriteString("prompt = no\n")
	builder.WriteString("[req_distinguished_name]\n")
	builder.WriteString("CN = localhost\n")
	builder.WriteString("[v3_req]\n")
	builder.WriteString("subjectAltName = @alt_names\n")
	builder.WriteString("[alt_names]\n")

	for _, host := range hosts {
		host = strings.TrimSpace(strings.Trim(strings.TrimSuffix(host, "."), "[]"))
		if host == "" || host == "0.0.0.0" || host == "::" {
			continue
		}

		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}

		if net.ParseIP(host) != nil {
			ipCount++
			fmt.Fprintf(&builder, "IP.%d = %s\n", ipCount, host)
			continue
		}

		dnsCount++
		fmt.Fprintf(&builder, "DNS.%d = %s\n", dnsCount, host)
	}

	return builder.String()
}

func runDevHTTPSClientForTest(t *testing.T) *http.Client {
	t.Helper()

	certPath, _ := runDevTLSCertPathsForTest(runDevRepoRootForTest(t))
	caPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read run-dev CA cert %s: %v", certPath, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("parse run-dev CA cert %s", certPath)
	}

	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				RootCAs:    pool,
			},
		},
	}
}

func runDevOwnerJWTForTest(t *testing.T, resultsPort int) string {
	t.Helper()

	repoRoot := runDevRepoRootForTest(t)
	tokenPath := filepath.Join(repoRoot, ".tmp", "state-test", ".wa-results-server.token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("read run-dev server token %s: %v", tokenPath, err)
	}

	currentUser, err := osuser.Current()
	if err != nil {
		t.Fatalf("get current user: %v", err)
	}

	form := url.Values{}
	form.Set("username", currentUser.Username)
	form.Set("password", strings.TrimSpace(string(token)))

	endpoint := fmt.Sprintf("https://127.0.0.1:%d/rest/v1/jwt", resultsPort)
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build JWT request for %s: %v", endpoint, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := runDevHTTPSClientForTest(t).Do(request)
	if err != nil {
		t.Fatalf("login to run-dev results server at %s: %v", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("JWT request returned %d and unreadable body: %v", response.StatusCode, readErr)
		}

		t.Fatalf("JWT request returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	var jwt string
	if err := json.NewDecoder(response.Body).Decode(&jwt); err != nil {
		t.Fatalf("decode JWT response: %v", err)
	}

	return jwt
}

type runDevCertificateOptionsForTest struct {
	certPath          string
	keyPath           string
	hostnameFQDN      string
	hostnameShort     string
	devAllowedOrigins string
	legacyBackendURL  string
	publicResultsURL  string
}

func TestRunDevScriptDevCertificateSubjectAltNames(t *testing.T) {
	convey.Convey("Bug 2: run-dev.sh generates a dev certificate trusted for local and public hostnames", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		certPath := filepath.Join(t.TempDir(), "wa-dev-cert.pem")
		keyPath := filepath.Join(t.TempDir(), "wa-dev-key.pem")
		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:         certPath,
			keyPath:          keyPath,
			hostnameFQDN:     "farm22-wrstat01.internal",
			hostnameShort:    "farm22-wrstat01",
			legacyBackendURL: "https://legacy-results.example.org:3671",
			publicResultsURL: "https://results-dev.example.org:3672",
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		cert := runDevReadCertificateForTest(t, certPath)
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "localhost")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "127.0.0.1")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "farm22-wrstat01.internal")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "farm22-wrstat01")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "legacy-results.example.org")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "results-dev.example.org")
		convey.So(cert.VerifyHostname("0.0.0.0"), convey.ShouldNotBeNil)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("Bug 2: run-dev.sh regenerates an existing localhost-only dev certificate when remote SANs are missing", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		certPath := filepath.Join(t.TempDir(), "wa-dev-cert.pem")
		keyPath := filepath.Join(t.TempDir(), "wa-dev-key.pem")
		runDevWriteLocalhostOnlyTLSCertificateForTest(t, certPath, keyPath)
		oldCert := runDevReadCertificateForTest(t, certPath)
		convey.So(oldCert.VerifyHostname("farm22-wrstat01"), convey.ShouldNotBeNil)

		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:      certPath,
			keyPath:       keyPath,
			hostnameFQDN:  "farm22-wrstat01.internal",
			hostnameShort: "farm22-wrstat01",
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		regeneratedCert := runDevReadCertificateForTest(t, certPath)
		convey.So(bytes.Equal(regeneratedCert.Raw, oldCert.Raw), convey.ShouldBeFalse)
		runDevAssertCertificateVerifiesHostnameForTest(t, regeneratedCert, "farm22-wrstat01")
		runDevAssertCertificateVerifiesHostnameForTest(t, regeneratedCert, "farm22-wrstat01.internal")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh does not expand wildcard allowed origins into dev certificate SANs", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		certPath := filepath.Join(t.TempDir(), "wa-dev-cert.pem")
		keyPath := filepath.Join(t.TempDir(), "wa-dev-key.pem")
		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:          certPath,
			keyPath:           keyPath,
			hostnameFQDN:      "farm22-wrstat01.internal",
			hostnameShort:     "farm22-wrstat01",
			devAllowedOrigins: "*, allowed-extra.example.org",
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		cert := runDevReadCertificateForTest(t, certPath)
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "allowed-extra.example.org")
		convey.So(cert.VerifyHostname("run-dev.sh"), convey.ShouldNotBeNil)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh strips allowed-origin ports before creating dev certificate SANs", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		certPath := filepath.Join(t.TempDir(), "wa-dev-cert.pem")
		keyPath := filepath.Join(t.TempDir(), "wa-dev-key.pem")
		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:      certPath,
			keyPath:       keyPath,
			hostnameFQDN:  "farm22-wrstat01.internal",
			hostnameShort: "farm22-wrstat01",
			devAllowedOrigins: strings.Join([]string{
				"dev-host.example.org:3672",
				"2001:db8::42",
				"[2001:db8::43]:3673",
				"bad-origin.example.org/path",
				"*.wildcard.example.org",
			}, ", "),
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		cert := runDevReadCertificateForTest(t, certPath)
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "dev-host.example.org")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "2001:db8::42")
		runDevAssertCertificateVerifiesHostnameForTest(t, cert, "2001:db8::43")
		convey.So(slices.Contains(cert.DNSNames, "dev-host.example.org"), convey.ShouldBeTrue)
		convey.So(slices.Contains(cert.DNSNames, "dev-host.example.org:3672"), convey.ShouldBeFalse)
		convey.So(slices.Contains(cert.DNSNames, "bad-origin.example.org"), convey.ShouldBeFalse)
		convey.So(cert.VerifyHostname("bad-origin.example.org"), convey.ShouldNotBeNil)
		convey.So(cert.VerifyHostname("leaf.wildcard.example.org"), convey.ShouldNotBeNil)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func startRunDevForDevCertificateTest(
	t *testing.T,
	repoRoot string,
	options runDevCertificateOptionsForTest,
) (*runDevProcess, string) {
	t.Helper()

	frontendPort := runDevFreePortForTest(t)
	resultsPort := runDevFreePortForTest(t)
	seqmetaPort := runDevFreePortForTest(t)
	snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
	binDir := t.TempDir()

	writeRunDevHostnameStubForTest(t, binDir, options.hostnameFQDN, options.hostnameShort)

	env := map[string]string{
		"PATH":                                  binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"WA_ENV":                                "development",
		"WA_DEV_RESULTS_HOST":                   "0.0.0.0",
		"WA_RESULTS_SERVER_CERT":                options.certPath,
		"WA_RESULTS_SERVER_KEY":                 options.keyPath,
		"WA_RESULTS_DB_PATH":                    filepath.Join(t.TempDir(), "results-dev.sqlite"),
		"WA_MLWH_DSN":                           "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
		"WA_RESULTS_LDAP_SERVER":                "ldap.example.org",
		"WA_RESULTS_LDAP_DN":                    "uid=%s,ou=people,dc=example,dc=org",
		"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
		"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
		"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
		"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
		"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
		"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q %d`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"), frontendPort),
		"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
		"WA_RUN_DEV_SEQMETA_CMD":                fmt.Sprintf(`node -e "require('node:http').createServer((_, response) => { response.writeHead(200, {'content-type':'application/json'}); response.end('[]'); }).listen(%d, '127.0.0.1')"`, seqmetaPort),
		"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/studies", seqmetaPort),
	}
	if options.publicResultsURL != "" {
		env["WA_RESULTS_SERVER_URL"] = options.publicResultsURL
	}
	if options.legacyBackendURL != "" {
		env["WA_RESULTS_BACKEND_URL"] = options.legacyBackendURL
	}
	if options.devAllowedOrigins != "" {
		env["WA_DEV_ALLOWED_ORIGINS"] = options.devAllowedOrigins
	}

	process := startRunDevForTest(t, repoRoot, runDevStartOptions{
		mode:         "dev",
		frontendPort: frontendPort,
		resultsPort:  resultsPort,
		seqmetaPort:  seqmetaPort,
		unsetEnv:     []string{"WA_DEV_RESULTS_HOST", "WA_RESULTS_SERVER_URL", "WA_RESULTS_BACKEND_URL", "WA_DEV_ALLOWED_ORIGINS"},
		env:          env,
	})

	return process, snapshotPath
}

func writeRunDevHostnameStubForTest(t *testing.T, binDir string, fqdn string, short string) {
	t.Helper()

	realHostname, err := exec.LookPath("hostname")
	convey.So(err, convey.ShouldBeNil)

	stub := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  -f) printf '%%s\n' %q ;;
  -s) printf '%%s\n' %q ;;
  *) exec %q "$@" ;;
esac
`, fqdn, short, realHostname)

	convey.So(os.WriteFile(filepath.Join(binDir, "hostname"), []byte(stub), 0o755), convey.ShouldBeNil)
}

func runDevReadCertificateForTest(t *testing.T, certPath string) *x509.Certificate {
	t.Helper()

	body, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read certificate %s: %v", certPath, err)
	}

	block, _ := pem.Decode(body)
	if block == nil {
		t.Fatalf("parse PEM certificate %s", certPath)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate %s: %v", certPath, err)
	}

	return cert
}

func runDevAssertCertificateVerifiesHostnameForTest(t *testing.T, cert *x509.Certificate, hostname string) {
	t.Helper()

	convey.So(cert.VerifyHostname(hostname), convey.ShouldBeNil)
}

func runDevWriteLocalhostOnlyTLSCertificateForTest(t *testing.T, certPath string, keyPath string) {
	t.Helper()

	runDevWriteTLSCertificateForHostnamesForTest(t, certPath, keyPath, []string{"localhost", "127.0.0.1"})
}

func startRunDevForResultsBindOutputTest(t *testing.T, bindHost string) (*runDevProcess, int, int) {
	t.Helper()

	return startRunDevForScenarioResultsBindOutputTest(t, "dev", bindHost)
}

func startRunDevForScenarioResultsBindOutputTest(
	t *testing.T,
	mode string,
	bindHost string,
) (*runDevProcess, int, int) {
	t.Helper()

	repoRoot := runDevRepoRootForTest(t)
	frontendPort := runDevFreePortForTest(t)
	resultsPort := runDevFreePortForTest(t)
	seqmetaPort := runDevFreePortForTest(t)
	snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
	env := map[string]string{
		"WA_ENV":                                "development",
		"WA_RESULTS_DB_PATH":                    filepath.Join(t.TempDir(), fmt.Sprintf("results-%s.sqlite", mode)),
		"WA_RESULTS_LDAP_SERVER":                "ldap.example.org",
		"WA_RESULTS_LDAP_DN":                    "uid=%s,ou=people,dc=example,dc=org",
		"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
		"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
		"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
		"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
		"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
		"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q %d`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"), frontendPort),
		"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
	}

	bindHostEnv := "WA_DEV_RESULTS_HOST"
	switch mode {
	case "prod":
		mlwhCachePath := filepath.Join(repoRoot, ".tmp", fmt.Sprintf("run-dev-prod-mlwh-%d.sqlite", seqmetaPort))
		t.Cleanup(func() {
			for _, suffix := range []string{"", "-shm", "-wal"} {
				_ = os.Remove(mlwhCachePath + suffix)
			}
		})

		env["WA_ENV"] = "production"
		env["WA_MLWH_CACHE_PATH"] = mlwhCachePath
		bindHostEnv = "WA_PROD_RESULTS_HOST"
	case "dev":
		env["WA_MLWH_DSN"] = "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test"
		env["WA_RUN_DEV_SEQMETA_CMD"] = fmt.Sprintf(`node -e "require('node:http').createServer((_, response) => { response.writeHead(200, {'content-type':'application/json'}); response.end('[]'); }).listen(%d, '127.0.0.1')"`, seqmetaPort)
		env["WA_RUN_DEV_SEQMETA_HEALTH_URL"] = fmt.Sprintf("http://127.0.0.1:%d/studies", seqmetaPort)
	default:
		t.Fatalf("unsupported run-dev bind output test mode %q", mode)
	}

	if bindHost != "" {
		env[bindHostEnv] = bindHost
	}

	process := startRunDevForTest(t, repoRoot, runDevStartOptions{
		mode:         mode,
		frontendPort: frontendPort,
		resultsPort:  resultsPort,
		seqmetaPort:  seqmetaPort,
		unsetEnv: []string{
			"WA_TEST_FRONTEND_PORT",
			"WA_TEST_RESULTS_PORT",
			"WA_TEST_SEQMETA_PORT",
			"WA_TEST_RESULTS_HOST",
			"WA_DEV_FRONTEND_PORT",
			"WA_DEV_RESULTS_PORT",
			"WA_DEV_SEQMETA_PORT",
			"WA_DEV_RESULTS_HOST",
			"WA_PROD_RESULTS_HOST",
			"WA_RESULTS_SERVER_URL",
		},
		env: env,
	})

	return process, frontendPort, resultsPort
}

func TestRunDevScriptReportsResultsBindAddress(t *testing.T) {
	convey.Convey("run-dev.sh --mode dev reports wildcard results bind separately from local client URLs", t, func() {
		process, frontendPort, resultsPort := startRunDevForResultsBindOutputTest(t, "0.0.0.0")

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		stdout := process.stdout.String()
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Starting results server on https://127.0.0.1:%d (mode=dev; bind=0.0.0.0:%d)", resultsPort, resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results: https://127.0.0.1:%d", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results bind: 0.0.0.0:%d (listening beyond loopback)", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, "Results public: not configured")
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Frontend: https://127.0.0.1:%d", frontendPort))
		convey.So(stdout, convey.ShouldNotContainSubstring, "Frontend bind:")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh --mode dev trims the configured results bind host before reporting it", t, func() {
		process, frontendPort, resultsPort := startRunDevForResultsBindOutputTest(t, " \t0.0.0.0 \t")

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		stdout := process.stdout.String()
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Starting results server on https://127.0.0.1:%d (mode=dev; bind=0.0.0.0:%d)", resultsPort, resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results: https://127.0.0.1:%d", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results bind: 0.0.0.0:%d (listening beyond loopback)", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Frontend: https://127.0.0.1:%d", frontendPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh --mode prod trims the configured results bind host before reporting it", t, func() {
		process, frontendPort, resultsPort := startRunDevForScenarioResultsBindOutputTest(t, "prod", " \t0.0.0.0 \t")

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		stdout := process.stdout.String()
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Starting results server on https://127.0.0.1:%d (mode=prod; bind=0.0.0.0:%d)", resultsPort, resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results: https://127.0.0.1:%d", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results bind: 0.0.0.0:%d (listening beyond loopback)", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Frontend: https://127.0.0.1:%d", frontendPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh --mode dev falls back to loopback when the configured bind host trims blank", t, func() {
		process, frontendPort, resultsPort := startRunDevForResultsBindOutputTest(t, " \t ")

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		stdout := process.stdout.String()
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Starting results server on https://127.0.0.1:%d (mode=dev; bind=127.0.0.1:%d)", resultsPort, resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results: https://127.0.0.1:%d", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results bind: 127.0.0.1:%d (loopback only)", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Frontend: https://127.0.0.1:%d", frontendPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh --mode dev reports loopback-only results bind when no bind host is configured", t, func() {
		process, frontendPort, resultsPort := startRunDevForResultsBindOutputTest(t, "")

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		stdout := process.stdout.String()
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Starting results server on https://127.0.0.1:%d (mode=dev; bind=127.0.0.1:%d)", resultsPort, resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results: https://127.0.0.1:%d", resultsPort))
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Results bind: 127.0.0.1:%d (loopback only)", resultsPort))
		convey.So(stdout, convey.ShouldNotContainSubstring, "Results public:")
		convey.So(stdout, convey.ShouldContainSubstring, fmt.Sprintf("Frontend: https://127.0.0.1:%d", frontendPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func runRunDevExpectingFailureWithinForTest(
	t *testing.T,
	repoRoot string,
	args []string,
	env map[string]string,
	unsetEnv []string,
	limit time.Duration,
) (string, string, error) {
	t.Helper()

	command := exec.Command("bash", append([]string{filepath.Join(repoRoot, "run-dev.sh")}, args...)...) //nolint:gosec
	command.Dir = repoRoot
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = applyTestEnvForTest(env, unsetEnv)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		t.Fatalf("start run-dev.sh: %v", err)
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- command.Wait()
	}()

	select {
	case err := <-waitCh:
		return stdout.String(), stderr.String(), err
	case <-time.After(limit):
		signalRunDevProcessGroupForTest(command, syscall.SIGKILL)
		select {
		case err := <-waitCh:
			return stdout.String(), stderr.String(), fmt.Errorf("run-dev.sh did not exit within %s: %w", limit, err)
		case <-time.After(2 * time.Second):
			return stdout.String(), stderr.String(), fmt.Errorf("run-dev.sh did not exit within %s", limit)
		}
	}
}

func runDevSnapshotTimeoutForTest(process *runDevProcess) time.Duration {
	timeout := runDevSnapshotStartupGraceForTest
	timeout += time.Duration(runDevHealthAttemptsForTest(process, "WA_RUN_DEV_FRONTEND_HEALTH_MAX_ATTEMPTS", runDevDefaultFrontendHealthAttemptsForTest)) *
		runDevHealthPollIntervalForTest

	if runDevStartsSeqmetaBeforeFrontendForTest(process) {
		timeout += time.Duration(runDevHealthAttemptsForTest(process, "WA_RUN_DEV_SEQMETA_HEALTH_MAX_ATTEMPTS", runDevDefaultSeqmetaHealthAttemptsForTest)) *
			runDevHealthPollIntervalForTest
	}

	return timeout
}

func runDevHealthAttemptsForTest(process *runDevProcess, key string, defaultAttempts int) int {
	value, found := runDevCommandEnvValueForTest(process, key)
	if !found || value == "" {
		return defaultAttempts
	}

	attempts, err := strconv.Atoi(value)
	if err != nil || attempts < 1 {
		return defaultAttempts
	}

	return attempts
}

func runDevStartsSeqmetaBeforeFrontendForTest(process *runDevProcess) bool {
	if process == nil {
		return false
	}

	if value, found := runDevCommandEnvValueForTest(process, "WA_RUN_DEV_SEQMETA_CMD"); found && value != "" {
		return true
	}

	value, found := runDevCommandEnvValueForTest(process, "WA_MLWH_DSN")

	return found && value != ""
}

func runDevCommandEnvValueForTest(process *runDevProcess, key string) (string, bool) {
	if process == nil || process.Command == nil {
		return "", false
	}

	for i := range len(process.Command.Env) {
		entry := process.Command.Env[len(process.Command.Env)-1-i]
		envKey, value, found := strings.Cut(entry, "=")
		if found && envKey == key {
			return value, true
		}
	}

	return "", false
}

func failRunDevSnapshotWaitForExitedProcess(t *testing.T, process *runDevProcess, snapshotPath string) {
	t.Helper()

	err := process.Wait()
	if err != nil {
		t.Fatalf("run-dev.sh exited before writing frontend snapshot %s: %v", snapshotPath, err)
	}

	t.Fatalf(
		"run-dev.sh exited before writing frontend snapshot %s\nstdout:\n%s\nstderr:\n%s",
		snapshotPath,
		process.stdout.String(),
		process.stderr.String(),
	)
}

func TestRunDevScriptUsesEphemeralMLWHCacheInTestMode(t *testing.T) {
	convey.Convey("E6.7: run-dev.sh test mode exports an ephemeral WA_MLWH_CACHE_PATH under .tmp/", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)

		convey.So(snapshot.MLWHCachePath, convey.ShouldNotBeBlank)
		convey.So(snapshot.MLWHCachePath, convey.ShouldContainSubstring, filepath.Join(repoRoot, ".tmp")+string(os.PathSeparator))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh --mode dev reuses already-healthy services on the configured dev ports", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		frontendSentinelPath := filepath.Join(t.TempDir(), "frontend-started.txt")
		seqmetaSentinelPath := filepath.Join(t.TempDir(), "seqmeta-started.txt")
		resultsDBPath := filepath.Join(t.TempDir(), "results-dev.sqlite")
		stateDir := t.TempDir()
		var seededResultsMu sync.Mutex
		seededResults := 0

		convey.So(os.WriteFile(filepath.Join(stateDir, ".wa-results-server.token"), []byte("stub-token"), 0o600), convey.ShouldBeNil)

		startRunDevResultsStubForTest(t, repoRoot, resultsPort, func() {
			seededResultsMu.Lock()
			defer seededResultsMu.Unlock()
			seededResults++
		})
		startStaticEndpointServerForTest(t, seqmetaPort, map[string][]byte{
			"/studies": []byte(`[]`),
		})

		startStaticEndpointServerForTest(t, frontendPort, map[string][]byte{
			"/api/health": []byte(`{"ok":true}`),
		})

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:          "dev",
			fixtures:      true,
			omitPortFlags: true,
			env: map[string]string{
				"WA_ENV":                                "development",
				"XDG_STATE_HOME":                        stateDir,
				"WA_RESULTS_DB_PATH":                    resultsDBPath,
				"WA_MLWH_DSN":                           "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
				"WA_DEV_FRONTEND_PORT":                  fmt.Sprintf("%d", frontendPort),
				"WA_DEV_RESULTS_PORT":                   fmt.Sprintf("%d", resultsPort),
				"WA_DEV_SEQMETA_PORT":                   fmt.Sprintf("%d", seqmetaPort),
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node -e "require('node:fs').writeFileSync(%q, 'started'); process.exit(1)"`, frontendSentinelPath),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_SEQMETA_CMD":                fmt.Sprintf(`node -e "require('node:fs').writeFileSync(%q, 'started'); process.exit(1)"`, seqmetaSentinelPath),
				"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/studies", seqmetaPort),
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
			},
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		stdout := process.stdout.String()
		resultsURL := runDevExtractURLForTest(t, stdout, "Results")
		mlwhURL := runDevExtractURLForTest(t, stdout, "MLWH")
		frontendURL := runDevExtractURLForTest(t, stdout, "Frontend")

		convey.So(frontendURL, convey.ShouldEqual, fmt.Sprintf("https://127.0.0.1:%d", frontendPort))
		convey.So(resultsURL, convey.ShouldEqual, fmt.Sprintf("https://127.0.0.1:%d", resultsPort))
		convey.So(mlwhURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", seqmetaPort))
		seededResultsMu.Lock()
		convey.So(seededResults, convey.ShouldEqual, 6)
		seededResultsMu.Unlock()
		convey.So(runDevPathExistsWithinForTest(frontendSentinelPath, 2*time.Second), convey.ShouldBeFalse)
		convey.So(runDevPathExistsWithinForTest(seqmetaSentinelPath, 2*time.Second), convey.ShouldBeFalse)
		convey.So(runDevProcessExitedWithinForTest(process, 2*time.Second), convey.ShouldBeFalse)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func TestRunDevScript(t *testing.T) {
	convey.Convey("run-dev.sh synthesizes a reusable .tmp MLWH cache path when MLWH is auto-managed from WA_MLWH_DSN", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		contents, err := os.ReadFile(filepath.Join(repoRoot, "run-dev.sh"))
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(contents), convey.ShouldContainSubstring, `MLWH_CACHE_PATH="$TMP_DIR/mlwh-$scenario.sqlite"`)
	})

	convey.Convey("run-dev.sh --mode prod refuses before starting results serve when no MLWH query source is configured", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{
			"--mode", "prod",
			"--frontend-port", "1",
			"--results-port", "1",
			"--seqmeta-port", "1",
		}, map[string]string{
			"WA_ENV":             "production",
			"WA_RESULTS_DB_PATH": filepath.Join(t.TempDir(), "results-prod.sqlite"),
		}, []string{"WA_TEST_FRONTEND_PORT", "WA_TEST_RESULTS_PORT", "WA_TEST_SEQMETA_PORT"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_SERVER_URL")
		convey.So(stdout+stderr, convey.ShouldContainSubstring, "WA_MLWH_CACHE_PATH")
		convey.So(stdout, convey.ShouldNotContainSubstring, "Starting results server")
	})

	convey.Convey("R1.1/R1.2/R1.3/R1.5: run-dev.sh builds wa, seeds three fixtures, skips MLWH when no command or MLWH config is present, and cleans up on SIGINT", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		resultsList := waitForSeededResultsForTest(t, resultsPort)
		fixtureSummary := summarizeRunDevFixturesForTest(t, repoRoot, resultsPort, resultsList)

		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, ".tmp", "wa")), convey.ShouldBeTrue)
		convey.So(resultsList, convey.ShouldHaveLength, 6)
		convey.So(fixtureSummary.nestedDirectoryCount, convey.ShouldBeGreaterThanOrEqualTo, 3)
		convey.So(fixtureSummary.hasSiblingDirectories, convey.ShouldBeTrue)
		convey.So(fixtureSummary.hasRepeatedFileTypes, convey.ShouldBeTrue)
		convey.So(fixtureSummary.fileKinds, convey.ShouldContain, "input")
		convey.So(fixtureSummary.fileKinds, convey.ShouldContain, "output")
		convey.So(fixtureSummary.fileKinds, convey.ShouldContain, "pipeline")
		convey.So(fixtureSummary.maxPreviewableImagesInDirectory, convey.ShouldBeGreaterThan, 100)
		convey.So(snapshot.ResultsBackendURL, convey.ShouldEqual, fmt.Sprintf("https://127.0.0.1:%d", resultsPort))
		convey.So(snapshot.ResultsCACert, convey.ShouldEqual, filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"))
		convey.So(runDevPathExistsForTest(snapshot.ResultsCACert), convey.ShouldBeTrue)
		convey.So(strings.TrimSpace(snapshot.MLWHBackendURL), convey.ShouldEqual, "")
		convey.So(snapshot.ResultsDBPath, convey.ShouldNotBeBlank)
		convey.So(snapshot.MLWHCachePath, convey.ShouldNotBeBlank)
		convey.So(snapshot.MLWHCachePath, convey.ShouldContainSubstring, filepath.Join(repoRoot, ".tmp")+string(os.PathSeparator))
		convey.So(runDevPathExistsForTest(snapshot.ResultsDBPath), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, "logs", "results.log")), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, "logs", "frontend.log")), convey.ShouldBeTrue)
		convey.So(runDevPathExistsForTest(filepath.Join(repoRoot, "logs", "mlwh.log")), convey.ShouldBeTrue)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
		convey.So(runDevPathExistsForTest(snapshot.ResultsDBPath), convey.ShouldBeFalse)
	})

	convey.Convey("R1.4: run-dev.sh starts MLWH and exports WA_MLWH_BACKEND_URL when an explicit MLWH command is set", t, func() {
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
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_SEQMETA_CMD":                runDevSeqmetaStubCommandForTest(),
				"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/api/health", seqmetaPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		waitForTCPPortForTest(t, seqmetaPort)

		convey.So(snapshot.MLWHBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", seqmetaPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh waits for MLWH readiness without printing transient curl probe errors", t, func() {
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
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
				"WA_RUN_DEV_SEQMETA_CMD":                runDevSeqmetaStubCommandForTest(),
				"WA_RUN_DEV_SEQMETA_HEALTH_URL":         fmt.Sprintf("http://127.0.0.1:%d/api/health", seqmetaPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, process, snapshotPath)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "curl:")
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "Failed to connect")
		convey.So(process.Stderr(), convey.ShouldNotContainSubstring, "Operation timed out")
	})

	convey.Convey("run-dev.sh surfaces MLWH log output when MLWH exits before becoming ready", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)

		stdout, stderr, err := runRunDevExpectingFailureForTest(t, repoRoot, []string{
			"--mode", "dev",
			"--frontend-port", fmt.Sprintf("%d", frontendPort),
			"--results-port", fmt.Sprintf("%d", resultsPort),
			"--seqmeta-port", fmt.Sprintf("%d", seqmetaPort),
		}, map[string]string{
			"WA_RESULTS_DB_PATH":                    filepath.Join(t.TempDir(), "results.db"),
			"WA_MLWH_DSN":                           "mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse",
			"WA_MLWH_CACHE_PATH":                    filepath.Join(t.TempDir(), "mlwh-cache.sqlite"),
			"WA_RESULTS_LDAP_SERVER":                "ldap.example.org",
			"WA_RESULTS_LDAP_DN":                    "uid=%s,ou=people,dc=example,dc=org",
			"WA_RUN_DEV_SEQMETA_CMD":                `node -e "console.error('mlwh boot failed: source db unavailable'); process.exit(23)"`,
			"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
			"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
			"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
			"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
			"WA_RUN_DEV_FRONTEND_DEV_CMD":           fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
			"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
		}, []string{"WA_ENV"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout, convey.ShouldContainSubstring, "Starting MLWH server")
		convey.So(stderr, convey.ShouldContainSubstring, "MLWH server exited before becoming ready")
		convey.So(stderr, convey.ShouldContainSubstring, "exit=23")
		convey.So(stderr, convey.ShouldContainSubstring, "mlwh boot failed: source db unavailable")
		convey.So(stderr, convey.ShouldContainSubstring, "logs/mlwh.log")
	})

	convey.Convey("run-dev.sh waits for MLWH studies readiness before starting the frontend by default", t, func() {
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

		if (( count < 14 )); then
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
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_SEQMETA_CMD":                runDevSeqmetaStubCommandForTest(),
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
		convey.So(waitForRunDevStdoutForTest(t, process, "Waiting for MLWH studies readiness at"), convey.ShouldBeTrue)

		convey.So(runDevPathExistsWithinForTest(snapshotPath, 1400*time.Millisecond), convey.ShouldBeFalse)

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)

		convey.So(snapshot.MLWHBackendURL, convey.ShouldEqual, fmt.Sprintf("http://127.0.0.1:%d", seqmetaPort))

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh tests can explicitly disable an inherited legacy seqmeta command without changing product behavior", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		t.Setenv("WA_RUN_DEV_SEQMETA_CMD", runDevSeqmetaStubCommandForTest())

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)

		convey.So(strings.TrimSpace(snapshot.MLWHBackendURL), convey.ShouldEqual, "")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	convey.Convey("run-dev.sh starts the results server without putting the DB path on the command line", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		cmdline := waitForRunDevChildCommandLineForTest(t, process.Command.Process.Pid, " results serve ")

		convey.So(cmdline, convey.ShouldContainSubstring, " results serve ")
		convey.So(cmdline, convey.ShouldNotContainSubstring, " --db ")
		convey.So(cmdline, convey.ShouldNotContainSubstring, snapshot.ResultsDBPath)

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
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
			env: map[string]string{
				"WA_RUN_DEV_ENV_SNAPSHOT":        snapshotPath,
				"WA_RUN_DEV_FRONTEND_DEV_CMD":    fmt.Sprintf(`node %q "$WA_TEST_FRONTEND_PORT"`, filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs")),
				"WA_RUN_DEV_FRONTEND_HEALTH_URL": fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		_ = waitForRunDevSnapshotForTest(t, process, snapshotPath)

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
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		_ = waitForRunDevSnapshotForTest(t, process, snapshotPath)

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
	if [[ "$4" != "--experimental-https" || "$5" != "--experimental-https-key" || "$6" != %q || "$7" != "--experimental-https-cert" || "$8" != %q ]]; then
		printf 'missing experimental HTTPS args: %%s\n' "$*" >&2
		exit 1
	fi
	exec node %q "$3"
fi

exit 0
`, pnpmLogPath, fmt.Sprintf("%d", frontendPort), filepath.Join(repoRoot, ".tmp", "wa-dev-key.pem"), filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"), filepath.Join(repoRoot, "cmd", "testdata", "run-dev-frontend-stub.mjs"))

		convey.So(os.WriteFile(pnpmPath, []byte(stub), 0o755), convey.ShouldBeNil)

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			frontendPort: frontendPort,
			resultsPort:  resultsPort,
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
			env: map[string]string{
				"WA_RESULTS_SERVER_CERT":                ".tmp/wa-dev-cert.pem",
				"WA_RESULTS_SERVER_KEY":                 ".tmp/wa-dev-key.pem",
				"PATH":                                  binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"WA_RUN_DEV_ENV_SNAPSHOT":               snapshotPath,
				"WA_RUN_DEV_FRONTEND_CHANGED_FILES_CMD": `:`,
				"WA_RUN_DEV_FRONTEND_LINT_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_FORMAT_CMD":        `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_TEST_CMD":          `node -e "process.exit(0)"`,
				"WA_RUN_DEV_FRONTEND_HEALTH_URL":        fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
			},
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		loggedArgs := waitForRunDevStepsForTest(t, pnpmLogPath, 1)

		convey.So(snapshot.ResultsServerCert, convey.ShouldEqual, filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"))
		convey.So(snapshot.ResultsServerKey, convey.ShouldEqual, filepath.Join(repoRoot, ".tmp", "wa-dev-key.pem"))
		convey.So(loggedArgs, convey.ShouldResemble, []string{
			fmt.Sprintf(
				"dev --port %d --experimental-https --experimental-https-key %s --experimental-https-cert %s",
				frontendPort,
				filepath.Join(repoRoot, ".tmp", "wa-dev-key.pem"),
				filepath.Join(repoRoot, ".tmp", "wa-dev-cert.pem"),
			),
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
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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
		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		convey.So(snapshot.ResultsBackendURL, convey.ShouldEqual, fmt.Sprintf("https://127.0.0.1:%d", resultsPort))

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
			unsetEnv:     append(runDevUnsetSeqmetaEnvForTest(), "WA_DEV_ALLOWED_ORIGINS"),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)

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
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		origins := splitRunDevOriginsForTest(snapshot.AllowedDevOrigins)

		convey.So(origins, convey.ShouldContain, "localhost")
		convey.So(origins, convey.ShouldContain, "extra-host.example.com")
		convey.So(origins, convey.ShouldContain, "another.example.com")

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

}

func runDevEnvForTest(unsetKeys []string) []string {
	unsetKeys = append(unsetKeys, []string{
		"WA_RUN_DEV_SEQMETA_CMD",
		"WA_RUN_DEV_SEQMETA_HEALTH_URL",
		"WA_MLWH_DSN",
		"WA_MLWH_PASSWORD",
		"WA_MLWH_CACHE_PATH",
		"WA_MLWH_CACHE_PASSWORD",
	}...)

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
	endpoint := fmt.Sprintf("https://127.0.0.1:%d/rest/v1/auth/results/%s/files", resultsPort, resultID)
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("build result files request for %s: %v", endpoint, err)
	}
	request.Header.Set("Authorization", "Bearer "+runDevOwnerJWTForTest(t, resultsPort))

	response, err := runDevHTTPSClientForTest(t).Do(request)
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
	readyCh := make(chan struct{})
	go func() {
		time.Sleep(delay)
		close(readyCh)
	}()

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			<-readyCh
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("[]"))
		}),
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on delayed HTTP server port %d: %v", port, err)
	}

	errCh := make(chan error, 1)
	go func() {
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

func waitForRunDevChildCommandLineForTest(t *testing.T, parentPID int, substring string) string {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		childPIDs, err := runDevChildPIDsForTest(parentPID)
		if err == nil {
			for _, childPID := range childPIDs {
				cmdline, readErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(childPID), "cmdline"))
				if readErr != nil {
					continue
				}

				formatted := " " + strings.ReplaceAll(string(cmdline), "\x00", " ") + " "
				if strings.Contains(formatted, substring) {
					return formatted
				}
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for child process containing %q", substring)

	return ""
}

func runDevChildPIDsForTest(parentPID int) ([]int, error) {
	command := exec.Command("ps", "-o", "pid=", "--ppid", strconv.Itoa(parentPID))
	output, err := command.Output()
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(output))
	childPIDs := make([]int, 0, len(fields))
	for _, field := range fields {
		childPID, convErr := strconv.Atoi(field)
		if convErr != nil {
			return nil, convErr
		}

		childPIDs = append(childPIDs, childPID)
	}

	return childPIDs, nil
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
			unsetEnv:     runDevUnsetSeqmetaEnvForTest(),
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

		_ = waitForRunDevSnapshotForTest(t, process, snapshotPath)
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
	Command  *exec.Cmd
	stdout   *bytes.Buffer
	stderr   *bytes.Buffer
	waitMu   sync.Mutex
	waitCh   chan error
	waitErr  error
	waitDone bool
}

func (process *runDevProcess) Wait() error {
	process.waitMu.Lock()
	if process.waitDone {
		err := process.waitErr
		process.waitMu.Unlock()
		if err != nil {
			return fmt.Errorf("%w\nstdout:\n%s\nstderr:\n%s", err, process.stdout.String(), process.stderr.String())
		}

		return nil
	}
	waitCh := process.waitCh
	process.waitMu.Unlock()

	err := <-waitCh

	process.waitMu.Lock()
	process.waitErr = err
	process.waitDone = true
	process.waitMu.Unlock()

	if err != nil {
		return fmt.Errorf("%w\nstdout:\n%s\nstderr:\n%s", err, process.stdout.String(), process.stderr.String())
	}

	return nil
}

func (process *runDevProcess) Stderr() string {
	return process.stderr.String()
}

func (process *runDevProcess) ExitedWithin(timeout time.Duration) bool {
	process.waitMu.Lock()
	if process.waitDone {
		process.waitMu.Unlock()
		return true
	}
	waitCh := process.waitCh
	process.waitMu.Unlock()

	select {
	case err := <-waitCh:
		process.waitMu.Lock()
		process.waitErr = err
		process.waitDone = true
		process.waitMu.Unlock()
		return true
	case <-time.After(timeout):
		return false
	}
}

type runDevStartOptions struct {
	mode          string
	fixtures      bool
	frontendPort  int
	resultsPort   int
	seqmetaPort   int
	omitPortFlags bool
	startDir      string
	unsetEnv      []string
	env           map[string]string
}

func startRunDevForTest(t *testing.T, repoRoot string, options runDevStartOptions) *runDevProcess {
	t.Helper()

	args := []string{
		filepath.Join(repoRoot, "run-dev.sh"),
	}

	if options.mode != "" {
		args = append(args, "--mode", options.mode)
	}

	if options.fixtures {
		args = append(args, "--fixtures")
	}

	if !options.omitPortFlags {
		args = append(args,
			"--frontend-port", fmt.Sprintf("%d", options.frontendPort),
			"--results-port", fmt.Sprintf("%d", options.resultsPort),
		)
	}

	if !options.omitPortFlags && options.seqmetaPort != 0 {
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

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- command.Wait()
	}()

	t.Cleanup(func() {
		if !(&runDevProcess{Command: command, waitCh: waitCh}).ExitedWithin(0) {
			terminateRunDevCommandForTest(command)
		}
	})

	return &runDevProcess{
		Command: command,
		stdout:  stdout,
		stderr:  stderr,
		waitCh:  waitCh,
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

func startStaticEndpointServerForTest(t *testing.T, port int, responses map[string][]byte) {
	t.Helper()

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			body, ok := responses[request.URL.Path]
			if !ok {
				writer.WriteHeader(http.StatusNotFound)
				return
			}

			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(body)
		}),
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on static health server port %d: %v", port, err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	t.Cleanup(func() {
		_ = server.Close()

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("static health server failed: %v", err)
			}
		default:
		}
	})
}

func startRunDevResultsStubForTest(t *testing.T, repoRoot string, port int, onSeed func()) {
	t.Helper()
	certPath, keyPath := runDevEnsureTLSCertificateForTest(t, repoRoot)

	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch {
			case request.Method == http.MethodGet && request.URL.Path == "/rest/v1/results/stats":
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`{"ok":true}`))
			case request.Method == http.MethodPost && request.URL.Path == "/rest/v1/jwt":
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(`"stub-jwt"`))
			case request.Method == http.MethodPost && request.URL.Path == "/rest/v1/auth/results":
				if onSeed != nil {
					onSeed()
				}

				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(http.StatusCreated)
				_, _ = writer.Write([]byte(`{"id":"seeded"}`))
			default:
				writer.WriteHeader(http.StatusNotFound)
			}
		}),
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("listen on results stub port %d: %v", port, err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ServeTLS(listener, certPath, keyPath)
	}()

	t.Cleanup(func() {
		_ = server.Close()

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Errorf("results stub server failed: %v", err)
			}
		default:
		}
	})
}

func waitForRunDevSnapshotForTest(t *testing.T, process *runDevProcess, snapshotPath string) runDevEnvSnapshot {
	t.Helper()

	timeout := runDevSnapshotTimeoutForTest(process)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(snapshotPath)
		if err == nil {
			var snapshot runDevEnvSnapshot
			if unmarshalErr := json.Unmarshal(body, &snapshot); unmarshalErr != nil {
				t.Fatalf("decode frontend snapshot: %v", unmarshalErr)
			}

			return snapshot
		}

		if process != nil && process.ExitedWithin(0) {
			failRunDevSnapshotWaitForExitedProcess(t, process, snapshotPath)
		}

		time.Sleep(100 * time.Millisecond)
	}

	if process != nil {
		t.Fatalf(
			"timed out after %s waiting for frontend snapshot %s\nstdout:\n%s\nstderr:\n%s",
			timeout,
			snapshotPath,
			process.stdout.String(),
			process.stderr.String(),
		)
	}

	t.Fatalf("timed out after %s waiting for frontend snapshot %s", timeout, snapshotPath)

	return runDevEnvSnapshot{}
}

func waitForSeededResultsForTest(t *testing.T, resultsPort int) []results.ResultSet {
	t.Helper()

	endpoint := fmt.Sprintf("https://127.0.0.1:%d/rest/v1/results", resultsPort)
	deadline := time.Now().Add(20 * time.Second)
	client := runDevHTTPSClientForTest(t)

	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
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

			if len(stored) == 6 {
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

func runDevProcessExitedWithinForTest(process *runDevProcess, timeout time.Duration) bool {
	return process.ExitedWithin(timeout)
}

func runDevExtractURLForTest(t *testing.T, stdout string, label string) string {
	t.Helper()

	prefix := label + ": "
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}

	t.Fatalf("did not find %s URL in stdout:\n%s", label, stdout)

	return ""
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

func TestRunDevMLWHDefaultReadinessBudgetAllowsColdMLWHSync(t *testing.T) {
	convey.Convey("run-dev.sh gives MLWH studies readiness at least five minutes by default", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		contents, err := os.ReadFile(filepath.Join(repoRoot, "run-dev.sh"))
		convey.So(err, convey.ShouldBeNil)

		match := regexp.MustCompile(`SEQMETA_HEALTH_MAX_ATTEMPTS="\$\{WA_RUN_DEV_SEQMETA_HEALTH_MAX_ATTEMPTS:-([0-9]+)\}"`).FindSubmatch(contents)
		convey.So(match, convey.ShouldHaveLength, 2)

		attempts, err := strconv.Atoi(string(match[1]))
		convey.So(err, convey.ShouldBeNil)
		convey.So(attempts, convey.ShouldBeGreaterThanOrEqualTo, 1200)
	})
}

func TestRunDevScriptMLWHBuiltInLaunchDropsLegacySyncFlag(t *testing.T) {
	convey.Convey("run-dev.sh auto-managed MLWH backend serves the current-state API", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		contents, err := os.ReadFile(filepath.Join(repoRoot, "run-dev.sh"))
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(contents), convey.ShouldNotContainSubstring, "--mlwh-sync-interval")
		convey.So(string(contents), convey.ShouldNotContainSubstring, "mlwhdiff serve")
		convey.So(string(contents), convey.ShouldContainSubstring, `mlwh_args=(mlwh serve --port "$seqmeta_port")`)
	})
}

func TestRunDevScriptCleansUpHungResultsAfterReadinessTimeout(t *testing.T) {
	convey.Convey("Bug 1: run-dev.sh escalates cleanup when a timed-out results server ignores SIGTERM", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		frontendPort := runDevFreePortForTest(t)
		resultsPort := runDevFreePortForTest(t)
		seqmetaPort := runDevFreePortForTest(t)
		binDir := t.TempDir()
		fakeResultsPIDPath := filepath.Join(t.TempDir(), "fake-results.pid")
		fakeWaPath := filepath.Join(repoRoot, ".tmp", "wa")

		writeRunDevHungResultsToolchainForTest(t, binDir)
		t.Cleanup(func() {
			_ = os.Remove(fakeWaPath)
		})

		stdout, stderr, err := runRunDevExpectingFailureWithinForTest(t, repoRoot, []string{
			"--mode", "dev",
			"--frontend-port", fmt.Sprintf("%d", frontendPort),
			"--results-port", fmt.Sprintf("%d", resultsPort),
			"--seqmeta-port", fmt.Sprintf("%d", seqmetaPort),
		}, map[string]string{
			"PATH":                           binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
			"WA_FAKE_RESULTS_PID_PATH":       fakeResultsPIDPath,
			"WA_RESULTS_DB_PATH":             filepath.Join(t.TempDir(), "results-dev.sqlite"),
			"WA_MLWH_DSN":                    "mlwh_humgen@tcp(localhost:3306)/mlwarehouse_test",
			"WA_RESULTS_LDAP_SERVER":         "ldap.example.org",
			"WA_RESULTS_LDAP_DN":             "uid=%s,ou=people,dc=example,dc=org",
			"WA_RUN_DEV_FRONTEND_DEV_CMD":    `node -e "process.exit(1)"`,
			"WA_RUN_DEV_FRONTEND_HEALTH_URL": fmt.Sprintf("http://127.0.0.1:%d/api/health", frontendPort),
		}, []string{"WA_ENV"}, 5*time.Second)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(stdout, convey.ShouldContainSubstring, "Starting results server")
		convey.So(stderr, convey.ShouldContainSubstring, "Timed out waiting for results server")
		convey.So(stderr, convey.ShouldContainSubstring, "did not exit after SIGTERM; sending SIGKILL")

		fakeResultsPID := runDevReadPIDForTest(t, fakeResultsPIDPath)
		t.Cleanup(func() {
			if runDevPIDExistsForTest(fakeResultsPID) {
				_ = syscall.Kill(fakeResultsPID, syscall.SIGKILL)
			}
		})
		convey.So(runDevPIDExistsForTest(fakeResultsPID), convey.ShouldBeFalse)
	})
}

func writeRunDevHungResultsToolchainForTest(t *testing.T, binDir string) {
	t.Helper()

	fakeGo := `#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "build" && "${2:-}" == "-o" && -n "${3:-}" ]]; then
	output="$3"
	mkdir -p "$(dirname "$output")"
	cat >"$output" <<'WAEOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "results" && "${2:-}" == "serve" ]]; then
	printf '%s\n' "$$" > "${WA_FAKE_RESULTS_PID_PATH:?}"
	trap "" TERM
	exec /bin/sleep 60
fi

printf 'unexpected fake wa args: %s\n' "$*" >&2
exit 2
WAEOF
	chmod +x "$output"
	exit 0
fi

printf 'unexpected fake go args: %s\n' "$*" >&2
exit 2
`
	convey.So(os.WriteFile(filepath.Join(binDir, "go"), []byte(fakeGo), 0o755), convey.ShouldBeNil)
	convey.So(os.WriteFile(filepath.Join(binDir, "curl"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 22\n"), 0o755), convey.ShouldBeNil)
	convey.So(os.WriteFile(filepath.Join(binDir, "sleep"), []byte("#!/usr/bin/env bash\nset -euo pipefail\nexit 0\n"), 0o755), convey.ShouldBeNil)
}

func runDevReadPIDForTest(t *testing.T, pidPath string) int {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(pidPath)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(body)))
			convey.So(parseErr, convey.ShouldBeNil)

			return pid
		}

		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for fake results pid at %s", pidPath)

	return 0
}

func runDevPIDExistsForTest(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}
