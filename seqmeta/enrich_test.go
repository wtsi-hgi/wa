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

package seqmeta

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestEnrichUsesMLWHDetailGraphs(t *testing.T) {
	ctx := context.Background()

	convey.Convey("D3.1: study enrichment emits library_details from mlwh.StudyDetail", t, func() {
		studyDetail := &mlwh.StudyDetail{
			Study: mlwh.Study{IDStudyLims: "6568", Name: "Study 6568"},
			Libraries: []mlwh.LibraryDetail{
				{
					Library: mlwh.Library{PipelineIDLims: "Standard", SampleCount: 3},
					Samples: []mlwh.Sample{
						{IDStudyLims: "6568", SangerID: "S1", Name: "Sample 1", LibraryType: "Standard"},
						{IDStudyLims: "6568", SangerID: "S2", Name: "Sample 2", LibraryType: "Standard"},
						{IDStudyLims: "6568", SangerID: "S3", Name: "Sample 3", LibraryType: "Standard"},
					},
				},
				{
					Library: mlwh.Library{PipelineIDLims: "Bespoke", SampleCount: 2},
					Samples: []mlwh.Sample{
						{IDStudyLims: "6568", SangerID: "S4", Name: "Sample 4", LibraryType: "Bespoke"},
						{IDStudyLims: "6568", SangerID: "S5", Name: "Sample 5", LibraryType: "Bespoke"},
					},
				},
			},
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				convey.So(identifier, convey.ShouldEqual, "6568")
				return &studyDetail.Study, nil
			},
			StudyDetailFunc: func(_ context.Context, studyLimsID string) (*mlwh.StudyDetail, error) {
				convey.So(studyLimsID, convey.ShouldEqual, "6568")
				return studyDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		graph := decoded["graph"].(map[string]any)
		studyNode := graph["study_detail"].(map[string]any)
		libraryDetails := studyNode["library_details"].([]any)
		totalSamples := 0

		for _, detail := range libraryDetails {
			totalSamples += len(detail.(map[string]any)["samples"].([]any))
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(len(libraryDetails), convey.ShouldEqual, 2)
		convey.So(totalSamples, convey.ShouldEqual, 5)
		convey.So(studyNode["study"].(map[string]any)["id_study_lims"], convey.ShouldEqual, "6568")
	})

	convey.Convey("D3.2: sample enrichment emits lanes from mlwh.SampleDetail", t, func() {
		sampleDetail := &mlwh.SampleDetail{
			Sample: mlwh.Sample{SangerID: "7607STDY14643771", Name: "Sample 7607STDY14643771", IDStudyLims: "6568", LibraryType: "Standard"},
			Lanes: []mlwh.Lane{{IDRun: 101, Position: 1, TagIndex: 10}, {IDRun: 101, Position: 2, TagIndex: 11}, {IDRun: 102, Position: 1, TagIndex: 12}},
		}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]mlwh.Sample, error) {
				convey.So(sangerID, convey.ShouldEqual, "7607STDY14643771")
				return []mlwh.Sample{sampleDetail.Sample}, nil
			},
			SampleDetailFunc: func(_ context.Context, sangerName string) (*mlwh.SampleDetail, error) {
				convey.So(sangerName, convey.ShouldEqual, "7607STDY14643771")
				return sampleDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		sampleNode := decoded["graph"].(map[string]any)["sample_detail"].(map[string]any)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(len(sampleNode["lanes"].([]any)), convey.ShouldEqual, 3)
		convey.So(sampleNode["sample"].(map[string]any)["sanger_id"], convey.ShouldEqual, "7607STDY14643771")
	})

	convey.Convey("D3.3: enrichment JSON omits legacy project and users graph keys", t, func() {
		studyDetail := &mlwh.StudyDetail{Study: mlwh.Study{IDStudyLims: "6568"}}
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return &studyDetail.Study, nil
			},
			StudyDetailFunc: func(_ context.Context, _ string) (*mlwh.StudyDetail, error) {
				return studyDetail, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		payload, marshalErr := json.Marshal(result)
		convey.So(marshalErr, convey.ShouldBeNil)

		var decoded map[string]any
		convey.So(json.Unmarshal(payload, &decoded), convey.ShouldBeNil)

		graph := decoded["graph"].(map[string]any)
		_, hasRemovedStudyOwner := graph["project"]
		_, hasUsers := graph["users"]

		convey.So(hasRemovedStudyOwner, convey.ShouldBeFalse)
		convey.So(hasUsers, convey.ShouldBeFalse)
	})

	convey.Convey("D3.4: library enrichment truncates the per-study library hop at MaxSamplesPerHop", t, func() {
		studies := []mlwh.Study{{IDStudyLims: "6568", Name: "Study 6568"}}
		initialMatches := []mlwh.Sample{{IDStudyLims: "6568", SangerID: "S0001", Name: "Sample 1", LibraryType: "RNA PolyA"}}
		librarySamples := make([]mlwh.Sample, 0, 1500)

		for sampleNumber := range 1500 {
			librarySamples = append(librarySamples, mlwh.Sample{
				IDStudyLims: "6568",
				SangerID:   "S" + string(rune('A'+(sampleNumber%26))),
				Name:       "Library Sample",
				LibraryType: "RNA PolyA",
			})
		}

		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "RNA PolyA")
				return initialMatches, nil
			},
			AllStudiesFunc: func(_ context.Context, limit, offset int) ([]mlwh.Study, error) {
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)
				return studies, nil
			},
			SamplesForLibraryFunc: func(_ context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(pipelineIDLims, convey.ShouldEqual, "RNA PolyA")
				convey.So(studyLimsID, convey.ShouldEqual, "6568")
				convey.So(limit, convey.ShouldEqual, MaxLibrarySamples)
				convey.So(offset, convey.ShouldEqual, 0)
				return librarySamples, nil
			},
			GetStudyFunc: func(_ context.Context, identifier string) (*mlwh.Study, error) {
				if identifier == "RNA PolyA" {
					return nil, mlwh.ErrNotFound
				}

				convey.So(identifier, convey.ShouldEqual, "6568")
				return &studies[0], nil
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldContain, MissingHop{Hop: HopLibraries, Reason: ReasonSamplesTruncated, Status: 200})
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, MaxLibrarySamples)
	})
}
