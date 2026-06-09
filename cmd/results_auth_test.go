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
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/go-resty/resty/v2"
	"github.com/smartystreets/goconvey/convey"
	gas "github.com/wtsi-hgi/go-authserver"
	"github.com/wtsi-hgi/wa/results"
)

const resultsAuthTestPassword = "secret"

func installResultsHTTPClientForTest(t *testing.T, client *http.Client) {
	t.Helper()

	original := resultsHTTPClient
	resultsHTTPClient = client

	t.Cleanup(func() {
		resultsHTTPClient = original
	})
}

type resultsAuthTestServer struct {
	*httptest.Server
	authHeaderCh chan string
	certPath     string
	passwordCh   chan string
	refreshCh    chan string
}

func newResultsAuthTestServer(t *testing.T, password, jwt string) *resultsAuthTestServer {
	t.Helper()

	server := &resultsAuthTestServer{
		authHeaderCh: make(chan string, 1),
		passwordCh:   make(chan string, 1),
		refreshCh:    make(chan string, 1),
	}

	server.Server = httptest.NewTLSServer(resultsAuthTestHandler(server, password, jwt))
	server.certPath = writeResultsAuthServerCertForTest(t, server.Certificate())

	return server
}

func TestResultsAuthClient(t *testing.T) {
	convey.Convey("D1.1: Given a private server token and no JWT, register uses bearer auth without a password prompt", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		token := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
		writeResultsAuthTokenForTest(t, filepath.Join(stateDir, resultsServerTokenBasename), token, 0o600)

		passwordHandler := &resultsAuthPasswordHandler{password: "wrong", terminal: true}
		installGasResultsClientCLIForTest(t, passwordHandler)

		server := newResultsAuthTestServer(t, token, "jwt-owner")
		defer server.Close()

		stdout, stderr, err := executeRootCommandWithInputForRegisterTest(t, resultsAuthRegisterArgs(t, server), nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stdout.String(), convey.ShouldContainSubstring, "result-123")
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(passwordHandler.out, convey.ShouldBeBlank)
		convey.So(passwordHandler.readCalled, convey.ShouldBeFalse)
		convey.So(receiveResultsAuthValueForTest(t, server.authHeaderCh, "auth header"), convey.ShouldEqual, "Bearer jwt-owner")
	})

	convey.Convey("D1.1b: Given a readable server token and stale owner JWT, register logs in with the server token instead of refreshing the stale JWT", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		token := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"
		writeResultsAuthTokenForTest(t, filepath.Join(stateDir, resultsServerTokenBasename), token, 0o600)
		writeResultsAuthTokenForTest(t, filepath.Join(stateDir, resultsJWTBasename), "stale-owner-jwt-abcdefghijklmnopqrstuvwxyz", 0o600)

		passwordHandler := &resultsAuthPasswordHandler{password: "wrong", terminal: true}
		installGasResultsClientCLIForTest(t, passwordHandler)

		server := newResultsAuthTestServer(t, token, "jwt-owner")
		defer server.Close()

		stdout, stderr, err := executeRootCommandWithInputForRegisterTest(t, resultsAuthRegisterArgs(t, server), nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(stdout.String(), convey.ShouldContainSubstring, "result-123")
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(passwordHandler.out, convey.ShouldBeBlank)
		convey.So(passwordHandler.readCalled, convey.ShouldBeFalse)
		convey.So(receiveResultsAuthValueForTest(t, server.passwordCh, "login password"), convey.ShouldEqual, token)
		convey.So(receiveResultsAuthValueForTest(t, server.authHeaderCh, "auth header"), convey.ShouldEqual, "Bearer jwt-owner")
		convey.So(len(server.refreshCh), convey.ShouldEqual, 0)
	})

	convey.Convey("D1.2: Given no token files and a terminal, register prompts for a password and stores a private JWT", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		passwordHandler := &resultsAuthPasswordHandler{password: resultsAuthTestPassword, terminal: true}
		installGasResultsClientCLIForTest(t, passwordHandler)

		server := newResultsAuthTestServer(t, resultsAuthTestPassword, "jwt-password")
		defer server.Close()

		_, _, err := executeRootCommandWithInputForRegisterTest(t, resultsAuthRegisterArgs(t, server), nil)

		convey.So(err, convey.ShouldBeNil)
		convey.So(passwordHandler.out, convey.ShouldEqual, "Password: \n")
		convey.So(passwordHandler.readCalled, convey.ShouldBeTrue)
		convey.So(receiveResultsAuthValueForTest(t, server.passwordCh, "login password"), convey.ShouldEqual, resultsAuthTestPassword)

		jwtPath := filepath.Join(stateDir, resultsJWTBasename)
		stat, statErr := os.Stat(jwtPath)
		convey.So(statErr, convey.ShouldBeNil)
		convey.So(stat.Mode(), convey.ShouldEqual, os.FileMode(0o600))
		convey.So(string(mustReadResultsAuthFileForTest(t, jwtPath)), convey.ShouldEqual, "jwt-password")
	})

	convey.Convey("D1.2b: Given WA_RESULTS_SERVER_CERT and a blank cert argument, register auth uses the env cert for JWT login", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		passwordHandler := &resultsAuthPasswordHandler{password: resultsAuthTestPassword, terminal: true}
		installGasResultsClientCLIForTest(t, passwordHandler)

		server := newResultsAuthTestServer(t, resultsAuthTestPassword, "jwt-password")
		defer server.Close()
		t.Setenv("WA_RESULTS_SERVER_CERT", server.certPath)

		responseBody, err := registerResults(context.Background(), server.URL, "", registerCommandRegistrationForTest())

		convey.So(err, convey.ShouldBeNil)
		convey.So(string(responseBody), convey.ShouldContainSubstring, "result-123")
		convey.So(passwordHandler.readCalled, convey.ShouldBeTrue)
		convey.So(receiveResultsAuthValueForTest(t, server.passwordCh, "login password"), convey.ShouldEqual, resultsAuthTestPassword)
		convey.So(receiveResultsAuthValueForTest(t, server.authHeaderCh, "auth header"), convey.ShouldEqual, "Bearer jwt-password")
	})

	convey.Convey("D1.3: Given loose JWT permissions, register returns the go-authserver permissions error without prompting", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		jwtPath := filepath.Join(stateDir, resultsJWTBasename)
		writeResultsAuthTokenForTest(t, jwtPath, "jwt-with-loose-permissions", 0o777)

		passwordHandler := &resultsAuthPasswordHandler{password: resultsAuthTestPassword, terminal: true}
		installGasResultsClientCLIForTest(t, passwordHandler)

		server := newResultsAuthTestServer(t, resultsAuthTestPassword, "jwt-password")
		defer server.Close()

		_, _, err := executeRootCommandWithInputForRegisterTest(t, resultsAuthRegisterArgs(t, server), nil)

		var permissionsErr gas.JWTPermissionsError
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.As(err, &permissionsErr), convey.ShouldBeTrue)
		convey.So(passwordHandler.out, convey.ShouldBeBlank)
		convey.So(passwordHandler.readCalled, convey.ShouldBeFalse)
	})

	convey.Convey("D1.4: Given an http server URL, authenticated commands reject it before prompting", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		outputDir, workflowPath := writeResultsAuthRegistrationInputsForTest(t)
		_, _, err := executeRootCommandWithInputForRegisterTest(t, []string{
			"results",
			"--server", "http://127.0.0.1:8080",
			"register",
			"--user", "alice",
			"--runid", "48522",
			"--workflow", workflowPath,
			outputDir,
		}, nil)

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldEqual, "results server URL must use https")
	})

	convey.Convey("D1.5: Given a server URL with a path, auth client creation rejects it", t, func() {
		_, err := newResultsAuthClient("https://host:8443/api", "")

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "origin URL with no path")
	})

	convey.Convey("D1: newResultsAuthClient passes host[:port], cert, basenames, and username to go-authserver", t, func() {
		call := installCapturingResultsClientCLIForTest(t)

		client, err := newResultsAuthClient("https://host:8443", "ca.pem", "alice")

		convey.So(err, convey.ShouldBeNil)
		convey.So(client, convey.ShouldNotBeNil)
		convey.So(call.jwtBasename, convey.ShouldEqual, resultsJWTBasename)
		convey.So(call.serverTokenBasename, convey.ShouldEqual, resultsServerTokenBasename)
		convey.So(call.addr, convey.ShouldEqual, "host:8443")
		convey.So(call.cert, convey.ShouldEqual, "ca.pem")
		convey.So(call.oktaMode, convey.ShouldBeFalse)
		convey.So(call.usernames, convey.ShouldResemble, []string{"alice"})
	})

	convey.Convey("D1: owner requests refresh owner login without changing ordinary authenticated requests", t, func() {
		stateDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", stateDir)

		client := &trackingResultsAuthClient{canReadServerToken: true}
		wrapped := &permissionCheckingResultsAuthClient{
			client:              client,
			jwtBasename:         "missing-results.jwt",
			serverTokenBasename: "missing-results-server.token",
		}

		request, err := wrapped.AuthenticatedRequest()

		convey.So(err, convey.ShouldBeNil)
		convey.So(request, convey.ShouldNotBeNil)
		convey.So(client.loginCalls, convey.ShouldEqual, 0)
		convey.So(client.authenticatedRequestCalls, convey.ShouldEqual, 1)

		request, err = wrapped.OwnerAuthenticatedRequest()

		convey.So(err, convey.ShouldBeNil)
		convey.So(request, convey.ShouldNotBeNil)
		convey.So(client.loginCalls, convey.ShouldEqual, 1)
		convey.So(client.authenticatedRequestCalls, convey.ShouldEqual, 2)
	})
}

func receiveResultsAuthValueForTest(t *testing.T, ch <-chan string, label string) string {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)

		return ""
	}
}

func writeResultsAuthTokenForTest(t *testing.T, tokenPath, token string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(tokenPath, []byte(token), mode); err != nil {
		t.Fatalf("write token: %v", err)
	}
	if err := os.Chmod(tokenPath, mode); err != nil {
		t.Fatalf("chmod token: %v", err)
	}
}

func installGasResultsClientCLIForTest(t *testing.T, passwordHandler gas.PasswordHandler) {
	t.Helper()

	original := resultsNewClientCLI
	resultsNewClientCLI = func(jwtBasename, serverTokenBasename, addr, cert string, oktaMode bool, username ...string) (resultsAuthClient, error) {
		client, err := gas.NewClientCLI(jwtBasename, serverTokenBasename, addr, cert, oktaMode, username...)
		if err != nil {
			return nil, err
		}

		setGasClientPasswordHandlerForTest(t, client, passwordHandler)

		return client, nil
	}

	t.Cleanup(func() {
		resultsNewClientCLI = original
	})
}

func setGasClientPasswordHandlerForTest(t *testing.T, client *gas.ClientCLI, passwordHandler gas.PasswordHandler) {
	t.Helper()

	field := reflect.ValueOf(client).Elem().FieldByName("passwordHandler")
	if !field.IsValid() {
		t.Fatal("go-authserver ClientCLI passwordHandler field was not found")
	}

	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).
		Elem().
		Set(reflect.ValueOf(passwordHandler))
}

func mustReadResultsAuthFileForTest(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return content
}

func writeResultsAuthRegistrationInputsForTest(t *testing.T) (string, string) {
	t.Helper()

	outputDir := t.TempDir()
	workflowPath := filepath.Join(t.TempDir(), "main.nf")
	writeRegisterCommandTestFile(t, filepath.Join(outputDir, "out.txt"), "result")
	writeRegisterCommandTestFile(t, workflowPath, "workflow { }\n")

	return outputDir, workflowPath
}

func installCapturingResultsClientCLIForTest(t *testing.T) *resultsClientCLICall {
	t.Helper()

	call := &resultsClientCLICall{}
	original := resultsNewClientCLI
	resultsNewClientCLI = func(jwtBasename, serverTokenBasename, addr, cert string, oktaMode bool, username ...string) (resultsAuthClient, error) {
		call.jwtBasename = jwtBasename
		call.serverTokenBasename = serverTokenBasename
		call.addr = addr
		call.cert = cert
		call.oktaMode = oktaMode
		call.usernames = append([]string(nil), username...)

		return &fakeResultsAuthClient{}, nil
	}

	t.Cleanup(func() {
		resultsNewClientCLI = original
	})

	return call
}

func resultsAuthRegisterArgs(t *testing.T, server *resultsAuthTestServer) []string {
	t.Helper()

	outputDir, workflowPath := writeResultsAuthRegistrationInputsForTest(t)

	return []string{
		"results",
		"--server", server.URL,
		"--cert", server.certPath,
		"register",
		"--user", "alice",
		"--runid", "48522",
		"--workflow", workflowPath,
		outputDir,
	}
}

func resultsAuthTestHandler(server *resultsAuthTestServer, password, jwt string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == gas.EndPointJWT:
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

				return
			}

			server.passwordCh <- r.Form.Get("password")
			if r.Form.Get("password") != password {
				http.Error(w, "authentication failed", http.StatusUnauthorized)

				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, "%q", jwt)
		case r.Method == http.MethodGet && r.URL.Path == gas.EndPointJWT:
			server.refreshCh <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, "%q", "jwt-refreshed")
		case r.Method == http.MethodPost && r.URL.Path == gas.EndPointAuth+"/results":
			server.authHeaderCh <- r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(results.ResultSet{ID: "result-123"})
		default:
			http.NotFound(w, r)
		}
	})
}

type resultsAuthPasswordHandler struct {
	out        string
	password   string
	readCalled bool
	terminal   bool
}

func (p *resultsAuthPasswordHandler) Prompt(msg string, args ...interface{}) {
	p.out += fmt.Sprintf(msg, args...)
}

func (p *resultsAuthPasswordHandler) ReadPassword() ([]byte, error) {
	p.readCalled = true

	return []byte(p.password), nil
}

func (p *resultsAuthPasswordHandler) IsTerminal() bool {
	return p.terminal
}

type resultsClientCLICall struct {
	jwtBasename         string
	serverTokenBasename string
	addr                string
	cert                string
	oktaMode            bool
	usernames           []string
}

type fakeResultsAuthClient struct{}

func (f *fakeResultsAuthClient) AuthenticatedRequest() (*resty.Request, error) {
	return resty.New().R(), nil
}

func (f *fakeResultsAuthClient) CanReadServerToken() bool {
	return false
}

type trackingResultsAuthClient struct {
	canReadServerToken        bool
	loginCalls                int
	authenticatedRequestCalls int
}

func (t *trackingResultsAuthClient) AuthenticatedRequest() (*resty.Request, error) {
	t.authenticatedRequestCalls++

	return resty.New().R(), nil
}

func (t *trackingResultsAuthClient) CanReadServerToken() bool {
	return t.canReadServerToken
}

func (t *trackingResultsAuthClient) Login(_ ...string) error {
	t.loginCalls++

	return nil
}

type passthroughResultsAuthClient struct {
	serverURL string
}

func (p *passthroughResultsAuthClient) AuthenticatedRequest() (*resty.Request, error) {
	client := resty.New()
	client.SetBaseURL(p.serverURL)
	client.SetAuthToken("test-jwt")
	if resultsHTTPClient != nil && resultsHTTPClient.Transport != nil {
		client.SetTransport(resultsHTTPClient.Transport)
	}

	return client.R(), nil
}

func (p *passthroughResultsAuthClient) CanReadServerToken() bool {
	return true
}

func installPassthroughResultsAuthClientForTest(t *testing.T) func() {
	t.Helper()

	original := resultsNewAuthClient
	resultsNewAuthClient = func(serverURL, _ string, _ ...string) (resultsAuthClient, error) {
		return &passthroughResultsAuthClient{serverURL: serverURL}, nil
	}

	restore := func() {
		resultsNewAuthClient = original
	}
	t.Cleanup(restore)

	return restore
}

func writeResultsAuthServerCertForTest(t *testing.T, cert *x509.Certificate) string {
	t.Helper()

	certPath := filepath.Join(t.TempDir(), "server.pem")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("write test certificate: %v", err)
	}

	return certPath
}
