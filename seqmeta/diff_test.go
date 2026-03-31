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

type diffTestItem struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

func TestDiff(t *testing.T) {
	idFunc := func(item diffTestItem) string { return item.ID }

	convey.Convey("C1: first poll returns all added", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		result, err := Diff(store, "q1", []diffTestItem{{ID: "a"}, {ID: "b"}, {ID: "c"}}, idFunc)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 3)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)

		result, err = Diff(store, "q2", []diffTestItem{}, idFunc)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldNotBeNil)
		convey.So(result.Modified, convey.ShouldNotBeNil)
		convey.So(result.Removed, convey.ShouldNotBeNil)
		convey.So(result.Added, convey.ShouldBeEmpty)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("C2: unchanged data returns empty diff", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		_, err = Diff(store, "q1", []diffTestItem{{ID: "a", Value: "1"}, {ID: "b", Value: "2"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)

		result, err := Diff(store, "q1", []diffTestItem{{ID: "a", Value: "1"}, {ID: "b", Value: "2"}}, idFunc)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldBeEmpty)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("C3: new, modified, and removed entries are detected", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		_, err = Diff(store, "q1", []diffTestItem{{ID: "a", Value: "1"}, {ID: "b", Value: "2"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)

		result, err := Diff(store, "q1", []diffTestItem{{ID: "b", Value: "3"}, {ID: "c", Value: "4"}}, idFunc)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []diffTestItem{{ID: "c", Value: "4"}})
		convey.So(result.Modified, convey.ShouldResemble, []diffTestItem{{ID: "b", Value: "3"}})
		convey.So(result.Removed, convey.ShouldResemble, []string{"a"})

		result, err = Diff(store, "q1", []diffTestItem{{ID: "a", Value: "9"}}, idFunc)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []diffTestItem{{ID: "a", Value: "9"}})
	})

	convey.Convey("C4: group hashing handles shared IDs deterministically", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		first := []diffTestItem{{ID: "s1", Value: "run1"}, {ID: "s1", Value: "run2"}}
		result, err := Diff(store, "q1", first, idFunc)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, first)

		entries, err := store.LoadEntries("q1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldHaveLength, 1)

		result, err = Diff(store, "q1", []diffTestItem{{ID: "s1", Value: "run1"}, {ID: "s1", Value: "run2"}, {ID: "s1", Value: "run3"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Modified, convey.ShouldResemble, []diffTestItem{{ID: "s1", Value: "run1"}, {ID: "s1", Value: "run2"}, {ID: "s1", Value: "run3"}})

		_, err = Diff(store, "q2", first, idFunc)
		convey.So(err, convey.ShouldBeNil)
		result, err = Diff(store, "q2", []diffTestItem{{ID: "s1", Value: "run2"}, {ID: "s1", Value: "run1"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldBeEmpty)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("C5: tombstones persist", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		_, err = Diff(store, "q1", []diffTestItem{{ID: "a", Value: "1"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)

		result, err := Diff(store, "q1", nil, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Removed, convey.ShouldResemble, []string{"a"})

		result, err = Diff(store, "q1", nil, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Removed, convey.ShouldBeEmpty)

		entries, err := store.LoadEntries("q1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries["a"].Tombstone, convey.ShouldBeTrue)
	})
}

func TestDiffStudySamples(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C6: DiffStudySamples diffs study samples", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			AllSamplesForStudyFunc: func(_ context.Context, studyID string) ([]saga.MLWHSample, error) {
				convey.So(studyID, convey.ShouldEqual, "100")

				return []saga.MLWHSample{{SangerID: "S1"}, {SangerID: "S2"}}, nil
			},
		}

		result, err := DiffStudySamples(ctx, provider, store, "100")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)

		entriesBefore, err := store.LoadEntries("study_samples:100")
		convey.So(err, convey.ShouldBeNil)

		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return nil, errors.New("boom")
		}

		_, err = DiffStudySamples(ctx, provider, store, "100")
		convey.So(err, convey.ShouldNotBeNil)

		entriesAfter, err := store.LoadEntries("study_samples:100")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entriesAfter, convey.ShouldResemble, entriesBefore)

		provider.AllSamplesForStudyFunc = func(_ context.Context, _ string) ([]saga.MLWHSample, error) {
			return []saga.MLWHSample{{SangerID: "S1"}, {SangerID: "S2"}, {SangerID: "S3"}}, nil
		}

		result, err = DiffStudySamples(ctx, provider, store, "100")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []saga.MLWHSample{{SangerID: "S3"}})
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})
}

func TestDiffSampleFiles(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C7: DiffSampleFiles diffs iRODS files", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			GetSampleFilesFunc: func(_ context.Context, sangerID string) ([]saga.IRODSFile, error) {
				convey.So(sangerID, convey.ShouldEqual, "SANG1")

				return []saga.IRODSFile{{Collection: "/a"}}, nil
			},
		}

		result, err := DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 1)

		entriesBefore, err := store.LoadEntries("sample_files:SANG1")
		convey.So(err, convey.ShouldBeNil)

		provider.GetSampleFilesFunc = func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
			return nil, errors.New("boom")
		}

		_, err = DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldNotBeNil)

		entriesAfter, err := store.LoadEntries("sample_files:SANG1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entriesAfter, convey.ShouldResemble, entriesBefore)

		provider.GetSampleFilesFunc = func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
			return []saga.IRODSFile{{Collection: "/a"}, {Collection: "/b"}}, nil
		}
		_, err = DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldBeNil)

		provider.GetSampleFilesFunc = func(_ context.Context, _ string) ([]saga.IRODSFile, error) {
			return []saga.IRODSFile{{Collection: "/a"}}, nil
		}
		result, err = DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Removed, convey.ShouldResemble, []string{"/b"})
	})
}
