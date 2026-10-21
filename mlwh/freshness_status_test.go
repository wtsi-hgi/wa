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
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestFreshnessStatus(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)

	convey.Convey("Given a cache where every table succeeded 10 minutes ago", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedAllTablesOK(t, cache.DB(), now.Add(-10*time.Minute))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), now: func() time.Time { return now }}

		convey.Convey("then every table and the overall status are ok, with the default threshold reported", func() {
			freshness, err := client.Freshness(context.Background())
			convey.So(err, convey.ShouldBeNil)
			convey.So(freshness.Status, convey.ShouldEqual, FreshnessStatusOK)
			convey.So(freshness.StaleAfterSeconds, convey.ShouldEqual, int64(DefaultStaleAfter/time.Second))
			convey.So(DefaultStaleAfter, convey.ShouldEqual, 2*time.Hour)

			notOK := 0
			for _, table := range freshness.Tables {
				if table.Status != FreshnessStatusOK {
					notOK++
				}
			}
			convey.So(notOK, convey.ShouldEqual, 0)
		})

		convey.Convey("then a shorter configured threshold makes them stale", func() {
			client.SetStaleAfter(5 * time.Minute)

			freshness, err := client.Freshness(context.Background())
			convey.So(err, convey.ShouldBeNil)
			convey.So(freshness.Status, convey.ShouldEqual, FreshnessStatusStale)
			convey.So(freshness.StaleAfterSeconds, convey.ShouldEqual, 300)
			convey.So(freshnessByTable(freshness)[syncTableSample].Status, convey.ShouldEqual, FreshnessStatusStale)
		})
	})

	convey.Convey("Given one table whose last success is older than the threshold but whose high_water never moves", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedAllTablesOK(t, cache.DB(), now.Add(-10*time.Minute), syncTableIseqRunStatusDict, syncTableStudyUsers)

		oldHighWater := now.Add(-400 * 24 * time.Hour)
		seedSyncStateRun(t, cache.DB(), syncTableIseqRunStatusDict, oldHighWater, now.Add(-5*time.Minute))
		seedSyncOutcomeForTest(t, cache.DB(), syncTableIseqRunStatusDict, now.Add(-5*time.Minute), "", time.Time{})

		seedSyncStateRun(t, cache.DB(), syncTableStudyUsers, now.Add(-3*time.Hour), now.Add(-3*time.Hour))
		seedSyncOutcomeForTest(t, cache.DB(), syncTableStudyUsers, now.Add(-3*time.Hour), "", time.Time{})

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), now: func() time.Time { return now }}

		convey.Convey("then staleness follows last_success_at, not high_water", func() {
			freshness, err := client.Freshness(context.Background())
			convey.So(err, convey.ShouldBeNil)

			byTable := freshnessByTable(freshness)
			convey.So(byTable[syncTableIseqRunStatusDict].Status, convey.ShouldEqual, FreshnessStatusOK)
			convey.So(byTable[syncTableStudyUsers].Status, convey.ShouldEqual, FreshnessStatusStale)
			convey.So(freshness.Status, convey.ShouldEqual, FreshnessStatusStale)
		})
	})

	convey.Convey("Given a stale table, a never-synced table and a failing table", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedAllTablesOK(t, cache.DB(), now.Add(-10*time.Minute), syncTableSample, syncTableStudy, syncTableEseqRun)
		seedSyncStateRun(t, cache.DB(), syncTableSample, now.Add(-5*time.Hour), now.Add(-5*time.Hour))
		seedSyncOutcomeForTest(t, cache.DB(), syncTableSample, now.Add(-5*time.Hour), "", time.Time{})
		seedSyncStateRun(t, cache.DB(), syncTableStudy, now.Add(-time.Minute), now.Add(-time.Minute))
		seedSyncOutcomeForTest(t, cache.DB(), syncTableStudy, now.Add(-time.Hour), "boom", now.Add(-time.Minute))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), now: func() time.Time { return now }}

		convey.Convey("then each reports its status and the overall status is failing", func() {
			freshness, err := client.Freshness(context.Background())
			convey.So(err, convey.ShouldBeNil)

			byTable := freshnessByTable(freshness)
			convey.So(byTable[syncTableSample].Status, convey.ShouldEqual, FreshnessStatusStale)
			convey.So(byTable[syncTableEseqRun].Status, convey.ShouldEqual, FreshnessStatusNeverSynced)
			convey.So(byTable[syncTableStudy].Status, convey.ShouldEqual, FreshnessStatusFailing)
			convey.So(byTable[syncTableStudy].LastSuccessAt, convey.ShouldEqual, "2026-10-10T11:00:00Z")
			convey.So(byTable[syncTableStudy].LastError, convey.ShouldEqual, "boom")
			convey.So(byTable[syncTableStudy].LastErrorAt, convey.ShouldEqual, "2026-10-10T11:59:00Z")
			convey.So(freshness.Status, convey.ShouldEqual, FreshnessStatusFailing)
		})

		convey.Convey("then without the failure the never-synced table outranks the stale one", func() {
			_, err := cache.DB().Exec(`UPDATE sync_outcome SET last_error = NULL, last_error_at = NULL, last_success_at = ? WHERE table_name = ?`,
				formatSyncTime(now.Add(-time.Minute)), syncTableStudy)
			convey.So(err, convey.ShouldBeNil)

			freshness, err := client.Freshness(context.Background())
			convey.So(err, convey.ShouldBeNil)
			convey.So(freshness.Status, convey.ShouldEqual, FreshnessStatusNeverSynced)
		})
	})

	convey.Convey("Given a never-synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache), now: func() time.Time { return now }}

		convey.Convey("then every table and the overall status are never_synced with empty outcome fields", func() {
			freshness, err := client.Freshness(context.Background())
			convey.So(err, convey.ShouldBeNil)
			convey.So(freshness.Status, convey.ShouldEqual, FreshnessStatusNeverSynced)

			table := freshnessByTable(freshness)[syncTableSample]
			convey.So(table.Status, convey.ShouldEqual, FreshnessStatusNeverSynced)
			convey.So(table.LastSuccessAt, convey.ShouldBeEmpty)
			convey.So(table.LastError, convey.ShouldBeEmpty)
			convey.So(table.BlockedBy, convey.ShouldNotBeNil)
			convey.So(table.BlockedBy, convey.ShouldBeEmpty)
		})
	})
}

// seedAllTablesOK gives every sync table a sync_state row and a success at
// lastSuccess, so a test can then make individual tables unhealthy.
func seedAllTablesOK(t *testing.T, db *sql.DB, lastSuccess time.Time, except ...string) {
	t.Helper()

	skip := make(map[string]bool, len(except))
	for _, table := range except {
		skip[table] = true
	}

	for _, table := range freshnessSyncTables {
		if skip[table] {
			continue
		}

		seedSyncStateRun(t, db, table, lastSuccess, lastSuccess)
		seedSyncOutcomeForTest(t, db, table, lastSuccess, "", time.Time{})
	}
}

func seedSyncOutcomeForTest(t *testing.T, db *sql.DB, table string, lastSuccess time.Time, lastError string, lastErrorAt time.Time) {
	t.Helper()

	var success, errorAt, errorText any
	if !lastSuccess.IsZero() {
		success = formatSyncTime(lastSuccess)
	}
	if lastError != "" {
		errorText = lastError
		errorAt = formatSyncTime(lastErrorAt)
	}

	if _, err := db.Exec(
		`INSERT INTO sync_outcome(table_name, last_success_at, last_error, last_error_at, blocked_by) VALUES (?, ?, ?, ?, NULL)`,
		table, success, errorText, errorAt,
	); err != nil {
		t.Fatalf("seedSyncOutcomeForTest(%s): %v", table, err)
	}
}
