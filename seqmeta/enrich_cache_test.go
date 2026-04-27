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
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestStoreEnrichCache(t *testing.T) {
	now := time.Date(2026, time.April, 24, 11, 30, 0, 0, time.UTC)

	convey.Convey("Given a fresh in-memory store", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		entry, err := store.LoadEnrichCache("x")

		convey.Convey("when LoadEnrichCache is called, then it returns no rows", func() {
			convey.So(entry, convey.ShouldBeNil)
			convey.So(errors.Is(err, sql.ErrNoRows), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a saved enrich cache entry", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		entry := enrichCacheEntry{
			Identifier: "x",
			Type:       IdentifierStudyID,
			Body:       []byte("{}"),
			FetchedAt:  now,
			TTL:        time.Hour,
		}
		convey.So(store.SaveEnrichCache(entry), convey.ShouldBeNil)

		convey.Convey("when LoadEnrichCache is called, then the saved values round-trip", func() {
			loaded, err := store.LoadEnrichCache("x")

			convey.So(err, convey.ShouldBeNil)
			convey.So(loaded, convey.ShouldNotBeNil)
			if loaded == nil {
				return
			}
			convey.So(loaded.Type, convey.ShouldEqual, IdentifierStudyID)
			convey.So(loaded.Body, convey.ShouldResemble, []byte("{}"))
			convey.So(loaded.TTL, convey.ShouldEqual, time.Hour)
			convey.So(loaded.Negative, convey.ShouldBeFalse)
		})

		convey.Convey("when DeleteEnrichCache is called, then the entry is removed", func() {
			convey.So(store.DeleteEnrichCache("x"), convey.ShouldBeNil)

			loaded, err := store.LoadEnrichCache("x")

			convey.So(loaded, convey.ShouldBeNil)
			convey.So(errors.Is(err, sql.ErrNoRows), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given enrich cache entries for two identifiers", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "abc",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"abc"}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "def",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"def"}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		convey.Convey("when one entry is deleted, then the other remains", func() {
			convey.So(store.DeleteEnrichCache("abc"), convey.ShouldBeNil)

			loaded, err := store.LoadEnrichCache("def")

			convey.So(err, convey.ShouldBeNil)
			convey.So(loaded, convey.ShouldNotBeNil)
			if loaded == nil {
				return
			}
			convey.So(loaded.Identifier, convey.ShouldEqual, "def")
		})
	})

	convey.Convey("Given a negative enrich cache entry", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "missing",
			Type:       "",
			Body:       []byte(`{"error":"missing"}`),
			FetchedAt:  now,
			TTL:        time.Minute,
			Negative:   true,
		}), convey.ShouldBeNil)

		convey.Convey("when loaded, then the negative cache flags round-trip", func() {
			loaded, err := store.LoadEnrichCache("missing")

			convey.So(err, convey.ShouldBeNil)
			convey.So(loaded, convey.ShouldNotBeNil)
			if loaded == nil {
				return
			}
			convey.So(loaded.Negative, convey.ShouldBeTrue)
			convey.So(loaded.Type, convey.ShouldEqual, IdentifierType(""))
		})
	})

}

func TestStoreInvalidateEnrichFor(t *testing.T) {
	now := time.Date(2026, time.April, 24, 11, 30, 0, 0, time.UTC)

	convey.Convey("Given a cached study enrichment identified by study ID", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "6568",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"6568","type":"study_id"}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		convey.Convey("when invalidating study_samples for that study, then the direct study entry is removed", func() {
			convey.So(store.InvalidateEnrichFor("study_samples", "6568"), convey.ShouldBeNil)

			loaded, err := store.LoadEnrichCache("6568")

			convey.So(loaded, convey.ShouldBeNil)
			convey.So(errors.Is(err, sql.ErrNoRows), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a cached sample enrichment whose body references a study", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "SANG1",
			Type:       IdentifierSangerSampleID,
			Body:       []byte(`{"identifier":"SANG1","graph":{"sample":{"sanger_sample_id":"SANG1","id_study_lims":"6568"}}}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		convey.Convey("when invalidating study_samples for that study, then the dependent sample entry is removed", func() {
			convey.So(store.InvalidateEnrichFor("study_samples", "6568"), convey.ShouldBeNil)

			loaded, err := store.LoadEnrichCache("SANG1")

			convey.So(loaded, convey.ShouldBeNil)
			convey.So(errors.Is(err, sql.ErrNoRows), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a cached sample enrichment for a sample_files query", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "SANG1",
			Type:       IdentifierSangerSampleID,
			Body:       []byte(`{"identifier":"SANG1"}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		convey.Convey("when invalidating sample_files for that identifier, then the direct entry is removed", func() {
			convey.So(store.InvalidateEnrichFor("sample_files", "SANG1"), convey.ShouldBeNil)

			loaded, err := store.LoadEnrichCache("SANG1")

			convey.So(loaded, convey.ShouldBeNil)
			convey.So(errors.Is(err, sql.ErrNoRows), convey.ShouldBeTrue)
		})
	})

	convey.Convey("Given a cached enrich entry for another identifier", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "OTHER",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"OTHER"}`),
			FetchedAt:  now,
			TTL:        time.Hour,
		}), convey.ShouldBeNil)

		convey.Convey("when invalidating sample_files for a different identifier, then the other entry remains", func() {
			convey.So(store.InvalidateEnrichFor("sample_files", "SANG1"), convey.ShouldBeNil)

			loaded, err := store.LoadEnrichCache("OTHER")

			convey.So(err, convey.ShouldBeNil)
			convey.So(loaded, convey.ShouldNotBeNil)
			if loaded == nil {
				return
			}
			convey.So(loaded.Identifier, convey.ShouldEqual, "OTHER")
		})
	})
}

func TestStoreEnrichCacheExpiry(t *testing.T) {
	now := time.Date(2026, time.April, 24, 11, 30, 0, 0, time.UTC)

	convey.Convey("Given an expired enrich cache entry", t, func() {
		store, err := OpenStore(":memory:")
		convey.So(err, convey.ShouldBeNil)
		convey.Reset(func() { _ = store.Close() })

		fetchedAt := now.Add(-time.Hour)
		convey.So(store.SaveEnrichCache(enrichCacheEntry{
			Identifier: "expired",
			Type:       IdentifierStudyID,
			Body:       []byte(`{"identifier":"expired"}`),
			FetchedAt:  fetchedAt,
			TTL:        10 * time.Millisecond,
		}), convey.ShouldBeNil)

		convey.Convey("when LoadEnrichCache is called, then the raw expired row still round-trips", func() {
			loaded, err := store.LoadEnrichCache("expired")

			convey.So(err, convey.ShouldBeNil)
			convey.So(loaded, convey.ShouldNotBeNil)
			if loaded == nil {
				return
			}
			convey.So(loaded.Identifier, convey.ShouldEqual, "expired")
			convey.So(loaded.FetchedAt, convey.ShouldEqual, fetchedAt)
			convey.So(loaded.TTL, convey.ShouldEqual, 10*time.Millisecond)
			convey.So(loaded.FetchedAt.Add(loaded.TTL).Before(time.Now()), convey.ShouldBeTrue)
		})
	})
}
