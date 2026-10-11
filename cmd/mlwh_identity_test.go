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
	"errors"
	"net/http"
	"net/http/httptest"
	"os/user"
	"path/filepath"
	"regexp"
	"sync"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

var errIdentityTestStopOpen = errors.New("identity test: stop after capturing config")

func TestMLWHCLIRemoteConfigD2(t *testing.T) {
	convey.Convey("D2.1: Given mlwhCLIRemoteConfig called twice, then both share one run ID and carry the CLI User-Agent and local username", t, func() {
		first := mlwhCLIRemoteConfig("http://x")
		second := mlwhCLIRemoteConfig("http://x")

		current, err := user.Current()
		convey.So(err, convey.ShouldBeNil)

		for _, cfg := range []mlwh.RemoteConfig{first, second} {
			convey.So(cfg.BaseURL, convey.ShouldEqual, "http://x")
			convey.So(regexp.MustCompile(`^wa-cli/\S+$`).MatchString(cfg.UserAgent), convey.ShouldBeTrue)
			convey.So(regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(cfg.Identity.ClientID), convey.ShouldBeTrue)
			convey.So(cfg.Identity.Username, convey.ShouldEqual, current.Username)
		}

		convey.So(second.Identity.ClientID, convey.ShouldEqual, first.Identity.ClientID)
	})

	convey.Convey("D2.2: Given waBuildVersion under go test, then it is non-empty and contains no space", t, func() {
		version := waBuildVersion()

		convey.So(version, convey.ShouldNotBeEmpty)
		convey.So(version, convey.ShouldNotContainSubstring, " ")
	})
}

func TestMLWHDiffCommandsSelectServerIdentityD2(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		asServer bool
	}{
		{name: "serve", args: []string{"mlwhdiff", "serve", "--port", "0"}, asServer: true},
		{name: "diff", args: []string{"mlwhdiff", "diff", "--study", "5901"}, asServer: false},
	}

	for _, tc := range cases {
		convey.Convey("D2.6: Given wa mlwhdiff "+tc.name+", then the MLWH config it opens has the expected AsServer", t, func() {
			t.Setenv("WA_MLWH_SERVER_URL", "http://mlwh.example:9000")
			t.Setenv("WA_MLWH_CACHE_PATH", "")

			original := openMLWHDiffClientFunc
			t.Cleanup(func() { openMLWHDiffClientFunc = original })

			var captured []mlwhdiffMLWHConfig
			openMLWHDiffClientFunc = func(_ context.Context, cfg mlwhdiffMLWHConfig) (mlwhdiffMLWHHandle, error) {
				captured = append(captured, cfg)

				return nil, errIdentityTestStopOpen
			}

			args := append(append([]string{}, tc.args...), "--db", filepath.Join(t.TempDir(), "mlwhdiff.db"))
			_, err := executeRootCommandForTest(t, args)

			convey.So(errors.Is(err, errIdentityTestStopOpen), convey.ShouldBeTrue)
			convey.So(captured, convey.ShouldHaveLength, 1)
			convey.So(captured[0].ServerURL, convey.ShouldEqual, "http://mlwh.example:9000")
			convey.So(captured[0].AsServer, convey.ShouldEqual, tc.asServer)
		})
	}
}

type identityHeaderStubForTest struct {
	url     string
	mu      sync.Mutex
	headers []http.Header
}

func startIdentityHeaderStubForTest(t *testing.T) *identityHeaderStubForTest {
	t.Helper()

	stub := &identityHeaderStubForTest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.headers = append(stub.headers, r.Header.Clone())
		stub.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(server.Close)
	stub.url = server.URL

	return stub
}

func (s *identityHeaderStubForTest) only(t *testing.T) http.Header {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.headers) != 1 {
		t.Fatalf("stub saw %d requests, want 1", len(s.headers))
	}

	return s.headers[0]
}

func TestInternalServerRemoteIdentitiesD2(t *testing.T) {
	convey.Convey("D2.4: Given the results serve MLWH client, when AllStudies runs, then it identifies as the results server with no client identity", t, func() {
		headers := startIdentityHeaderStubForTest(t)

		handle, err := openResultsServeMLWHClientWithConfig(context.Background(), resultsServeMLWHConfig{ServerURL: headers.url})
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = handle.Close() }()

		_, err = handle.AllStudies(context.Background(), 1, 0)
		convey.So(err, convey.ShouldBeNil)

		header := headers.only(t)
		convey.So(header.Get("User-Agent"), convey.ShouldEqual, mlwh.UserAgentProductResultsServer+"/"+waBuildVersion())
		convey.So(header.Values(mlwh.HeaderClientID), convey.ShouldBeEmpty)
		convey.So(header.Values(mlwh.HeaderClientUser), convey.ShouldBeEmpty)
	})

	convey.Convey("D2.5: Given the mlwhdiff MLWH client opened as a server, then it identifies as the mlwhdiff server with no client identity", t, func() {
		header := mlwhdiffRemoteRequestHeaderForTest(t, true)

		convey.So(header.Get("User-Agent"), convey.ShouldEqual, mlwh.UserAgentProductMLWHDiffServer+"/"+waBuildVersion())
		convey.So(header.Values(mlwh.HeaderClientID), convey.ShouldBeEmpty)
		convey.So(header.Values(mlwh.HeaderClientUser), convey.ShouldBeEmpty)
	})

	convey.Convey("D2.5: Given the mlwhdiff MLWH client opened for the CLI, then it identifies as the CLI with a run ID", t, func() {
		header := mlwhdiffRemoteRequestHeaderForTest(t, false)

		convey.So(header.Get("User-Agent"), convey.ShouldEqual, mlwh.UserAgentProductCLI+"/"+waBuildVersion())
		convey.So(header.Get(mlwh.HeaderClientID), convey.ShouldEqual, mlwhCLIRemoteConfig("").Identity.ClientID)
		convey.So(header.Get(mlwh.HeaderClientID), convey.ShouldNotBeEmpty)
	})
}

func mlwhdiffRemoteRequestHeaderForTest(t *testing.T, asServer bool) http.Header {
	t.Helper()

	headers := startIdentityHeaderStubForTest(t)

	handle, err := openMLWHDiffClientWithConfig(context.Background(), mlwhdiffMLWHConfig{ServerURL: headers.url, AsServer: asServer})
	if err != nil {
		t.Fatalf("open mlwhdiff MLWH client: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })

	remote, ok := handle.(*mlwh.RemoteClient)
	if !ok {
		t.Fatalf("mlwhdiff MLWH handle is %T, want *mlwh.RemoteClient", handle)
	}

	if _, err = remote.AllStudies(context.Background(), 1, 0); err != nil {
		t.Fatalf("AllStudies: %v", err)
	}

	return headers.only(t)
}

func TestMLWHCommandsUseCLIRemoteConfigD2(t *testing.T) {
	const serverURL = "http://mlwh.example:8091"

	cases := []struct {
		name string
		args []string
	}{
		{name: "info", args: []string{"mlwh", "info", "DN1234", "--server", serverURL}},
		{name: "latest", args: []string{"mlwh", "latest", "5901", "--server", serverURL}},
		{name: "search", args: []string{"mlwh", "search", "lung", "--server", serverURL}},
		{name: "runs", args: []string{"mlwh", "runs", "--server", serverURL}},
		{name: "export", args: []string{"mlwh", "export", "runs", "sample", "DN1234", "--server", serverURL}},
		{name: "studies", args: []string{"mlwh", "studies", "--programme", "Human Genetics", "--server", serverURL}},
		{name: "programmes", args: []string{"mlwh", "programmes", "--server", serverURL}},
		{name: "people", args: []string{"mlwh", "people", "carl", "--server", serverURL}},
	}

	for _, tc := range cases {
		convey.Convey("D2.3: Given wa mlwh "+tc.name+" --server <url>, then its remote config equals mlwhCLIRemoteConfig(<url>)", t, func() {
			clearMLWHIdentityCommandEnvForTest(t)
			captured := captureMLWHCommandRemoteConfigsForTest(t)

			_, err := executeRootCommandForTest(t, tc.args)

			convey.So(errors.Is(err, errIdentityTestStopOpen), convey.ShouldBeTrue)
			convey.So(*captured, convey.ShouldHaveLength, 1)
			convey.So((*captured)[0], convey.ShouldResemble, mlwhCLIRemoteConfig(serverURL))
		})
	}
}

func clearMLWHIdentityCommandEnvForTest(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"WA_MLWH_DSN", "WA_MLWH_SERVER_URL", "WA_MLWH_BACKEND_URL", "WA_MLWH_CACHE_PATH",
		"WA_MLWH_CACHE_PASSWORD", "WA_ENV", "WA_TEST_SEQMETA_PORT", "WA_DEV_SEQMETA_PORT",
		"WA_PROD_SEQMETA_PORT",
	} {
		t.Setenv(name, "")
	}
}

// captureMLWHCommandRemoteConfigsForTest replaces every wa mlwh remote opener
// with one that records its config and fails, so the command stops there.
func captureMLWHCommandRemoteConfigsForTest(t *testing.T) *[]mlwh.RemoteConfig {
	t.Helper()

	captured := &[]mlwh.RemoteConfig{}
	record := func(cfg mlwh.RemoteConfig) error {
		*captured = append(*captured, cfg)

		return errIdentityTestStopOpen
	}

	originalInfo, originalLatest := openMLWHInfoRemoteClient, openMLWHLatestRemoteClient
	originalSearch, originalRuns := openMLWHSearchRemoteClient, openMLWHRunsRemoteClient
	originalExport, originalStudies := openMLWHExportRemoteClient, openMLWHStudiesRemoteClient
	t.Cleanup(func() {
		openMLWHInfoRemoteClient, openMLWHLatestRemoteClient = originalInfo, originalLatest
		openMLWHSearchRemoteClient, openMLWHRunsRemoteClient = originalSearch, originalRuns
		openMLWHExportRemoteClient, openMLWHStudiesRemoteClient = originalExport, originalStudies
	})

	openMLWHInfoRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhInfoClient, error) {
		return nil, record(cfg)
	}
	openMLWHLatestRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhLatestClient, error) {
		return nil, record(cfg)
	}
	openMLWHSearchRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhSearchClient, error) {
		return nil, record(cfg)
	}
	openMLWHRunsRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhRunsClient, error) {
		return nil, record(cfg)
	}
	openMLWHExportRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhExportClient, error) {
		return nil, record(cfg)
	}
	openMLWHStudiesRemoteClient = func(_ context.Context, cfg mlwh.RemoteConfig) (mlwhStudiesClient, error) {
		return nil, record(cfg)
	}

	return captured
}
