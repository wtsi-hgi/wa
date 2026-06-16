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

package mlwhdiff

import (
	"context"
	"errors"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/wtsi-hgi/wa/mlwh"
)

func TestDiffStudySamples(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C6: DiffStudySamples diffs study samples", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			SamplesForStudyFunc: func(_ context.Context, studyID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(studyID, convey.ShouldEqual, "100")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
		}

		result, err := DiffStudySamples(ctx, provider, store, "100")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 2)
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)

		entriesBefore, err := store.LoadEntries("study_samples:100")
		convey.So(err, convey.ShouldBeNil)

		provider.SamplesForStudyFunc = func(_ context.Context, _ string, _ int, _ int) ([]mlwh.Sample, error) {
			return nil, errors.New("boom")
		}

		_, err = DiffStudySamples(ctx, provider, store, "100")
		convey.So(err, convey.ShouldNotBeNil)

		entriesAfter, err := store.LoadEntries("study_samples:100")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entriesAfter, convey.ShouldResemble, entriesBefore)

		provider.SamplesForStudyFunc = func(_ context.Context, _ string, _ int, _ int) ([]mlwh.Sample, error) {
			return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}, {Name: "S3"}}, nil
		}

		result, err = DiffStudySamples(ctx, provider, store, "100")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []mlwh.Sample{{Name: "S3"}})
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldBeEmpty)
	})

	convey.Convey("D4/C4: DiffStudySamples uses cache-backed SamplesForStudy", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			SamplesForStudyFunc: func(_ context.Context, studyID string, limit, offset int) ([]mlwh.Sample, error) {
				convey.So(studyID, convey.ShouldEqual, "6568")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.Sample{{Name: "S1"}, {Name: "S2"}}, nil
			},
		}

		result, err := DiffStudySamples(ctx, provider, store, "6568")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []mlwh.Sample{{Name: "S1"}, {Name: "S2"}})
	})

	convey.Convey("C2.4: DiffStudySamples hashes only the queried study pairing for fan-out samples", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		sample6568 := mlwh.Sample{
			Name:      "S1",
			Studies:   []mlwh.Study{{IDStudyLims: "6568"}, {IDStudyLims: "6569"}},
			Libraries: []mlwh.Library{{PipelineIDLims: "Standard", IDStudyLims: "6568"}, {PipelineIDLims: "Chromium", IDStudyLims: "6569"}},
		}
		sample6569 := mlwh.Sample{
			Name:      "S1",
			Studies:   []mlwh.Study{{IDStudyLims: "6568"}, {IDStudyLims: "6569"}},
			Libraries: []mlwh.Library{{PipelineIDLims: "Standard", IDStudyLims: "6568"}, {PipelineIDLims: "Chromium", IDStudyLims: "6569"}},
		}

		prepared6568, err := prepareDiffStudySamples(store, "6568", []mlwh.Sample{sample6568})
		convey.So(err, convey.ShouldBeNil)
		convey.So(prepared6568.Result.Added, convey.ShouldResemble, []mlwh.Sample{{Name: "S1", Studies: []mlwh.Study{{IDStudyLims: "6568"}}, Libraries: []mlwh.Library{{PipelineIDLims: "Standard", IDStudyLims: "6568"}}}})
		convey.So(prepared6568.Commit(), convey.ShouldBeNil)

		prepared6569, err := prepareDiffStudySamples(store, "6569", []mlwh.Sample{sample6569})
		convey.So(err, convey.ShouldBeNil)
		convey.So(prepared6569.Result.Added, convey.ShouldResemble, []mlwh.Sample{{Name: "S1", Studies: []mlwh.Study{{IDStudyLims: "6569"}}, Libraries: []mlwh.Library{{PipelineIDLims: "Chromium", IDStudyLims: "6569"}}}})
	})
}

func TestDiffSampleFiles(t *testing.T) {
	ctx := context.Background()

	convey.Convey("C7: DiffSampleFiles diffs iRODS files", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			IRODSPathsForSampleFunc: func(_ context.Context, sangerID string, limit, offset int) ([]mlwh.IRODSPath, error) {
				convey.So(sangerID, convey.ShouldEqual, "SANG1")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.IRODSPath{{Collection: "/a", DataObject: "a.bam", IRODSPath: "/a/a.bam"}}, nil
			},
		}

		result, err := DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldHaveLength, 1)

		entriesBefore, err := store.LoadEntries("sample_files:SANG1")
		convey.So(err, convey.ShouldBeNil)

		provider.IRODSPathsForSampleFunc = func(_ context.Context, _ string, _ int, _ int) ([]mlwh.IRODSPath, error) {
			return nil, errors.New("boom")
		}

		_, err = DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldNotBeNil)

		entriesAfter, err := store.LoadEntries("sample_files:SANG1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entriesAfter, convey.ShouldResemble, entriesBefore)

		provider.IRODSPathsForSampleFunc = func(_ context.Context, _ string, _ int, _ int) ([]mlwh.IRODSPath, error) {
			return []mlwh.IRODSPath{{Collection: "/a", DataObject: "a.bam", IRODSPath: "/a/a.bam"}, {Collection: "/b", DataObject: "b.bam", IRODSPath: "/b/b.bam"}}, nil
		}
		_, err = DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldBeNil)

		provider.IRODSPathsForSampleFunc = func(_ context.Context, _ string, _ int, _ int) ([]mlwh.IRODSPath, error) {
			return []mlwh.IRODSPath{{Collection: "/a", DataObject: "a.bam", IRODSPath: "/a/a.bam"}}, nil
		}
		result, err = DiffSampleFiles(ctx, provider, store, "SANG1")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Removed, convey.ShouldResemble, []string{"/b/b.bam"})
	})

	convey.Convey("D4/C5: DiffSampleFiles uses cache-backed IRODSPathsForSample", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			IRODSPathsForSampleFunc: func(_ context.Context, sangerName string, limit, offset int) ([]mlwh.IRODSPath, error) {
				convey.So(sangerName, convey.ShouldEqual, "7607STDY14643771")
				convey.So(limit, convey.ShouldEqual, providerFetchLimit)
				convey.So(offset, convey.ShouldEqual, 0)

				return []mlwh.IRODSPath{{IDProduct: "id-product-1", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}}, nil
			},
		}

		result, err := DiffSampleFiles(ctx, provider, store, "7607STDY14643771")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []mlwh.IRODSPath{{IDProduct: "id-product-1", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}})
	})

	convey.Convey("D4/C6: DiffSampleFiles keys removals by id_product", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		provider := &MockProvider{
			IRODSPathsForSampleFunc: func(_ context.Context, _ string, _ int, _ int) ([]mlwh.IRODSPath, error) {
				return []mlwh.IRODSPath{{IDProduct: "old-product", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}}, nil
			},
		}

		_, err = DiffSampleFiles(ctx, provider, store, "7607STDY14643771")
		convey.So(err, convey.ShouldBeNil)

		provider.IRODSPathsForSampleFunc = func(_ context.Context, _ string, _ int, _ int) ([]mlwh.IRODSPath, error) {
			return []mlwh.IRODSPath{{IDProduct: "new-product", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}}, nil
		}

		result, err := DiffSampleFiles(ctx, provider, store, "7607STDY14643771")
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []mlwh.IRODSPath{{IDProduct: "new-product", Collection: "/a", DataObject: "a.cram", IRODSPath: "/a/a.cram"}})
		convey.So(result.Modified, convey.ShouldBeEmpty)
		convey.So(result.Removed, convey.ShouldResemble, []string{"old-product"})
	})
}

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

	convey.Convey("Diff returns an error instead of panicking when hashing fails", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		_, err = Diff(store, "bad", []diffBadItem{{ID: "a", Invalid: func() {}}}, func(item diffBadItem) string {
			return item.ID
		})

		convey.So(err, convey.ShouldNotBeNil)
		convey.So(err.Error(), convey.ShouldContainSubstring, "marshal hash item")
	})

	convey.Convey("Diff preserves deterministic first-seen order for added and modified items", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		current := []diffTestItem{{ID: "b", Value: "1"}, {ID: "a", Value: "2"}, {ID: "a", Value: "3"}}
		result, err := Diff(store, "ordered", current, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, current)

		updated := []diffTestItem{{ID: "b", Value: "4"}, {ID: "a", Value: "5"}, {ID: "a", Value: "6"}}
		result, err = Diff(store, "ordered", updated, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Modified, convey.ShouldResemble, updated)
	})

	convey.Convey("PreparedDiff can roll back a committed snapshot after output failure", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		_, err = Diff(store, "rollback", []diffTestItem{{ID: "a", Value: "1"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)

		prepared, err := PrepareDiff(store, "rollback", []diffTestItem{{ID: "a", Value: "2"}, {ID: "b", Value: "3"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(prepared.Commit(), convey.ShouldBeNil)
		convey.So(prepared.Rollback(), convey.ShouldBeNil)

		entries, err := store.LoadEntries("rollback")
		convey.So(err, convey.ShouldBeNil)
		convey.So(entries, convey.ShouldHaveLength, 1)
		convey.So(entries["a"].Tombstone, convey.ShouldBeFalse)
		convey.So(entries["a"].EntryHash, convey.ShouldNotBeBlank)
		convey.So(entries, convey.ShouldNotContainKey, "b")

		result, err := Diff(store, "rollback", []diffTestItem{{ID: "a", Value: "2"}, {ID: "b", Value: "3"}}, idFunc)
		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Added, convey.ShouldResemble, []diffTestItem{{ID: "b", Value: "3"}})
		convey.So(result.Modified, convey.ShouldResemble, []diffTestItem{{ID: "a", Value: "2"}})
	})
}

type diffBadItem struct {
	ID      string      `json:"id"`
	Invalid interface{} `json:"invalid"`
}
