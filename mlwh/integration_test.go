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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/smartystreets/goconvey/convey"
)

func TestLiveMLWHSyncQueriesMatchDevelopmentSchema(t *testing.T) {
	ctx := context.Background()
	config, skipReason := loadLiveMLWHConfigForTest(t)
	if skipReason != "" {
		t.Skip(skipReason)
	}

	resolvedDSN, err := ResolveDSN(config.DSN, config.Password)
	if err != nil {
		t.Fatalf("ResolveDSN(): %v", err)
	}

	sourceDB, err := sql.Open("mysql", resolvedDSN)
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() { _ = sourceDB.Close() })

	err = sourceDB.PingContext(ctx)
	if err != nil {
		t.Fatalf("PingContext(): %v", err)
	}

	convey.Convey("Given live MLWH credentials from the development env path", t, func() {
		err = explainLiveQuery(ctx, sourceDB, `EXPLAIN `+sampleSyncSourceQuery(), formatSyncTime(time.Now().UTC()))
		convey.So(err, convey.ShouldBeNil)

		err = explainLiveQuery(ctx, sourceDB, `EXPLAIN `+flowcellSyncSourceQuery(), formatSyncTime(time.Now().UTC()))
		convey.So(err, convey.ShouldBeNil)

		err = explainLiveQuery(ctx, sourceDB, `EXPLAIN SELECT `+sampleSelectColumns+` FROM sample WHERE uuid_sample_lims = ? LIMIT 1`, "00000000-0000-0000-0000-000000000000")

		convey.Convey("when the sync and cold-cache sample queries are explained against the live source, then they compile without schema errors", func() {
			convey.So(err, convey.ShouldBeNil)
		})
	})
}

func explainLiveQuery(ctx context.Context, db *sql.DB, query string, args ...any) error {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	return rows.Err()
}

func loadLiveMLWHConfigForTest(t *testing.T) (Config, string) {
	t.Helper()

	if dsn := strings.TrimSpace(os.Getenv("WA_MLWH_DSN")); dsn != "" {
		return Config{
			DSN:      dsn,
			Password: strings.TrimSpace(os.Getenv("WA_MLWH_PASSWORD")),
			Cache:    CacheConfig{Path: filepath.Join(t.TempDir(), "mlwh-live-cache.sqlite")},
		}, ""
	}

	repoRoot, err := findRepoRootForTest()
	if err != nil {
		return Config{}, "skipping live MLWH integration: could not locate repository root to load development env files"
	}

	envFiles := []string{
		filepath.Join(repoRoot, ".env.development.local"),
		filepath.Join(repoRoot, ".env.local"),
		filepath.Join(repoRoot, ".env.development"),
		filepath.Join(repoRoot, ".env"),
	}

	loaded := map[string]string{}
	for _, envFile := range envFiles {
		values, readErr := godotenv.Read(envFile)
		if readErr != nil {
			continue
		}

		for key, value := range values {
			if _, exists := loaded[key]; !exists {
				loaded[key] = value
			}
		}
	}

	dsn := strings.TrimSpace(loaded["WA_MLWH_DSN"])
	if dsn == "" {
		return Config{}, "skipping live MLWH integration: WA_MLWH_DSN not set in environment and no development dotenv file with MLWH credentials was found"
	}

	return Config{
		DSN:      dsn,
		Password: strings.TrimSpace(loaded["WA_MLWH_PASSWORD"]),
		Cache:    CacheConfig{Path: filepath.Join(t.TempDir(), "mlwh-live-cache.sqlite")},
	}, ""
}

func findRepoRootForTest() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	current := wd
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return current, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", os.ErrNotExist
		}

		current = parent
	}
}
