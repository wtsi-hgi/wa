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
	"errors"
	"net/http"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

func TestEnrichStudyID(t *testing.T) {
	ctx := context.Background()
	study := &saga.Study{IDStudyLims: "6568", AccessionNumber: "ERP001"}
	samples := []saga.MLWHSample{
		{IDStudyLims: "6568", SangerID: "S1", LibraryType: "A"},
		{IDStudyLims: "6568", SangerID: "S2", LibraryType: "A"},
		{IDStudyLims: "6568", SangerID: "S3", LibraryType: "B"},
	}

	convey.Convey("C1: study_id enrichment resolves a complete graph from study and study samples", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		allSamplesCalls := 0
		allSamplesForStudyCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByRunIDCalls := 0
		findSamplesByLibraryTypeCalls := 0
		findSamplesByAccessionNumberCalls := 0
		studyForSampleCalls := 0
		getSampleFilesCalls := 0
		listProjectsCalls := 0
		listProjectStudiesCalls := 0
		listProjectSamplesCalls := 0
		listProjectUsersCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, studyID string) (*saga.Study, error) {
				getStudyCalls++
				convey.So(studyID, convey.ShouldEqual, "6568")

				return study, nil
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, nil
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return nil, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
				allSamplesForStudyCalls++
				convey.So(studyID, convey.ShouldEqual, "6568")

				return samples, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++

				return nil, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++

				return nil, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++

				return nil, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByLibraryTypeCalls++

				return nil, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++

				return nil, nil
			},
			StudyForSampleFn: func(_ context.Context, _ saga.MLWHSample) (*saga.Study, error) {
				studyForSampleCalls++

				return nil, nil
			},
			GetSampleFilesFunc: func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
				getSampleFilesCalls++

				return nil, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				listProjectsCalls++

				return nil, nil
			},
			ListProjectStudiesFn: func(_ context.Context, _ int) ([]saga.ProjectStudy, error) {
				listProjectStudiesCalls++

				return nil, nil
			},
			ListProjectSamplesFn: func(_ context.Context, _ int) ([]saga.ProjectSample, error) {
				listProjectSamplesCalls++

				return nil, nil
			},
			ListProjectUsersFn: func(_ context.Context, _ int) ([]saga.ProjectUser, error) {
				listProjectUsersCalls++

				return nil, nil
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		if result.Graph.Study == nil {
			return
		}

		convey.So(result.Graph.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.Samples, convey.ShouldResemble, samples)
		convey.So(result.Graph.Libraries, convey.ShouldHaveLength, 2)
		convey.So(result.Graph.Libraries, convey.ShouldContain, Library{LibraryType: "A", IDStudyLims: "6568"})
		convey.So(result.Graph.Libraries, convey.ShouldContain, Library{LibraryType: "B", IDStudyLims: "6568"})
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)

		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesForStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 0)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 0)
		convey.So(studyForSampleCalls, convey.ShouldEqual, 0)
		convey.So(getSampleFilesCalls, convey.ShouldEqual, 0)
		convey.So(listProjectsCalls, convey.ShouldEqual, 0)
		convey.So(listProjectStudiesCalls, convey.ShouldEqual, 0)
		convey.So(listProjectSamplesCalls, convey.ShouldEqual, 0)
		convey.So(listProjectUsersCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("C1: study_id enrichment returns a partial result when the samples hop fails", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return study, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
		}

		result, err := Enrich(ctx, provider, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		convey.So(result.Graph.Samples, convey.ShouldBeEmpty)
		convey.So(result.Graph.Libraries, convey.ShouldBeEmpty)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopSamples,
			Reason: ReasonUpstreamError,
			Status: 502,
		}})
	})
}

func TestEnrichStudyAccession(t *testing.T) {
	ctx := context.Background()
	study := saga.Study{IDStudyLims: "6568", AccessionNumber: "ERP001"}
	samples := []saga.MLWHSample{
		{IDStudyLims: "6568", SangerID: "S1", LibraryType: "RNA PolyA"},
		{IDStudyLims: "6568", SangerID: "S2", LibraryType: "PCR free"},
	}

	convey.Convey("C2: study_accession enrichment resolves the accession to a study ID and reuses the study pipeline", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		allSamplesForStudyCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++
				convey.So(identifier, convey.ShouldEqual, "ERP001")

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return []saga.Study{study}, nil
			},
			AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
				allSamplesForStudyCalls++
				convey.So(studyID, convey.ShouldEqual, "6568")

				return samples, nil
			},
		}

		result, err := Enrich(ctx, provider, "ERP001")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyAccession)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		if result.Graph.Study == nil {
			return
		}

		convey.So(result.Graph.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.Samples, convey.ShouldResemble, samples)
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesForStudyCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C2: study lookup 5xx hops are recorded when a later classifier resolves the identifier", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				convey.So(identifier, convey.ShouldEqual, "S1")

				return nil, saga.ErrServerError
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]saga.MLWHSample, error) {
				convey.So(sangerID, convey.ShouldEqual, "S1")

				return []saga.MLWHSample{samples[0]}, nil
			},
			StudyForSampleFn: func(_ context.Context, sample saga.MLWHSample) (*saga.Study, error) {
				convey.So(sample.SangerID, convey.ShouldEqual, "S1")

				return &study, nil
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.SangerID, convey.ShouldEqual, "S1")
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: 502},
			{Hop: HopClassify, Reason: ReasonUpstreamError, Status: 502},
		})
	})
}

func TestEnrichSangerSampleID(t *testing.T) {
	ctx := context.Background()
	sample := saga.MLWHSample{SangerID: "S1", IDStudyLims: "6568", LibraryType: "RNA PolyA"}
	study := &saga.Study{IDStudyLims: "6568", AccessionNumber: "ERP001"}

	convey.Convey("C3: sanger_sample_id enrichment resolves a sample, study, and library after study lookups miss client-side", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		allSamplesCalls := 0
		findSamplesBySangerIDCalls := 0
		studyForSampleCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++
				convey.So(identifier, convey.ShouldEqual, "S1")

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, saga.ErrNotFound
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return nil, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++
				convey.So(sangerID, convey.ShouldEqual, "S1")

				return []saga.MLWHSample{sample}, nil
			},
			StudyForSampleFn: func(_ context.Context, givenSample saga.MLWHSample) (*saga.Study, error) {
				studyForSampleCalls++
				convey.So(givenSample, convey.ShouldResemble, sample)

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
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.SangerID, convey.ShouldEqual, "S1")
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.Samples[0], convey.ShouldResemble, sample)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		if result.Graph.Study == nil {
			return
		}

		convey.So(result.Graph.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.Library, convey.ShouldResemble, &Library{LibraryType: "RNA PolyA", IDStudyLims: "6568"})
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(studyForSampleCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C3: sanger_sample_id enrichment is partial when the study hop fails upstream", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, studyID string) (*saga.Study, error) {
				if studyID == "6568" {
					return &saga.Study{IDStudyLims: studyID}, nil
				}

				if studyID == "S1" {
					return nil, saga.ErrNotFound
				}

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]saga.MLWHSample, error) {
				convey.So(sangerID, convey.ShouldEqual, "S1")

				return []saga.MLWHSample{sample}, nil
			},
			StudyForSampleFn: func(_ context.Context, givenSample saga.MLWHSample) (*saga.Study, error) {
				convey.So(givenSample, convey.ShouldResemble, sample)

				return nil, saga.ErrServerError
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Graph.Study, convey.ShouldBeNil)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopStudy,
			Reason: ReasonUpstreamError,
			Status: 502,
		}})
	})

	convey.Convey("C3: sanger_sample_id enrichment returns ErrUnknownIdentifier when every classification hop is empty or not found", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
		}

		result, err := Enrich(ctx, provider, "S1")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
	})
}

func TestEnrichSampleLimsID(t *testing.T) {
	ctx := context.Background()
	sample := saga.MLWHSample{IDSampleLims: "LIMS456", IDStudyLims: "6568", LibraryType: "RNA PolyA"}
	study := &saga.Study{IDStudyLims: "6568", AccessionNumber: "ERP001"}

	convey.Convey("C4: sample_lims_id enrichment resolves a sample, study, and library after earlier classifiers miss client-side", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		allSamplesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		studyForSampleCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++
				convey.So(identifier, convey.ShouldEqual, "LIMS456")

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, saga.ErrNotFound
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, idSampleLims string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++
				convey.So(idSampleLims, convey.ShouldEqual, "LIMS456")

				return []saga.MLWHSample{sample}, nil
			},
			StudyForSampleFn: func(_ context.Context, givenSample saga.MLWHSample) (*saga.Study, error) {
				studyForSampleCalls++
				convey.So(givenSample, convey.ShouldResemble, sample)

				return study, nil
			},
		}

		result, err := Enrich(ctx, provider, "LIMS456")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSampleLimsID)
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.IDSampleLims, convey.ShouldEqual, "LIMS456")
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.Samples[0], convey.ShouldResemble, sample)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		if result.Graph.Study == nil {
			return
		}

		convey.So(result.Graph.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.Library, convey.ShouldResemble, &Library{LibraryType: "RNA PolyA", IDStudyLims: "6568"})
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(studyForSampleCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
	})
}

func TestEnrichSampleAccession(t *testing.T) {
	ctx := context.Background()
	sample := saga.MLWHSample{AccessionNumber: "SAM123", IDStudyLims: "6568", LibraryType: "RNA PolyA"}
	study := &saga.Study{IDStudyLims: "6568", AccessionNumber: "ERP001"}

	convey.Convey("C5: sample_accession enrichment resolves a sample, study, and library after earlier classifiers miss client-side", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		allSamplesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		studyForSampleCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++
				convey.So(identifier, convey.ShouldEqual, "SAM123")

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, saga.ErrNotFound
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, accessionNumber string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++
				convey.So(accessionNumber, convey.ShouldEqual, "SAM123")

				return []saga.MLWHSample{sample}, nil
			},
			StudyForSampleFn: func(_ context.Context, givenSample saga.MLWHSample) (*saga.Study, error) {
				studyForSampleCalls++
				convey.So(givenSample, convey.ShouldResemble, sample)

				return study, nil
			},
		}

		result, err := Enrich(ctx, provider, "SAM123")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSampleAccession)
		convey.So(result.Graph.Sample, convey.ShouldNotBeNil)
		if result.Graph.Sample == nil {
			return
		}

		convey.So(result.Graph.Sample.AccessionNumber, convey.ShouldEqual, "SAM123")
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.Samples[0], convey.ShouldResemble, sample)
		convey.So(result.Graph.Study, convey.ShouldNotBeNil)
		if result.Graph.Study == nil {
			return
		}

		convey.So(result.Graph.Study.IDStudyLims, convey.ShouldEqual, "6568")
		convey.So(result.Graph.Library, convey.ShouldResemble, &Library{LibraryType: "RNA PolyA", IDStudyLims: "6568"})
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(studyForSampleCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
	})
}

func TestEnrichRunID(t *testing.T) {
	ctx := context.Background()
	study := &saga.Study{IDStudyLims: "6568", AccessionNumber: "ERP001"}
	samples := []saga.MLWHSample{
		{IDRun: 34134, SangerID: "S1", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
		{IDRun: 34134, SangerID: "S2", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
	}

	convey.Convey("C6: run_id enrichment resolves samples, studies, and libraries for a numeric run identifier", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		findSamplesByRunIDCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++

				switch identifier {
				case "34134":
					return nil, saga.ErrNotFound
				case "6568":
					return study, nil
				default:
					return nil, saga.ErrNotFound
				}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, idRun int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++
				convey.So(idRun, convey.ShouldEqual, 34134)

				return samples, nil
			},
		}

		result, err := Enrich(ctx, provider, "34134")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Graph.Samples, convey.ShouldResemble, samples)
		convey.So(result.Graph.Studies, convey.ShouldResemble, []saga.Study{*study})
		convey.So(result.Graph.Libraries, convey.ShouldResemble, []Library{{LibraryType: "RNA PolyA", IDStudyLims: "6568"}})
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 1)
		convey.So(getStudyCalls, convey.ShouldEqual, 2)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 0)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C6: run_id enrichment is partial when one study lookup fails upstream", t, func() {
		studyACalls := 0
		studyBCalls := 0
		samplesAcrossStudies := []saga.MLWHSample{
			{IDRun: 34134, SangerID: "S1", IDStudyLims: "A", LibraryType: "RNA PolyA"},
			{IDRun: 34134, SangerID: "S2", IDStudyLims: "B", LibraryType: "RNA PolyA"},
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				switch identifier {
				case "34134":
					return nil, saga.ErrNotFound
				case "A":
					studyACalls++

					return &saga.Study{IDStudyLims: "A", AccessionNumber: "ERP-A"}, nil
				case "B":
					studyBCalls++

					return nil, saga.ErrServerError
				default:
					return nil, saga.ErrNotFound
				}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, idRun int) ([]saga.MLWHSample, error) {
				convey.So(idRun, convey.ShouldEqual, 34134)

				return samplesAcrossStudies, nil
			},
		}

		result, err := Enrich(ctx, provider, "34134")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Graph.Studies, convey.ShouldResemble, []saga.Study{{IDStudyLims: "A", AccessionNumber: "ERP-A"}})
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopStudies,
			Reason: ReasonUpstreamError,
			Status: 502,
		}})
		convey.So(studyACalls, convey.ShouldEqual, 1)
		convey.So(studyBCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C6: run_id enrichment skips the run classifier for a non-numeric identifier", t, func() {
		findSamplesByRunIDCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++

				return nil, nil
			},
		}

		result, err := Enrich(ctx, provider, "abc")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 0)
	})
}

func TestEnrichLibraryType(t *testing.T) {
	ctx := context.Background()
	samples := []saga.MLWHSample{
		{SangerID: "S1", IDStudyLims: "A", LibraryType: "RNA PolyA"},
		{SangerID: "S2", IDStudyLims: "A", LibraryType: "RNA PolyA"},
		{SangerID: "S3", IDStudyLims: "B", LibraryType: "RNA PolyA"},
		{SangerID: "S4", IDStudyLims: "B", LibraryType: "RNA PolyA"},
		{SangerID: "S5", IDStudyLims: "C", LibraryType: "RNA PolyA"},
	}
	studies := map[string]saga.Study{
		"A": {IDStudyLims: "A", AccessionNumber: "ERP-A"},
		"B": {IDStudyLims: "B", AccessionNumber: "ERP-B"},
		"C": {IDStudyLims: "C", AccessionNumber: "ERP-C"},
	}

	convey.Convey("C7: library_type enrichment resolves samples, libraries, and studies after earlier classifiers miss client-side", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		findSamplesByRunIDCalls := 0
		findSamplesByLibraryTypeCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++

				switch identifier {
				case "RNA PolyA":
					return nil, saga.ErrNotFound
				case "A", "B", "C":
					study := studies[identifier]

					return &study, nil
				default:
					return nil, saga.ErrNotFound
				}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]saga.MLWHSample, error) {
				findSamplesByLibraryTypeCalls++
				convey.So(libraryType, convey.ShouldEqual, "RNA PolyA")

				return samples, nil
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Samples, convey.ShouldResemble, samples)
		convey.So(result.Graph.Libraries, convey.ShouldResemble, []Library{
			{LibraryType: "RNA PolyA", IDStudyLims: "A"},
			{LibraryType: "RNA PolyA", IDStudyLims: "B"},
			{LibraryType: "RNA PolyA", IDStudyLims: "C"},
		})
		convey.So(result.Graph.Studies, convey.ShouldResemble, []saga.Study{
			studies["A"],
			studies["B"],
			studies["C"],
		})
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 1)
		convey.So(getStudyCalls, convey.ShouldEqual, 4)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 0)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C7: library_type enrichment truncates graph samples at the fan-out cap while keeping libraries and studies complete", t, func() {
		truncatedSamples := make([]saga.MLWHSample, 0, 1500)

		for i := 0; i < 1500; i++ {
			studyID := "A"
			if i >= 500 {
				studyID = "B"
			}
			if i >= 1000 {
				studyID = "C"
			}

			truncatedSamples = append(truncatedSamples, saga.MLWHSample{
				SangerID:     "S",
				IDStudyLims:  studyID,
				LibraryType:  "RNA PolyA",
				IDSampleLims: "L",
			})
		}

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				switch identifier {
				case "RNA PolyA":
					return nil, saga.ErrNotFound
				case "A", "B", "C":
					study := studies[identifier]

					return &study, nil
				default:
					return nil, saga.ErrNotFound
				}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, libraryType string) ([]saga.MLWHSample, error) {
				convey.So(libraryType, convey.ShouldEqual, "RNA PolyA")

				return truncatedSamples, nil
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, MaxLibrarySamples)
		convey.So(result.Graph.Samples[:5], convey.ShouldResemble, truncatedSamples[:5])
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopSamples,
			Reason: ReasonSamplesTruncated,
			Status: 200,
		}})
		convey.So(result.Graph.Libraries, convey.ShouldResemble, []Library{
			{LibraryType: "RNA PolyA", IDStudyLims: "A"},
			{LibraryType: "RNA PolyA", IDStudyLims: "B"},
			{LibraryType: "RNA PolyA", IDStudyLims: "C"},
		})
		convey.So(result.Graph.Studies, convey.ShouldResemble, []saga.Study{
			studies["A"],
			studies["B"],
			studies["C"],
		})
	})

	convey.Convey("C7: library_type enrichment returns ErrUnknownIdentifier when the classifier finds no samples", t, func() {
		findSamplesByLibraryTypeCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByLibraryTypeCalls++

				return []saga.MLWHSample{}, nil
			},
		}

		result, err := Enrich(ctx, provider, "RNA PolyA")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 1)
	})
}

func TestEnrichProjectName(t *testing.T) {
	ctx := context.Background()
	project := saga.Project{ID: 7, Name: "MyProject"}
	projectStudies := []saga.ProjectStudy{
		{ID: 1, IDStudyLims: "6568"},
		{ID: 2, IDStudyLims: "7777"},
	}
	studies := map[string]*saga.Study{
		"6568": {IDStudyLims: "6568", AccessionNumber: "ERP001"},
		"7777": {IDStudyLims: "7777", AccessionNumber: "ERP002"},
	}
	projectSamples := []saga.ProjectSample{
		{ID: 1, SangerID: "S1"},
		{ID: 2, SangerID: "S2"},
		{ID: 3, SangerID: "S3"},
	}
	resolvedSamples := map[string]saga.MLWHSample{
		"S1": {SangerID: "S1", IDStudyLims: "6568", LibraryType: "RNA PolyA"},
		"S2": {SangerID: "S2", IDStudyLims: "6568", LibraryType: "PCR free"},
		"S3": {SangerID: "S3", IDStudyLims: "7777", LibraryType: "RNA PolyA"},
	}
	users := []saga.ProjectUser{{ID: 1, Username: "user1"}, {ID: 2, Username: "user2"}}

	convey.Convey("C8: project_name enrichment resolves project studies, samples, libraries, and users after earlier classifiers miss", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		findSamplesByRunIDCalls := 0
		findSamplesByLibraryTypeCalls := 0
		listProjectsCalls := 0
		listProjectStudiesCalls := 0
		listProjectSamplesCalls := 0
		listProjectUsersCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++

				if identifier == "MyProject" {
					return nil, saga.ErrNotFound
				}

				study, ok := studies[identifier]
				convey.So(ok, convey.ShouldBeTrue)

				return study, nil
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return []saga.Study{}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++
				if sangerID == "MyProject" {
					return []saga.MLWHSample{}, nil
				}

				sample, ok := resolvedSamples[sangerID]
				convey.So(ok, convey.ShouldBeTrue)

				return []saga.MLWHSample{sample}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				findSamplesByLibraryTypeCalls++

				return []saga.MLWHSample{}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				listProjectsCalls++

				return []saga.Project{project}, nil
			},
			ListProjectStudiesFn: func(_ context.Context, projectID int) ([]saga.ProjectStudy, error) {
				listProjectStudiesCalls++
				convey.So(projectID, convey.ShouldEqual, 7)

				return projectStudies, nil
			},
			ListProjectSamplesFn: func(_ context.Context, projectID int) ([]saga.ProjectSample, error) {
				listProjectSamplesCalls++
				convey.So(projectID, convey.ShouldEqual, 7)

				return projectSamples, nil
			},
			ListProjectUsersFn: func(_ context.Context, projectID int) ([]saga.ProjectUser, error) {
				listProjectUsersCalls++
				convey.So(projectID, convey.ShouldEqual, 7)

				return users, nil
			},
		}

		result, err := Enrich(ctx, provider, "MyProject")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)
		convey.So(result.Graph.Project, convey.ShouldResemble, &project)
		convey.So(result.Graph.Studies, convey.ShouldResemble, []saga.Study{*studies["6568"], *studies["7777"]})
		convey.So(result.Graph.Samples, convey.ShouldResemble, []saga.MLWHSample{
			resolvedSamples["S1"],
			resolvedSamples["S2"],
			resolvedSamples["S3"],
		})
		convey.So(result.Graph.Libraries, convey.ShouldResemble, []Library{
			{LibraryType: "RNA PolyA", IDStudyLims: "6568"},
			{LibraryType: "PCR free", IDStudyLims: "6568"},
			{LibraryType: "RNA PolyA", IDStudyLims: "7777"},
		})
		convey.So(result.Graph.Users, convey.ShouldResemble, users)
		convey.So(result.Partial, convey.ShouldBeFalse)
		convey.So(result.Missing, convey.ShouldBeNil)
		convey.So(getStudyCalls, convey.ShouldEqual, 3)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 4)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 0)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 0)
		convey.So(listProjectsCalls, convey.ShouldEqual, 1)
		convey.So(listProjectStudiesCalls, convey.ShouldEqual, 1)
		convey.So(listProjectSamplesCalls, convey.ShouldEqual, 1)
		convey.So(listProjectUsersCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C8: project_name enrichment returns a partial result when the users hop fails", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				if identifier == "MyProject" {
					return nil, saga.ErrNotFound
				}

				study, ok := studies[identifier]
				convey.So(ok, convey.ShouldBeTrue)

				return study, nil
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return []saga.Study{}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, sangerID string) ([]saga.MLWHSample, error) {
				if sangerID == "MyProject" {
					return []saga.MLWHSample{}, nil
				}

				sample, ok := resolvedSamples[sangerID]
				convey.So(ok, convey.ShouldBeTrue)

				return []saga.MLWHSample{sample}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return []saga.Project{project}, nil
			},
			ListProjectStudiesFn: func(_ context.Context, _ int) ([]saga.ProjectStudy, error) {
				return projectStudies, nil
			},
			ListProjectSamplesFn: func(_ context.Context, _ int) ([]saga.ProjectSample, error) {
				return projectSamples, nil
			},
			ListProjectUsersFn: func(_ context.Context, _ int) ([]saga.ProjectUser, error) {
				return nil, saga.ErrServerError
			},
		}

		result, err := Enrich(ctx, provider, "MyProject")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)
		convey.So(result.Graph.Project, convey.ShouldResemble, &project)
		convey.So(result.Graph.Studies, convey.ShouldResemble, []saga.Study{*studies["6568"], *studies["7777"]})
		convey.So(result.Graph.Samples, convey.ShouldResemble, []saga.MLWHSample{
			resolvedSamples["S1"],
			resolvedSamples["S2"],
			resolvedSamples["S3"],
		})
		convey.So(result.Graph.Libraries, convey.ShouldResemble, []Library{
			{LibraryType: "RNA PolyA", IDStudyLims: "6568"},
			{LibraryType: "PCR free", IDStudyLims: "6568"},
			{LibraryType: "RNA PolyA", IDStudyLims: "7777"},
		})
		convey.So(result.Graph.Users, convey.ShouldBeEmpty)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopUsers,
			Reason: ReasonUpstreamError,
			Status: 502,
		}})
	})
}

func TestEnrichAllClassificationHopsFail(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C9: all classification hops returning 5xx surfaces ErrAllHopsFailed", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrServerError
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return nil, saga.ErrServerError
			},
		}

		result, err := Enrich(ctx, provider, "unknown thing")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrAllHopsFailed), convey.ShouldBeTrue)
	})
}

func TestEnrichContextErrors(t *testing.T) {
	convey.Convey("G1: context deadline errors on a classification hop are preserved", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, context.DeadlineExceeded
			},
		}

		result, err := Enrich(context.Background(), provider, "S1")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, context.DeadlineExceeded), convey.ShouldBeTrue)
	})

	convey.Convey("G1: context cancellation on a secondary hop is preserved", t, func() {
		sample := saga.MLWHSample{SangerID: "S1", IDStudyLims: "6568", LibraryType: "RNA PolyA"}
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{sample}, nil
			},
			StudyForSampleFn: func(_ context.Context, _ saga.MLWHSample) (*saga.Study, error) {
				return nil, context.Canceled
			},
		}

		result, err := Enrich(context.Background(), provider, "S1")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, context.Canceled), convey.ShouldBeTrue)
	})
}

func TestEnrichUnknownIdentifier(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C10: every classification hop returning empty or not-found surfaces ErrUnknownIdentifier", t, func() {
		getStudyCalls := 0
		allStudiesCalls := 0
		findSamplesBySangerIDCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		findSamplesByLibraryTypeCalls := 0
		listProjectsCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				getStudyCalls++
				convey.So(identifier, convey.ShouldEqual, "xyz")

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++
				convey.So(identifier, convey.ShouldEqual, "xyz")

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++
				convey.So(identifier, convey.ShouldEqual, "xyz")

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++
				convey.So(identifier, convey.ShouldEqual, "xyz")

				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesByLibraryTypeCalls++
				convey.So(identifier, convey.ShouldEqual, "xyz")

				return []saga.MLWHSample{}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				listProjectsCalls++

				return []saga.Project{}, nil
			},
		}

		result, err := Enrich(ctx, provider, "xyz")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
		convey.So(getStudyCalls, convey.ShouldEqual, 1)
		convey.So(allStudiesCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 0)
		convey.So(listProjectsCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("C10: empty identifiers surface ErrUnknownIdentifier without upstream calls", t, func() {
		upstreamCalls := 0

		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				upstreamCalls++

				return nil, nil
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				upstreamCalls++

				return nil, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				upstreamCalls++

				return nil, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				upstreamCalls++

				return nil, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				upstreamCalls++

				return nil, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				upstreamCalls++

				return nil, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				upstreamCalls++

				return nil, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				upstreamCalls++

				return nil, nil
			},
		}

		result, err := Enrich(ctx, provider, "")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
		convey.So(upstreamCalls, convey.ShouldEqual, 0)
	})
}

func TestEnrichTargetedFilterErrors(t *testing.T) {
	convey.Convey("G1: a sanger_id filter server error is carried into a later project match as upstream context", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return []saga.Project{{ID: 7, Name: "S1"}}, nil
			},
			ListProjectStudiesFn: func(_ context.Context, _ int) ([]saga.ProjectStudy, error) {
				return []saga.ProjectStudy{}, nil
			},
			ListProjectSamplesFn: func(_ context.Context, _ int) ([]saga.ProjectSample, error) {
				return []saga.ProjectSample{}, nil
			},
			ListProjectUsersFn: func(_ context.Context, _ int) ([]saga.ProjectUser, error) {
				return []saga.ProjectUser{{ID: 1, Username: "user1"}}, nil
			},
		}

		result, err := Enrich(context.Background(), provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopClassify,
			Reason: ReasonUpstreamError,
			Status: http.StatusBadGateway,
		}})
	})

	convey.Convey("G1: a run_id filter server error is carried into a later project match as upstream context", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, _ int) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return []saga.Project{{ID: 7, Name: "34134"}}, nil
			},
			ListProjectStudiesFn: func(_ context.Context, _ int) ([]saga.ProjectStudy, error) {
				return []saga.ProjectStudy{}, nil
			},
			ListProjectSamplesFn: func(_ context.Context, _ int) ([]saga.ProjectSample, error) {
				return []saga.ProjectSample{}, nil
			},
			ListProjectUsersFn: func(_ context.Context, _ int) ([]saga.ProjectUser, error) {
				return []saga.ProjectUser{{ID: 1, Username: "user1"}}, nil
			},
		}

		result, err := Enrich(context.Background(), provider, "34134")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopClassify,
			Reason: ReasonUpstreamError,
			Status: http.StatusBadGateway,
		}})
	})

	convey.Convey("G1: a library_type filter server error is carried into a later project match as upstream context", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return []saga.MLWHSample{}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, saga.ErrServerError
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return []saga.Project{{ID: 7, Name: "Project 7"}}, nil
			},
			ListProjectStudiesFn: func(_ context.Context, _ int) ([]saga.ProjectStudy, error) {
				return []saga.ProjectStudy{}, nil
			},
			ListProjectSamplesFn: func(_ context.Context, _ int) ([]saga.ProjectSample, error) {
				return []saga.ProjectSample{}, nil
			},
			ListProjectUsersFn: func(_ context.Context, _ int) ([]saga.ProjectUser, error) {
				return []saga.ProjectUser{{ID: 1, Username: "user1"}}, nil
			},
		}

		result, err := Enrich(context.Background(), provider, "Project 7")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)
		convey.So(result.Partial, convey.ShouldBeTrue)
		convey.So(result.Missing, convey.ShouldResemble, []MissingHop{{
			Hop:    HopClassify,
			Reason: ReasonUpstreamError,
			Status: http.StatusBadGateway,
		}})
	})
}

func TestEnrichRunIDPrecedence(t *testing.T) {
	convey.Convey("numeric identifiers are enriched as run IDs before sample IDs", t, func() {
		findSamplesBySangerIDCalls := 0
		findSamplesByRunIDCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, runID int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++
				convey.So(runID, convey.ShouldEqual, 34134)

				return []saga.MLWHSample{{IDRun: 34134, SangerID: "34134", IDStudyLims: "6568", LibraryType: "RNA PolyA"}}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++
				convey.So(identifier, convey.ShouldEqual, "34134")

				return []saga.MLWHSample{{SangerID: "34134", IDStudyLims: "9999", LibraryType: "wrong"}}, nil
			},
			GetSampleFilesFunc: func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
				return nil, saga.ErrNotFound
			},
		}

		result, err := Enrich(context.Background(), provider, "34134")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Graph.Samples, convey.ShouldHaveLength, 1)
		convey.So(result.Graph.Samples[0].IDRun, convey.ShouldEqual, 34134)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 0)
	})
}
