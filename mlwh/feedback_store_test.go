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

package mlwh

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestFeedbackStoreA2(t *testing.T) {
	ctx := context.Background()
	t1 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	convey.Convey("A2.1: Given a new path, when opened, then the file exists and List is empty", t, func() {
		path := filepath.Join(t.TempDir(), "feedback.db")
		store := openTestFeedbackStore(t, path)

		_, err := os.Stat(path)
		convey.So(err, convey.ShouldBeNil)

		page, err := store.List(ctx, FeedbackFilter{}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldNotBeNil)
		convey.So(page, convey.ShouldResemble, Page[FeedbackReport]{Items: []FeedbackReport{}, Total: 0, NextOffset: -1})

		raw, err := sql.Open("sqlite", path)
		convey.So(err, convey.ShouldBeNil)

		defer func() { _ = raw.Close() }()

		var journalMode string
		convey.So(raw.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode), convey.ShouldBeNil)
		convey.So(journalMode, convey.ShouldEqual, "wal")
	})

	convey.Convey("A2.2: Given now fixed, when Add is called with a full submission, then the report holds every field", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		store.now = func() time.Time { return t1.In(time.FixedZone("UTC+1", 3600)) }

		sub := FeedbackSubmission{
			Category:         FeedbackCategoryNoEndpoint,
			Description:      "No endpoint lists sample consent.",
			UserRequest:      "Which samples have withdrawn consent?",
			ToolsTried:       []string{"mlwh_search_samples", "mlwh_call_endpoint"},
			MCPServerVersion: "0.4.0",
			WAAPIVersion:     "1.9.0",
			Transport:        "stdio",
			ClientName:       "claude-code",
			ClientVersion:    "2.1.0",
			ClientUserAgent:  "agent/1.0",
		}

		want := FeedbackReport{
			ID:               1,
			CreatedAt:        "2026-10-01T12:00:00Z",
			Category:         sub.Category,
			Description:      sub.Description,
			UserRequest:      sub.UserRequest,
			ToolsTried:       sub.ToolsTried,
			MCPServerVersion: sub.MCPServerVersion,
			WAAPIVersion:     sub.WAAPIVersion,
			Transport:        sub.Transport,
			ClientName:       sub.ClientName,
			ClientVersion:    sub.ClientVersion,
			ClientUserAgent:  sub.ClientUserAgent,
			RemoteAddr:       "10.0.0.5",
		}

		report, err := store.Add(ctx, sub, "10.0.0.5")
		convey.So(err, convey.ShouldBeNil)
		convey.So(report, convey.ShouldResemble, want)

		page, err := store.List(ctx, FeedbackFilter{}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldResemble, []FeedbackReport{want})
	})

	convey.Convey("A2.3: Given nil ToolsTried, when added and listed, then ToolsTried is an empty slice marshalled as []", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))

		report, err := store.Add(ctx, FeedbackSubmission{Category: FeedbackCategoryOther, Description: "d"}, "")
		convey.So(err, convey.ShouldBeNil)
		convey.So(report.ToolsTried, convey.ShouldNotBeNil)
		convey.So(report.ToolsTried, convey.ShouldBeEmpty)

		page, err := store.List(ctx, FeedbackFilter{}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items, convey.ShouldHaveLength, 1)
		convey.So(page.Items[0].ToolsTried, convey.ShouldNotBeNil)
		convey.So(page.Items[0].ToolsTried, convey.ShouldBeEmpty)

		encoded, err := json.Marshal(page.Items[0])
		convey.So(err, convey.ShouldBeNil)
		convey.So(string(encoded), convey.ShouldContainSubstring, `"tools_tried":[]`)
	})

	convey.Convey("A2.4: Given 3 reports, when the store is closed and reopened, then List returns [3, 2, 1]", t, func() {
		path := filepath.Join(t.TempDir(), "feedback.db")
		store, err := OpenFeedbackStore(ctx, path)
		convey.So(err, convey.ShouldBeNil)
		t.Cleanup(func() { _ = store.Close() })

		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryOther, FeedbackCategoryOther)
		convey.So(store.Close(), convey.ShouldBeNil)

		reopened := openTestFeedbackStore(t, path)
		convey.So(listFeedbackIDs(ctx, reopened, FeedbackFilter{}), convey.ShouldResemble, []int64{3, 2, 1})
	})

	convey.Convey("A2.5: Given 5 reports, when paged by 2, then items, Total and NextOffset follow the offset", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryOther, FeedbackCategoryOther,
			FeedbackCategoryOther, FeedbackCategoryOther)

		page, err := store.List(ctx, FeedbackFilter{}, 2, 2)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reportIDs(page.Items), convey.ShouldResemble, []int64{3, 2})
		convey.So(page.Total, convey.ShouldEqual, 5)
		convey.So(page.NextOffset, convey.ShouldEqual, 4)

		page, err = store.List(ctx, FeedbackFilter{}, 2, 4)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reportIDs(page.Items), convey.ShouldResemble, []int64{1})
		convey.So(page.Total, convey.ShouldEqual, 5)
		convey.So(page.NextOffset, convey.ShouldEqual, -1)
	})

	convey.Convey("A2.6: Given id 1 acknowledged and id 2 not, when filtered by Acknowledged, then each state is selected", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryOther)

		_, err := store.SetAcknowledged(ctx, 1, true)
		convey.So(err, convey.ShouldBeNil)

		unacked, acked := false, true
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{Acknowledged: &unacked}), convey.ShouldResemble, []int64{2})
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{Acknowledged: &acked}), convey.ShouldResemble, []int64{1})
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{}), convey.ShouldResemble, []int64{2, 1})

		page, err := store.List(ctx, FeedbackFilter{Acknowledged: &acked}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Total, convey.ShouldEqual, 1)
	})

	convey.Convey("A2.7: Given categories other and no_endpoint, when filtered by no_endpoint, then only id 2 is counted", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryNoEndpoint)

		page, err := store.List(ctx, FeedbackFilter{Category: FeedbackCategoryNoEndpoint}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reportIDs(page.Items), convey.ShouldResemble, []int64{2})
		convey.So(page.Total, convey.ShouldEqual, 1)
		convey.So(page.NextOffset, convey.ShouldEqual, -1)
	})

	convey.Convey("A2.7: Given other acknowledged, no_endpoint unacknowledged and no_endpoint acknowledged, "+
		"when filtered by acknowledged no_endpoint, then only id 3 matches", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryNoEndpoint, FeedbackCategoryNoEndpoint)

		for _, id := range []int64{1, 3} {
			_, err := store.SetAcknowledged(ctx, id, true)
			convey.So(err, convey.ShouldBeNil)
		}

		acked := true
		page, err := store.List(ctx, FeedbackFilter{Acknowledged: &acked, Category: FeedbackCategoryNoEndpoint}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(reportIDs(page.Items), convey.ShouldResemble, []int64{3})
		convey.So(page.Total, convey.ShouldEqual, 1)
	})

	convey.Convey("A2.8: Given acknowledgement at T1, then re-acknowledging at T2 keeps T1 and un-acknowledging clears it", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther)

		store.now = func() time.Time { return t1 }
		report, err := store.SetAcknowledged(ctx, 1, true)
		convey.So(err, convey.ShouldBeNil)
		convey.So(report.ID, convey.ShouldEqual, 1)
		convey.So(report.Acknowledged, convey.ShouldBeTrue)
		convey.So(report.AcknowledgedAt, convey.ShouldEqual, "2026-10-01T12:00:00Z")

		store.now = func() time.Time { return t1.Add(time.Hour) }
		report, err = store.SetAcknowledged(ctx, 1, true)
		convey.So(err, convey.ShouldBeNil)
		convey.So(report.Acknowledged, convey.ShouldBeTrue)
		convey.So(report.AcknowledgedAt, convey.ShouldEqual, "2026-10-01T12:00:00Z")

		report, err = store.SetAcknowledged(ctx, 1, false)
		convey.So(err, convey.ShouldBeNil)
		convey.So(report.Acknowledged, convey.ShouldBeFalse)
		convey.So(report.AcknowledgedAt, convey.ShouldEqual, "")

		page, err := store.List(ctx, FeedbackFilter{}, 50, 0)
		convey.So(err, convey.ShouldBeNil)
		convey.So(page.Items[0].Acknowledged, convey.ShouldBeFalse)
		convey.So(page.Items[0].AcknowledgedAt, convey.ShouldEqual, "")
	})

	convey.Convey("A2.9: Given id 99 absent, then SetAcknowledged and Delete return ErrNotFound", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther)

		_, err := store.SetAcknowledged(ctx, 99, true)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)

		_, err = store.SetAcknowledged(ctx, 99, false)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)

		err = store.Delete(ctx, 99)
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
	})

	convey.Convey("A2.10: Given ids 1 and 2, when 1 then the highest id are deleted and reports added, then ids are never reused", t, func() {
		store := openTestFeedbackStore(t, filepath.Join(t.TempDir(), "feedback.db"))
		addTestFeedback(ctx, store, FeedbackCategoryOther, FeedbackCategoryOther)

		convey.So(store.Delete(ctx, 1), convey.ShouldBeNil)
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{}), convey.ShouldResemble, []int64{2})

		addTestFeedback(ctx, store, FeedbackCategoryOther)
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{}), convey.ShouldResemble, []int64{3, 2})

		convey.So(store.Delete(ctx, 3), convey.ShouldBeNil)
		addTestFeedback(ctx, store, FeedbackCategoryOther)
		convey.So(listFeedbackIDs(ctx, store, FeedbackFilter{}), convey.ShouldResemble, []int64{4, 2})
	})

	convey.Convey("A2.11: Given a closed store, when Add is called, then it returns an error", t, func() {
		store, err := OpenFeedbackStore(ctx, filepath.Join(t.TempDir(), "feedback.db"))
		convey.So(err, convey.ShouldBeNil)
		t.Cleanup(func() { _ = store.Close() })
		convey.So(store.Close(), convey.ShouldBeNil)

		_, err = store.Add(ctx, FeedbackSubmission{Category: FeedbackCategoryOther, Description: "d"}, "")
		convey.So(err, convey.ShouldNotBeNil)
	})
}

func openTestFeedbackStore(t *testing.T, path string) *FeedbackStore {
	t.Helper()

	store, err := OpenFeedbackStore(context.Background(), path)
	convey.So(err, convey.ShouldBeNil)

	t.Cleanup(func() { _ = store.Close() })

	return store
}

func addTestFeedback(ctx context.Context, store *FeedbackStore, categories ...FeedbackCategory) {
	for _, category := range categories {
		_, err := store.Add(ctx, FeedbackSubmission{Category: category, Description: "problem"}, "127.0.0.1")
		convey.So(err, convey.ShouldBeNil)
	}
}

func listFeedbackIDs(ctx context.Context, store *FeedbackStore, filter FeedbackFilter) []int64 {
	page, err := store.List(ctx, filter, 50, 0)
	convey.So(err, convey.ShouldBeNil)

	return reportIDs(page.Items)
}

func reportIDs(reports []FeedbackReport) []int64 {
	ids := make([]int64, 0, len(reports))
	for _, report := range reports {
		ids = append(ids, report.ID)
	}

	return ids
}
