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

package mlwh

import (
	"context"
	"net/http"
	"net/netip"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

const (
	clientIPTestProxyHost    = usageTestRemoteHost // 192.0.2.1
	clientIPTestProxyCIDR    = "192.0.2.0/24"
	clientIPTestEndUser      = "203.0.113.9"
	clientIPTestSpoofed      = "198.51.100.50"
	clientIPTestUntrusted    = "198.51.100.7"
	clientIPTestInnerProxy   = "10.1.2.3"
	clientIPTestInnerCIDR    = "10.0.0.0/8"
	clientIPTestCLIUserAgent = "wa-cli/v0.11.0"
)

func TestParseTrustedProxies(t *testing.T) {
	convey.Convey("Given a blank value, then no proxy is trusted", t, func() {
		for _, value := range []string{"", "  ", " , ,"} {
			proxies, err := ParseTrustedProxies(value)
			convey.So(err, convey.ShouldBeNil)
			convey.So(proxies, convey.ShouldBeEmpty)
		}
	})

	convey.Convey("Given comma and space separated IPs and CIDRs, then each becomes a masked prefix", t, func() {
		proxies, err := ParseTrustedProxies(" 10.42.0.0/16, 192.0.2.5  2001:db8::/32,::1 10.1.2.3/8")
		convey.So(err, convey.ShouldBeNil)
		convey.So(proxies, convey.ShouldResemble, []netip.Prefix{
			netip.MustParsePrefix("10.42.0.0/16"),
			netip.MustParsePrefix("192.0.2.5/32"),
			netip.MustParsePrefix("2001:db8::/32"),
			netip.MustParsePrefix("::1/128"),
			netip.MustParsePrefix("10.0.0.0/8"),
		})
	})

	convey.Convey("Given an invalid entry, then parsing fails naming it", t, func() {
		for _, bad := range []string{"nope", "10.0.0.0/33", "fe80::1%eth0", "10.0.0.1/x", "300.1.1.1"} {
			proxies, err := ParseTrustedProxies("10.0.0.0/8," + bad)
			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldContainSubstring, `"`+bad+`"`)
			convey.So(proxies, convey.ShouldBeNil)
		}
	})
}

func TestUsageRemoteAddrTrustedProxies(t *testing.T) {
	convey.Convey("Given no trusted proxies, when a client sends X-Forwarded-For and X-Real-IP, "+
		"then the stored address is the socket IP", t, func() {
		convey.So(usageRemoteAddrForTest(t, nil, map[string]string{
			"X-Forwarded-For": clientIPTestEndUser,
			"X-Real-IP":       clientIPTestEndUser,
		}), convey.ShouldEqual, clientIPTestProxyHost)
	})

	convey.Convey("Given the socket IP is a trusted proxy, when it sends X-Forwarded-For, "+
		"then the stored address is the forwarded client", t, func() {
		convey.So(usageRemoteAddrForTest(t, []string{clientIPTestProxyCIDR}, map[string]string{
			"X-Forwarded-For": clientIPTestEndUser,
		}), convey.ShouldEqual, clientIPTestEndUser)
	})

	convey.Convey("Given the socket IP is a trusted proxy, when it sends only X-Real-IP, "+
		"then the stored address is that client", t, func() {
		convey.So(usageRemoteAddrForTest(t, []string{clientIPTestProxyHost}, map[string]string{
			"X-Real-IP": clientIPTestEndUser,
		}), convey.ShouldEqual, clientIPTestEndUser)
	})

	convey.Convey("Given a trusted proxy that sends no forwarding header, then the stored address is the socket IP", t, func() {
		convey.So(usageRemoteAddrForTest(t, []string{clientIPTestProxyCIDR}, nil), convey.ShouldEqual, clientIPTestProxyHost)
	})

	convey.Convey("Given trusted proxies, when an untrusted client sends a spoofed X-Forwarded-For, "+
		"then the header is ignored and the socket IP is stored", t, func() {
		convey.So(usageRemoteAddrFromForTest(t, []string{clientIPTestProxyCIDR}, clientIPTestUntrusted, map[string]string{
			"X-Forwarded-For": clientIPTestSpoofed,
			"X-Real-IP":       clientIPTestSpoofed,
		}), convey.ShouldEqual, clientIPTestUntrusted)
	})

	convey.Convey("Given two trusted proxy hops, when the client also spoofs a leading X-Forwarded-For entry, "+
		"then the stored address is the right-most untrusted hop", t, func() {
		convey.So(usageRemoteAddrForTest(t, []string{clientIPTestProxyCIDR, clientIPTestInnerCIDR}, map[string]string{
			"X-Forwarded-For": clientIPTestSpoofed + ", " + clientIPTestEndUser + ", " + clientIPTestInnerProxy,
		}), convey.ShouldEqual, clientIPTestEndUser)
	})

	convey.Convey("Given only the outer proxy is trusted, when the header lists an untrusted inner hop, "+
		"then that hop is stored", t, func() {
		convey.So(usageRemoteAddrForTest(t, []string{clientIPTestProxyCIDR}, map[string]string{
			"X-Forwarded-For": clientIPTestEndUser + ", " + clientIPTestInnerProxy,
		}), convey.ShouldEqual, clientIPTestInnerProxy)
	})
}

func TestFeedbackRemoteAddrTrustedProxies(t *testing.T) {
	ctx := context.Background()

	convey.Convey("Given a trusted proxy, when it forwards a feedback POST, then remote_addr is the forwarded client", t, func() {
		store := newFeedbackServerTestStore(t)
		router := newFeedbackTestRouterWithOptions(store, WithTrustedProxies(mustParseTrustedProxiesForTest(clientIPTestProxyCIDR)))

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.Header.Set("X-Forwarded-For", clientIPTestEndUser)
		})

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(storedFeedbackRemoteAddrs(ctx, store), convey.ShouldResemble, []string{clientIPTestEndUser})
	})

	convey.Convey("Given a trusted proxy, when an untrusted client POSTs a spoofed X-Forwarded-For, "+
		"then remote_addr is the socket IP", t, func() {
		store := newFeedbackServerTestStore(t)
		router := newFeedbackTestRouterWithOptions(store, WithTrustedProxies(mustParseTrustedProxiesForTest(clientIPTestProxyCIDR)))

		response := postFeedbackForTest(router, feedbackExampleBodyForTest, func(request *http.Request) {
			request.RemoteAddr = clientIPTestUntrusted + ":4321"
			request.Header.Set("X-Forwarded-For", clientIPTestSpoofed)
		})

		convey.So(response.Code, convey.ShouldEqual, http.StatusCreated)
		convey.So(storedFeedbackRemoteAddrs(ctx, store), convey.ShouldResemble, []string{clientIPTestUntrusted})
	})
}

func mustParseTrustedProxiesForTest(entries ...string) []netip.Prefix {
	proxies := make([]netip.Prefix, 0, len(entries))

	for _, entry := range entries {
		parsed, err := ParseTrustedProxies(entry)
		convey.So(err, convey.ShouldBeNil)

		proxies = append(proxies, parsed...)
	}

	return proxies
}

// usageRemoteAddrForTest serves one counted request from the proxy host and
// returns the stored remote_addr.
func usageRemoteAddrForTest(t *testing.T, trusted []string, headers map[string]string) string {
	t.Helper()

	return usageRemoteAddrFromForTest(t, trusted, clientIPTestProxyHost, headers)
}

// usageRemoteAddrFromForTest serves one counted request whose socket IP is
// socketIP, with trusted as the server's trusted proxies, and returns the
// stored remote_addr.
func usageRemoteAddrFromForTest(t *testing.T, trusted []string, socketIP string, headers map[string]string) string {
	t.Helper()

	router, store, path := newUsageTestRouter(t, &feedbackStudiesQueryerForTest{},
		WithTrustedProxies(mustParseTrustedProxiesForTest(trusted...)))

	request := map[string]string{"User-Agent": clientIPTestCLIUserAgent}
	for name, value := range headers {
		request[name] = value
	}

	response := usageRequestFromForTest(router, socketIP, http.MethodGet, "/studies", request)
	convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

	rows := flushedUsageRowsForTest(store, path)
	convey.So(rows, convey.ShouldHaveLength, 1)

	return rows[0].RemoteAddr
}
