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
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/saga"
)

func TestValidate(t *testing.T) {
	ctx := context.Background()

	convey.Convey("D1: study identifiers are validated", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, studyID string) (*saga.Study, error) {
				if studyID == "6568" {
					return &saga.Study{Name: "HCA"}, nil
				}

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return []saga.Study{{AccessionNumber: "ERP001", Name: "Study"}}, nil
			},
		}

		result, err := Validate(ctx, provider, "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(result.Object.(*saga.Study).Name, convey.ShouldEqual, "HCA")

		provider.GetStudyFunc = func(_ context.Context, _ string) (*saga.Study, error) {
			return nil, saga.ErrNotFound
		}
		result, err = Validate(ctx, provider, "ERP001")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyAccession)
		convey.So(result.Object.(saga.Study).AccessionNumber, convey.ShouldEqual, "ERP001")
	})

	convey.Convey("D2-D4: sample, project, and unknown identifiers are validated in priority order", t, func() {
		allSamplesCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, identifier string) (*saga.Study, error) {
				if identifier == "6568" {
					return &saga.Study{IDStudyLims: "6568"}, nil
				}

				return nil, saga.ErrNotFound
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) { return nil, nil },
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return nil, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				if identifier != "SANG123" {
					return nil, nil
				}

				return []saga.MLWHSample{{SangerID: "SANG123"}}, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				if identifier != "LIMS456" {
					return nil, nil
				}

				return []saga.MLWHSample{{IDSampleLims: "LIMS456"}}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				if identifier != "SAM789" {
					return nil, nil
				}

				return []saga.MLWHSample{{AccessionNumber: "SAM789"}}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, identifier int) ([]saga.MLWHSample, error) {
				if identifier != 12345 {
					return nil, nil
				}

				return []saga.MLWHSample{{IDRun: 12345}}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				if identifier != "Chromium single cell" {
					return nil, nil
				}

				return []saga.MLWHSample{{LibraryType: "Chromium single cell"}}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return []saga.Project{{Name: "MyProject"}}, nil
			},
		}

		result, err := Validate(ctx, provider, "SANG123")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)

		result, err = Validate(ctx, provider, "LIMS456")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSampleLimsID)

		result, err = Validate(ctx, provider, "SAM789")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSampleAccession)

		result, err = Validate(ctx, provider, "12345")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)

		result, err = Validate(ctx, provider, "Chromium single cell")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)

		result, err = Validate(ctx, provider, "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyID)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)

		provider.GetStudyFunc = func(_ context.Context, _ string) (*saga.Study, error) { return nil, saga.ErrNotFound }
		result, err = Validate(ctx, provider, "MyProject")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)

		_, err = Validate(ctx, provider, "xyz")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)

		_, err = Validate(ctx, provider, "")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
	})

	convey.Convey("a non-404 GetStudy error for a sample identifier falls through to the samples lookup", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.APIError{StatusCode: 422, Message: "Unprocessable Entity"}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) { return nil, nil },
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				if identifier != "SANG205" {
					return nil, nil
				}

				return []saga.MLWHSample{{SangerID: "SANG205"}}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) { return nil, nil },
		}

		result, err := Validate(ctx, provider, "SANG205")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Object.(saga.MLWHSample).SangerID, convey.ShouldEqual, "SANG205")
	})

	convey.Convey("E6: sample validation uses the targeted Sanger sample lookup without changing the object shape", t, func() {
		allSamplesCalls := 0
		findSamplesBySangerIDCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.APIError{StatusCode: 422, Message: "Unprocessable Entity"}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, nil
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return []saga.MLWHSample{{SangerID: "S1"}}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesBySangerIDCalls++
				if identifier != "S1" {
					return nil, saga.ErrNotFound
				}

				return []saga.MLWHSample{{SangerID: "S1", IDSampleLims: "L1"}}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return nil, nil
			},
		}

		result, err := Validate(ctx, provider, "S1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Object, convey.ShouldHaveSameTypeAs, saga.MLWHSample{})
		sample, ok := result.Object.(saga.MLWHSample)
		convey.So(ok, convey.ShouldBeTrue)
		convey.So(sample.SangerID, convey.ShouldEqual, "S1")
		convey.So(sample.IDSampleLims, convey.ShouldEqual, "L1")
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("validate uses targeted sample-like lookups for the remaining identifier types without calling AllSamples", t, func() {
		allSamplesCalls := 0
		findSamplesByIDSampleLimsCalls := 0
		findSamplesByAccessionNumberCalls := 0
		findSamplesByRunIDCalls := 0
		findSamplesByLibraryTypeCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.APIError{StatusCode: 422, Message: "Unprocessable Entity"}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				return nil, nil
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return []saga.MLWHSample{{SangerID: "fallback"}}, nil
			},
			FindSamplesBySangerIDFn: func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
				return nil, nil
			},
			FindSamplesByIDSampleLimsFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesByIDSampleLimsCalls++
				if identifier != "L1" {
					return nil, nil
				}

				return []saga.MLWHSample{{IDSampleLims: "L1", SangerID: "S1"}}, nil
			},
			FindSamplesByAccessionNumberFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesByAccessionNumberCalls++
				if identifier != "SAM1" {
					return nil, nil
				}

				return []saga.MLWHSample{{AccessionNumber: "SAM1", SangerID: "S2"}}, nil
			},
			FindSamplesByRunIDFn: func(_ context.Context, identifier int) ([]saga.MLWHSample, error) {
				findSamplesByRunIDCalls++
				if identifier != 42 {
					return nil, nil
				}

				return []saga.MLWHSample{{IDRun: 42, SangerID: "S3"}}, nil
			},
			FindSamplesByLibraryTypeFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
				findSamplesByLibraryTypeCalls++
				if identifier != "RNA PolyA" {
					return nil, nil
				}

				return []saga.MLWHSample{{LibraryType: "RNA PolyA", SangerID: "S4"}}, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				return nil, nil
			},
		}

		limsResult, limsErr := Validate(ctx, provider, "L1")
		accessionResult, accessionErr := Validate(ctx, provider, "SAM1")
		runResult, runErr := Validate(ctx, provider, "42")
		libraryResult, libraryErr := Validate(ctx, provider, "RNA PolyA")

		convey.So(limsErr, convey.ShouldBeNil)
		convey.So(limsResult.Type, convey.ShouldEqual, IdentifierSampleLimsID)
		convey.So(limsResult.Object.(saga.MLWHSample).IDSampleLims, convey.ShouldEqual, "L1")

		convey.So(accessionErr, convey.ShouldBeNil)
		convey.So(accessionResult.Type, convey.ShouldEqual, IdentifierSampleAccession)
		convey.So(accessionResult.Object.(saga.MLWHSample).AccessionNumber, convey.ShouldEqual, "SAM1")

		convey.So(runErr, convey.ShouldBeNil)
		convey.So(runResult.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(runResult.Object.(saga.MLWHSample).IDRun, convey.ShouldEqual, 42)

		convey.So(libraryErr, convey.ShouldBeNil)
		convey.So(libraryResult.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(libraryResult.Object.(saga.MLWHSample).LibraryType, convey.ShouldEqual, "RNA PolyA")

		convey.So(findSamplesByIDSampleLimsCalls, convey.ShouldEqual, 4)
		convey.So(findSamplesByAccessionNumberCalls, convey.ShouldEqual, 3)
		convey.So(findSamplesByRunIDCalls, convey.ShouldEqual, 1)
		convey.So(findSamplesByLibraryTypeCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("validate falls back to AllSamples when a targeted filter is known unsupported", t, func() {
		allSamplesCalls := 0
		findSamplesBySangerIDCalls := 0
		provider := &filterSupportProvider{
			MockProvider: &MockProvider{
				GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
					return nil, saga.APIError{StatusCode: 422, Message: "Unprocessable Entity"}
				},
				AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
					return nil, nil
				},
				AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
					allSamplesCalls++

					return []saga.MLWHSample{{SangerID: "S1", IDSampleLims: "L1"}}, nil
				},
				FindSamplesBySangerIDFn: func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
					findSamplesBySangerIDCalls++
					convey.So(identifier, convey.ShouldEqual, "S1")

					return nil, saga.ErrServerError
				},
				ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
					return nil, nil
				},
			},
			supported: map[string]bool{mlwhFilterSangerID: false},
		}

		result, err := Validate(ctx, provider, "S1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		if result == nil {
			return
		}

		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleID)
		convey.So(result.Object.(saga.MLWHSample).SangerID, convey.ShouldEqual, "S1")
		convey.So(findSamplesBySangerIDCalls, convey.ShouldEqual, 1)
		convey.So(allSamplesCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("a non-404 GetStudy error for an unknown identifier surfaces as ErrUnknownIdentifier", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.APIError{StatusCode: 422, Message: "Unprocessable Entity"}
			},
			AllStudiesFunc:   func(_ context.Context) ([]saga.Study, error) { return nil, nil },
			AllSamplesFunc:   func(_ context.Context) ([]saga.MLWHSample, error) { return nil, nil },
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) { return nil, nil },
		}

		_, err := Validate(ctx, provider, "SANG205")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
	})

	convey.Convey("a 5xx GetStudy error propagates without triggering the cascade", t, func() {
		allStudiesCalls := 0
		allSamplesCalls := 0
		listProjectsCalls := 0
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, &saga.APIError{StatusCode: 500, Message: "Server Error"}
			},
			AllStudiesFunc: func(_ context.Context) ([]saga.Study, error) {
				allStudiesCalls++

				return nil, nil
			},
			AllSamplesFunc: func(_ context.Context) ([]saga.MLWHSample, error) {
				allSamplesCalls++

				return nil, nil
			},
			ListProjectsFunc: func(_ context.Context) ([]saga.Project, error) {
				listProjectsCalls++

				return nil, nil
			},
		}

		_, err := Validate(ctx, provider, "SANG001")
		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "HTTP 500")
		convey.So(allStudiesCalls, convey.ShouldEqual, 0)
		convey.So(allSamplesCalls, convey.ShouldEqual, 0)
		convey.So(listProjectsCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("D5: upstream errors propagate immediately", t, func() {
		provider := &MockProvider{}

		provider.GetStudyFunc = func(_ context.Context, _ string) (*saga.Study, error) {
			return nil, saga.ErrNotFound
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return nil, errors.New("502 bad gateway")
		}
		_, err := Validate(ctx, provider, "ERP001")
		convey.So(err.Error(), convey.ShouldContainSubstring, "502 bad gateway")

		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) { return nil, nil }
		provider.FindSamplesBySangerIDFn = func(_ context.Context, identifier string) ([]saga.MLWHSample, error) {
			if identifier != "SANG123" {
				return nil, nil
			}

			return nil, errors.New("unauthorized")
		}
		_, err = Validate(ctx, provider, "SANG123")
		convey.So(err.Error(), convey.ShouldContainSubstring, "unauthorized")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeFalse)

		provider.FindSamplesBySangerIDFn = func(_ context.Context, _ string) ([]saga.MLWHSample, error) { return nil, nil }
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return nil, errors.New("timeout")
		}
		_, err = Validate(ctx, provider, "MyProject")
		convey.So(err.Error(), convey.ShouldContainSubstring, "timeout")
	})
}
