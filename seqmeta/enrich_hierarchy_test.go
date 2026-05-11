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
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestEnrichmentHierarchy(t *testing.T) {
	ctx := context.Background()

	convey.Convey("H1: study enrichment preserves library grouping in StudyDetail", t, func() {
		study := &mlwh.Study{IDStudyLims: "6568", Name: "Test Study", AccessionNumber: "ERP001"}
		samples := []mlwh.Sample{
			{IDStudyLims: "6568", IDSampleLims: "SL1", SangerID: "S1", Name: "Sample 1", LibraryType: "A"},
			{IDStudyLims: "6568", IDSampleLims: "SL2", SangerID: "S2", Name: "Sample 2", LibraryType: "A"},
			{IDStudyLims: "6568", IDSampleLims: "SL3", SangerID: "S3", Name: "Sample 3", LibraryType: "B"},
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return study, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return samples, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetail, convey.ShouldNotBeNil)
		if result.Graph.StudyDetail == nil {
			return
		}

		convey.So(result.Graph.StudyDetail.Study, convey.ShouldResemble, *study)
		convey.So(result.Graph.StudyDetail.Libraries, convey.ShouldHaveLength, 2)
		for _, detail := range result.Graph.StudyDetail.Libraries {
			convey.So([]string{"A", "B"}, convey.ShouldContain, detail.Library.PipelineIDLims)
			convey.So(detail.Samples, convey.ShouldNotBeEmpty)
		}
	})

	convey.Convey("H2: sample enrichment preserves sample detail and linked study", t, func() {
		study := &mlwh.Study{IDStudyLims: "6568", Name: "Test Study"}
		samples := []mlwh.Sample{{IDStudyLims: "6568", SangerID: "S1", Name: "Sample 1", LibraryType: "A"}}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return samples, nil
			},
			StudyForSampleFunc: func(_ context.Context, _ string) (*mlwh.Study, error) {
				return study, nil
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Graph.Sample, convey.ShouldResemble, &samples[0])
		convey.So(result.Graph.Study, convey.ShouldResemble, study)
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		if result.Graph.SampleDetail == nil {
			return
		}

		convey.So(result.Graph.SampleDetail.Sample.SangerID, convey.ShouldEqual, "S1")
		convey.So(result.Graph.SampleDetail.Sample.Name, convey.ShouldEqual, "Sample 1")
		convey.So(result.Graph.SampleDetail.Sample, convey.ShouldResemble, samples[0])
		convey.So(result.Graph.SampleDetail.Lanes, convey.ShouldBeEmpty)
	})

	convey.Convey("H3: run enrichment groups samples into StudyDetails", t, func() {
		study1 := mlwh.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := mlwh.Study{IDStudyLims: "7890", Name: "Study 2"}
		samples := []mlwh.Sample{
			{IDStudyLims: "6568", SangerID: "S1", Name: "Sample 1", LibraryType: "A"},
			{IDStudyLims: "6568", SangerID: "S2", Name: "Sample 2", LibraryType: "B"},
			{IDStudyLims: "7890", SangerID: "S3", Name: "Sample 3", LibraryType: "C"},
		}

		provider := &MockProvider{
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]mlwh.Sample, error) {
				return samples, nil
			},
			GetStudyFunc: func(_ context.Context, studyID string) (*mlwh.Study, error) {
				switch studyID {
				case "6568":
					return &study1, nil
				case "7890":
					return &study2, nil
				default:
					return nil, nil
				}
			},
		}

		result, err := Enrich(ctx, provider, "100")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.Studies, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)
	})

	convey.Convey("H4: library enrichment groups samples into study details", t, func() {
		study1 := mlwh.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := mlwh.Study{IDStudyLims: "7890", Name: "Study 2"}
		samples := []mlwh.Sample{
			{IDStudyLims: "6568", SangerID: "S1", Name: "Sample 1", LibraryType: "RNA PolyA"},
			{IDStudyLims: "7890", SangerID: "S2", Name: "Sample 2", LibraryType: "RNA PolyA"},
		}

		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return samples, nil
			},
			GetStudyFunc: func(_ context.Context, studyID string) (*mlwh.Study, error) {
				switch studyID {
				case "6568":
					return &study1, nil
				case "7890":
					return &study2, nil
				default:
					return nil, nil
				}
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Studies, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)
	})
}
