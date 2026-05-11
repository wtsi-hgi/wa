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
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestNewClientAdapterSatisfiesProvider(t *testing.T) {
	convey.Convey("Given NewClientAdapter(client)", t, func() {
		provider := Provider(NewClientAdapter(&MockProvider{}))

		convey.Convey("when assigned to a variable of type Provider, then it compiles", func() {
			convey.So(provider, convey.ShouldHaveSameTypeAs, &ClientAdapter{})
		})
	})
}

func TestClientAdapterFindSamplesByLibraryTypeUsesDirectLookup(t *testing.T) {
	convey.Convey("Given a client adapter wrapping a provider", t, func() {
		provider := &MockProvider{}
		adapter := NewClientAdapter(provider)

		convey.Convey("when finding samples by library type, then it uses the provider's direct library-type lookup", func() {
			provider.AllStudiesFunc = func(_ context.Context, _, _ int) ([]mlwh.Study, error) {
				return nil, errors.New("unexpected AllStudies call")
			}
			provider.SamplesForLibraryFunc = func(_ context.Context, _, _ string, _, _ int) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected SamplesForLibrary call")
			}
			provider.SamplesForLibraryTypeFunc = func(_ context.Context, libraryType string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Chromium single cell 3 prime v3")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{{
					Name:           "Sample 1",
					SangerSampleID: "S1",
					Studies:        []mlwh.Study{{IDStudyLims: "6568"}},
					Libraries:      []mlwh.Library{{PipelineIDLims: libraryType, IDStudyLims: "6568"}},
				}}, nil
			}

			samples, err := adapter.FindSamplesByLibraryType(context.Background(), "Chromium single cell 3 prime v3")

			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []mlwh.Sample{{
				Name:           "Sample 1",
				SangerSampleID: "S1",
				Studies:        []mlwh.Study{{IDStudyLims: "6568"}},
				Libraries:      []mlwh.Library{{PipelineIDLims: "Chromium single cell 3 prime v3", IDStudyLims: "6568"}},
			}})
		})
	})
}
