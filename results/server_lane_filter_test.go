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

package results

import (
	"net/http"
	"testing"

	convey "github.com/smartystreets/goconvey/convey"
)

// TestSeqmetaLaneFiltering is a regression test for bugfix 260501-4.
//
// Before: Lane rows in seqmeta details had no Filter button, and seqmeta_lane was not recognized as a filter.
// After: Lane filtering works as a first-class citizen alongside samples, libraries, and studies.
func TestSeqmetaLaneFiltering(t *testing.T) {
	convey.Convey("Given results with seqmeta_lane metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		seedResultSetForTest(t, store, searchRegistrationForTest("run-lane1", func(reg *Registration) {
			reg.Requester = "bob"
			reg.Metadata = map[string]string{"seqmeta_lane": "12345_1#10"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-lane2", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_lane": "12345_2#20"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-no-lane", func(reg *Registration) {
			reg.Metadata = map[string]string{"seqmeta_sampleid": "SANG1"}
		}))

		convey.Convey("When filtered by seqmeta_lane=12345_1#10, then only the matching result is returned", func() {
			response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?seqmeta_lane=12345_1%2310", nil)

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var results []ResultSet
			decodeJSONResponseForTest(t, response, &results)

			convey.So(results, convey.ShouldHaveLength, 1)
			convey.So(results[0].Metadata["seqmeta_lane"], convey.ShouldEqual, "12345_1#10")
		})

		convey.Convey("When filtered by seqmeta_lane=12345_2#20, then the other lane is returned", func() {
			response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?seqmeta_lane=12345_2%2320", nil)

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var results []ResultSet
			decodeJSONResponseForTest(t, response, &results)

			convey.So(results, convey.ShouldHaveLength, 1)
			convey.So(results[0].Metadata["seqmeta_lane"], convey.ShouldEqual, "12345_2#20")
		})

		convey.Convey("When filtered by a non-matching lane, then no results are returned", func() {
			response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?seqmeta_lane=99999_9%2399", nil)

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var results []ResultSet
			decodeJSONResponseForTest(t, response, &results)

			convey.So(results, convey.ShouldHaveLength, 0)
		})

		convey.Convey("When filtered by seqmeta_lane combined with user, then both filters are applied", func() {
			seedResultSetForTest(t, store, searchRegistrationForTest("run-alice-lane", func(reg *Registration) {
				reg.Requester = "alice"
				reg.Metadata = map[string]string{"seqmeta_lane": "12345_1#10"}
			}))

			response := performResultsRequestForTest(t, NewServer(store, nil, nil).Handler(), http.MethodGet, "/results?seqmeta_lane=12345_1%2310&user=alice", nil)

			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var results []ResultSet
			decodeJSONResponseForTest(t, response, &results)

			// Should only return the alice+lane result, not the original lane-only result
			convey.So(results, convey.ShouldHaveLength, 1)
			convey.So(results[0].Requester, convey.ShouldEqual, "alice")
			convey.So(results[0].Metadata["seqmeta_lane"], convey.ShouldEqual, "12345_1#10")
		})
	})
}
