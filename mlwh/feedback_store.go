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
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const feedbackSchema = `
CREATE TABLE IF NOT EXISTS feedback (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at         TEXT NOT NULL,
    category           TEXT NOT NULL,
    description        TEXT NOT NULL,
    user_request       TEXT NOT NULL DEFAULT '',
    tools_tried        TEXT NOT NULL DEFAULT '[]',
    mcp_server_version TEXT NOT NULL DEFAULT '',
    wa_api_version     TEXT NOT NULL DEFAULT '',
    transport          TEXT NOT NULL DEFAULT '',
    client_name        TEXT NOT NULL DEFAULT '',
    client_version     TEXT NOT NULL DEFAULT '',
    client_user_agent  TEXT NOT NULL DEFAULT '',
    remote_addr        TEXT NOT NULL DEFAULT '',
    acknowledged_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS feedback_ack_id ON feedback (acknowledged_at, id);
`

const feedbackColumns = `id, created_at, category, description, user_request, tools_tried,
    mcp_server_version, wa_api_version, transport, client_name, client_version,
    client_user_agent, remote_addr, acknowledged_at`

// FeedbackStore persists agent feedback reports in their own SQLite database,
// independent of the MLWH cache and of sync.
type FeedbackStore struct {
	db  *sql.DB
	now func() time.Time
}

// OpenFeedbackStore opens, creating if needed, the SQLite feedback database
// at path.
func OpenFeedbackStore(ctx context.Context, path string) (*FeedbackStore, error) {
	db, err := sql.Open("sqlite", sqliteWritableDSN(path))
	if err != nil {
		return nil, fmt.Errorf("mlwh: open feedback store: %w", err)
	}

	if _, err = db.ExecContext(ctx, feedbackSchema); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("mlwh: create feedback schema: %w", err)
	}

	return &FeedbackStore{db: db, now: time.Now}, nil
}

// Close closes the underlying database.
func (s *FeedbackStore) Close() error {
	return s.db.Close()
}

// Add stores sub as received, without validating it, and returns the stored
// report. Nil ToolsTried is stored as [].
func (s *FeedbackStore) Add(ctx context.Context, sub FeedbackSubmission, remoteAddr string) (FeedbackReport, error) {
	tools := sub.ToolsTried
	if tools == nil {
		tools = []string{}
	}

	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return FeedbackReport{}, fmt.Errorf("mlwh: encode feedback tools_tried: %w", err)
	}

	report := FeedbackReport{
		CreatedAt:        s.timestamp(),
		Category:         sub.Category,
		Description:      sub.Description,
		UserRequest:      sub.UserRequest,
		ToolsTried:       tools,
		MCPServerVersion: sub.MCPServerVersion,
		WAAPIVersion:     sub.WAAPIVersion,
		Transport:        sub.Transport,
		ClientName:       sub.ClientName,
		ClientVersion:    sub.ClientVersion,
		ClientUserAgent:  sub.ClientUserAgent,
		RemoteAddr:       remoteAddr,
	}

	result, err := s.db.ExecContext(ctx, `INSERT INTO feedback (created_at, category, description,
    user_request, tools_tried, mcp_server_version, wa_api_version, transport, client_name,
    client_version, client_user_agent, remote_addr) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.CreatedAt, string(report.Category), report.Description, report.UserRequest,
		string(toolsJSON), report.MCPServerVersion, report.WAAPIVersion, report.Transport,
		report.ClientName, report.ClientVersion, report.ClientUserAgent, report.RemoteAddr)
	if err != nil {
		return FeedbackReport{}, fmt.Errorf("mlwh: insert feedback: %w", err)
	}

	report.ID, err = result.LastInsertId()
	if err != nil {
		return FeedbackReport{}, fmt.Errorf("mlwh: feedback insert id: %w", err)
	}

	return report, nil
}

func (s *FeedbackStore) timestamp() string {
	return s.now().UTC().Format(time.RFC3339)
}

// List returns the reports matching filter, newest (highest id) first. Total
// counts all matching rows; NextOffset is -1 on the last page.
func (s *FeedbackStore) List(ctx context.Context, filter FeedbackFilter, limit, offset int) (Page[FeedbackReport], error) {
	where, args := feedbackFilterClause(filter)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Page[FeedbackReport]{}, fmt.Errorf("mlwh: begin feedback list: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	page := Page[FeedbackReport]{Items: []FeedbackReport{}, NextOffset: -1}

	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM feedback"+where, args...).Scan(&page.Total)
	if err != nil {
		return Page[FeedbackReport]{}, fmt.Errorf("mlwh: count feedback: %w", err)
	}

	rows, err := tx.QueryContext(ctx, "SELECT "+feedbackColumns+" FROM feedback"+where+
		" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return Page[FeedbackReport]{}, fmt.Errorf("mlwh: list feedback: %w", err)
	}

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		report, scanErr := scanFeedbackReport(rows)
		if scanErr != nil {
			return Page[FeedbackReport]{}, scanErr
		}

		page.Items = append(page.Items, report)
	}

	if err = rows.Err(); err != nil {
		return Page[FeedbackReport]{}, fmt.Errorf("mlwh: list feedback: %w", err)
	}

	if next := offset + len(page.Items); next < page.Total {
		page.NextOffset = next
	}

	return page, nil
}

func feedbackFilterClause(filter FeedbackFilter) (string, []any) {
	var (
		conds []string
		args  []any
	)

	if filter.Acknowledged != nil {
		if *filter.Acknowledged {
			conds = append(conds, "acknowledged_at != ''")
		} else {
			conds = append(conds, "acknowledged_at = ''")
		}
	}

	if filter.Category != "" {
		conds = append(conds, "category = ?")
		args = append(args, string(filter.Category))
	}

	if len(conds) == 0 {
		return "", nil
	}

	return " WHERE " + strings.Join(conds, " AND "), args
}

func scanFeedbackReport(row rowScanner) (FeedbackReport, error) {
	var (
		report    FeedbackReport
		category  string
		toolsJSON string
	)

	err := row.Scan(&report.ID, &report.CreatedAt, &category, &report.Description, &report.UserRequest,
		&toolsJSON, &report.MCPServerVersion, &report.WAAPIVersion, &report.Transport, &report.ClientName,
		&report.ClientVersion, &report.ClientUserAgent, &report.RemoteAddr, &report.AcknowledgedAt)
	if err != nil {
		return FeedbackReport{}, fmt.Errorf("mlwh: scan feedback: %w", err)
	}

	report.ToolsTried = []string{}
	if err = json.Unmarshal([]byte(toolsJSON), &report.ToolsTried); err != nil {
		return FeedbackReport{}, fmt.Errorf("mlwh: decode feedback %d tools_tried: %w", report.ID, err)
	}

	report.Category = FeedbackCategory(category)
	report.Acknowledged = report.AcknowledgedAt != ""

	return report, nil
}

// SetAcknowledged marks report id acknowledged or not and returns it.
// Acknowledging an already acknowledged report keeps its original time.
// A missing id returns an error wrapping ErrNotFound.
func (s *FeedbackStore) SetAcknowledged(ctx context.Context, id int64, acknowledged bool) (FeedbackReport, error) {
	ackAt := ""
	if acknowledged {
		ackAt = s.timestamp()
	}

	row := s.db.QueryRowContext(ctx, `UPDATE feedback
    SET acknowledged_at = CASE WHEN ? = '' OR acknowledged_at = '' THEN ? ELSE acknowledged_at END
    WHERE id = ? RETURNING `+feedbackColumns, ackAt, ackAt, id)

	report, err := scanFeedbackReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return FeedbackReport{}, fmt.Errorf("mlwh: feedback %d: %w", id, ErrNotFound)
	}

	if err != nil {
		return FeedbackReport{}, fmt.Errorf("mlwh: acknowledge feedback %d: %w", id, err)
	}

	return report, nil
}

// Delete removes report id. A missing id returns an error wrapping
// ErrNotFound.
func (s *FeedbackStore) Delete(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM feedback WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("mlwh: delete feedback %d: %w", id, err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("mlwh: delete feedback %d: %w", id, err)
	}

	if n == 0 {
		return fmt.Errorf("mlwh: feedback %d: %w", id, ErrNotFound)
	}

	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}
