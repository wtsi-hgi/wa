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
	"encoding/json"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestMatchJSONCasingE1(t *testing.T) {
	convey.Convey("E1.1: Given a Match with no related pointers, when marshalled, then keys are snake_case and absent pointers are omitted", t, func() {
		data, err := json.Marshal(Match{Kind: KindStudyLimsID, Canonical: "6568"})
		convey.So(err, convey.ShouldBeNil)

		var decoded map[string]json.RawMessage
		convey.So(json.Unmarshal(data, &decoded), convey.ShouldBeNil)

		convey.So(decoded, convey.ShouldContainKey, "kind")
		convey.So(decoded, convey.ShouldContainKey, "canonical")
		convey.So(decoded, convey.ShouldNotContainKey, "Kind")
		convey.So(decoded, convey.ShouldNotContainKey, "Canonical")

		convey.So(string(decoded["kind"]), convey.ShouldEqual, `"study_lims_id"`)
		convey.So(string(decoded["canonical"]), convey.ShouldEqual, `"6568"`)

		convey.So(decoded, convey.ShouldNotContainKey, "sample")
		convey.So(decoded, convey.ShouldNotContainKey, "study")
		convey.So(decoded, convey.ShouldNotContainKey, "run")
		convey.So(decoded, convey.ShouldNotContainKey, "library")
	})

	convey.Convey("E1.1: Given a Match with a populated pointer, when marshalled, then the populated relation appears under its snake_case key", t, func() {
		data, err := json.Marshal(Match{Kind: KindRunID, Canonical: "100", Run: &Run{IDRun: 100}})
		convey.So(err, convey.ShouldBeNil)

		var decoded map[string]json.RawMessage
		convey.So(json.Unmarshal(data, &decoded), convey.ShouldBeNil)

		convey.So(decoded, convey.ShouldContainKey, "run")
		convey.So(decoded, convey.ShouldNotContainKey, "sample")
		convey.So(decoded, convey.ShouldNotContainKey, "study")
		convey.So(decoded, convey.ShouldNotContainKey, "library")
	})
}

func TestMatchJSONRoundTripE1(t *testing.T) {
	convey.Convey("E1.1: Given a marshalled Match, when unmarshalled back, then the Go value round-trips losslessly", t, func() {
		original := Match{
			Kind:      KindStudyLimsID,
			Canonical: "6568",
			Study:     &Study{IDStudyLims: "6568", Name: "Malaria genomics"},
		}

		data, err := json.Marshal(original)
		convey.So(err, convey.ShouldBeNil)

		var restored Match
		convey.So(json.Unmarshal(data, &restored), convey.ShouldBeNil)
		convey.So(restored, convey.ShouldResemble, original)
	})
}

func TestTaggedIDJSONCasingE1(t *testing.T) {
	convey.Convey("E1.3: Given a TaggedID, when marshalled, then the JSON keys are kind and canonical", t, func() {
		data, err := json.Marshal(TaggedID{Kind: KindRunID, Canonical: "100"})
		convey.So(err, convey.ShouldBeNil)

		var decoded map[string]json.RawMessage
		convey.So(json.Unmarshal(data, &decoded), convey.ShouldBeNil)

		convey.So(decoded, convey.ShouldContainKey, "kind")
		convey.So(decoded, convey.ShouldContainKey, "canonical")
		convey.So(decoded, convey.ShouldNotContainKey, "Kind")
		convey.So(decoded, convey.ShouldNotContainKey, "Canonical")

		convey.So(string(decoded["kind"]), convey.ShouldEqual, `"run_id"`)
		convey.So(string(decoded["canonical"]), convey.ShouldEqual, `"100"`)
	})

	convey.Convey("E1.3: Given a marshalled TaggedID, when unmarshalled back, then the Go value round-trips losslessly", t, func() {
		original := TaggedID{Kind: KindRunID, Canonical: "100"}

		data, err := json.Marshal(original)
		convey.So(err, convey.ShouldBeNil)

		var restored TaggedID
		convey.So(json.Unmarshal(data, &restored), convey.ShouldBeNil)
		convey.So(restored, convey.ShouldResemble, original)
	})
}
