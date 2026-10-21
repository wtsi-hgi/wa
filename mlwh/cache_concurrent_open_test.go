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
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

const concurrentCacheOpeners = 8

// lockedWriter lets concurrent openers log migrations to one buffer.
type lockedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.Write(p)
}

func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.buf.String()
}

func captureConcurrentCacheMigrationOutput(t *testing.T) *lockedWriter {
	t.Helper()

	original := cacheMigrationStderr
	writer := &lockedWriter{}
	cacheMigrationStderr = writer
	t.Cleanup(func() { cacheMigrationStderr = original })

	return writer
}

// openCacheConcurrently opens cfg from n goroutines released together, each
// with its own connections as separate processes would have, and returns
// every open error.
func openCacheConcurrently(cfg CacheConfig, n int) []error {
	var (
		start sync.WaitGroup
		done  sync.WaitGroup
		mu    sync.Mutex
		errs  []error
	)

	start.Add(1)
	for range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			cache, err := OpenCache(ctx, cfg)
			if err == nil {
				err = cache.Close()
			}
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}

	start.Done()
	done.Wait()

	return errs
}

// setCacheSchemaVersion makes an opened cache look like it was written by an
// older wa, so the next open migrates it.
func setCacheSchemaVersion(t *testing.T, db *sql.DB, version int) {
	t.Helper()

	for _, stmt := range []string{`DELETE FROM schema_version`, `INSERT INTO schema_version(version, applied_at) VALUES (?, CURRENT_TIMESTAMP)`} {
		var args []any
		if strings.HasPrefix(stmt, "INSERT") {
			args = append(args, version)
		}

		if _, err := db.Exec(stmt, args...); err != nil {
			t.Fatalf("set schema version: %v", err)
		}
	}
}

func assertCacheSchemaCurrent(t *testing.T, cfg CacheConfig) {
	t.Helper()

	cache, err := OpenCache(context.Background(), cfg)
	convey.So(err, convey.ShouldBeNil)
	defer func() { _ = cache.Close() }()

	var rows, version int
	convey.So(cache.DB().QueryRow(`SELECT COUNT(*), COALESCE(MAX(version), 0) FROM schema_version`).Scan(&rows, &version), convey.ShouldBeNil)
	convey.So(rows, convey.ShouldEqual, 1)
	convey.So(version, convey.ShouldEqual, CacheSchemaVersion)
}

func TestConcurrentOpensOfAFreshSQLiteCache(t *testing.T) {
	convey.Convey("Given a SQLite cache path that does not exist yet", t, func() {
		cfg := CacheConfig{Path: filepath.Join(t.TempDir(), "fresh.sqlite")}

		convey.Convey("when several openers create it at the same moment, then all succeed with one current schema", func() {
			convey.So(openCacheConcurrently(cfg, concurrentCacheOpeners), convey.ShouldBeEmpty)
			assertCacheSchemaCurrent(t, cfg)
		})
	})
}

func TestConcurrentOpensMigrateAnOldSQLiteCacheOnce(t *testing.T) {
	convey.Convey("Given a SQLite cache written by an older schema version", t, func() {
		cfg := CacheConfig{Path: filepath.Join(t.TempDir(), "old.sqlite")}

		cache, err := OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)
		setCacheSchemaVersion(t, cache.DB(), CacheSchemaVersion-1)
		convey.So(cache.Close(), convey.ShouldBeNil)

		migrations := captureConcurrentCacheMigrationOutput(t)

		convey.Convey("when several openers open it at the same moment, then all succeed and only one migrates it", func() {
			convey.So(openCacheConcurrently(cfg, concurrentCacheOpeners), convey.ShouldBeEmpty)
			convey.So(strings.Count(migrations.String(), "mlwh cache: schema v"), convey.ShouldEqual, 1)
			assertCacheSchemaCurrent(t, cfg)
		})
	})
}

// TestConcurrentProcessesOpenAFreshSQLiteCache opens fresh caches from several
// processes at once, as `wa mlwh serve` and `wa mlwh sync` do when a pod
// starts. Each process opens one cache per round, so a race that only
// sometimes loses still fails the test.
func TestConcurrentProcessesOpenAFreshSQLiteCache(t *testing.T) {
	const rounds = 10

	if dir := os.Getenv("WA_TEST_SQLITE_CONCURRENT_OPEN_DIR"); dir != "" {
		os.Exit(openFreshSQLiteCachesInRounds(dir, os.Getenv("WA_TEST_SQLITE_CONCURRENT_OPEN_AT"), rounds))
	}

	convey.Convey("Given SQLite cache paths that do not exist yet", t, func() {
		dir := t.TempDir()
		startAt := time.Now().Add(2 * time.Second).Format(time.RFC3339Nano)

		convey.Convey("when several processes open each at the same moment, then all succeed with one current schema", func() {
			commands := make([]*exec.Cmd, concurrentCacheOpeners)
			outputs := make([]*bytes.Buffer, concurrentCacheOpeners)
			for i := range commands {
				outputs[i] = &bytes.Buffer{}
				commands[i] = exec.Command(os.Args[0], "-test.run=^TestConcurrentProcessesOpenAFreshSQLiteCache$")
				commands[i].Env = append(os.Environ(),
					"WA_TEST_SQLITE_CONCURRENT_OPEN_DIR="+dir,
					"WA_TEST_SQLITE_CONCURRENT_OPEN_AT="+startAt,
				)
				commands[i].Stdout = outputs[i]
				commands[i].Stderr = outputs[i]
				convey.So(commands[i].Start(), convey.ShouldBeNil)
			}

			var failures []string
			for i, command := range commands {
				if err := command.Wait(); err != nil {
					failures = append(failures, fmt.Sprintf("%v: %s", err, strings.TrimSpace(outputs[i].String())))
				}
			}

			convey.So(failures, convey.ShouldBeEmpty)
			for round := range rounds {
				assertCacheSchemaCurrent(t, CacheConfig{Path: filepath.Join(dir, fmt.Sprintf("round-%d.sqlite", round))})
			}
		})
	})
}

// openFreshSQLiteCachesInRounds is the helper process body: for each round it
// waits until that round's start time, then opens that round's cache. It
// returns the process exit code.
func openFreshSQLiteCachesInRounds(dir, startAt string, rounds int) int {
	start, err := time.Parse(time.RFC3339Nano, startAt)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 2
	}

	failed := 0
	for round := range rounds {
		time.Sleep(time.Until(start.Add(time.Duration(round) * 300 * time.Millisecond)))

		cache, err := OpenCache(context.Background(), CacheConfig{Path: filepath.Join(dir, fmt.Sprintf("round-%d.sqlite", round))})
		if err == nil {
			err = cache.Close()
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "round %d: %v\n", round, err)
			failed = 1
		}
	}

	return failed
}

func TestRealMySQLConcurrentOpensOfAFreshCache(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)

	convey.Convey("Given an empty MySQL cache database", t, func() {
		cfg := CacheConfig{Path: createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password), Password: password}

		convey.Convey("when several openers set it up at the same moment, then all succeed with one current schema", func() {
			convey.So(openCacheConcurrently(cfg, concurrentCacheOpeners), convey.ShouldBeEmpty)
			assertCacheSchemaCurrent(t, cfg)
		})
	})
}

func TestRealMySQLConcurrentOpensMigrateAnOldCacheOnce(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)

	convey.Convey("Given a MySQL cache written by an older schema version", t, func() {
		cfg := CacheConfig{Path: createThrowawayMySQLCacheDBOrSkip(t, baseDSN, password), Password: password}

		cache, err := OpenCache(context.Background(), cfg)
		convey.So(err, convey.ShouldBeNil)
		setCacheSchemaVersion(t, cache.DB(), CacheSchemaVersion-1)
		convey.So(cache.Close(), convey.ShouldBeNil)

		migrations := captureConcurrentCacheMigrationOutput(t)

		convey.Convey("when several openers open it at the same moment, then all succeed and only one migrates it", func() {
			convey.So(openCacheConcurrently(cfg, concurrentCacheOpeners), convey.ShouldBeEmpty)
			convey.So(strings.Count(migrations.String(), "mlwh cache: schema v"), convey.ShouldEqual, 1)
			assertCacheSchemaCurrent(t, cfg)
		})
	})
}
