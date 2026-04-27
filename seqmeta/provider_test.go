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

func TestMockProviderGetStudy(t *testing.T) {
	convey.Convey("Given a MockProvider implementing SAGAProvider that returns a study for 6568", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, studyID string) (*saga.Study, error) {
				convey.So(studyID, convey.ShouldEqual, "6568")

				return &saga.Study{IDStudyLims: "6568", Name: "Study 6568"}, nil
			},
		}

		study, err := provider.GetStudy(context.Background(), "6568")

		convey.Convey("when GetStudy is called, then the study is returned with no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(study, convey.ShouldNotBeNil)
			convey.So(study.IDStudyLims, convey.ShouldEqual, "6568")
			convey.So(study.Name, convey.ShouldEqual, "Study 6568")
		})
	})
}

func TestMockProviderGetStudyNotFound(t *testing.T) {
	convey.Convey("Given a MockProvider configured to return saga.ErrNotFound for GetStudy", t, func() {
		provider := &MockProvider{
			GetStudyFunc: func(_ context.Context, _ string) (*saga.Study, error) {
				return nil, saga.ErrNotFound
			},
		}

		study, err := provider.GetStudy(context.Background(), "6568")

		convey.Convey("when GetStudy is called, then errors.Is reports saga.ErrNotFound", func() {
			convey.So(study, convey.ShouldBeNil)
			convey.So(errors.Is(err, saga.ErrNotFound), convey.ShouldBeTrue)
		})
	})
}

func TestMockProviderFindSamplesBySangerIDZeroValue(t *testing.T) {
	convey.Convey("Given a zero-value MockProvider", t, func() {
		provider := &MockProvider{}

		samples, err := provider.FindSamplesBySangerID(context.Background(), "x")

		convey.Convey("when FindSamplesBySangerID is called, then an empty slice and nil error are returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldNotBeNil)
			convey.So(samples, convey.ShouldBeEmpty)
		})
	})
}

func TestMockProviderFindSamplesByRunID(t *testing.T) {
	convey.Convey("Given a MockProvider with FindSamplesByRunIDFn returning one sample for run 42", t, func() {
		provider := &MockProvider{
			FindSamplesByRunIDFn: func(_ context.Context, id int) ([]saga.MLWHSample, error) {
				convey.So(id, convey.ShouldEqual, 42)

				return []saga.MLWHSample{{IDRun: 42, SangerID: "SANG42"}}, nil
			},
		}

		samples, err := provider.FindSamplesByRunID(context.Background(), 42)

		convey.Convey("when FindSamplesByRunID is called, then the configured sample is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldHaveLength, 1)
			convey.So(samples[0].IDRun, convey.ShouldEqual, 42)
			convey.So(samples[0].SangerID, convey.ShouldEqual, "SANG42")
		})
	})
}

func TestMockProviderStudyForSampleNotFound(t *testing.T) {
	convey.Convey("Given a MockProvider with StudyForSampleFn returning saga.ErrNotFound", t, func() {
		provider := &MockProvider{
			StudyForSampleFn: func(_ context.Context, sample saga.MLWHSample) (*saga.Study, error) {
				convey.So(sample.SangerID, convey.ShouldEqual, "SANG1")

				return nil, saga.ErrNotFound
			},
		}

		study, err := provider.StudyForSample(context.Background(), saga.MLWHSample{SangerID: "SANG1"})

		convey.Convey("when StudyForSample is called, then errors.Is reports saga.ErrNotFound", func() {
			convey.So(study, convey.ShouldBeNil)
			convey.So(errors.Is(err, saga.ErrNotFound), convey.ShouldBeTrue)
		})
	})
}
