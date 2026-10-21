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
	"regexp"
	"slices"
	"strings"
	"time"
)

// syncOutcomeTable records how each table's most recent sync attempt ended.
// It is a table of its own rather than extra sync_state columns because a wa
// release from before it validates sync_state's exact column set on open and
// rebuilds every mirror on a mismatch, while it ignores tables it does not
// know. Rolling back, or an older wa opening the same cache, is then safe.
//
// Rules:
//   - last_success_at is set when a table's sync completes without error, and
//     only then.
//   - last_error and last_error_at are set when a table's sync fails or is
//     skipped, and cleared by its next successful sync; a failure keeps the
//     earlier last_success_at.
//   - blocked_by lists the failed dependency tables when the table was skipped
//     because of them, and is cleared by its next attempt.
const syncOutcomeTable = "sync_outcome"

const (
	// syncErrorMaxBytes caps a stored last_error.
	syncErrorMaxBytes = 1000

	// syncErrorTruncatedSuffix ends a last_error cut to syncErrorMaxBytes.
	syncErrorTruncatedSuffix = " [truncated]"

	syncOutcomeWriteTimeout = 30 * time.Second

	syncErrorRedacted = "[redacted]"

	// syncSecretMinRedactLength is the shortest known secret replaced where it
	// appears verbatim; a shorter one would mangle ordinary words, while the
	// patterns still redact it in DSNs and parameters.
	syncSecretMinRedactLength = 4
)

var (
	syncOutcomeSuccessColumns = []string{"table_name", "last_success_at", "last_error", "last_error_at", "blocked_by"}
	syncOutcomeFailureColumns = []string{"table_name", "last_error", "last_error_at", "blocked_by"}

	// syncErrorUserinfoPattern matches the password in user:password@ as it
	// appears in MySQL DSNs and URLs. The password runs to the last @ before
	// the next space, slash or quote, so a password containing @ is redacted
	// whole.
	syncErrorUserinfoPattern = regexp.MustCompile(`([^\s:/@'"()]+):[^\s/'"]+@`)

	// syncErrorSecretParamPattern matches secret-looking key=value parameters,
	// with the value bare or quoted.
	syncErrorSecretParamPattern = regexp.MustCompile(
		`(?i)\b(password|passwd|pwd|secret|token)=(?:"[^"]*"|'[^']*'|[^\s&;,]+)`)

	// syncErrorSecretJSONPattern matches secret-looking JSON string fields.
	syncErrorSecretJSONPattern = regexp.MustCompile(
		`(?i)("(?:password|passwd|pwd|secret|token)"\s*:\s*)"(?:[^"\\]|\\.)*"`)
)

// ensureSyncOutcomeSchema creates sync_outcome on a cache that predates it,
// then backfills last_success_at from last_run for tables whose sync_state has
// no resume cursor (their last sync ran to the end) and no sync_outcome row.
// The backfill runs only when a read-only check finds such tables, so opening
// a cache that already has its rows never waits for a sync's write
// transaction (CREATE TABLE IF NOT EXISTS on an existing table does not wait
// either). Both writes are idempotent and safe when several processes open
// the cache at once: CREATE TABLE IF NOT EXISTS is atomic, and the backfill
// ignores rows that exist.
func ensureSyncOutcomeSchema(ctx context.Context, db *sql.DB, dialect string) error {
	body, err := cacheSchemaFS.ReadFile(fmt.Sprintf("cache_schema/%s/%s.sql", dialect, syncOutcomeTable))
	if err != nil {
		return fmt.Errorf("mlwh: load %s schema: %w", syncOutcomeTable, err)
	}

	if _, err = db.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("mlwh: create %s: %w", syncOutcomeTable, err)
	}

	var missing int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_state s WHERE s.resume_cursor IS NULL
		AND NOT EXISTS (SELECT 1 FROM sync_outcome o WHERE o.table_name = s.table_name)`).Scan(&missing); err != nil {
		return fmt.Errorf("mlwh: check %s backfill: %w", syncOutcomeTable, err)
	}

	if missing == 0 {
		return nil
	}

	insert := "INSERT OR IGNORE"
	if dialect == "mysql" {
		insert = "INSERT IGNORE"
	}

	if _, err = db.ExecContext(ctx, insert+` INTO sync_outcome(table_name, last_success_at)
		SELECT table_name, last_run FROM sync_state WHERE resume_cursor IS NULL`); err != nil {
		return fmt.Errorf("mlwh: backfill %s: %w", syncOutcomeTable, err)
	}

	return nil
}

// failedSyncDependencies returns, sorted, the tables in dependencies whose
// sync failed.
func failedSyncDependencies(results []syncResult, dependencies map[string]struct{}) []string {
	var failed []string
	for _, result := range results {
		if result.err == nil {
			continue
		}

		if _, ok := dependencies[result.report.Table]; ok {
			failed = append(failed, result.report.Table)
		}
	}

	slices.Sort(failed)

	return failed
}

// syncOutcome is one table's sync_outcome row; zero values mean NULL.
type syncOutcome struct {
	LastSuccessAt time.Time
	LastError     string
	LastErrorAt   time.Time
	BlockedBy     []string
}

// readSyncOutcome reads a table's sync_outcome row, returning the zero value
// when it has none.
func readSyncOutcome(ctx context.Context, db *sql.DB, table string) (syncOutcome, error) {
	var lastSuccess, lastError, lastErrorAt, blockedBy sql.NullString

	err := db.QueryRowContext(ctx,
		`SELECT last_success_at, last_error, last_error_at, blocked_by FROM sync_outcome WHERE table_name = ?`, table,
	).Scan(&lastSuccess, &lastError, &lastErrorAt, &blockedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return syncOutcome{}, nil
	}

	if err != nil {
		return syncOutcome{}, fmt.Errorf("%w: query sync outcome for %s: %w", ErrUpstreamImpaired, table, err)
	}

	outcome := syncOutcome{LastError: lastError.String}
	if outcome.LastSuccessAt, err = parseNullableSyncTime(lastSuccess); err != nil {
		return syncOutcome{}, fmt.Errorf("mlwh: parse sync outcome last_success_at for %s: %w", table, err)
	}

	if outcome.LastErrorAt, err = parseNullableSyncTime(lastErrorAt); err != nil {
		return syncOutcome{}, fmt.Errorf("mlwh: parse sync outcome last_error_at for %s: %w", table, err)
	}

	if blockedBy.String != "" {
		outcome.BlockedBy = strings.Split(blockedBy.String, ",")
	}

	return outcome, nil
}

func parseNullableSyncTime(value sql.NullString) (time.Time, error) {
	if !value.Valid || value.String == "" {
		return time.Time{}, nil
	}

	return parseSyncTimeString(value.String)
}

// recordSyncOutcome records how a table's sync attempt ended.
func (c *Client) recordSyncOutcome(ctx context.Context, table string, syncErr error) error {
	if syncErr != nil {
		return c.recordSyncFailure(ctx, table, syncErr, nil)
	}

	return c.writeSyncOutcome(ctx, table, syncOutcomeSuccessColumns,
		[]any{table, formatSyncTime(c.outcomeNow()), nil, nil, nil})
}

// recordSyncFailure records a failed or skipped sync of table, keeping its
// last_success_at. blockedBy names the failed dependencies of a skipped table.
func (c *Client) recordSyncFailure(ctx context.Context, table string, syncErr error, blockedBy []string) error {
	var blocked any
	if len(blockedBy) > 0 {
		blocked = strings.Join(blockedBy, ",")
	}

	return c.writeSyncOutcome(ctx, table, syncOutcomeFailureColumns,
		[]any{table, c.syncErrorText(syncErr), formatSyncTime(c.outcomeNow()), blocked})
}

// recordSyncFailureForAllTables records syncErr against every sync table, for
// a failure that stops the whole sync, such as an unreachable source.
func (c *Client) recordSyncFailureForAllTables(ctx context.Context, syncErr error) error {
	var errs []error
	for _, table := range supportedSyncTables {
		errs = append(errs, c.recordSyncFailure(ctx, table, syncErr, nil))
	}

	return errors.Join(errs...)
}

// writeSyncOutcome upserts the given sync_outcome columns. It runs even when
// ctx is cancelled, so an interrupted sync still records its failure.
func (c *Client) writeSyncOutcome(ctx context.Context, table string, columns []string, args []any) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), syncOutcomeWriteTimeout)
	defer cancel()

	stmt := buildUpsertStatement(c.cache.Dialect(), syncOutcomeTable, columns, []string{"table_name"})

	err := withSyncWriteTx(writeCtx, c.cache, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(writeCtx, stmt, args...)

		return execErr
	})
	if err != nil {
		return fmt.Errorf("mlwh: record sync outcome for %s: %w", table, err)
	}

	return nil
}

func (c *Client) outcomeNow() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}

	return time.Now().UTC()
}

// syncErrorText returns err's message fit for storing and serving: the
// client's known secrets, DSN or URL passwords and secret-looking parameters
// are redacted, and the result is capped at syncErrorMaxBytes.
func (c *Client) syncErrorText(err error) string {
	text := err.Error()

	for _, secret := range c.syncSecrets {
		if len(secret) >= syncSecretMinRedactLength {
			text = strings.ReplaceAll(text, secret, syncErrorRedacted)
		}
	}

	text = syncErrorUserinfoPattern.ReplaceAllString(text, "$1:"+syncErrorRedacted+"@")
	text = syncErrorSecretParamPattern.ReplaceAllString(text, "$1="+syncErrorRedacted)
	text = syncErrorSecretJSONPattern.ReplaceAllString(text, `$1"`+syncErrorRedacted+`"`)

	if len(text) <= syncErrorMaxBytes {
		return text
	}

	return truncateUTF8(text, syncErrorMaxBytes-len(syncErrorTruncatedSuffix)) + syncErrorTruncatedSuffix
}
