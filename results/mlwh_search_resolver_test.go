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

package results

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestA2MLWHSearchResolver(t *testing.T) {
	convey.Convey("A2.1: Given results/server.go after the change, the old outbound seqmeta sample resolver is gone", t, func() {
		source, err := os.ReadFile("server.go")
		convey.So(err, convey.ShouldBeNil)
		text := string(source)

		oldResolverName := "Seqmeta" + "SampleResolver"
		studyHTTPPath := `"/` + `study/`
		enrichHTTPPath := `"/` + `enrich/`
		convey.So(text, convey.ShouldNotContainSubstring, oldResolverName)
		convey.So(text, convey.ShouldNotContainSubstring, studyHTTPPath)
		convey.So(text, convey.ShouldNotContainSubstring, enrichHTTPPath)
	})

	convey.Convey("A2.2: Given ExpandSearchValues returns named search values, Expand returns samples, runs, and lanes", t, func() {
		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindStudyLimsID)
				convey.So(canonical, convey.ShouldEqual, "6568")

				return mlwh.SearchValues{
					Samples: []string{"A", "B"},
					Runs:    []string{"100"},
					Lanes:   []string{"100_1_0"},
				}, nil
			},
		}

		samples, runs, lanes, err := NewMLWHSearchResolver(expander).Expand(context.Background(), mlwh.KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldResemble, []string{"A", "B"})
		convey.So(runs, convey.ShouldResemble, []string{"100"})
		convey.So(lanes, convey.ShouldResemble, []string{"100_1_0"})
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)
	})

	convey.Convey("A2.2: Given direct sample metadata expansion, Expand still calls ExpandSearchValues", t, func() {
		expander := &mockSearchExpander{
			searchValuesFunc: func(_ context.Context, kind mlwh.IdentifierKind, canonical string) (mlwh.SearchValues, error) {
				convey.So(kind, convey.ShouldEqual, mlwh.KindSupplierName)
				convey.So(canonical, convey.ShouldEqual, "Hek_R1")

				return mlwh.SearchValues{Samples: []string{"7607STDY14643771"}}, nil
			},
			sampleNamesFunc: func(context.Context, mlwh.IdentifierKind, string) ([]string, error) {
				return nil, fmt.Errorf("ExpandSampleSearchValues must not be called by Expand")
			},
		}

		samples, runs, lanes, err := NewMLWHSearchResolver(expander).Expand(context.Background(), mlwh.KindSupplierName, "Hek_R1")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldResemble, []string{"7607STDY14643771"})
		convey.So(runs, convey.ShouldBeEmpty)
		convey.So(lanes, convey.ShouldBeEmpty)
		convey.So(expander.expandCalls, convey.ShouldEqual, 1)
		convey.So(expander.sampleOnlyCalls, convey.ShouldEqual, 0)
	})

	convey.Convey("A2.3: Given ExpandSearchValues returns not found, Expand returns empty values and no error", t, func() {
		expander := &mockSearchExpander{
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, mlwh.ErrNotFound
			},
		}

		samples, runs, lanes, err := NewMLWHSearchResolver(expander).Expand(context.Background(), mlwh.KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldBeNil)
		convey.So(samples, convey.ShouldBeEmpty)
		convey.So(runs, convey.ShouldBeEmpty)
		convey.So(lanes, convey.ShouldBeEmpty)
	})

	convey.Convey("A2.4: Given ExpandSearchValues returns cache-never-synced, Expand preserves that sentinel in the error", t, func() {
		expander := &mockSearchExpander{
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, mlwh.ErrCacheNeverSynced
			},
		}

		_, _, _, err := NewMLWHSearchResolver(expander).Expand(context.Background(), mlwh.KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, mlwh.ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrMLWHFailed), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "results: mlwh unavailable")
		convey.So(err.Error(), convey.ShouldNotContainSubstring, "seqmeta unavailable")
	})

	convey.Convey("A2.4: Given ExpandSearchValues returns joined cache-never-synced and not-found, Expand preserves the cache error", t, func() {
		expander := &mockSearchExpander{
			searchValuesFunc: func(context.Context, mlwh.IdentifierKind, string) (mlwh.SearchValues, error) {
				return mlwh.SearchValues{}, errors.Join(mlwh.ErrCacheNeverSynced, mlwh.ErrNotFound)
			},
		}

		samples, runs, lanes, err := NewMLWHSearchResolver(expander).Expand(context.Background(), mlwh.KindStudyLimsID, "6568")

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(errors.Is(err, ErrMLWHFailed), convey.ShouldBeTrue)
		convey.So(errors.Is(err, mlwh.ErrCacheNeverSynced), convey.ShouldBeTrue)
		convey.So(samples, convey.ShouldBeNil)
		convey.So(runs, convey.ShouldBeNil)
		convey.So(lanes, convey.ShouldBeNil)
	})
}
