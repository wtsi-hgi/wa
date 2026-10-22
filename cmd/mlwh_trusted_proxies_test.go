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
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

const mlwhTrustedProxiesTestEndUser = "203.0.113.9"

func TestMLWHServeTrustedProxies(t *testing.T) {
	convey.Convey("Given no trusted proxies, when a loopback client sends X-Forwarded-For, "+
		"then feedback stores the socket IP", t, func() {
		convey.So(mlwhServeFeedbackRemoteAddrForTest(t, nil), convey.ShouldEqual, "127.0.0.1")
	})

	convey.Convey("Given WA_MLWH_TRUSTED_PROXIES names loopback, when it forwards a client, "+
		"then feedback stores the forwarded client", t, func() {
		convey.So(mlwhServeFeedbackRemoteAddrForTest(t, map[string]string{"WA_MLWH_TRUSTED_PROXIES": "10.0.0.0/8, 127.0.0.1"}),
			convey.ShouldEqual, mlwhTrustedProxiesTestEndUser)
	})

	convey.Convey("Given --trusted-proxies names loopback, then it overrides an env value that does not", t, func() {
		convey.So(mlwhServeFeedbackRemoteAddrForTest(t, map[string]string{"WA_MLWH_TRUSTED_PROXIES": "10.0.0.0/8"},
			"--trusted-proxies", "127.0.0.0/8"), convey.ShouldEqual, mlwhTrustedProxiesTestEndUser)
	})

	convey.Convey("Given an invalid WA_MLWH_TRUSTED_PROXIES, then mlwh serve fails naming it before starting", t, func() {
		fakeAuth := newFakeMLWHServeAuthServer()
		installFakeMLWHServeAuthServer(t, fakeAuth)
		t.Setenv("WA_MLWH_TRUSTED_PROXIES", "10.0.0.0/8,not-an-ip")

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0",
			"--mlwh-cache", prepareMLWHServeCacheForTest(t, true)})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "WA_MLWH_TRUSTED_PROXIES")
		convey.So(err.Error(), convey.ShouldContainSubstring, `"not-an-ip"`)
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 0)
	})

	convey.Convey("Given an invalid --trusted-proxies, then mlwh serve fails naming the flag", t, func() {
		fakeAuth := newFakeMLWHServeAuthServer()
		installFakeMLWHServeAuthServer(t, fakeAuth)

		_, err := executeRootCommandForTest(t, []string{"mlwh", "serve", "--port", "0",
			"--mlwh-cache", prepareMLWHServeCacheForTest(t, true), "--trusted-proxies", "10.0.0.0/33"})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "--trusted-proxies")
		convey.So(fakeAuth.startCalls, convey.ShouldHaveLength, 0)
	})
}

// mlwhServeFeedbackRemoteAddrForTest runs the real mlwh serve with feedback on,
// posts one report from loopback carrying X-Forwarded-For, and returns the
// stored remote_addr.
func mlwhServeFeedbackRemoteAddrForTest(t *testing.T, env map[string]string, extraArgs ...string) string {
	t.Helper()

	stateDir := t.TempDir()
	baseURL, stop := startMLWHServeEndToEndWithEnvForTest(t, stateDir, env,
		append([]string{"--feedback-db", filepath.Join(stateDir, "fb.sqlite")}, extraArgs...)...)

	submit := doMLWHServeHTTPRequestWithHeaderForTest(t, http.MethodPost, baseURL+"/feedback",
		mlwhFeedbackExampleSubmissionForTest, http.Header{"X-Forwarded-For": {mlwhTrustedProxiesTestEndUser}})
	convey.So(submit.status, convey.ShouldEqual, http.StatusCreated)

	token, err := os.ReadFile(filepath.Join(stateDir, ".wa-mlwh-server.token"))
	convey.So(err, convey.ShouldBeNil)

	list := doMLWHServeHTTPRequestForTest(t, http.MethodGet, baseURL+"/feedback", "", "Bearer "+string(token))
	convey.So(list.status, convey.ShouldEqual, http.StatusOK)
	convey.So(stop(), convey.ShouldBeNil)

	var page mlwh.Page[mlwh.FeedbackReport]
	convey.So(json.Unmarshal(list.body, &page), convey.ShouldBeNil)
	convey.So(page.Items, convey.ShouldHaveLength, 1)

	return page.Items[0].RemoteAddr
}
