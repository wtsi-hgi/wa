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
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

func TestSyncErrorTextIsRedactedAndCapped(t *testing.T) {
	convey.Convey("Given a client that knows its source and cache passwords", t, func() {
		client := &Client{syncSecrets: []string{"src-pass-123", "cache-pass-456"}}

		convey.Convey("then DSN userinfo, password parameters and known secrets are redacted", func() {
			raw := errors.New("dial bob:hunter2@tcp(db:3306)/mlwh failed; " +
				"url mysql://alice:pw%21x@host/db; password=topsecret&x=1; " +
				"token: src-pass-123 and cache-pass-456; Error 1045: Access denied for user 'bob'@'host' (using password: YES)")

			text := client.syncErrorText(raw)

			for _, secret := range []string{"hunter2", "pw%21x", "topsecret", "src-pass-123", "cache-pass-456"} {
				convey.So(text, convey.ShouldNotContainSubstring, secret)
			}
			convey.So(text, convey.ShouldContainSubstring, "bob:[redacted]@tcp(db:3306)")
			convey.So(text, convey.ShouldContainSubstring, "Access denied for user 'bob'@'host'")
		})

		convey.Convey("then a DSN password containing @ is redacted whole", func() {
			text := client.syncErrorText(errors.New("dial bob:p@ss@word@tcp(db:3306)/mlwh failed"))

			convey.So(text, convey.ShouldEqual, "dial bob:[redacted]@tcp(db:3306)/mlwh failed")
		})

		convey.Convey("then quoted password parameters are redacted whole", func() {
			text := client.syncErrorText(errors.New(`bad config password="a b c" and token='x y' end`))

			convey.So(text, convey.ShouldEqual, `bad config password=[redacted] and token=[redacted] end`)
		})

		convey.Convey("then JSON secret fields are redacted", func() {
			text := client.syncErrorText(errors.New(`decode {"user":"bob","password":"p\"w d","Token": "tk1"} failed`))

			convey.So(text, convey.ShouldEqual, `decode {"user":"bob","password":"[redacted]","Token": "[redacted]"} failed`)
		})

		convey.Convey("then a very short known secret does not mangle the text", func() {
			short := &Client{syncSecrets: []string{"a", "err"}}
			text := short.syncErrorText(errors.New("a read error occurred"))

			convey.So(text, convey.ShouldEqual, "a read error occurred")
		})

		convey.Convey("then a long error is capped", func() {
			text := client.syncErrorText(errors.New(strings.Repeat("é", 5000)))

			convey.So(len(text), convey.ShouldBeLessThanOrEqualTo, syncErrorMaxBytes)
			convey.So(strings.HasSuffix(text, syncErrorTruncatedSuffix), convey.ShouldBeTrue)
		})
	})
}

// TestRealMySQLOpenCacheDoesNotWaitForAnInProgressSync is the MySQL twin of
// TestOpenCacheDoesNotWaitForAnInProgressSync: a sync holding row locks on
// sync_state and sync_outcome must not block another process's open.
func TestRealMySQLOpenCacheDoesNotWaitForAnInProgressSync(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)
	lastRun := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)

	convey.Convey("Given a synced MySQL cache whose sync holds row locks", t, func() {
		cfg := CacheConfig{Path: createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password), Password: password}

		cache, err := OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)
		seedSyncStateRun(t, cache.DB(), syncTableStudy, lastRun, lastRun)
		convey.So(cache.Close(), convey.ShouldBeNil)

		cache, err = OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = cache.Close() }()

		tx, err := cache.DB().BeginTx(context.Background(), nil)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = tx.Rollback() }()

		for _, stmt := range []string{
			`UPDATE sync_state SET last_run = last_run WHERE table_name = ?`,
			`UPDATE sync_outcome SET last_success_at = last_success_at WHERE table_name = ?`,
		} {
			_, err = tx.ExecContext(context.Background(), stmt, syncTableStudy)
			convey.So(err, convey.ShouldBeNil)
		}

		convey.Convey("then another process opens it without waiting for the locks", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client, openErr := OpenCacheOnly(ctx, cfg)
			convey.So(openErr, convey.ShouldBeNil)
			defer func() { _ = client.Close() }()
		})
	})
}

// fixedClock returns a now func whose time the test moves with set.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fixedClock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = now
}

func TestSyncRecordsPerTableOutcome(t *testing.T) {
	convey.Convey("Given a SQLite cache and a source where every table succeeds", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
		clock := &fixedClock{now: base.Add(time.Hour)}

		source := openSyncTestSourceDB(t, syncOutcomeSourcePlans(base))
		defer func() { _ = source.Close() }()

		client := newSyncOutcomeTestClient(cache, source, clock)

		_, err := client.Sync(context.Background())
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("then every table reports ok with last_success_at at the completion time and no error", func() {
			byTable := syncOutcomeFreshness(t, client)
			convey.So(len(byTable), convey.ShouldEqual, len(freshnessSyncTables))

			notOK := 0
			for _, table := range freshnessSyncTables {
				got := byTable[table]
				if got.Status != FreshnessStatusOK || got.LastSuccessAt != "2026-10-01T10:00:00Z" ||
					got.LastError != "" || got.LastErrorAt != "" || len(got.BlockedBy) != 0 {
					notOK++
				}
			}

			convey.So(notOK, convey.ShouldEqual, 0)
		})
	})

	convey.Convey("Given a table that fails before its first commit", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
		clock := &fixedClock{now: base.Add(time.Hour)}

		plans := syncOutcomeSourcePlans(base)
		plans[syncTableStudy] = syncTestSourcePlan{queryErr: errors.New("forced study failure")}
		source := openSyncTestSourceDB(t, plans)
		defer func() { _ = source.Close() }()

		client := newSyncOutcomeTestClient(cache, source, clock)

		_, err := client.Sync(context.Background())
		convey.So(err, convey.ShouldNotBeNil)

		convey.Convey("then that table reports failing with the error and no success, and the others are ok", func() {
			byTable := syncOutcomeFreshness(t, client)

			study := byTable[syncTableStudy]
			convey.So(study.Status, convey.ShouldEqual, FreshnessStatusFailing)
			convey.So(study.EverSynced, convey.ShouldBeFalse)
			convey.So(study.LastSuccessAt, convey.ShouldBeEmpty)
			convey.So(study.LastError, convey.ShouldContainSubstring, "forced study failure")
			convey.So(study.LastErrorAt, convey.ShouldEqual, "2026-10-01T10:00:00Z")

			convey.So(byTable[syncTableSample].Status, convey.ShouldEqual, FreshnessStatusOK)
			convey.So(byTable[syncTableSeqProductIRODSLocations].Status, convey.ShouldEqual, FreshnessStatusOK)
		})
	})
}

func syncOutcomeSourcePlans(base time.Time) map[string]syncTestSourcePlan {
	return map[string]syncTestSourcePlan{
		syncTableSample: {
			columns: sampleSyncSourceColumns,
			rows:    [][]driver.Value{{int64(1), "SQSCP", "101", "sample-uuid-1", "sample-a", "sanger-a", "supplier-a", "acc-a", "donor-a", int64(9606), "human", "desc-a", formatSyncTime(base)}},
		},
		syncTableStudy: {
			columns: studySyncSourceColumns,
			rows:    [][]driver.Value{studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", base)},
		},
		syncTableIseqFlowcell: {
			columns: []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "last_updated"},
			rows:    [][]driver.Value{{"Standard", int64(1), "5001", formatSyncTime(base)}},
		},
		syncTableIseqProductMetrics: {
			columns: iseqProductMetricsSyncSourceColumns,
			rows:    [][]driver.Value{{"product-1001", int64(3001), int64(2001), int64(9001), int64(1), int64(1), int64(1), "5001", int64(1), int64(1), int64(1), formatSyncTime(base)}},
		},
		syncTableSeqProductIRODSLocations: {
			columns: seqProductIRODSLocationsSyncSourceColumns,
			rows:    [][]driver.Value{{int64(7001), "product-1001", "/seq", "run/file.cram", int64(1), "5001", formatSyncTime(base), formatSyncTime(base), "illumina"}},
		},
	}
}

func syncOutcomeFreshness(t *testing.T, client *Client) map[string]TableFreshness {
	t.Helper()

	freshness, err := client.Freshness(context.Background())
	if err != nil {
		t.Fatalf("Freshness(): %v", err)
	}

	return freshnessByTable(freshness)
}

func TestSyncFailureMidTableKeepsLastSuccess(t *testing.T) {
	convey.Convey("Given a sample table that synced once and then fails after committing a batch", t, func() {
		withSyncColdBatchSizeForTest(t, syncBatchSize)

		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.October, 2, 9, 0, 0, 0, time.UTC)
		firstSync := base.Add(time.Hour)
		secondSync := firstSync.Add(30 * time.Minute)
		thirdSync := secondSync.Add(30 * time.Minute)
		clock := &fixedClock{now: firstSync}

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableSample: {
				columns: sampleSyncSourceColumns,
				queryResults: []syncTestQueryResult{
					{rows: sampleSyncRowsForRange(1, 10, base, nil)},
					{rows: sampleSyncRowsForRange(11, 1510, base.Add(time.Minute), nil), finalErr: errors.New("forced mid-table failure")},
					{rows: sampleSyncRowsForRange(1011, 1510, base.Add(time.Minute), nil)},
				},
			},
		})
		defer func() { _ = source.Close() }()

		client := newSyncOutcomeTestClient(cache, source, clock)

		_, err := syncSelectedTablesWithOutcomeForTest(context.Background(), client, syncTableSample)
		convey.So(err, convey.ShouldBeNil)

		clock.set(secondSync)
		_, err = syncSelectedTablesWithOutcomeForTest(context.Background(), client, syncTableSample)
		convey.So(err, convey.ShouldNotBeNil)

		convey.Convey("then the committed rows stay, last_success_at stays at the first sync, and the error is recorded", func() {
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM sample_mirror`), convey.ShouldBeGreaterThan, 10)

			sample := syncOutcomeFreshness(t, client)[syncTableSample]
			convey.So(sample.Status, convey.ShouldEqual, FreshnessStatusFailing)
			convey.So(sample.EverSynced, convey.ShouldBeTrue)
			convey.So(sample.LastSuccessAt, convey.ShouldEqual, "2026-10-02T10:00:00Z")
			convey.So(sample.LastError, convey.ShouldContainSubstring, "forced mid-table failure")
			convey.So(sample.LastErrorAt, convey.ShouldEqual, "2026-10-02T10:30:00Z")

			convey.Convey("and a later successful sync clears the error and moves last_success_at", func() {
				clock.set(thirdSync)
				_, err = syncSelectedTablesWithOutcomeForTest(context.Background(), client, syncTableSample)
				convey.So(err, convey.ShouldBeNil)

				sample = syncOutcomeFreshness(t, client)[syncTableSample]
				convey.So(sample.Status, convey.ShouldEqual, FreshnessStatusOK)
				convey.So(sample.LastSuccessAt, convey.ShouldEqual, "2026-10-02T11:00:00Z")
				convey.So(sample.LastError, convey.ShouldBeEmpty)
				convey.So(sample.LastErrorAt, convey.ShouldBeEmpty)
			})
		})
	})
}

// syncSelectedTablesWithOutcomeForTest syncs the given tables through the same
// per-table path Client.Sync uses, outcome recording included.
func syncSelectedTablesWithOutcomeForTest(ctx context.Context, client *Client, tables ...string) ([]SyncReport, error) {
	reports, errs := collectSyncResults(client.syncTablesInParallel(ctx, tables), len(tables))

	return reports, errors.Join(errs...)
}

func TestSyncRecordsDependencySkip(t *testing.T) {
	convey.Convey("Given a failing iRODS dependency table", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.October, 3, 9, 0, 0, 0, time.UTC)
		clock := &fixedClock{now: base.Add(time.Hour)}

		plans := syncOutcomeSourcePlans(base)
		plans[syncTableIseqProductMetrics] = syncTestSourcePlan{queryErr: errors.New("forced product metrics failure")}
		source := openSyncTestSourceDB(t, plans)
		defer func() { _ = source.Close() }()

		client := newSyncOutcomeTestClient(cache, source, clock)

		_, err := client.Sync(context.Background())
		convey.So(err, convey.ShouldNotBeNil)

		convey.Convey("then the skipped iRODS table reports failing, blocked by that dependency", func() {
			irods := syncOutcomeFreshness(t, client)[syncTableSeqProductIRODSLocations]
			convey.So(irods.Status, convey.ShouldEqual, FreshnessStatusFailing)
			convey.So(irods.BlockedBy, convey.ShouldResemble, []string{syncTableIseqProductMetrics})
			convey.So(irods.LastError, convey.ShouldContainSubstring, "skipped")
			convey.So(irods.LastError, convey.ShouldContainSubstring, syncTableIseqProductMetrics)
			convey.So(irods.LastErrorAt, convey.ShouldEqual, "2026-10-03T10:00:00Z")
			convey.So(countRows(t, cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror`), convey.ShouldEqual, 0)
		})
	})
}

func TestOpenCacheDoesNotWaitForAnInProgressSync(t *testing.T) {
	lastRun := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)

	convey.Convey("Given a synced SQLite cache whose sync holds a write transaction", t, func() {
		path := filepath.Join(t.TempDir(), "busy.sqlite")
		cfg := CacheConfig{Path: path}

		cache, err := OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)
		seedSyncStateRun(t, cache.DB(), syncTableStudy, lastRun, lastRun)
		convey.So(cache.Close(), convey.ShouldBeNil)

		// A second open backfills the study outcome, as a real deployment's
		// first open after upgrading would.
		cache, err = OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)
		convey.So(cache.Close(), convey.ShouldBeNil)

		writer, err := sql.Open("sqlite", sqliteWritableDSN(path))
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = writer.Close() }()

		conn, err := writer.Conn(context.Background())
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = conn.Close() }()

		_, err = conn.ExecContext(context.Background(), `BEGIN IMMEDIATE`)
		convey.So(err, convey.ShouldBeNil)
		defer func() { _, _ = conn.ExecContext(context.Background(), `ROLLBACK`) }()

		_, err = conn.ExecContext(context.Background(), `UPDATE sync_state SET last_run = last_run`)
		convey.So(err, convey.ShouldBeNil)

		convey.Convey("then a reader such as serve or freshness opens it without waiting for the lock", func() {
			started := time.Now()
			client, openErr := OpenCacheOnly(context.Background(), cfg)
			elapsed := time.Since(started)

			convey.So(openErr, convey.ShouldBeNil)
			defer func() { _ = client.Close() }()

			convey.So(elapsed, convey.ShouldBeLessThan, 2*time.Second)

			client.now = func() time.Time { return lastRun.Add(time.Minute) }
			convey.So(syncOutcomeFreshness(t, client)[syncTableStudy].LastSuccessAt, convey.ShouldEqual, "2026-10-04T09:00:00Z")
		})
	})
}

func TestSyncRecordsCancelledSyncFailure(t *testing.T) {
	convey.Convey("Given a study sync whose context is cancelled mid-table, as a pod rollout would", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		base := time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC)
		clock := &fixedClock{now: base.Add(time.Hour)}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		source := openSyncTestSourceDB(t, map[string]syncTestSourcePlan{
			syncTableStudy: {
				columns: studySyncSourceColumns,
				rows: [][]driver.Value{
					studyRowValues(10, "SQSCP", "5001", "study-uuid-1", "study-a", "acc-study-a", base),
					studyRowValues(11, "SQSCP", "5002", "study-uuid-2", "study-b", "acc-study-b", base),
				},
				afterFirstRow: cancel,
			},
		})
		defer func() { _ = source.Close() }()

		client := newSyncOutcomeTestClient(cache, source, clock)

		_, err := syncSelectedTablesWithOutcomeForTest(ctx, client, syncTableStudy)
		convey.So(err, convey.ShouldNotBeNil)

		convey.Convey("then the cancelled table still records its failure", func() {
			study := syncOutcomeFreshness(t, client)[syncTableStudy]
			convey.So(study.Status, convey.ShouldEqual, FreshnessStatusFailing)
			convey.So(study.LastError, convey.ShouldContainSubstring, "context canceled")
			convey.So(study.LastErrorAt, convey.ShouldEqual, "2026-10-05T10:00:00Z")
		})
	})
}

func TestSyncRecordsDroppedIndexRepairFailure(t *testing.T) {
	convey.Convey("Given a sample mirror whose dropped indexes cannot be rebuilt after a sync", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedUnrepairableDroppedIndexes(t, cache.DB(), syncTableSample)

		clock := &fixedClock{now: time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)}
		client := newSyncOutcomeTestClient(cache, nil, clock)

		errs := client.repairDroppedMirrorIndexesAfterSync(context.Background())
		convey.So(errs, convey.ShouldNotBeEmpty)

		convey.Convey("then the sample table's stored outcome records the rebuild error", func() {
			sample, readErr := readSyncOutcome(context.Background(), cache.DB(), syncTableSample)
			convey.So(readErr, convey.ShouldBeNil)
			convey.So(sample.LastError, convey.ShouldContainSubstring, "rebuild dropped indexes")
			convey.So(sample.LastErrorAt, convey.ShouldEqual, clock.Now())
		})
	})
}

// seedUnrepairableDroppedIndexes leaves table's sync_state saying a cold load
// dropped its indexes and finished, with a high_water that cannot be parsed,
// so rebuilding its indexes fails.
func seedUnrepairableDroppedIndexes(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, 'not-a-time', ?, NULL, 1)`,
		table, formatSyncTime(time.Date(2026, time.October, 5, 9, 0, 0, 0, time.UTC)),
	); err != nil {
		t.Fatalf("seed unrepairable sync_state for %s: %v", table, err)
	}
}

// TestRealMySQLSyncRecordsDependencyIndexRepairSkip covers the MySQL-only skip
// of the iRODS table when its product dependency's indexes cannot be rebuilt.
func TestRealMySQLSyncRecordsDependencyIndexRepairSkip(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)

	convey.Convey("Given a MySQL cache whose product metrics indexes cannot be rebuilt", t, func() {
		cache, err := OpenCache(context.Background(),
			CacheConfig{Path: createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password), Password: password})
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = cache.Close() }()

		seedUnrepairableDroppedIndexes(t, cache.DB(), syncTableIseqProductMetrics)

		clock := &fixedClock{now: time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC)}
		client := newSyncOutcomeTestClient(cache, nil, clock)

		_, errs := client.syncSeqProductIRODSLocationsStage(context.Background(), nil)
		convey.So(errs, convey.ShouldNotBeEmpty)

		convey.Convey("then the iRODS table's stored outcome records it as skipped for the repair error", func() {
			irods, readErr := readSyncOutcome(context.Background(), cache.DB(), syncTableSeqProductIRODSLocations)
			convey.So(readErr, convey.ShouldBeNil)
			convey.So(irods.LastError, convey.ShouldContainSubstring, "skipped: repair dependency indexes")
			convey.So(irods.LastErrorAt, convey.ShouldEqual, clock.Now())
		})
	})
}

func newSyncOutcomeTestClient(cache Cache, source *sql.DB, clock *fixedClock) *Client {
	return &Client{
		cache:           cache,
		cacheReader:     cacheReadDB(cache),
		syncSource:      source,
		disableSyncLock: true,
		now:             clock.Now,
	}
}

// TestRealMySQLSyncOutcomeMigrationAndRecording is the MySQL twin of the
// SQLite migration and recording tests. It runs against a throwaway database
// on the real cache server and skips when that server is not configured.
func TestRealMySQLSyncOutcomeMigrationAndRecording(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)
	lastRun := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)

	convey.Convey("Given a current-version MySQL cache created before sync_outcome existed", t, func() {
		cfg := CacheConfig{Path: createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password), Password: password}

		cache, err := OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)

		seedOldShapeCache(t, cache.DB(), lastRun)
		convey.So(cache.Close(), convey.ShouldBeNil)

		convey.Convey("when many processes open it at once", func() {
			const openers = 6

			clients := make([]*Client, openers)
			errs := make([]error, openers)

			var wg sync.WaitGroup
			for i := range openers {
				wg.Add(1)

				go func() {
					defer wg.Done()

					clients[i], errs[i] = OpenCacheOnly(context.Background(), cfg)
				}()
			}

			wg.Wait()

			failed := 0
			for i := range openers {
				if errs[i] != nil {
					t.Logf("opener %d: %v", i, errs[i])
					failed++

					continue
				}

				defer func() { _ = clients[i].Close() }()
			}

			convey.So(failed, convey.ShouldEqual, 0)

			client := clients[0]
			clock := &fixedClock{now: lastRun.Add(time.Minute)}
			client.now = clock.Now

			convey.Convey("then every opener succeeds and the mirrors survive, with completed tables backfilled", func() {
				assertOldShapeCacheMigrated(t, client)
				convey.So(countRows(t, client.cache.DB(), `SELECT COUNT(*) FROM sync_outcome`), convey.ShouldEqual, 1)
			})

			convey.Convey("then a sync with a failing table records failure and success per table", func() {
				clock.set(lastRun.Add(time.Hour))

				plans := syncOutcomeSourcePlans(lastRun)
				plans[syncTableStudy] = syncTestSourcePlan{queryErr: errors.New("forced mysql study failure")}
				source := openSyncTestSourceDB(t, plans)
				client.syncSource = source
				client.disableSyncLock = true

				_, syncErr := client.Sync(context.Background())
				convey.So(syncErr, convey.ShouldNotBeNil)

				byTable := syncOutcomeFreshness(t, client)
				study := byTable[syncTableStudy]
				convey.So(study.Status, convey.ShouldEqual, FreshnessStatusFailing)
				convey.So(study.LastSuccessAt, convey.ShouldEqual, "2026-10-04T09:00:00Z")
				convey.So(study.LastError, convey.ShouldContainSubstring, "forced mysql study failure")
				convey.So(study.LastErrorAt, convey.ShouldEqual, "2026-10-04T10:00:00Z")

				flowcell := byTable[syncTableIseqFlowcell]
				convey.So(flowcell.Status, convey.ShouldEqual, FreshnessStatusOK)
				convey.So(flowcell.LastSuccessAt, convey.ShouldEqual, "2026-10-04T10:00:00Z")
			})
		})
	})
}

func seedOldShapeCache(t *testing.T, db *sql.DB, lastRun time.Time) {
	t.Helper()

	for _, stmt := range []string{
		`DROP TABLE sync_outcome`,
		`INSERT INTO common_name_word_mirror(word, common_name) VALUES ('homo', 'homo sapiens')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed old cache %q: %v", stmt, err)
		}
	}

	seedSyncStateRun(t, db, syncTableStudy, lastRun, lastRun)
	if _, err := db.Exec(
		`INSERT INTO sync_state(table_name, high_water, last_run, resume_cursor, indexes_dropped) VALUES (?, ?, ?, ?, 0)`,
		syncTableSample, formatSyncTime(lastRun), formatSyncTime(lastRun), "id_sample_tmp_desc\t1000",
	); err != nil {
		t.Fatalf("seed interrupted sample sync_state: %v", err)
	}
}

func assertOldShapeCacheMigrated(t *testing.T, client *Client) {
	t.Helper()

	convey.So(countRows(t, client.cache.DB(), `SELECT COUNT(*) FROM common_name_word_mirror`), convey.ShouldEqual, 1)
	convey.So(countRows(t, client.cache.DB(), `SELECT COUNT(*) FROM sync_state`), convey.ShouldEqual, 2)
	convey.So(countRows(t, client.cache.DB(), `SELECT COUNT(*) FROM schema_version WHERE version = `+fmt.Sprint(CacheSchemaVersion)), convey.ShouldEqual, 1)

	byTable := syncOutcomeFreshness(t, client)
	convey.So(byTable[syncTableStudy].LastSuccessAt, convey.ShouldEqual, "2026-10-04T09:00:00Z")
	convey.So(byTable[syncTableSample].LastSuccessAt, convey.ShouldBeEmpty)
	convey.So(byTable[syncTableSample].Status, convey.ShouldEqual, FreshnessStatusStale)
}

func TestOpenForSyncRecordsSourceConnectFailure(t *testing.T) {
	convey.Convey("Given an unreachable MLWH source and a password", t, func() {
		cachePath := filepath.Join(t.TempDir(), "sync.sqlite")
		cfg := Config{
			DSN:      "mlwh_user@tcp(127.0.0.1:1)/mlwarehouse?timeout=2s",
			Password: "very-secret-pw",
			Cache:    CacheConfig{Path: cachePath},
		}

		client, err := OpenForSync(context.Background(), cfg)
		convey.So(client, convey.ShouldBeNil)
		convey.So(err, convey.ShouldNotBeNil)

		convey.Convey("then every sync table records the redacted connect failure", func() {
			reopened, openErr := OpenCacheOnly(context.Background(), CacheConfig{Path: cachePath})
			convey.So(openErr, convey.ShouldBeNil)
			defer func() { _ = reopened.Close() }()

			byTable := syncOutcomeFreshness(t, reopened)
			failing := 0
			leaked := 0
			for _, table := range freshnessSyncTables {
				got := byTable[table]
				if got.Status == FreshnessStatusFailing && strings.Contains(got.LastError, "source") {
					failing++
				}
				if strings.Contains(got.LastError, "very-secret-pw") {
					leaked++
				}
			}

			convey.So(failing, convey.ShouldEqual, len(freshnessSyncTables))
			convey.So(leaked, convey.ShouldEqual, 0)
		})
	})
}

func TestOpenCacheAddsSyncOutcomeToOldCache(t *testing.T) {
	lastRun := time.Date(2026, time.October, 4, 9, 0, 0, 0, time.UTC)

	convey.Convey("Given a current-version SQLite cache created before sync_outcome existed", t, func() {
		path := oldShapeSQLiteCache(t, lastRun)

		client, err := OpenCacheOnly(context.Background(), CacheConfig{Path: path})
		convey.So(err, convey.ShouldBeNil)
		defer func() { _ = client.Close() }()

		client.now = func() time.Time { return lastRun.Add(time.Minute) }

		convey.Convey("then opening keeps the mirrors and backfills completed tables from last_run", func() {
			assertOldShapeCacheMigrated(t, client)
		})
	})

	convey.Convey("Given the same old cache opened by many processes at once", t, func() {
		path := oldShapeSQLiteCache(t, lastRun)

		const openers = 8

		clients := make([]*Client, openers)
		errs := make([]error, openers)

		var wg sync.WaitGroup
		for i := range openers {
			wg.Add(1)

			go func() {
				defer wg.Done()

				clients[i], errs[i] = OpenCacheOnly(context.Background(), CacheConfig{Path: path})
			}()
		}

		wg.Wait()

		failed := 0
		for i := range openers {
			if errs[i] != nil {
				t.Logf("opener %d: %v", i, errs[i])
				failed++

				continue
			}

			defer func() { _ = clients[i].Close() }()
		}

		convey.So(failed, convey.ShouldEqual, 0)

		convey.Convey("then every opener succeeds and the migration happened once without dropping mirrors", func() {
			clients[0].now = func() time.Time { return lastRun.Add(time.Minute) }
			assertOldShapeCacheMigrated(t, clients[0])
			convey.So(countRows(t, clients[0].cache.DB(), `SELECT COUNT(*) FROM sync_outcome`), convey.ShouldEqual, 1)
		})
	})
}

// oldShapeSQLiteCache builds a current-version cache as a release before
// sync_outcome existed left it: mirror data, sync_state rows, no sync_outcome.
func oldShapeSQLiteCache(t *testing.T, lastRun time.Time) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "old.sqlite")

	cache, err := OpenCache(context.Background(), CacheConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenCache(): %v", err)
	}

	seedOldShapeCache(t, cache.DB(), lastRun)

	if err = cache.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	return path
}
