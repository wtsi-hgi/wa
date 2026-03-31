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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestOpenStore(t *testing.T) {
	convey.Convey("Given path :memory:", t, func() {
		store, err := OpenStore(":memory:")
		convey.Reset(func() { _ = store.Close() })

		convey.Convey("when OpenStore is called, then the store opens", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(store, convey.ShouldNotBeNil)
		})
	})

	convey.Convey("Given a file-backed database path", t, func() {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.db")

		store, err := OpenStore(path)
		convey.Reset(func() { _ = store.Close() })

		convey.Convey("when OpenStore is called, then the file is created", func() {
			convey.So(err, convey.ShouldBeNil)
			_, statErr := os.Stat(path)
			convey.So(statErr, convey.ShouldBeNil)
		})
	})

	convey.Convey("Given an open store", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("when Close is called, then no error is returned", func() {
			convey.So(store.Close(), convey.ShouldBeNil)
		})

		convey.Convey("when Close is called twice, then there is no panic", func() {
			convey.So(store.Close(), convey.ShouldBeNil)
			convey.So(store.Close(), convey.ShouldBeNil)
		})
	})

	convey.Convey("Given an invalid database path", t, func() {
		store, err := OpenStore("/proc/nonexistent/db")

		convey.Convey("when OpenStore is called, then the error wraps ErrStoreOpen", func() {
			convey.So(store, convey.ShouldBeNil)
			convey.So(errors.Is(err, ErrStoreOpen), convey.ShouldBeTrue)
		})
	})
}

func TestStoreLoadAndSaveEntries(t *testing.T) {
	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)

	convey.Convey("Given an empty store", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		entries, err := store.LoadEntries("q1")

		convey.Convey("when LoadEntries is called, then an empty non-nil map is returned", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(entries, convey.ShouldNotBeNil)
			convey.So(entries, convey.ShouldBeEmpty)
		})
	})

	convey.Convey("Given entries saved to the store", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.Convey("when a non-tombstoned entry is saved and loaded, then it round-trips", func() {
			err = store.SaveEntries("q1", map[string]StoredEntry{
				"e1": {EntryHash: "abc", UpdatedAt: now},
			})
			convey.So(err, convey.ShouldBeNil)

			entries, err := store.LoadEntries("q1")

			convey.So(err, convey.ShouldBeNil)
			convey.So(entries["e1"].EntryHash, convey.ShouldEqual, "abc")
			convey.So(entries["e1"].Tombstone, convey.ShouldBeFalse)
		})

		convey.Convey("when an existing entry is saved again, then it is updated", func() {
			convey.So(store.SaveEntries("q1", map[string]StoredEntry{
				"e1": {EntryHash: "abc", UpdatedAt: now},
			}), convey.ShouldBeNil)
			convey.So(store.SaveEntries("q1", map[string]StoredEntry{
				"e1": {EntryHash: "def", UpdatedAt: now.Add(time.Minute)},
			}), convey.ShouldBeNil)

			entries, err := store.LoadEntries("q1")

			convey.So(err, convey.ShouldBeNil)
			convey.So(entries["e1"].EntryHash, convey.ShouldEqual, "def")
		})

		convey.Convey("when entries exist under two keys, then LoadEntries isolates by key", func() {
			convey.So(store.SaveEntries("q1", map[string]StoredEntry{
				"e1": {EntryHash: "abc", UpdatedAt: now},
			}), convey.ShouldBeNil)
			convey.So(store.SaveEntries("q2", map[string]StoredEntry{
				"e2": {EntryHash: "xyz", UpdatedAt: now},
			}), convey.ShouldBeNil)

			entries, err := store.LoadEntries("q1")

			convey.So(err, convey.ShouldBeNil)
			convey.So(entries, convey.ShouldHaveLength, 1)
			convey.So(entries, convey.ShouldContainKey, "e1")
		})

		convey.Convey("when a tombstone is saved, then it is loaded as tombstoned", func() {
			convey.So(store.SaveEntries("q1", map[string]StoredEntry{
				"e1": {EntryHash: "abc", Tombstone: true, UpdatedAt: now},
			}), convey.ShouldBeNil)

			entries, err := store.LoadEntries("q1")

			convey.So(err, convey.ShouldBeNil)
			convey.So(entries["e1"].Tombstone, convey.ShouldBeTrue)
		})

		convey.Convey("when later saves omit an existing entry, then the omitted entry persists", func() {
			convey.So(store.SaveEntries("q1", map[string]StoredEntry{
				"e1": {EntryHash: "abc", UpdatedAt: now},
			}), convey.ShouldBeNil)
			convey.So(store.SaveEntries("q1", map[string]StoredEntry{
				"e2": {EntryHash: "def", UpdatedAt: now.Add(time.Minute)},
			}), convey.ShouldBeNil)

			entries, err := store.LoadEntries("q1")

			convey.So(err, convey.ShouldBeNil)
			convey.So(entries, convey.ShouldHaveLength, 2)
			convey.So(entries["e1"].EntryHash, convey.ShouldEqual, "abc")
			convey.So(entries["e2"].EntryHash, convey.ShouldEqual, "def")
		})
	})
}
