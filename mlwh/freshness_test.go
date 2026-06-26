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
	"reflect"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// D2 acceptance test 2: a never-synced cache (no sync_state rows) reports five
// tables, all ever_synced=false with empty timestamps, and does NOT error.
func TestFreshnessNeverSyncedReturnsFiveAbsentWithoutError(t *testing.T) {
	convey.Convey("Given a never-synced cache with no sync_state rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		freshness, err := client.Freshness(context.Background())

		convey.Convey("when Freshness runs, then it returns five absent tables and no error", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(freshness.Tables), convey.ShouldEqual, 5)

			everSyncedCount := 0
			nonEmptyTimestamps := 0
			for _, table := range freshness.Tables {
				if table.EverSynced {
					everSyncedCount++
				}
				if table.HighWater != "" || table.LastRun != "" {
					nonEmptyTimestamps++
				}
			}

			convey.So(everSyncedCount, convey.ShouldEqual, 0)
			convey.So(nonEmptyTimestamps, convey.ShouldEqual, 0)
		})
	})
}

// D2 acceptance test 1: only sample and study have sync_state rows; freshness
// reports five tables, the two synced ones carrying their exact high_water and
// last_run, the other three ever_synced=false with empty timestamps.
func TestFreshnessReportsPerTableSyncState(t *testing.T) {
	convey.Convey("Given a cache where only sample and study have sync_state rows", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		highWater := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
		lastRun := time.Date(2026, time.June, 1, 10, 5, 0, 0, time.UTC)
		seedSyncStateRun(t, cache.DB(), syncTableSample, highWater, lastRun)
		seedSyncStateRun(t, cache.DB(), syncTableStudy, highWater, lastRun)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		freshness, err := client.Freshness(context.Background())

		convey.Convey("when Freshness runs, then it returns five tables with the synced two populated", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(len(freshness.Tables), convey.ShouldEqual, 5)

			byTable := freshnessByTable(freshness)
			convey.So(len(byTable), convey.ShouldEqual, 5)

			for _, table := range []string{syncTableSample, syncTableStudy} {
				convey.So(byTable[table].EverSynced, convey.ShouldBeTrue)
				convey.So(byTable[table].HighWater, convey.ShouldEqual, "2026-06-01T10:00:00Z")
				convey.So(byTable[table].LastRun, convey.ShouldEqual, "2026-06-01T10:05:00Z")
			}

			for _, table := range []string{syncTableIseqFlowcell, syncTableIseqProductMetrics, syncTableSeqProductIRODSLocations} {
				convey.So(byTable[table].EverSynced, convey.ShouldBeFalse)
				convey.So(byTable[table].HighWater, convey.ShouldBeEmpty)
				convey.So(byTable[table].LastRun, convey.ShouldBeEmpty)
			}
		})
	})
}

// The five mirrored tables must always be reported, in the spec's declared order,
// regardless of which ones have a sync_state row, so callers get a stable shape.
func TestFreshnessReportsAllFiveTablesInOrder(t *testing.T) {
	convey.Convey("Given a cache with a single sync_state row", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSyncStateRun(t, cache.DB(), syncTableIseqFlowcell,
			time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC),
			time.Date(2026, time.June, 2, 9, 1, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		freshness, err := client.Freshness(context.Background())

		convey.Convey("when Freshness runs, then all five mirrored tables appear in declared order", func() {
			convey.So(err, convey.ShouldBeNil)

			tables := make([]string, 0, len(freshness.Tables))
			for _, table := range freshness.Tables {
				tables = append(tables, table.Table)
			}

			convey.So(tables, convey.ShouldResemble, []string{
				syncTableStudy,
				syncTableSample,
				syncTableIseqFlowcell,
				syncTableIseqProductMetrics,
				syncTableSeqProductIRODSLocations,
			})
		})
	})
}

// D2 acceptance test 4: the RemoteClient.Freshness round-trip through the gin
// server and JSON decoding equals the local Client's Freshness result.
func TestFreshnessRemoteClientRoundTripEqualsLocalD2(t *testing.T) {
	convey.Convey("D2.4: Given a Client over a seeded cache served by an httptest server", t, func() {
		local := newParityClient(t)
		defer closeParityClientForTest(t, local)

		highWater := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
		lastRun := time.Date(2026, time.June, 1, 10, 5, 0, 0, time.UTC)
		seedSyncStateRun(t, local.cache.DB(), syncTableSample, highWater, lastRun)
		seedSyncStateRun(t, local.cache.DB(), syncTableStudy, highWater, lastRun)

		remote := newParityRemoteClientForTest(t, local)
		defer closeRemoteClientForTest(t, remote)

		localResult, localErr := local.Freshness(context.Background())
		remoteResult, remoteErr := remote.Freshness(context.Background())

		convey.Convey("when Freshness runs locally and remotely, then the decoded Freshness equals the local result", func() {
			convey.So(localErr, convey.ShouldBeNil)
			convey.So(remoteErr, convey.ShouldBeNil)
			convey.So(len(localResult.Tables), convey.ShouldEqual, 5)
			convey.So(reflect.DeepEqual(localResult, remoteResult), convey.ShouldBeTrue)
		})
	})
}

// seedSyncStateRun inserts a sync_state row with explicit high_water and last_run
// values (formatted as the sync writer stores them), so freshness tests can assert
// the two timestamps independently. The seedSyncState helper always stamps last_run
// with time.Now(), which cannot exercise D2's distinct-timestamp expectations.
func seedSyncStateRun(t *testing.T, db *sql.DB, table string, highWater, lastRun time.Time) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`,
		table,
		formatSyncTime(highWater),
		formatSyncTime(lastRun),
	)
	if err != nil {
		t.Fatalf("seedSyncStateRun(%s): %v", table, err)
	}
}

// A cold load writes a sync_state row before pulling data, stamping high_water with
// the zero time (formatSyncTime(time.Time{}) == "0001-01-01T00:00:00Z"). If that
// cold load is interrupted before the first batch advances high_water, the row
// persists with that zero high_water. Freshness must report an empty high_water for
// it (matching the "empty if never synced" contract), never the bogus year-0001
// timestamp, while still rendering a genuine non-zero last_run.
func TestFreshnessZeroHighWaterRendersEmpty(t *testing.T) {
	convey.Convey("Given a sync_state row whose high_water is the zero cold-load value", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		lastRun := time.Date(2026, time.June, 1, 10, 5, 0, 0, time.UTC)

		_, err := cache.DB().Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 1)`,
			syncTableSample,
			formatSyncTime(time.Time{}),
			formatSyncTime(lastRun),
		)
		convey.So(err, convey.ShouldBeNil)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		freshness, freshErr := client.Freshness(context.Background())

		convey.Convey("when Freshness runs, then high_water is empty and last_run keeps its UTC value", func() {
			convey.So(freshErr, convey.ShouldBeNil)

			byTable := freshnessByTable(freshness)
			sample := byTable[syncTableSample]
			convey.So(sample.HighWater, convey.ShouldBeEmpty)
			convey.So(sample.LastRun, convey.ShouldEqual, "2026-06-01T10:05:00Z")
		})
	})
}

// D2 acceptance test 3: a non-UTC stored time is normalised to UTC RFC3339 ending
// in Z on the way out.
func TestFreshnessNormalisesNonUTCTimeToUTC(t *testing.T) {
	convey.Convey("Given a sync_state row whose stored time carries a non-UTC offset", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		offsetZone := time.FixedZone("+05:00", 5*60*60)
		highWater := time.Date(2026, time.June, 1, 15, 0, 0, 0, offsetZone)
		lastRun := time.Date(2026, time.June, 1, 15, 30, 0, 0, offsetZone)

		_, err := cache.DB().Exec(
			`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, NULL, 0)`,
			syncTableStudy,
			highWater.Format(time.RFC3339Nano),
			lastRun.Format(time.RFC3339Nano),
		)
		convey.So(err, convey.ShouldBeNil)

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		freshness, freshErr := client.Freshness(context.Background())

		convey.Convey("when Freshness runs, then high_water is normalised to UTC RFC3339 ending in Z", func() {
			convey.So(freshErr, convey.ShouldBeNil)

			byTable := freshnessByTable(freshness)
			study := byTable[syncTableStudy]
			convey.So(study.EverSynced, convey.ShouldBeTrue)
			convey.So(study.HighWater, convey.ShouldEqual, "2026-06-01T10:00:00Z")
			convey.So(study.LastRun, convey.ShouldEqual, "2026-06-01T10:30:00Z")
		})
	})
}

// freshnessByTable indexes a Freshness result by table name for assertions.
func freshnessByTable(f Freshness) map[string]TableFreshness {
	byTable := make(map[string]TableFreshness, len(f.Tables))
	for _, table := range f.Tables {
		byTable[table.Table] = table
	}

	return byTable
}
