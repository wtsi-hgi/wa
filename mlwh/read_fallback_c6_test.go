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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

type failReadFallbackQuerier struct {
	t *testing.T
}

func (q failReadFallbackQuerier) QueryContext(_ context.Context, query string, args ...any) (*sql.Rows, error) {
	q.t.Helper()
	q.t.Fatalf("unexpected live-MLWH read query: %s args=%v", query, args)

	return nil, fmt.Errorf("unexpected live-MLWH read query")
}

func TestReadPathsAllowNilSyncSource(t *testing.T) {
	convey.Convey("C6.1: Given a cache-backed client with a nil sync source", t, func() {
		client := newC6ReadOnlyClient(t, nil)
		defer func() { convey.So(client.cache.Close(), convey.ShouldBeNil) }()

		assertC6ReadPaths(t, client)
	})
}

func TestReadPathsNeverTouchSyncSource(t *testing.T) {
	convey.Convey("C6.2: Given a sync source that fails any QueryContext call", t, func() {
		client := newC6ReadOnlyClient(t, failReadFallbackQuerier{t: t})
		defer func() { convey.So(client.cache.Close(), convey.ShouldBeNil) }()

		assertC6ReadPaths(t, client)
	})
}

func TestAllStudiesPartialSyncEmptyCacheReturnsNeverSynced(t *testing.T) {
	convey.Convey("C6.3: Given an empty study_mirror and no study sync_state row on a partially synced cache", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()

		seedSyncState(t, cache.DB(), syncTableSample, time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC))

		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		studies, err := client.AllStudies(context.Background(), 100, 0)

		convey.So(studies, convey.ShouldResemble, []Study{})
		convey.So(errors.Is(err, ErrNotFound), convey.ShouldBeTrue)
		convey.So(errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
	})
}

func newC6ReadOnlyClient(t *testing.T, syncSource Querier) *Client {
	t.Helper()

	cache := openSQLiteSyncTestCache(t)
	for _, table := range []string{
		syncTableSample,
		syncTableStudy,
		syncTableIseqFlowcell,
		syncTableIseqProductMetrics,
		syncTableSeqProductIRODSLocations,
	} {
		seedSyncState(t, cache.DB(), table, time.Date(2026, time.May, 11, 11, 0, 0, 0, time.UTC))
	}

	return &Client{cache: cache, cacheReader: cacheReadDB(cache), syncSource: syncSource}
}

func assertC6ReadPaths(t *testing.T, client *Client) {
	t.Helper()

	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{name: "ResolveSample", call: func() error { _, err := client.ResolveSample(ctx, "missing-sample"); return err }},
		{name: "ResolveStudy", call: func() error { _, err := client.ResolveStudy(ctx, "missing-study"); return err }},
		{name: "ResolveLibrary", call: func() error { _, err := client.ResolveLibrary(ctx, "missing-library"); return err }},
		{name: "ResolveRun", call: func() error { _, err := client.ResolveRun(ctx, "12345"); return err }},
		{name: "SamplesForStudy", call: func() error { _, err := client.SamplesForStudy(ctx, "6568", 100, 0); return err }},
		{name: "SamplesForLibrary", call: func() error { _, err := client.SamplesForLibrary(ctx, "Standard", "6568", 100, 0); return err }},
		{name: "SamplesForLibraryType", call: func() error { _, err := client.SamplesForLibraryType(ctx, "Standard", 100, 0); return err }},
		{name: "SamplesForRun", call: func() error { _, err := client.SamplesForRun(ctx, "12345", 100, 0); return err }},
		{name: "LibrariesForStudy", call: func() error { _, err := client.LibrariesForStudy(ctx, "6568", 100, 0); return err }},
		{name: "RunsForStudy", call: func() error { _, err := client.RunsForStudy(ctx, "6568", 100, 0); return err }},
		{name: "LanesForSample", call: func() error { _, err := client.LanesForSample(ctx, "missing-sample", 100, 0); return err }},
		{name: "IRODSPathsForSample", call: func() error { _, err := client.IRODSPathsForSample(ctx, "missing-sample", 100, 0); return err }},
		{name: "IRODSPathsForStudy", call: func() error { _, err := client.IRODSPathsForStudy(ctx, "6568", 100, 0); return err }},
		{name: "AllStudies", call: func() error { _, err := client.AllStudies(ctx, 100, 0); return err }},
	}

	for _, tc := range cases {
		err := tc.call()
		convey.So(err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrCacheNeverSynced), convey.ShouldBeTrue)
		if err != nil {
			convey.So(errors.Is(err, ErrUpstreamImpaired), convey.ShouldBeFalse)
			convey.So(err.Error(), convey.ShouldNotContainSubstring, "sync source not configured")
		}
	}
}
