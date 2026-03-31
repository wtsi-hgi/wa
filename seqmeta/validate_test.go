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

				return []saga.MLWHSample{
					{SangerID: "SANG123"},
					{IDSampleLims: "LIMS456"},
					{AccessionNumber: "SAM789"},
					{IDRun: 12345},
					{LibraryType: "Chromium single cell"},
					{SangerID: "6568"},
				}, nil
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
		convey.So(allSamplesCalls, convey.ShouldEqual, 5)

		provider.GetStudyFunc = func(_ context.Context, _ string) (*saga.Study, error) { return nil, saga.ErrNotFound }
		result, err = Validate(ctx, provider, "MyProject")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Type, convey.ShouldEqual, IdentifierProjectName)

		_, err = Validate(ctx, provider, "xyz")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)

		_, err = Validate(ctx, provider, "")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
	})

	convey.Convey("D5: upstream errors propagate immediately", t, func() {
		provider := &MockProvider{}

		provider.GetStudyFunc = func(_ context.Context, _ string) (*saga.Study, error) {
			return nil, errors.New("connection refused")
		}
		_, err := Validate(ctx, provider, "6568")
		convey.So(err.Error(), convey.ShouldContainSubstring, "connection refused")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeFalse)

		provider.GetStudyFunc = func(_ context.Context, _ string) (*saga.Study, error) {
			return nil, saga.ErrNotFound
		}
		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) {
			return nil, errors.New("502 bad gateway")
		}
		_, err = Validate(ctx, provider, "ERP001")
		convey.So(err.Error(), convey.ShouldContainSubstring, "502 bad gateway")

		provider.AllStudiesFunc = func(_ context.Context) ([]saga.Study, error) { return nil, nil }
		provider.AllSamplesFunc = func(_ context.Context) ([]saga.MLWHSample, error) {
			return nil, errors.New("unauthorized")
		}
		_, err = Validate(ctx, provider, "SANG123")
		convey.So(err.Error(), convey.ShouldContainSubstring, "unauthorized")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeFalse)

		provider.AllSamplesFunc = func(_ context.Context) ([]saga.MLWHSample, error) { return nil, nil }
		provider.ListProjectsFunc = func(_ context.Context) ([]saga.Project, error) {
			return nil, errors.New("timeout")
		}
		_, err = Validate(ctx, provider, "MyProject")
		convey.So(err.Error(), convey.ShouldContainSubstring, "timeout")
	})
}
