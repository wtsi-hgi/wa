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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const (
	runDevTLSHostFQDNForTest  = "farm22-wrstat01.internal"
	runDevTLSHostShortForTest = "farm22-wrstat01"
	// runDevTLSMaxValidityForTest is the longest validity run-dev.sh may give
	// a generated dev certificate: under Apple's 398-day limit for TLS server
	// certificates.
	runDevTLSMaxValidityForTest = 397 * 24 * time.Hour
)

var (
	runDevOIDBasicConstraintsForTest = asn1.ObjectIdentifier{2, 5, 29, 19}
	runDevOIDKeyUsageForTest         = asn1.ObjectIdentifier{2, 5, 29, 15}
)

// runDevTestCertSpecForTest describes a certificate the tests write for
// run-dev.sh to find.
type runDevTestCertSpecForTest struct {
	hosts []string
	// validity is NotAfter minus NotBefore; NotBefore is an hour ago.
	validity time.Duration
	// serverAuth adds the extensions macOS requires of a TLS server cert.
	serverAuth bool
	// issuer signs the cert; nil makes it self-signed.
	issuer *runDevTestCAForTest
}

type runDevTestCAForTest struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	pemPath string
}

func runDevAllTLSHostsForTest() []string {
	return []string{"localhost", "127.0.0.1", "::1", runDevTLSHostFQDNForTest, runDevTLSHostShortForTest}
}

func TestRunDevGeneratedDevCertificateMeetsMacOSServerRules(t *testing.T) {
	convey.Convey("run-dev.sh generates a dev certificate macOS accepts as a TLS server certificate", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		certPath, keyPath := runDevFreshGeneratedTLSCertPathsForTest(t)

		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:      certPath,
			keyPath:       keyPath,
			hostnameFQDN:  runDevTLSHostFQDNForTest,
			hostnameShort: runDevTLSHostShortForTest,
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		cert := runDevReadCertificateForTest(t, certPath)
		runDevAssertMacOSServerCertificateForTest(t, cert)
		for _, host := range runDevAllTLSHostsForTest() {
			runDevAssertCertificateVerifiesHostnameForTest(t, cert, host)
		}

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})

	staleCases := []struct {
		name string
		spec runDevTestCertSpecForTest
	}{
		{
			name: "lacks extendedKeyUsage serverAuth",
			spec: runDevTestCertSpecForTest{validity: 365 * 24 * time.Hour},
		},
		{
			name: "is valid for longer than 397 days",
			spec: runDevTestCertSpecForTest{validity: 825 * 24 * time.Hour, serverAuth: true},
		},
		{
			name: "expires within 30 days",
			spec: runDevTestCertSpecForTest{validity: 7 * 24 * time.Hour, serverAuth: true},
		},
	}

	for _, staleCase := range staleCases {
		convey.Convey("run-dev.sh regenerates an existing generated dev certificate that "+staleCase.name, t, func() {
			repoRoot := runDevRepoRootForTest(t)
			certPath, keyPath := runDevFreshGeneratedTLSCertPathsForTest(t)
			spec := staleCase.spec
			spec.hosts = runDevAllTLSHostsForTest()
			runDevWriteTestCertificateForTest(t, certPath, keyPath, spec)
			oldCert := runDevReadCertificateForTest(t, certPath)

			process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
				certPath:      certPath,
				keyPath:       keyPath,
				hostnameFQDN:  runDevTLSHostFQDNForTest,
				hostnameShort: runDevTLSHostShortForTest,
			})

			convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

			cert := runDevReadCertificateForTest(t, certPath)
			convey.So(bytes.Equal(cert.Raw, oldCert.Raw), convey.ShouldBeFalse)
			runDevAssertMacOSServerCertificateForTest(t, cert)

			convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
			convey.So(process.Wait(), convey.ShouldBeNil)
		})
	}

	convey.Convey("run-dev.sh reuses an existing compliant generated dev certificate", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		certPath, keyPath := runDevFreshGeneratedTLSCertPathsForTest(t)
		runDevWriteTestCertificateForTest(t, certPath, keyPath, runDevTestCertSpecForTest{
			hosts:      runDevAllTLSHostsForTest(),
			validity:   runDevTLSMaxValidityForTest,
			serverAuth: true,
		})
		before := runDevFileStateForTest(t, certPath)

		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:      certPath,
			keyPath:       keyPath,
			hostnameFQDN:  runDevTLSHostFQDNForTest,
			hostnameShort: runDevTLSHostShortForTest,
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)
		// run-dev.sh may reset a generated cert's mode, but not rewrite it.
		after := runDevFileStateForTest(t, certPath)
		convey.So(bytes.Equal(after.Body, before.Body), convey.ShouldBeTrue)
		convey.So(after.ModTime, convey.ShouldEqual, before.ModTime)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)
	})
}

func TestRunDevSuppliedDevCertificateIsNeverRewritten(t *testing.T) {
	convey.Convey("run-dev.sh serves a read-only CA-issued WA_TLS_CERT missing a SAN without rewriting it", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		dir := t.TempDir()
		ca := runDevWriteTestCAForTest(t, filepath.Join(dir, "ca.crt"))
		certPath := filepath.Join(dir, "devhost.crt")
		keyPath := filepath.Join(dir, "devhost.key")
		runDevWriteTestCertificateForTest(t, certPath, keyPath, runDevTestCertSpecForTest{
			hosts:      []string{"localhost", "127.0.0.1", "::1", runDevTLSHostFQDNForTest},
			validity:   runDevTLSMaxValidityForTest,
			serverAuth: true,
			issuer:     ca,
		})
		convey.So(os.Chmod(certPath, 0o444), convey.ShouldBeNil)
		convey.So(os.Chmod(keyPath, 0o400), convey.ShouldBeNil)
		certBefore := runDevFileStateForTest(t, certPath)
		keyBefore := runDevFileStateForTest(t, keyPath)
		generatedCert, generatedKey := runDevFreshGeneratedTLSCertPathsForTest(t)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		pnpmLogPath := filepath.Join(t.TempDir(), "pnpm-args.txt")

		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			// As .env.development sets them: the dedicated vars win.
			certPath:        generatedCert,
			keyPath:         generatedKey,
			hostnameFQDN:    runDevTLSHostFQDNForTest,
			hostnameShort:   runDevTLSHostShortForTest,
			pnpmArgsLogPath: pnpmLogPath,
			extraEnv: map[string]string{
				"WA_TLS_CERT":             certPath,
				"WA_TLS_KEY":              keyPath,
				"WA_TLS_CA_CERT":          ca.pemPath,
				"WA_RUN_DEV_ENV_SNAPSHOT": snapshotPath,
			},
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)
		convey.So(runDevMissingSANsWarnedForTest(process.stderr.String()), convey.ShouldResemble, []string{runDevTLSHostShortForTest})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		convey.So(snapshot.ResultsServerCert, convey.ShouldEqual, certPath)
		convey.So(snapshot.ResultsServerKey, convey.ShouldEqual, keyPath)
		convey.So(snapshot.ResultsCACert, convey.ShouldEqual, ca.pemPath)
		convey.So(runDevServedCertificateForTest(t, snapshot.ResultsBackendURL).Raw, convey.ShouldResemble, runDevReadCertificateForTest(t, certPath).Raw)

		loggedArgs := waitForRunDevStepsForTest(t, pnpmLogPath, 1)
		convey.So(loggedArgs, convey.ShouldHaveLength, 1)
		convey.So(runDevFlagValueForTest(loggedArgs[0], "--experimental-https-key"), convey.ShouldEqual, keyPath)
		convey.So(runDevFlagValueForTest(loggedArgs[0], "--experimental-https-cert"), convey.ShouldEqual, certPath)

		convey.So(runDevPathExistsForTest(generatedCert), convey.ShouldBeFalse)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)

		convey.So(runDevFileStateForTest(t, certPath), convey.ShouldResemble, certBefore)
		convey.So(runDevFileStateForTest(t, keyPath), convey.ShouldResemble, keyBefore)
	})

	convey.Convey("run-dev.sh warns about, but never regenerates, a WA_RESULTS_SERVER_CERT outside its temp dir that misses a SAN", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		dir := t.TempDir()
		certPath := filepath.Join(dir, "personal.crt")
		keyPath := filepath.Join(dir, "personal.key")
		runDevWriteTestCertificateForTest(t, certPath, keyPath, runDevTestCertSpecForTest{
			hosts:      []string{"localhost", "127.0.0.1", "::1", runDevTLSHostShortForTest},
			validity:   runDevTLSMaxValidityForTest,
			serverAuth: true,
		})
		certBefore := runDevFileStateForTest(t, certPath)
		keyBefore := runDevFileStateForTest(t, keyPath)
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")

		process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
			certPath:      certPath,
			keyPath:       keyPath,
			hostnameFQDN:  runDevTLSHostFQDNForTest,
			hostnameShort: runDevTLSHostShortForTest,
			extraEnv:      map[string]string{"WA_RUN_DEV_ENV_SNAPSHOT": snapshotPath},
		})

		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)
		convey.So(runDevMissingSANsWarnedForTest(process.stderr.String()), convey.ShouldResemble, []string{runDevTLSHostFQDNForTest})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		convey.So(snapshot.ResultsServerCert, convey.ShouldEqual, certPath)
		convey.So(snapshot.ResultsCACert, convey.ShouldEqual, certPath)

		convey.So(process.Command.Process.Signal(syscall.SIGINT), convey.ShouldBeNil)
		convey.So(process.Wait(), convey.ShouldBeNil)

		convey.So(runDevFileStateForTest(t, certPath), convey.ShouldResemble, certBefore)
		convey.So(runDevFileStateForTest(t, keyPath), convey.ShouldResemble, keyBefore)
	})

	refusals := []struct {
		name    string
		message string
		write   func(t *testing.T, certPath string, keyPath string)
	}{
		{
			name:    "whose key does not match the certificate",
			message: "does not match",
			write: func(t *testing.T, certPath string, keyPath string) {
				t.Helper()

				spec := runDevTestCertSpecForTest{hosts: runDevAllTLSHostsForTest(), validity: runDevTLSMaxValidityForTest, serverAuth: true}
				runDevWriteTestCertificateForTest(t, certPath, keyPath, spec)
				runDevWriteTestCertificateForTest(t, certPath+".other", keyPath+".other", spec)
				convey.So(os.Rename(keyPath+".other", keyPath), convey.ShouldBeNil)
			},
		},
		{
			name:    "that does not cover 127.0.0.1",
			message: "127.0.0.1",
			write: func(t *testing.T, certPath string, keyPath string) {
				t.Helper()

				runDevWriteTestCertificateForTest(t, certPath, keyPath, runDevTestCertSpecForTest{
					hosts:      []string{"localhost", runDevTLSHostFQDNForTest},
					validity:   runDevTLSMaxValidityForTest,
					serverAuth: true,
				})
			},
		},
		{
			name:    "that has expired",
			message: "expired",
			write: func(t *testing.T, certPath string, keyPath string) {
				t.Helper()

				runDevWriteTestCertificateForTest(t, certPath, keyPath, runDevTestCertSpecForTest{
					hosts:      runDevAllTLSHostsForTest(),
					validity:   time.Minute,
					serverAuth: true,
				})
			},
		},
	}

	for _, refusal := range refusals {
		convey.Convey("run-dev.sh refuses, without touching it, a supplied dev certificate "+refusal.name, t, func() {
			repoRoot := runDevRepoRootForTest(t)
			dir := t.TempDir()
			certPath := filepath.Join(dir, "personal.crt")
			keyPath := filepath.Join(dir, "personal.key")
			refusal.write(t, certPath, keyPath)
			certBefore := runDevFileStateForTest(t, certPath)
			keyBefore := runDevFileStateForTest(t, keyPath)

			process, _ := startRunDevForDevCertificateTest(t, repoRoot, runDevCertificateOptionsForTest{
				hostnameFQDN:  runDevTLSHostFQDNForTest,
				hostnameShort: runDevTLSHostShortForTest,
				extraEnv: map[string]string{
					"WA_TLS_CERT": certPath,
					"WA_TLS_KEY":  keyPath,
				},
			})

			err := runDevWaitWithinForTest(process, time.Minute)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err, convey.ShouldNotEqual, errRunDevStillRunningForTest)
			convey.So(process.stderr.String(), convey.ShouldContainSubstring, certPath)
			convey.So(process.stderr.String(), convey.ShouldContainSubstring, refusal.message)
			convey.So(process.stdout.String(), convey.ShouldNotContainSubstring, "Development environment is ready.")
			convey.So(runDevFileStateForTest(t, certPath), convey.ShouldResemble, certBefore)
			convey.So(runDevFileStateForTest(t, keyPath), convey.ShouldResemble, keyBefore)
		})
	}
}

func TestRunDevProdServesDevCertificateFromEnvFile(t *testing.T) {
	convey.Convey("make prod serves the prod stack with a read-only WA_TLS_CERT set in .env.production.local, without writing to it", t, func() {
		repoRoot := runDevRepoRootForTest(t)
		ports := runDevFeedbackPortsForTest(t)
		binDir := t.TempDir()
		pnpmLogPath := filepath.Join(t.TempDir(), "pnpm-args.txt")
		snapshotPath := filepath.Join(t.TempDir(), "frontend-env.json")
		certDir := t.TempDir()
		certPath := filepath.Join(certDir, "farm.crt")
		keyPath := filepath.Join(certDir, "farm.key")
		runDevWriteTestCertificateForTest(t, certPath, keyPath, runDevTestCertSpecForTest{
			hosts:      runDevAllTLSHostsForTest(),
			validity:   runDevTLSMaxValidityForTest,
			serverAuth: true,
		})
		convey.So(os.Chmod(certPath, 0o444), convey.ShouldBeNil)
		convey.So(os.Chmod(keyPath, 0o400), convey.ShouldBeNil)
		certBefore := runDevFileStateForTest(t, certPath)
		keyBefore := runDevFileStateForTest(t, keyPath)
		generatedCert, generatedKey := runDevFreshGeneratedTLSCertPathsForTest(t)

		writeRunDevMLWHServeToolchainForTest(t, binDir, filepath.Join(t.TempDir(), "wa-invocations.log"))
		writeRunDevProdPnpmStubForTest(t, repoRoot, binDir, pnpmLogPath, 0)
		writeRunDevHostnameStubForTest(t, binDir, runDevTLSHostFQDNForTest, runDevTLSHostShortForTest)

		// make prod sources the env files from its working directory and runs
		// ./run-dev.sh there; this one hands over to the checkout's script.
		makeDir := t.TempDir()
		convey.So(os.WriteFile(filepath.Join(makeDir, ".env.production"), []byte(
			"WA_ENV=production\nWA_RESULTS_SERVER_CERT="+generatedCert+"\nWA_RESULTS_SERVER_KEY="+generatedKey+"\n",
		), 0o600), convey.ShouldBeNil)
		convey.So(os.WriteFile(filepath.Join(makeDir, ".env.production.local"), []byte(
			"WA_TLS_CERT="+certPath+"\nWA_TLS_KEY="+keyPath+"\n",
		), 0o600), convey.ShouldBeNil)
		convey.So(os.WriteFile(filepath.Join(makeDir, "run-dev.sh"), []byte(
			"#!/usr/bin/env bash\nexec bash "+strconv.Quote(filepath.Join(repoRoot, "run-dev.sh"))+" \"$@\"\n",
		), 0o755), convey.ShouldBeNil)

		env := runDevProdEnvForTest(t, binDir, ports, map[string]string{
			"WA_PROD_FRONTEND_PORT":   strconv.Itoa(ports.frontend),
			"WA_PROD_RESULTS_PORT":    strconv.Itoa(ports.results),
			"WA_PROD_SEQMETA_PORT":    strconv.Itoa(ports.seqmeta),
			"WA_RUN_DEV_ENV_SNAPSHOT": snapshotPath,
		})
		delete(env, "WA_ENV")

		process := startRunDevForTest(t, repoRoot, runDevStartOptions{
			mode:     "prod",
			command:  []string{"-c", "exec make --no-print-directory -C " + strconv.Quote(makeDir) + " -f " + strconv.Quote(filepath.Join(repoRoot, "Makefile")) + " prod"},
			unsetEnv: append(runDevUnsetProdRefusedEnvForTest(), "WA_RUN_DEV_FRONTEND_DEV_CMD", "WA_ENV", "MAKEFLAGS", "MAKELEVEL", "MFLAGS"),
			env:      env,
		})

		snapshot := waitForRunDevSnapshotForTest(t, process, snapshotPath)
		convey.So(waitForRunDevStdoutForTest(t, process, "Development environment is ready."), convey.ShouldBeTrue)

		convey.So(snapshot.ResultsServerCert, convey.ShouldEqual, certPath)
		convey.So(snapshot.ResultsServerKey, convey.ShouldEqual, keyPath)
		convey.So(snapshot.ResultsCACert, convey.ShouldEqual, certPath)
		convey.So(waitForRunDevStepsForTest(t, pnpmLogPath, 2), convey.ShouldResemble, []string{
			"build",
			fmt.Sprintf("start:https --port %d --key %s --cert %s", ports.frontend, keyPath, certPath),
		})
		convey.So(runDevPathExistsForTest(generatedCert), convey.ShouldBeFalse)

		// Interrupt make's whole process group, as Ctrl-C would.
		signalRunDevProcessGroupForTest(process.Command, syscall.SIGINT)
		convey.So(runDevWaitWithinForTest(process, time.Minute), convey.ShouldNotEqual, errRunDevStillRunningForTest)

		convey.So(runDevFileStateForTest(t, certPath), convey.ShouldResemble, certBefore)
		convey.So(runDevFileStateForTest(t, keyPath), convey.ShouldResemble, keyBefore)
	})
}

var errRunDevStillRunningForTest = errors.New("run-dev.sh still running")

// runDevWaitWithinForTest returns process's exit error, or
// errRunDevStillRunningForTest when it is still running after timeout.
func runDevWaitWithinForTest(process *runDevProcess, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errRunDevStillRunningForTest
	}
}

// runDevAssertMacOSServerCertificateForTest asserts cert has what macOS
// requires before it will trust a TLS server certificate.
func runDevAssertMacOSServerCertificateForTest(t *testing.T, cert *x509.Certificate) {
	t.Helper()

	convey.So(cert.BasicConstraintsValid, convey.ShouldBeTrue)
	convey.So(cert.IsCA, convey.ShouldBeFalse)
	convey.So(runDevExtensionIsCriticalForTest(cert, runDevOIDBasicConstraintsForTest), convey.ShouldBeTrue)
	convey.So(cert.KeyUsage&x509.KeyUsageDigitalSignature, convey.ShouldNotEqual, 0)
	convey.So(cert.KeyUsage&x509.KeyUsageKeyEncipherment, convey.ShouldNotEqual, 0)
	convey.So(runDevExtensionIsCriticalForTest(cert, runDevOIDKeyUsageForTest), convey.ShouldBeTrue)
	convey.So(slices.Contains(cert.ExtKeyUsage, x509.ExtKeyUsageServerAuth), convey.ShouldBeTrue)
	convey.So(cert.NotAfter.Sub(cert.NotBefore), convey.ShouldBeLessThanOrEqualTo, runDevTLSMaxValidityForTest)
	convey.So(time.Until(cert.NotAfter), convey.ShouldBeGreaterThan, 390*24*time.Hour)
}

func runDevExtensionIsCriticalForTest(cert *x509.Certificate, oid asn1.ObjectIdentifier) bool {
	for _, extension := range cert.Extensions {
		if extension.Id.Equal(oid) {
			return extension.Critical
		}
	}

	return false
}

// runDevMissingSANsWarnedForTest returns the hostnames run-dev.sh warned a
// supplied dev certificate does not cover.
func runDevMissingSANsWarnedForTest(stderr string) []string {
	const marker = "does not cover: "

	for line := range strings.SplitSeq(stderr, "\n") {
		_, missing, found := strings.Cut(line, marker)
		if !found {
			continue
		}

		return strings.Split(strings.TrimSpace(missing), ", ")
	}

	return nil
}

type runDevFileStateTestValue struct {
	Body    []byte
	ModTime time.Time
	Mode    os.FileMode
}

func runDevFileStateForTest(t *testing.T, path string) runDevFileStateTestValue {
	t.Helper()

	info, err := os.Stat(path)
	convey.So(err, convey.ShouldBeNil)

	body, err := os.ReadFile(path)
	convey.So(err, convey.ShouldBeNil)

	return runDevFileStateTestValue{Body: body, ModTime: info.ModTime(), Mode: info.Mode()}
}

// runDevServedCertificateForTest returns the leaf certificate served at
// rawURL's host.
func runDevServedCertificateForTest(t *testing.T, rawURL string) *x509.Certificate {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	convey.So(err, convey.ShouldBeNil)

	conn, err := tls.Dial("tcp", parsed.Host, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
	convey.So(err, convey.ShouldBeNil)

	defer func() { _ = conn.Close() }()

	peers := conn.ConnectionState().PeerCertificates
	convey.So(peers, convey.ShouldNotBeEmpty)

	return peers[0]
}

func runDevWriteTestCAForTest(t *testing.T, pemPath string) *runDevTestCAForTest {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	convey.So(err, convey.ShouldBeNil)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "wa test dev CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	convey.So(err, convey.ShouldBeNil)

	cert, err := x509.ParseCertificate(der)
	convey.So(err, convey.ShouldBeNil)

	convey.So(os.WriteFile(pemPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644), convey.ShouldBeNil)

	return &runDevTestCAForTest{cert: cert, key: key, pemPath: pemPath}
}

// runDevWriteTestCertificateForTest writes a cert matching spec to certPath
// and, unless keyPath already exists, a new private key to keyPath.
func runDevWriteTestCertificateForTest(t *testing.T, certPath string, keyPath string, spec runDevTestCertSpecForTest) {
	t.Helper()

	key := runDevLoadOrCreateTestKeyForTest(t, keyPath)
	notBefore := time.Now().Add(-time.Hour)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    notBefore,
		NotAfter:     notBefore.Add(spec.validity),
	}

	for _, host := range spec.hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, host)
		}
	}

	if spec.serverAuth {
		template.BasicConstraintsValid = true
		template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}

	parent := template
	var signer crypto.Signer = key
	if spec.issuer != nil {
		parent, signer = spec.issuer.cert, spec.issuer.key
	}

	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, signer)
	convey.So(err, convey.ShouldBeNil)

	convey.So(os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644), convey.ShouldBeNil)
}

// runDevLoadOrCreateTestKeyForTest returns the RSA key at keyPath, creating
// it first if needed; wa results serve accepts only RSA keys.
func runDevLoadOrCreateTestKeyForTest(t *testing.T, keyPath string) *rsa.PrivateKey {
	t.Helper()

	if body, err := os.ReadFile(keyPath); err == nil {
		block, _ := pem.Decode(body)
		convey.So(block, convey.ShouldNotBeNil)

		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		convey.So(parseErr, convey.ShouldBeNil)

		key, ok := parsed.(*rsa.PrivateKey)
		convey.So(ok, convey.ShouldBeTrue)

		return key
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	convey.So(err, convey.ShouldBeNil)

	der, err := x509.MarshalPKCS8PrivateKey(key)
	convey.So(err, convey.ShouldBeNil)

	convey.So(os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600), convey.ShouldBeNil)

	return key
}
