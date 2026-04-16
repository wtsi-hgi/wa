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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

func TestLooksLikeStudySearch(t *testing.T) {
	convey.Convey("looksLikeStudySearch recognises study-oriented queries", t, func() {
		convey.So(looksLikeStudySearch("6568"), convey.ShouldBeTrue)
		convey.So(looksLikeStudySearch("EGAS00001005445"), convey.ShouldBeTrue)
		convey.So(looksLikeStudySearch("my study title"), convey.ShouldBeTrue)
		convey.So(looksLikeStudySearch("AM762808"), convey.ShouldBeFalse)
		convey.So(looksLikeStudySearch("WTSI_wEMB10524782"), convey.ShouldBeFalse)
	})
}

func TestIRODSSampleCandidateIDs(t *testing.T) {
	convey.Convey("irodsSampleCandidateIDs deduplicates matching sample identifiers", t, func() {
		sample := saga.IRODSSample{
			SourceID: "1913216340",
			Data: map[string]any{
				"avu:sample": []any{"AM762808", "AM762808"},
			},
			Curated: map[string]any{
				"sanger_id": []any{"AM762808"},
			},
		}

		ids := irodsSampleCandidateIDs(sample, "AM762808")

		convey.So(ids, convey.ShouldResemble, []string{"AM762808"})
	})
}

func TestIRODSSampleCandidateIDsIncludesMatchingSourceID(t *testing.T) {
	convey.Convey("irodsSampleCandidateIDs includes a matching source ID", t, func() {
		sample := saga.IRODSSample{SourceID: "folder-456"}

		ids := irodsSampleCandidateIDs(sample, "folder-456")

		convey.So(ids, convey.ShouldResemble, []string{"folder-456"})
	})
}

func TestSagaInspectWritesToCommandOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/integrations/mlwh/studies/6568":
			_, _ = w.Write([]byte(`{"id_study_lims":"6568","name":"HCA"}`))
		case "/integrations/irods/samples/6568":
			http.NotFound(w, r)
		case "/integrations/irods/samples":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		case "/samples/MLWH/studies/6568":
			_, _ = w.Write([]byte(`[]`))
		case "/integrations/mlwh/samples":
			_, _ = w.Write([]byte(`{"items":[],"total":0,"offset":0,"limit":100}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	convey.Convey("saga inspect writes runtime output to the command buffer", t, func() {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		command := NewRootCommand()
		command.SetOut(stdout)
		command.SetErr(stderr)
		command.SetArgs([]string{"saga", "inspect", "6568", "--token", "test", "--base-url", server.URL})

		err := command.Execute()

		convey.So(err, convey.ShouldBeNil)
		convey.So(stderr.String(), convey.ShouldBeBlank)
		convey.So(stdout.String(), convey.ShouldContainSubstring, "Query: 6568")
		convey.So(stdout.String(), convey.ShouldContainSubstring, "MLWH study")
	})
}
