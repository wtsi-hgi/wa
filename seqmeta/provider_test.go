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
