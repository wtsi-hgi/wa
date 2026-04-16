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
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	_ "modernc.org/sqlite"
)

func TestStoreGetFilesVerify(t *testing.T) {
	convey.Convey("C5.1 verify: GetFiles returns all tracked files with correct paths, sizes, and kinds", t, func() {
		store := newSQLiteStoreForGetFilesVerify(t)
		reg := registrationForGetFilesVerify()
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

	convey.Convey("C5.2 verify: GetFiles returns an empty slice with no error for a missing result ID", t, func() {
		store := newSQLiteStoreForGetFilesVerify(t)

		files, err := store.GetFiles(context.Background(), "missing-id")

		convey.So(err, convey.ShouldBeNil)
		convey.So(files, convey.ShouldResemble, []FileEntry{})
	})
}

func newSQLiteStoreForGetFilesVerify(t *testing.T) *Store {
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

func registrationForGetFilesVerify() *Registration {
	return &Registration{
		PipelineIdentifier: "pipe",
		RunKey:             "run",
		Requester:          "alice",
		Operator:           "bob",
		Command:            "nextflow run pipe",
		PipelineName:       "nf-pipe",
		PipelineVersion:    "1.2.3",
		OutputDirectory:    "/tmp/results/run",
		Metadata:           map[string]string{"library": "exon"},
	}
}
