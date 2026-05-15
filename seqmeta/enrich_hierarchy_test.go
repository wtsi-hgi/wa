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
			hierarchySample("6568", "SL1", "S1", "Sample 1", "A"),
			hierarchySample("6568", "SL2", "S2", "Sample 2", "A"),
			hierarchySample("6568", "SL3", "S3", "Sample 3", "B"),
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

	convey.Convey("Bug 1: study enrichment keeps same-type libraries separate by library identifiers", t, func() {
		study := &mlwh.Study{IDStudyLims: "6568", Name: "Test Study"}
		samples := []mlwh.Sample{
			{
				IDSampleLims:   "SL1",
				SangerSampleID: "S1",
				Name:           "Sample 1",
				Studies:        []mlwh.Study{*study},
				Libraries: []mlwh.Library{{
					PipelineIDLims: "RNA PolyA",
					IDStudyLims:    "6568",
					LibraryID:      "1001",
					IDLibraryLims:  "DN111:A1",
				}},
			},
			{
				IDSampleLims:   "SL2",
				SangerSampleID: "S2",
				Name:           "Sample 2",
				Studies:        []mlwh.Study{*study},
				Libraries: []mlwh.Library{{
					PipelineIDLims: "RNA PolyA",
					IDStudyLims:    "6568",
					LibraryID:      "1002",
					IDLibraryLims:  "DN222:B1",
				}},
			},
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
		if result == nil || result.Graph.StudyDetail == nil {
			return
		}

		convey.So(result.Graph.StudyDetail.Libraries, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.StudyDetail.Libraries[0].Library.IDLibraryLims, convey.ShouldEqual, "DN111:A1")
		convey.So(result.Graph.StudyDetail.Libraries[0].Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetail.Libraries[1].Library.IDLibraryLims, convey.ShouldEqual, "DN222:B1")
		convey.So(result.Graph.StudyDetail.Libraries[1].Samples, convey.ShouldHaveLength, 1)
	})

	convey.Convey("H2: sample enrichment preserves sample detail and linked study", t, func() {
		study := &mlwh.Study{IDStudyLims: "6568", Name: "Test Study"}
		samples := []mlwh.Sample{hierarchySample("6568", "", "S1", "Sample 1", "A")}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return samples, nil
			},
			StudiesForSampleFunc: func(_ context.Context, _ string) ([]mlwh.Study, error) {
				return []mlwh.Study{*study}, nil
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.SangerSampleID, convey.ShouldEqual, "S1")
		convey.So(result.Graph.Sample.Name, convey.ShouldEqual, "Sample 1")
		convey.So(result.Graph.Sample.Studies, convey.ShouldResemble, []mlwh.Study{*study})
		convey.So(result.Graph.Study, convey.ShouldResemble, study)
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		if result.Graph.SampleDetail == nil {
			return
		}

		convey.So(result.Graph.SampleDetail.Sample.SangerSampleID, convey.ShouldEqual, "S1")
		convey.So(result.Graph.SampleDetail.Sample.Name, convey.ShouldEqual, "Sample 1")
		convey.So(result.Graph.SampleDetail.Sample.Studies, convey.ShouldResemble, []mlwh.Study{*study})
		convey.So(result.Graph.SampleDetail.Lanes, convey.ShouldBeEmpty)
	})

	convey.Convey("H2.1/C1: sample enrichment preserves all linked studies from StudiesForSample", t, func() {
		study1 := mlwh.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := mlwh.Study{IDStudyLims: "6569", Name: "Study 2"}
		samples := []mlwh.Sample{{SangerSampleID: "S1", Name: "Sample 1"}}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]mlwh.Sample, error) {
				return samples, nil
			},
			StudiesForSampleFunc: func(_ context.Context, _ string) ([]mlwh.Study, error) {
				return []mlwh.Study{study1, study2}, nil
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Graph.Study, convey.ShouldBeNil)
		convey.So(result.Graph.Studies, convey.ShouldResemble, []mlwh.Study{study1, study2})
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.Studies, convey.ShouldResemble, []mlwh.Study{study1, study2})
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		if result.Graph.SampleDetail == nil {
			return
		}

		convey.So(result.Graph.SampleDetail.Study, convey.ShouldBeNil)
		convey.So(result.Graph.SampleDetail.Sample.Studies, convey.ShouldResemble, []mlwh.Study{study1, study2})
	})

	convey.Convey("Bug 5: sample enrichment uses specific library identifiers from sample details", t, func() {
		study := mlwh.Study{IDStudyLims: "7607", Name: "Study 7607"}
		sample := mlwh.Sample{
			IDSampleLims:   "SMP001",
			SangerSampleID: "7607STDY14643771",
			Name:           "7607STDY14643771",
		}
		library := mlwh.Library{
			PipelineIDLims: "Custom",
			IDStudyLims:    "7607",
			LibraryID:      "71046409",
			IDLibraryLims:  "SQPP-47463-G:B1",
		}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]mlwh.Sample, error) {
				convey.So(identifier, convey.ShouldEqual, "7607STDY14643771")

				return []mlwh.Sample{sample}, nil
			},
			SampleDetailFunc: func(_ context.Context, sampleName string) (*mlwh.SampleDetail, error) {
				convey.So(sampleName, convey.ShouldEqual, "7607STDY14643771")

				return &mlwh.SampleDetail{
					Sample:    sample,
					Study:     &study,
					Libraries: []mlwh.Library{library},
				}, nil
			},
		}

		result, err := Enrich(ctx, provider, "7607STDY14643771")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Graph.Library, convey.ShouldResemble, &Library{
			LibraryType:   "Custom",
			IDStudyLims:   "7607",
			LibraryID:     "71046409",
			IDLibraryLims: "SQPP-47463-G:B1",
		})
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.Libraries, convey.ShouldResemble, []mlwh.Library{library})
	})

	convey.Convey("H3: run enrichment groups samples into StudyDetails", t, func() {
		study1 := mlwh.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := mlwh.Study{IDStudyLims: "7890", Name: "Study 2"}
		samples := []mlwh.Sample{
			hierarchySample("6568", "", "S1", "Sample 1", "A"),
			hierarchySample("6568", "", "S2", "Sample 2", "B"),
			hierarchySample("7890", "", "S3", "Sample 3", "C"),
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

	convey.Convey("Bug 3: run enrichment exposes library identifiers and grouped run samples", t, func() {
		study := mlwh.Study{IDStudyLims: "7607", Name: "Run Study"}
		samples := []mlwh.Sample{
			{
				IDSampleLims:   "SMP001",
				SangerSampleID: "S1",
				Name:           "Sample 1",
				Studies:        []mlwh.Study{study},
				Libraries: []mlwh.Library{{
					PipelineIDLims: "Custom",
					IDStudyLims:    "7607",
					LibraryID:      "71046409",
					IDLibraryLims:  "SQPP-47463-G:B1",
				}},
			},
			{
				IDSampleLims:   "SMP002",
				SangerSampleID: "S2",
				Name:           "Sample 2",
				Studies:        []mlwh.Study{study},
				Libraries: []mlwh.Library{{
					PipelineIDLims: "Custom",
					IDStudyLims:    "7607",
					LibraryID:      "71046409",
					IDLibraryLims:  "SQPP-47463-G:B1",
				}},
			},
		}

		provider := &MockProvider{
			FindSamplesByRunIDFn: func(_ context.Context, idRun int) ([]mlwh.Sample, error) {
				convey.So(idRun, convey.ShouldEqual, 48522)

				return samples, nil
			},
			GetStudyFunc: func(_ context.Context, studyID string) (*mlwh.Study, error) {
				if studyID == "48522" {
					return nil, nil
				}
				convey.So(studyID, convey.ShouldEqual, "7607")

				return &study, nil
			},
		}

		result, err := Enrich(ctx, provider, "48522")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.Libraries[0].LibraryID, convey.ShouldEqual, "71046409")
		convey.So(result.Graph.Libraries[0].IDLibraryLims, convey.ShouldEqual, "SQPP-47463-G:B1")
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails[0].Libraries, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.StudyDetails[0].Libraries[0].Library.IDLibraryLims, convey.ShouldEqual, "SQPP-47463-G:B1")
		convey.So(result.Graph.StudyDetails[0].Libraries[0].Samples, convey.ShouldHaveLength, 2)
	})

	convey.Convey("H4: library enrichment groups samples into study details", t, func() {
		study1 := mlwh.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := mlwh.Study{IDStudyLims: "7890", Name: "Study 2"}
		samples := []mlwh.Sample{
			hierarchySample("6568", "", "S1", "Sample 1", "RNA PolyA"),
			hierarchySample("7890", "", "S2", "Sample 2", "RNA PolyA"),
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

func hierarchySample(studyID, sampleLimsID, sangerSampleID, name, libraryType string) mlwh.Sample {
	sample := mlwh.Sample{
		IDSampleLims:   sampleLimsID,
		SangerSampleID: sangerSampleID,
		Name:           name,
	}

	if studyID != "" {
		sample.Studies = []mlwh.Study{{IDStudyLims: studyID}}
	}

	if studyID != "" || libraryType != "" {
		sample.Libraries = []mlwh.Library{{PipelineIDLims: libraryType, IDStudyLims: studyID}}
	}

	return sample
}
