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

package saga

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestMLWHFindSamplesByRunID(t *testing.T) {
	Convey("Given MLWH supports a run-id filters query for samples", t, func() {
		var requestedPath string
		var requestedFiltersValue string
		var requestedFilter map[string]any

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestedPath = r.URL.Path
			requestedFiltersValue = r.URL.Query().Get("filters")

			if err := json.Unmarshal([]byte(requestedFiltersValue), &requestedFilter); err != nil {
				t.Fatalf("failed to decode filters: %v", err)
			}

			_, _ = w.Write([]byte(`{"items":[{"id_run":34134},{"id_run":34134},{"id_run":34134}],"total":3,"offset":0,"limit":3}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.MLWH().FindSamplesByRunID(context.Background(), 34134)

		Convey("when FindSamplesByRunID is called, then it sends the live contract's normally encoded run_id filter", func() {
			So(err, ShouldBeNil)
			So(requestedPath, ShouldEqual, "/integrations/mlwh/samples")
			So(requestedFiltersValue, ShouldEqual, `{"run_id":"34134"}`)
			So(requestedFilter, ShouldResemble, map[string]any{"run_id": "34134"})
			So(samples, ShouldHaveLength, 3)
		})
	})

	Convey("Given MLWH returns no samples for a run-id filter", t, func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		}))
		Reset(server.Close)

		client, err := NewClient("test-key", WithBaseURL(server.URL))
		So(err, ShouldBeNil)
		Reset(client.Close)

		samples, err := client.MLWH().FindSamplesByRunID(context.Background(), 34134)

		Convey("when FindSamplesByRunID is called, then it returns an empty non-nil slice and no error", func() {
			So(err, ShouldBeNil)
			So(samples, ShouldNotBeNil)
			So(samples, ShouldHaveLength, 0)
		})
	})
}
