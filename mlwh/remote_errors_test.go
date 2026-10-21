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
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/smartystreets/goconvey/convey"
)

// remoteErrorInjectPrefix marks ResolveStudy identifiers for which
// newRemoteErrorInjectingServerForTest writes an error envelope itself; every
// other request reaches the real MLWH server routes.
const remoteErrorInjectPrefix = "inject-"

type remoteInjectedEnvelope struct {
	status  int
	code    string
	message string
}

// newRemoteErrorInjectingServerForTest serves the real MLWH routes, except that
// GET /resolve/study/inject-<name> answers with envelopes[name] through the server's
// own error writer.
func newRemoteErrorInjectingServerForTest(t *testing.T, envelopes map[string]remoteInjectedEnvelope) *httptest.Server {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		name, ok := strings.CutPrefix(c.Request.URL.Path, "/resolve/study/"+remoteErrorInjectPrefix)
		if !ok {
			return
		}

		envelope, found := envelopes[name]
		if !found {
			return
		}

		writeErrorEnvelope(c, envelope.status, envelope.code, envelope.message)
		c.Abort()
	})
	NewServer(&serverFakeQueryer{}).RegisterRoutes(router, nil)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return server
}

func TestRemoteClientMapsBadRequestFromRealServer(t *testing.T) {
	convey.Convey("Given a real MLWH server", t, func() {
		server := newRemoteErrorInjectingServerForTest(t, nil)
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when a search asks for more than the maximum limit, then the 400 bad_request is a bad request "+
			"carrying the server's message and status, not an impaired upstream", func() {
			_, err := client.SearchStudiesPage(context.Background(), "malar", SearchMaxLimit+1, 0)

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldEqual, fmt.Sprintf("mlwh: bad request: limit must not exceed %d", SearchMaxLimit))
			convey.So(errors.Is(err, ErrBadRequest), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrRequestRejected), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeFalse)

			var requestErr *RemoteRequestError
			convey.So(errors.As(err, &requestErr), convey.ShouldBeTrue)
			convey.So(requestErr.StatusCode, convey.ShouldEqual, http.StatusBadRequest)
			convey.So(requestErr.Code, convey.ShouldEqual, "bad_request")
			convey.So(requestErr.Message, convey.ShouldEqual, fmt.Sprintf("limit must not exceed %d", SearchMaxLimit))
		})
	})
}

func TestRemoteClientMapsUnknownClientErrorsFromRealServer(t *testing.T) {
	convey.Convey("Given a real MLWH server that answers some requests with 4xx codes the client has no sentinel for", t, func() {
		envelopes := map[string]remoteInjectedEnvelope{
			"unauthorized": {status: http.StatusUnauthorized, code: "unauthorized", message: "missing token"},
			"too-large":    {status: http.StatusRequestEntityTooLarge, code: "payload_too_large", message: "body too large"},
			"teapot":       {status: http.StatusTeapot, code: "teapot_code", message: "short and stout"},
		}
		server := newRemoteErrorInjectingServerForTest(t, envelopes)
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		for _, name := range []string{"unauthorized", "too-large", "teapot"} {
			envelope := envelopes[name]
			convey.Convey(fmt.Sprintf("when the server answers %d %s, then the error is a rejected request keeping the "+
				"server's status, code and message, and is neither a bad request nor an impaired upstream", envelope.status, envelope.code), func() {
				_, err := client.ResolveStudy(context.Background(), remoteErrorInjectPrefix+name)

				convey.So(err, convey.ShouldNotBeNil)
				convey.So(err.Error(), convey.ShouldEqual,
					fmt.Sprintf("mlwh: request rejected (%d %s): %s", envelope.status, envelope.code, envelope.message))
				convey.So(errors.Is(err, ErrRequestRejected), convey.ShouldBeTrue)
				convey.So(errors.Is(err, ErrBadRequest), convey.ShouldBeFalse)
				convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeFalse)

				var requestErr *RemoteRequestError
				convey.So(errors.As(err, &requestErr), convey.ShouldBeTrue)
				convey.So(requestErr.StatusCode, convey.ShouldEqual, envelope.status)
				convey.So(requestErr.Code, convey.ShouldEqual, envelope.code)
				convey.So(requestErr.Message, convey.ShouldEqual, envelope.message)
			})
		}
	})

	convey.Convey("Given a real gin router without the MLWH routes, as an older server lacking an endpoint is", t, func() {
		gin.SetMode(gin.TestMode)
		server := httptest.NewServer(gin.New())
		defer server.Close()
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when its plain-text 404 comes back, then the error is a rejected request that still explains the "+
			"missing envelope, not an impaired upstream", func() {
			_, err := client.ResolveStudy(context.Background(), "7568")

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(errors.Is(err, ErrRequestRejected), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrBadRequest), convey.ShouldBeFalse)
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeFalse)
			convey.So(err.Error(), convey.ShouldStartWith, "mlwh: request rejected (404): ")
			convey.So(err.Error(), convey.ShouldContainSubstring, "remote ResolveStudy returned 404 without a valid MLWH error envelope")

			var requestErr *RemoteRequestError
			convey.So(errors.As(err, &requestErr), convey.ShouldBeTrue)
			convey.So(requestErr.StatusCode, convey.ShouldEqual, http.StatusNotFound)
			convey.So(requestErr.Code, convey.ShouldEqual, "")
		})
	})
}

func TestRemoteClientMapsUnknownServerErrorsFromRealServer(t *testing.T) {
	convey.Convey("Given a real MLWH server that answers 500 internal_error, a 5xx code the client has no sentinel for", t, func() {
		server := newRemoteErrorInjectingServerForTest(t, map[string]remoteInjectedEnvelope{
			"internal": {status: http.StatusInternalServerError, code: "internal_error", message: "store failed"},
		})
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		convey.Convey("when ResolveStudy gets it, then the error is still an impaired upstream with the server's message", func() {
			_, err := client.ResolveStudy(context.Background(), remoteErrorInjectPrefix+"internal")

			convey.So(err, convey.ShouldNotBeNil)
			convey.So(err.Error(), convey.ShouldEqual, "store failed: "+ErrUpstreamImpaired.Error())
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeTrue)
			convey.So(errors.Is(err, ErrRequestRejected), convey.ShouldBeFalse)

			var requestErr *RemoteRequestError
			convey.So(errors.As(err, &requestErr), convey.ShouldBeFalse)
		})
	})
}

func TestRemoteClientKeepsMappedCodesFromRealServer(t *testing.T) {
	convey.Convey("Given a real MLWH server answering each envelope code the client already maps", t, func() {
		cases := []struct {
			name     string
			envelope remoteInjectedEnvelope
			sentinel error
		}{
			{"not-found", remoteInjectedEnvelope{http.StatusNotFound, "not_found", "study x not found"}, ErrNotFound},
			{"ambiguous", remoteInjectedEnvelope{http.StatusConflict, "ambiguous", "x matches 2 studies"}, ErrAmbiguous},
			{"unsupported", remoteInjectedEnvelope{http.StatusUnprocessableEntity, "unsupported_identifier", "bad form"}, ErrUnsupportedIdentifier},
			{"impaired", remoteInjectedEnvelope{http.StatusBadGateway, "upstream_impaired", "db down"}, ErrUpstreamImpaired},
			{"never-synced", remoteInjectedEnvelope{http.StatusServiceUnavailable, "cache_never_synced", "no sync"}, ErrCacheNeverSynced},
		}
		envelopes := make(map[string]remoteInjectedEnvelope, len(cases))
		for _, tc := range cases {
			envelopes[tc.name] = tc.envelope
		}
		server := newRemoteErrorInjectingServerForTest(t, envelopes)
		client := newRemoteClientForTest(t, server.URL, "")
		defer closeRemoteClientForTest(t, client)

		for _, tc := range cases {
			convey.Convey("when the server answers "+tc.envelope.code+", then the error keeps its sentinel and message format", func() {
				_, err := client.ResolveStudy(context.Background(), remoteErrorInjectPrefix+tc.name)

				convey.So(err, convey.ShouldNotBeNil)
				convey.So(err.Error(), convey.ShouldEqual, tc.envelope.message+": "+tc.sentinel.Error())
				convey.So(errors.Is(err, tc.sentinel), convey.ShouldBeTrue)
				convey.So(errors.Is(err, ErrRequestRejected), convey.ShouldBeFalse)
				convey.So(errors.Is(err, ErrBadRequest), convey.ShouldBeFalse)
			})
		}
	})
}
