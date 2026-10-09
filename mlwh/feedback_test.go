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
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestFeedbackCategoriesA1(t *testing.T) {
	convey.Convey("A1.1: Given FeedbackCategories(), then it lists the five categories in order, each valid with its table description", t, func() {
		convey.So(FeedbackCategories(), convey.ShouldResemble, []FeedbackCategory{
			"could_not_answer", "agent_mistake", "no_endpoint", "user_unhappy", "other",
		})

		want := map[FeedbackCategory]string{
			"could_not_answer": "the agent could not work out how to answer the request",
			"agent_mistake":    "the agent made a mistake while answering",
			"no_endpoint":      "no endpoint could possibly answer the question",
			"user_unhappy":     "the user was unhappy with the answer",
			"other":            "any other problem with the request",
		}

		for _, c := range FeedbackCategories() {
			convey.So(c.Valid(), convey.ShouldBeTrue)
			convey.So(c.Description(), convey.ShouldEqual, want[c])
		}
	})

	convey.Convey("A1.2: Given unknown or wrongly cased categories, then Valid is false and Description is empty", t, func() {
		for _, c := range []FeedbackCategory{"bogus", "Other", ""} {
			convey.So(c.Valid(), convey.ShouldBeFalse)
			convey.So(c.Description(), convey.ShouldEqual, "")
		}
	})
}

func TestFeedbackSubmissionValidateA1(t *testing.T) {
	valid := func() FeedbackSubmission {
		return FeedbackSubmission{Category: FeedbackCategoryOther, Description: "x"}
	}

	convey.Convey("A1.3: Given a minimal valid submission, when validated, then the error is nil", t, func() {
		convey.So(valid().Validate(), convey.ShouldBeNil)
	})

	convey.Convey("A1.4: Given category bogus, then ErrFeedbackInvalid names the category", t, func() {
		s := valid()
		s.Category = "bogus"

		err := s.Validate()
		convey.So(errors.Is(err, ErrFeedbackInvalid), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeFalse)
		convey.So(err.Error(), convey.ShouldContainSubstring, `invalid category "bogus"`)
	})

	convey.Convey("A1.5: Given a blank or empty description, then ErrFeedbackInvalid with description is required", t, func() {
		for _, d := range []string{"  \n", ""} {
			s := valid()
			s.Description = d

			err := s.Validate()
			convey.So(errors.Is(err, ErrFeedbackInvalid), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, "description is required")
		}
	})

	convey.Convey("A1.6: Given a description at and over 16384 bytes, then nil at the cap and ErrFeedbackTooLarge over it", t, func() {
		s := valid()
		s.Description = strings.Repeat("a", 16384)
		convey.So(s.Validate(), convey.ShouldBeNil)

		s.Description = strings.Repeat("a", 16385)
		err := s.Validate()
		convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrFeedbackInvalid), convey.ShouldBeFalse)
		convey.So(err.Error(), convey.ShouldContainSubstring, "description exceeds 16384 bytes")
	})

	convey.Convey("A1.7: Given user_request at and over 16384 bytes, then nil at the cap and ErrFeedbackTooLarge naming user_request over it", t, func() {
		s := valid()
		s.UserRequest = strings.Repeat("a", 16384)
		convey.So(s.Validate(), convey.ShouldBeNil)

		s.UserRequest = strings.Repeat("a", 16385)
		err := s.Validate()
		convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "user_request")
	})

	convey.Convey("A1.8: Given tools_tried at and over its item and length caps", t, func() {
		tools := make([]string, 50)
		for i := range tools {
			tools[i] = strings.Repeat("t", 128)
		}

		s := valid()
		s.ToolsTried = tools
		convey.So(s.Validate(), convey.ShouldBeNil)

		s.ToolsTried = append(slices.Clone(tools), "t")
		err := s.Validate()
		convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "tools_tried")

		s.ToolsTried = []string{strings.Repeat("t", 129)}
		err = s.Validate()
		convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "tools_tried")
	})

	convey.Convey("A1.9: Given each metadata field at 256 and 257 bytes, then nil at the cap and ErrFeedbackTooLarge naming that field over it", t, func() {
		fields := map[string]func(*FeedbackSubmission, string){
			"mcp_server_version": func(s *FeedbackSubmission, v string) { s.MCPServerVersion = v },
			"wa_api_version":     func(s *FeedbackSubmission, v string) { s.WAAPIVersion = v },
			"transport":          func(s *FeedbackSubmission, v string) { s.Transport = v },
			"client_name":        func(s *FeedbackSubmission, v string) { s.ClientName = v },
			"client_version":     func(s *FeedbackSubmission, v string) { s.ClientVersion = v },
			"client_user_agent":  func(s *FeedbackSubmission, v string) { s.ClientUserAgent = v },
		}

		for name, set := range fields {
			s := valid()
			set(&s, strings.Repeat("m", 256))
			convey.So(s.Validate(), convey.ShouldBeNil)

			set(&s, strings.Repeat("m", 257))
			err := s.Validate()
			convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeTrue)
			convey.So(err.Error(), convey.ShouldContainSubstring, name+" exceeds 256 bytes")
		}
	})

	convey.Convey("A1.10: Given an invalid category and an oversized description, then ErrFeedbackTooLarge because caps come first", t, func() {
		s := FeedbackSubmission{Category: "bogus", Description: strings.Repeat("a", 16385)}

		err := s.Validate()
		convey.So(errors.Is(err, ErrFeedbackTooLarge), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrFeedbackInvalid), convey.ShouldBeFalse)
	})

	convey.Convey("A1.11: Given 4096 two-byte runes (8192 bytes) as description, then nil because caps count bytes", t, func() {
		s := valid()
		s.Description = strings.Repeat("é", 4096)
		convey.So(len(s.Description), convey.ShouldEqual, 8192)
		convey.So(s.Validate(), convey.ShouldBeNil)

		s.Description = strings.Repeat("é", 8193)
		convey.So(errors.Is(s.Validate(), ErrFeedbackTooLarge), convey.ShouldBeTrue)
	})

	convey.Convey("A1.12: Given the zero submission, then ErrFeedbackInvalid naming the empty category before the description", t, func() {
		err := FeedbackSubmission{}.Validate()
		convey.So(errors.Is(err, ErrFeedbackInvalid), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, `invalid category ""`)
	})
}
