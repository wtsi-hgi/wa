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

func TestValidate(t *testing.T) {
	ctx := context.Background()

	convey.Convey("study matches are converted into IdentifierResult values", t, func() {
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "6568")
				study := &mlwh.Study{IDStudyLims: "6568", Name: "HCA"}
				return mlwh.Match{Kind: mlwh.KindStudyLimsID, Canonical: "6568", Study: study}, nil
			},
		}

		result, err := Validate(ctx, provider, "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Identifier, convey.ShouldEqual, "6568")
		convey.So(result.Type, convey.ShouldEqual, IdentifierStudyLimsID)
		convey.So(result.Object, convey.ShouldResemble, mlwh.Study{IDStudyLims: "6568", Name: "HCA"})
	})

	convey.Convey("sample matches preserve the mlwh sample pointer and canonical identifier", t, func() {
		sample := &mlwh.Sample{Name: "7607STDY14643771", SangerID: "S1"}
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "S1")
				return mlwh.Match{Kind: mlwh.KindSangerSampleName, Canonical: sample.Name, Sample: sample}, nil
			},
		}

		result, err := Validate(ctx, provider, "S1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Identifier, convey.ShouldEqual, sample.Name)
		convey.So(result.Type, convey.ShouldEqual, IdentifierSangerSampleName)
		convey.So(result.Object, convey.ShouldResemble, sample)
	})

	convey.Convey("run matches preserve the mlwh run pointer and canonical identifier", t, func() {
		run := &mlwh.Run{IDRun: 12345}
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "12345")
				return mlwh.Match{Kind: mlwh.KindRunID, Canonical: "12345", Run: run}, nil
			},
		}

		result, err := Validate(ctx, provider, "12345")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Identifier, convey.ShouldEqual, "12345")
		convey.So(result.Type, convey.ShouldEqual, IdentifierRunID)
		convey.So(result.Object, convey.ShouldResemble, run)
	})

	convey.Convey("library matches preserve the mlwh library pointer and canonical identifier", t, func() {
		library := &mlwh.Library{PipelineIDLims: "Standard"}
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(_ context.Context, raw string) (mlwh.Match, error) {
				convey.So(raw, convey.ShouldEqual, "Standard")
				return mlwh.Match{Kind: mlwh.KindLibraryType, Canonical: "Standard", Library: library}, nil
			},
		}

		result, err := Validate(ctx, provider, "Standard")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Identifier, convey.ShouldEqual, "Standard")
		convey.So(result.Type, convey.ShouldEqual, IdentifierLibraryType)
		convey.So(result.Object, convey.ShouldResemble, library)
	})

	convey.Convey("unsupported identifiers preserve the underlying mlwh error and raw value", t, func() {
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(context.Context, string) (mlwh.Match, error) {
				return mlwh.Match{}, errors.Join(mlwh.ErrUnsupportedIdentifier, errors.New("SQSCP"))
			},
		}

		_, err := Validate(ctx, provider, "SQSCP")
		convey.So(errors.Is(err, mlwh.ErrUnsupportedIdentifier), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "SQSCP")
	})

	convey.Convey("mlwh.ErrNotFound is translated into ErrUnknownIdentifier", t, func() {
		provider := &MockProvider{
			ClassifyIdentifierFunc: func(context.Context, string) (mlwh.Match, error) {
				return mlwh.Match{}, mlwh.ErrNotFound
			},
		}

		_, err := Validate(ctx, provider, "unknown")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
	})

	convey.Convey("empty identifiers are rejected before provider calls", t, func() {
		provider := &MockProvider{}
		_, err := Validate(ctx, provider, "")
		convey.So(errors.Is(err, ErrUnknownIdentifier), convey.ShouldBeTrue)
	})
}
