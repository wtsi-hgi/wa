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
	"time"
)

// sourceQueryPrepareTimeout bounds each PREPARE so one stalled statement
// cannot hang a source check.
const sourceQueryPrepareTimeout = 30 * time.Second

// SourceQueryCheck is the verdict for one sync source query prepared against
// the upstream MLWH. Error holds the server's message when OK is false.
type SourceQueryCheck struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// PrepareSyncSourceQueries PREPAREs each query against db and closes the
// statement without executing it, so the server validates every table and
// column reference while reading and changing nothing. It returns one check
// per query, in input order. Pass AllSyncSourceQueries() to check everything
// sync reads.
func PrepareSyncSourceQueries(ctx context.Context, db *sql.DB, queries []SyncSourceQuery) []SourceQueryCheck {
	checks := make([]SourceQueryCheck, 0, len(queries))
	for _, query := range queries {
		check := SourceQueryCheck{Name: query.Name, OK: true}
		if err := prepareAndCloseQuery(ctx, db, query.Query); err != nil {
			check.OK = false
			check.Error = err.Error()
		}

		checks = append(checks, check)
	}

	return checks
}

func prepareAndCloseQuery(ctx context.Context, db *sql.DB, query string) error {
	ctx, cancel := context.WithTimeout(ctx, sourceQueryPrepareTimeout)
	defer cancel()

	stmt, err := db.PrepareContext(ctx, query)
	if err != nil {
		return err
	}

	return stmt.Close()
}
