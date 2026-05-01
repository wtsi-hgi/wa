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
	"github.com/wtsi-hgi/wa/saga"
)

// TestEnrichmentHierarchy validates that enrichment results preserve hierarchical relationships.
func TestEnrichmentHierarchy(t *testing.T) {
	ctx := context.Background()

	convey.Convey("H1: Study enrichment preserves library-to-sample hierarchy", t, func() {
		study := &saga.Study{
			IDStudyLims:     "6568",
			Name:            "Test Study",
			AccessionNumber: "ERP001",
		}

		// Study has 2 libraries (A and B), library A has 2 samples, library B has 1 sample
		samples := []saga.MLWHSample{
			{IDStudyLims: "6568", SangerID: "S1", SampleName: "Sample 1", IDSampleLims: "SL1", LibraryType: "A", IDRun: 100, Lane: 1},
			{IDStudyLims: "6568", SangerID: "S2", SampleName: "Sample 2", IDSampleLims: "SL2", LibraryType: "A", IDRun: 101, Lane: 2},
			{IDStudyLims: "6568", SangerID: "S3", SampleName: "Sample 3", IDSampleLims: "SL3", LibraryType: "B", IDRun: 102, Lane: 1},
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return study, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return samples, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)

		// Backward compatibility: flat arrays still present
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 2)

		// New hierarchical structure: StudyDetail contains LibraryDetails
		convey.So(result.Graph.StudyDetail, convey.ShouldNotBeNil)
		if result.Graph.StudyDetail == nil {
			return
		}

		convey.So(result.Graph.StudyDetail.Study, convey.ShouldResemble, *study)
		convey.So(result.Graph.StudyDetail.LibraryDetails, convey.ShouldHaveLength, 2)

		// Verify libraries are listed but samples are not preloaded (loaded JIT on expansion)
		var libA, libB *LibraryDetail
		for i := range result.Graph.StudyDetail.LibraryDetails {
			lib := &result.Graph.StudyDetail.LibraryDetails[i]
			switch lib.LibraryType {
			case "A":
				libA = lib
			case "B":
				libB = lib
			}
		}

		convey.So(libA, convey.ShouldNotBeNil)
		convey.So(libA.Samples, convey.ShouldHaveLength, 0) // Samples loaded JIT
		convey.So(libA.IDStudyLims, convey.ShouldEqual, "6568")

		// Verify library B is listed without preloaded samples
		convey.So(libB, convey.ShouldNotBeNil)
		convey.So(libB.Samples, convey.ShouldHaveLength, 0) // Samples loaded JIT
		convey.So(libB.IDStudyLims, convey.ShouldEqual, "6568")
	})

	convey.Convey("H2: Sample enrichment groups lanes by run_id and lane", t, func() {
		study := &saga.Study{
			IDStudyLims: "6568",
			Name:        "Test Study",
		}

		// Same sample sequenced on multiple lanes
		samples := []saga.MLWHSample{
			{IDStudyLims: "6568", SangerID: "S1", SampleName: "Sample 1", LibraryType: "A", IDRun: 100, Lane: 1, TagIndex: 10},
			{IDStudyLims: "6568", SangerID: "S1", SampleName: "Sample 1", LibraryType: "A", IDRun: 100, Lane: 2, TagIndex: 10},
			{IDStudyLims: "6568", SangerID: "S1", SampleName: "Sample 1", LibraryType: "A", IDRun: 101, Lane: 1, TagIndex: 10},
		}

		provider := &MockProvider{
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return samples, nil
			},
			StudyForSampleFn: func(_ context.Context, _ saga.MLWHSample) (*saga.Study, error) {
				return study, nil
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)

		// Backward compatibility
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 3)

		// New hierarchical structure: SampleDetail groups lanes
		convey.So(result.Graph.SampleDetail, convey.ShouldNotBeNil)
		if result.Graph.SampleDetail == nil {
			return
		}

		convey.So(result.Graph.SampleDetail.SangerID, convey.ShouldEqual, "S1")
		convey.So(result.Graph.SampleDetail.Lanes, convey.ShouldHaveLength, 3)

		// Verify lanes are present
		laneKeys := make(map[string]bool)
		for _, lane := range result.Graph.SampleDetail.Lanes {
			laneKeys[lane.IDRun+"/"+lane.Lane] = true
		}

		convey.So(laneKeys, convey.ShouldContainKey, "100/1")
		convey.So(laneKeys, convey.ShouldContainKey, "100/2")
		convey.So(laneKeys, convey.ShouldContainKey, "101/1")
	})

	convey.Convey("H3: RunID enrichment groups samples by study and library", t, func() {
		study1 := saga.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := saga.Study{IDStudyLims: "7890", Name: "Study 2"}

		// Run 100 has samples from 2 studies with different libraries
		samples := []saga.MLWHSample{
			{IDStudyLims: "6568", SangerID: "S1", LibraryType: "A", IDRun: 100},
			{IDStudyLims: "6568", SangerID: "S2", LibraryType: "A", IDRun: 100},
			{IDStudyLims: "6568", SangerID: "S3", LibraryType: "B", IDRun: 100},
			{IDStudyLims: "7890", SangerID: "S4", LibraryType: "C", IDRun: 100},
		}

		callCount := 0
		provider := &MockProvider{
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				return samples, nil
			},
			GetStudyFunc: func(_ context.Context, studyID string) (*saga.Study, error) {
				callCount++
				if studyID == "6568" {
					return &study1, nil
				}
				if studyID == "7890" {
					return &study2, nil
				}

				return nil, nil
			},
		}

		result, err := Enrich(ctx, provider, "100")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)

		// Backward compatibility
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 4)
		convey.So(result.Graph.Studies, convey.ShouldHaveLength, 2)

		// New hierarchical structure
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)

		// Find study details
		var detail1, detail2 *StudyDetail
		for i := range result.Graph.StudyDetails {
			detail := &result.Graph.StudyDetails[i]
			switch detail.Study.IDStudyLims {
			case "6568":
				detail1 = detail
			case "7890":
				detail2 = detail
			}
		}

		convey.So(detail1, convey.ShouldNotBeNil)
		convey.So(detail1.LibraryDetails, convey.ShouldHaveLength, 2)
		// Samples not preloaded (loaded JIT on expansion)
		for _, lib := range detail1.LibraryDetails {
			convey.So(lib.Samples, convey.ShouldHaveLength, 0)
		}

		convey.So(detail2, convey.ShouldNotBeNil)
		convey.So(detail2.LibraryDetails, convey.ShouldHaveLength, 1)
		convey.So(detail2.LibraryDetails[0].LibraryType, convey.ShouldEqual, "C")
		convey.So(detail2.LibraryDetails[0].Samples, convey.ShouldHaveLength, 0) // Samples loaded JIT
	})

	convey.Convey("H4: Library type enrichment groups samples hierarchically by study and library", t, func() {
		study1 := saga.Study{IDStudyLims: "6568", Name: "Study 1"}
		study2 := saga.Study{IDStudyLims: "7890", Name: "Study 2"}

		// Library type "RNA PolyA" appears in 2 studies
		samples := []saga.MLWHSample{
			{IDStudyLims: "6568", SangerID: "S1", LibraryType: "RNA PolyA"},
			{IDStudyLims: "6568", SangerID: "S2", LibraryType: "RNA PolyA"},
			{IDStudyLims: "7890", SangerID: "S3", LibraryType: "RNA PolyA"},
		}

		provider := &MockProvider{
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return samples, nil
			},
			GetStudyFunc: func(_ context.Context, studyID string) (*saga.Study, error) {
				if studyID == "6568" {
					return &study1, nil
				}
				if studyID == "7890" {
					return &study2, nil
				}

				return nil, nil
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)

		// Hierarchical structure should group by study
		convey.So(result.Graph.StudyDetails, convey.ShouldHaveLength, 2)

		var detail1, detail2 *StudyDetail
		for i := range result.Graph.StudyDetails {
			detail := &result.Graph.StudyDetails[i]
			switch detail.Study.IDStudyLims {
			case "6568":
				detail1 = detail
			case "7890":
				detail2 = detail
			}
		}

		convey.So(detail1, convey.ShouldNotBeNil)
		convey.So(detail1.LibraryDetails, convey.ShouldHaveLength, 1)
		convey.So(detail1.LibraryDetails[0].Samples, convey.ShouldHaveLength, 0) // Samples loaded JIT

		convey.So(detail2, convey.ShouldNotBeNil)
		convey.So(detail2.LibraryDetails, convey.ShouldHaveLength, 1)
		convey.So(detail2.LibraryDetails[0].Samples, convey.ShouldHaveLength, 0) // Samples loaded JIT
	})
}
