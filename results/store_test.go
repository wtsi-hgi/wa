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
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

func TestNewStore(t *testing.T) {
	convey.Convey("C1.1: Given an in-memory SQLite DB, when NewStore is called, then it returns a non-nil store and creates the schema", t, func() {
		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)

		store, err := NewStore(db)
		convey.Reset(func() {
			if store != nil {
				_ = store.Close()
			}
		})

		convey.So(err, convey.ShouldBeNil)
		convey.So(store, convey.ShouldNotBeNil)
		convey.So(store.db, convey.ShouldEqual, db)

		for _, tableName := range []string{"result_sets", "result_files", "result_metadata"} {
			convey.So(sqliteTableExists(db, tableName), convey.ShouldBeTrue)
		}
	})

	convey.Convey("C1.2: Given a valid store, when Close is called, then no error is returned", t, func() {
		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)

		store, err := NewStore(db)
		convey.So(err, convey.ShouldBeNil)

		convey.So(store.Close(), convey.ShouldBeNil)
	})

	convey.Convey("C1.3: Given NewStore called twice on the same DB, then the second call succeeds", t, func() {
		db, err := sql.Open("sqlite", ":memory:")
		convey.So(err, convey.ShouldBeNil)

		firstStore, err := NewStore(db)
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			if firstStore != nil {
				_ = firstStore.Close()
			}
		})

		secondStore, err := NewStore(db)
		convey.So(err, convey.ShouldBeNil)
		convey.So(secondStore, convey.ShouldNotBeNil)
		convey.So(secondStore.db, convey.ShouldEqual, db)
	})
}

func sqliteTableExists(db *sql.DB, tableName string) bool {
	var existingName string

	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
		tableName,
	).Scan(&existingName)

	return err == nil && existingName == tableName
}

func TestStoreUpsert(t *testing.T) {
	convey.Convey("C2.1: Given an empty store and a valid registration, when Upsert is called, then it stores and returns the result set metadata", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()

		before := time.Now().Add(-time.Second)
		result, err := store.Upsert(ctx, reg)
		after := time.Now().Add(time.Second)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(result.ID, convey.ShouldEqual, CompositeKeyID("pipe", "run"))
		convey.So(result.Requester, convey.ShouldEqual, "alice")
		convey.So(result.Metadata, convey.ShouldResemble, map[string]string{"library": "exon"})
		convey.So(result.CreatedAt.After(before) || result.CreatedAt.Equal(before), convey.ShouldBeTrue)
		convey.So(result.CreatedAt.Before(after) || result.CreatedAt.Equal(after), convey.ShouldBeTrue)
	})

	convey.Convey("C2.2: Given an existing result set, when Upsert is called with the same key, then created_at is preserved and updated_at advances", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()

		initial, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		preservedCreatedAt := time.Date(2026, time.April, 1, 10, 11, 12, 0, time.UTC)
		_, err = store.db.Exec(
			`UPDATE result_sets SET created_at = ?, updated_at = ? WHERE id = ?`,
			preservedCreatedAt.Format(time.RFC3339Nano),
			preservedCreatedAt.Format(time.RFC3339Nano),
			initial.ID,
		)
		convey.So(err, convey.ShouldBeNil)

		updatedReg := testRegistration()
		updatedReg.Requester = "charlie"

		result, err := store.Upsert(ctx, updatedReg)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result.Requester, convey.ShouldEqual, "charlie")
		convey.So(result.CreatedAt, convey.ShouldEqual, preservedCreatedAt)
		convey.So(result.UpdatedAt.After(preservedCreatedAt), convey.ShouldBeTrue)
	})

	convey.Convey("C2.3: Given an upserted result set, when Upsert is called again with fewer files, then the old files are replaced", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()
		reg.Files = []FileEntry{
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 9, 0, 0, 0, time.UTC), Size: 10, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 9, 1, 0, 0, time.UTC), Size: 20, Kind: "output"},
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 9, 2, 0, 0, time.UTC), Size: 30, Kind: "input"},
		}

		result, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		reg.Files = []FileEntry{
			{Path: "/tmp/results/run/out-replacement.txt", Mtime: time.Date(2026, time.April, 2, 9, 0, 0, 0, time.UTC), Size: 40, Kind: "output"},
		}

		_, err = store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		files := resultFilesForTest(t, store.db, result.ID)
		convey.So(files, convey.ShouldHaveLength, 1)
		convey.So(files[0].Path, convey.ShouldEqual, "/tmp/results/run/out-replacement.txt")
	})

	convey.Convey("C2.4: Given an empty pipeline identifier, when Upsert is called, then it returns an invalid input error", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.PipelineIdentifier = ""

		result, err := store.Upsert(context.Background(), reg)

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
	})

	convey.Convey("C2.5: Given an empty run key, when Upsert is called, then it returns an invalid input error", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.RunKey = ""

		result, err := store.Upsert(context.Background(), reg)

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
	})
}

func TestStoreSearch(t *testing.T) {
	convey.Convey("Search hydrates metadata reliably with in-memory SQLite while iterating result rows", t, func() {
		store := newSQLiteStoreForTest(t)
		store.db.SetMaxOpenConns(2)
		store.db.SetMaxIdleConns(2)

		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-search-1", func(reg *Registration) {
			reg.Metadata = map[string]string{"library": "exon", "study": "alpha"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-search-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"library": "intron", "study": "beta"}
		}))

		results, err := store.Search(ctx, SearchParams{})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(results[0].Metadata, convey.ShouldNotBeNil)
		convey.So(results[1].Metadata, convey.ShouldNotBeNil)
		convey.So(results[0].Metadata["library"], convey.ShouldNotBeBlank)
		convey.So(results[1].Metadata["library"], convey.ShouldNotBeBlank)
	})

	convey.Convey("C4.1: Given 3 result sets with requesters alice, alice, bob, when Search is filtered by requester alice, then it returns 2 result sets", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice-1", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-alice-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-bob-1", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Requester = "bob"
		}))

		results, err := store.Search(ctx, SearchParams{Requester: "alice"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 2)
		convey.So(resultRequestersForTest(results), convey.ShouldResemble, []string{"alice", "alice"})
	})

	convey.Convey("C4.2: Given a result set with metadata library=exon and another with library=intron, when Search is filtered by metadata, then it returns 1 exact match", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-exon", func(reg *Registration) {
			reg.Metadata = map[string]string{"library": "exon", "study": "alpha"}
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-intron", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Metadata = map[string]string{"library": "intron", "study": "alpha"}
		}))

		results, err := store.Search(ctx, SearchParams{Meta: map[string]string{"library": "exon"}})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].Metadata, convey.ShouldResemble, map[string]string{"library": "exon", "study": "alpha"})
	})

	convey.Convey("C4.3: Given result sets with output directories /a/b/c and /a/d/e, when Search is filtered by output directory prefix /a/b, then it returns 1 result set", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-a", func(reg *Registration) {
			reg.OutputDirectory = "/a/b/c"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-b", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.OutputDirectory = "/a/d/e"
		}))

		results, err := store.Search(ctx, SearchParams{OutputDirPrefix: "/a/b"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].OutputDirectory, convey.ShouldEqual, "/a/b/c")
	})

	convey.Convey("Search escapes wildcard characters in output directory prefixes", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-percent", func(reg *Registration) {
			reg.OutputDirectory = "/a/100%/run"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-literal", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.OutputDirectory = "/a/100x/run"
		}))

		results, err := store.Search(ctx, SearchParams{OutputDirPrefix: "/a/100%"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].OutputDirectory, convey.ShouldEqual, "/a/100%/run")
	})

	convey.Convey("C4.4: Given empty SearchParams, when Search is called, then it returns all stored result sets", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-1", func(reg *Registration) {}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-2", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-3", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
		}))

		results, err := store.Search(ctx, SearchParams{})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 3)
	})

	convey.Convey("C4.5: Given SearchParams with Requester alice and PipelineName nf, when Search is called, then only result sets matching both filters are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-match", func(reg *Registration) {
			reg.Requester = "alice"
			reg.PipelineName = "nf"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-requester-only", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-2"
			reg.Requester = "alice"
			reg.PipelineName = "other"
		}))
		seedResultSetForTest(t, store, searchRegistrationForTest("run-pipeline-only", func(reg *Registration) {
			reg.PipelineIdentifier = "pipe-3"
			reg.Requester = "bob"
			reg.PipelineName = "nf"
		}))

		results, err := store.Search(ctx, SearchParams{Requester: "alice", PipelineName: "nf"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldHaveLength, 1)
		convey.So(results[0].RunKey, convey.ShouldEqual, "run-match")
	})

	convey.Convey("C4.6: Given no matching results, when searched, then Search returns an empty non-nil slice with no error", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()

		seedResultSetForTest(t, store, searchRegistrationForTest("run-1", func(reg *Registration) {}))

		results, err := store.Search(ctx, SearchParams{Requester: "nobody"})

		convey.So(err, convey.ShouldBeNil)
		convey.So(results, convey.ShouldNotBeNil)
		convey.So(results, convey.ShouldHaveLength, 0)
	})

	convey.Convey("Upsert rejects registrations missing required requester metadata", t, func() {
		store := newSQLiteStoreForTest(t)

		reg := testRegistration()
		reg.Requester = ""

		result, err := store.Upsert(context.Background(), reg)

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "requester is required")
	})
}

func TestStoreGet(t *testing.T) {
	convey.Convey("C3.1: Given a stored result set with metadata, when Get is called, then metadata and scalar fields are returned", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()
		reg.Metadata = map[string]string{"k": "v", "library": "exon"}

		stored, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		result, err := store.Get(ctx, stored.ID)

		convey.So(err, convey.ShouldBeNil)
		convey.So(result, convey.ShouldNotBeNil)
		convey.So(result.ID, convey.ShouldEqual, stored.ID)
		convey.So(result.PipelineIdentifier, convey.ShouldEqual, reg.PipelineIdentifier)
		convey.So(result.RunKey, convey.ShouldEqual, reg.RunKey)
		convey.So(result.Requester, convey.ShouldEqual, reg.Requester)
		convey.So(result.Operator, convey.ShouldEqual, reg.Operator)
		convey.So(result.Command, convey.ShouldEqual, reg.Command)
		convey.So(result.PipelineName, convey.ShouldEqual, reg.PipelineName)
		convey.So(result.PipelineVersion, convey.ShouldEqual, reg.PipelineVersion)
		convey.So(result.OutputDirectory, convey.ShouldEqual, reg.OutputDirectory)
		convey.So(result.Metadata, convey.ShouldResemble, reg.Metadata)
		convey.So(result.CreatedAt, convey.ShouldEqual, stored.CreatedAt)
		convey.So(result.UpdatedAt, convey.ShouldEqual, stored.UpdatedAt)
	})

	convey.Convey("C3.2: Given a non-existent ID, when Get is called, then it wraps ErrNotFound", t, func() {
		store := newSQLiteStoreForTest(t)

		result, err := store.Get(context.Background(), "missing")

		convey.So(result, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
	})
}

func TestStoreGetFiles(t *testing.T) {
	convey.Convey("C5.1: Given a result set with 2 output files and 1 input file, when GetFiles is called, then it returns all tracked files with correct paths, sizes, and kinds", t, func() {
		store := newSQLiteStoreForTest(t)
		reg := testRegistration()
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "output"},
		}

		result, err := store.Upsert(context.Background(), reg)
		convey.So(err, convey.ShouldBeNil)

		files, err := store.GetFiles(context.Background(), result.ID)

		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "output"},
		})
	})

	convey.Convey("C5.2: Given a non-existent result ID, when called, then GetFiles returns an empty slice with no error", t, func() {
		store := newSQLiteStoreForTest(t)

		files, err := store.GetFiles(context.Background(), "missing-id")

		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{})
	})
}

func TestStoreReplaceOutputFiles(t *testing.T) {
	convey.Convey("C6.1: Given a result set with 3 output files and 1 input file, when ReplaceOutputFiles is called, then GetFiles returns 3 total with the original input preserved", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 3, 0, 0, time.UTC), Size: 404, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "output"},
			{Path: "/tmp/results/run/out-3.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "output"},
		}

		result, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		err = store.ReplaceOutputFiles(ctx, result.ID, []FileEntry{
			{Path: "/tmp/results/run/out-new-1.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 505, Kind: "output"},
			{Path: "/tmp/results/run/out-new-2.txt", Mtime: time.Date(2026, time.April, 2, 12, 1, 0, 0, time.UTC), Size: 606, Kind: "output"},
		})
		convey.So(err, convey.ShouldBeNil)

		files, err := store.GetFiles(ctx, result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 3, 0, 0, time.UTC), Size: 404, Kind: "input"},
			{Path: "/tmp/results/run/out-new-1.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 505, Kind: "output"},
			{Path: "/tmp/results/run/out-new-2.txt", Mtime: time.Date(2026, time.April, 2, 12, 1, 0, 0, time.UTC), Size: 606, Kind: "output"},
		})
	})

	convey.Convey("C6.2: Given ReplaceOutputFiles with an empty slice, then all output files are removed but input files remain", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()
		reg.Files = []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "input"},
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/results/run/out-2.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "output"},
		}

		result, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		err = store.ReplaceOutputFiles(ctx, result.ID, []FileEntry{})
		convey.So(err, convey.ShouldBeNil)

		files, err := store.GetFiles(ctx, result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 2, 0, 0, time.UTC), Size: 303, Kind: "input"},
		})
	})

	convey.Convey("C6.3: Given a non-existent result ID, when ReplaceOutputFiles is called, then error wraps ErrNotFound", t, func() {
		store := newSQLiteStoreForTest(t)

		err := store.ReplaceOutputFiles(context.Background(), "missing-id", []FileEntry{
			{Path: "/tmp/results/run/out-new-1.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 505, Kind: "output"},
		})

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
	})

	convey.Convey("ReplaceOutputFiles rejects files outside the stored output directory", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()

		result, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		err = store.ReplaceOutputFiles(ctx, result.ID, []FileEntry{
			{Path: "/other-tree/out.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 505, Kind: "output"},
		})

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "outside output directory")
	})

	convey.Convey("ReplaceOutputFiles rejects duplicate output paths", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()

		result, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		err = store.ReplaceOutputFiles(ctx, result.ID, []FileEntry{
			{Path: "/tmp/results/run/out-dup.txt", Mtime: time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC), Size: 505, Kind: "output"},
			{Path: "/tmp/results/run/out-dup.txt", Mtime: time.Date(2026, time.April, 2, 12, 1, 0, 0, time.UTC), Size: 606, Kind: "output"},
		})

		convey.So(errors.Is(err, ErrInvalidInput), convey.ShouldBeTrue)
		convey.So(err.Error(), convey.ShouldContainSubstring, "duplicate file path")
	})
}

func TestStoreDelete(t *testing.T) {
	convey.Convey("C7.1: Given a stored result set with files and metadata, when Delete is called, then Get returns ErrNotFound, GetFiles returns empty, and associated rows are cascaded", t, func() {
		store := newSQLiteStoreForTest(t)
		ctx := context.Background()
		reg := testRegistration()
		reg.Metadata = map[string]string{"library": "exon", "study": "alpha"}

		result, err := store.Upsert(ctx, reg)
		convey.So(err, convey.ShouldBeNil)

		err = store.Delete(ctx, result.ID)
		convey.So(err, convey.ShouldBeNil)

		deleted, err := store.Get(ctx, result.ID)
		convey.So(deleted, convey.ShouldBeNil)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)

		files, err := store.GetFiles(ctx, result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{})

		convey.So(countRowsForTest(t, store.db, "result_metadata", result.ID), convey.ShouldEqual, 0)
		convey.So(countRowsForTest(t, store.db, "result_files", result.ID), convey.ShouldEqual, 0)
	})

	convey.Convey("C7.2: Given a non-existent ID, when Delete is called, then error wraps ErrNotFound", t, func() {
		store := newSQLiteStoreForTest(t)

		err := store.Delete(context.Background(), "missing-id")

		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
	})

	convey.Convey("Delete cascades correctly even when SQLite uses a different pooled connection", t, func() {
		dbPath := filepath.Join(t.TempDir(), "results.db")
		db, err := sql.Open("sqlite", dbPath)
		convey.So(err, convey.ShouldBeNil)
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(2)

		store, err := NewStore(db)
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() {
			_ = store.Close()
		})

		ctx := context.Background()
		result, err := store.Upsert(ctx, testRegistration())
		convey.So(err, convey.ShouldBeNil)

		conn, err := db.Conn(ctx)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = conn.Close() }()

		err = store.Delete(ctx, result.ID)
		convey.So(err, convey.ShouldBeNil)
		convey.So(countRowsForTest(t, db, "result_metadata", result.ID), convey.ShouldEqual, 0)
		convey.So(countRowsForTest(t, db, "result_files", result.ID), convey.ShouldEqual, 0)
	})
}

func newSQLiteStoreForTest(t *testing.T) *Store {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	store, err := NewStore(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create store: %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
	})

	return store
}

func seedResultSetForTest(t *testing.T, store *Store, reg *Registration) ResultSet {
	t.Helper()

	result, err := store.Upsert(context.Background(), reg)
	if err != nil {
		t.Fatalf("seed result set: %v", err)
	}

	return *result
}

func searchRegistrationForTest(runKey string, mutate func(*Registration)) *Registration {
	reg := testRegistration()
	reg.RunKey = runKey
	defaultOutputDirectory := reg.OutputDirectory

	if mutate != nil {
		mutate(reg)
	}

	if reg.OutputDirectory != defaultOutputDirectory {
		for i := range reg.Files {
			if reg.Files[i].Kind != "output" || !pathWithinDirectory(defaultOutputDirectory, reg.Files[i].Path) {
				continue
			}

			reg.Files[i].Path = filepath.Join(reg.OutputDirectory, filepath.Base(reg.Files[i].Path))
		}
	}

	return reg
}

func testRegistration() *Registration {
	return &Registration{
		PipelineIdentifier: "pipe",
		RunKey:             "run",
		Requester:          "alice",
		Operator:           "bob",
		Command:            "nextflow run pipe",
		PipelineName:       "nf-pipe",
		PipelineVersion:    "1.2.3",
		OutputDirectory:    "/tmp/results/run",
		Files: []FileEntry{
			{Path: "/tmp/results/run/out-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC), Size: 101, Kind: "output"},
			{Path: "/tmp/input-1.txt", Mtime: time.Date(2026, time.April, 1, 12, 1, 0, 0, time.UTC), Size: 202, Kind: "input"},
		},
		Metadata: map[string]string{"library": "exon"},
	}
}

func countRowsForTest(t *testing.T, db *sql.DB, tableName string, resultID string) int {
	t.Helper()

	var count int

	err := db.QueryRow(
		`SELECT COUNT(*) FROM `+tableName+` WHERE result_id = ?`,
		resultID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count %s rows: %v", tableName, err)
	}

	return count
}

func resultRequestersForTest(results []ResultSet) []string {
	requesters := make([]string, len(results))

	for i, result := range results {
		requesters[i] = result.Requester
	}

	return requesters
}

func resultFilesForTest(t *testing.T, db *sql.DB, resultID string) []FileEntry {
	t.Helper()

	rows, err := db.Query(
		`SELECT path, mtime, size, kind FROM result_files WHERE result_id = ? ORDER BY path`,
		resultID,
	)
	if err != nil {
		t.Fatalf("query result files: %v", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var files []FileEntry

	for rows.Next() {
		var file FileEntry
		var mtime string

		if err := rows.Scan(&file.Path, &mtime, &file.Size, &file.Kind); err != nil {
			t.Fatalf("scan result file: %v", err)
		}

		file.Mtime, err = time.Parse(time.RFC3339Nano, mtime)
		if err != nil {
			t.Fatalf("parse file mtime: %v", err)
		}

		files = append(files, file)
	}

	if err := rows.Err(); err != nil {
		t.Fatalf("iterate result files: %v", err)
	}

	return files
}
