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
			provider.SamplesForLibraryTypeFunc = func(_ context.Context, _ string, _, _ int) ([]mlwh.Sample, error) {
				return nil, errors.New("unexpected SamplesForLibraryType call")
			}
			provider.FindSamplesByLibraryTypeFn = func(_ context.Context, libraryType string) ([]mlwh.Sample, error) {
				convey.So(libraryType, convey.ShouldEqual, "Chromium single cell 3 prime v3")

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

func TestClientAdapterFindSamplesBySangerIDUsesDirectLookup(t *testing.T) {
	convey.Convey("Given a client adapter wrapping a provider", t, func() {
		provider := &MockProvider{}
		adapter := NewClientAdapter(provider)

		convey.Convey("when finding samples by Sanger ID, then it uses the provider's direct Sanger ID lookup", func() {
			provider.ResolveSampleFunc = func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.New("unexpected ResolveSample call: " + raw)
			}
			provider.FindSamplesBySangerIDFn = func(_ context.Context, sangerID string) ([]mlwh.Sample, error) {
				convey.So(sangerID, convey.ShouldEqual, "SANGER123")

				return []mlwh.Sample{{
					SangerSampleID: sangerID,
					Name:           "sample-by-sanger-id",
				}}, nil
			}

			samples, err := adapter.FindSamplesBySangerID(context.Background(), "SANGER123")

			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []mlwh.Sample{{
				SangerSampleID: "SANGER123",
				Name:           "sample-by-sanger-id",
			}})
		})
	})
}

func TestClientAdapterFindSamplesByIDSampleLimsUsesDirectLookup(t *testing.T) {
	convey.Convey("Given a client adapter wrapping a provider", t, func() {
		provider := &MockProvider{}
		adapter := NewClientAdapter(provider)

		convey.Convey("when finding samples by id_sample_lims, then it uses the provider's direct id_sample_lims lookup", func() {
			provider.ResolveSampleFunc = func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.New("unexpected ResolveSample call: " + raw)
			}
			provider.FindSamplesByIDSampleLimsFn = func(_ context.Context, idSampleLims string) ([]mlwh.Sample, error) {
				convey.So(idSampleLims, convey.ShouldEqual, "12345")

				return []mlwh.Sample{{
					IDSampleLims: idSampleLims,
					Name:         "sample-by-id-sample-lims",
				}}, nil
			}

			samples, err := adapter.FindSamplesByIDSampleLims(context.Background(), "12345")

			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []mlwh.Sample{{
				IDSampleLims: "12345",
				Name:         "sample-by-id-sample-lims",
			}})
		})
	})
}

func TestClientAdapterFindSamplesByAccessionNumberUsesDirectLookup(t *testing.T) {
	convey.Convey("Given a client adapter wrapping a provider", t, func() {
		provider := &MockProvider{}
		adapter := NewClientAdapter(provider)

		convey.Convey("when finding samples by accession number, then it uses the provider's direct accession lookup", func() {
			provider.ResolveSampleFunc = func(_ context.Context, raw string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.New("unexpected ResolveSample call: " + raw)
			}
			provider.FindSamplesByAccessionNumberFn = func(_ context.Context, accessionNumber string) ([]mlwh.Sample, error) {
				convey.So(accessionNumber, convey.ShouldEqual, "ACC123")

				return []mlwh.Sample{{
					AccessionNumber: accessionNumber,
					Name:            "sample-by-accession",
				}}, nil
			}

			samples, err := adapter.FindSamplesByAccessionNumber(context.Background(), "ACC123")

			convey.So(err, convey.ShouldBeNil)
			convey.So(samples, convey.ShouldResemble, []mlwh.Sample{{
				AccessionNumber: "ACC123",
				Name:            "sample-by-accession",
			}})
		})
	})
}
