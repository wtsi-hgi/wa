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
)

var studySelectColumns = studyMirrorSelectColumns

func scanStudyRow(scan func(dest ...any) error) (Study, error) {
	study := Study{}
	if err := scan(
		&study.IDStudyTmp,
		&study.IDLims,
		&study.IDStudyLims,
		&study.UUIDStudyLims,
		&study.Name,
		&study.AccessionNumber,
		&study.StudyTitle,
		&study.FacultySponsor,
		&study.State,
		&study.Abstract,
		&study.Abbreviation,
		&study.Description,
		&study.DataReleaseStrategy,
		&study.DataAccessGroup,
		&study.HMDMCNumber,
		&study.Programme,
		&study.Created,
		&study.ReferenceGenome,
		&study.EthicallyApproved,
		&study.StudyType,
		&study.ContainsHumanDNA,
		&study.ContaminatedHumanDNA,
		&study.StudyVisibility,
		&study.EGADACAccessionNumber,
		&study.EGAPolicyAccessionNumber,
		&study.DataReleaseTiming,
	); err != nil {
		return Study{}, err
	}

	return study, nil
}

// AllStudies returns a paged ordered study list from the cache, falling back to
// a direct MLWH query on a cold cache and writing the fetched rows back into
// study_mirror without advancing the sync watermark.
func (c *Client) AllStudies(ctx context.Context, limit, offset int) ([]Study, error) {
	cacheDB := c.readCacheDB()
	if cacheDB == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	studies, err := c.queryAllStudiesCache(ctx, cacheDB, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(studies) > 0 {
		return studies, nil
	}

	warm, err := c.studyCacheWarm(ctx, cacheDB)
	if err != nil {
		return nil, err
	}
	if warm {
		return studies, nil
	}

	return c.queryAllStudiesSource(ctx, limit, offset)
}

func (c *Client) queryAllStudiesCache(ctx context.Context, db *sql.DB, limit, offset int) ([]Study, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_lims = 'SQSCP' ORDER BY id_study_lims LIMIT ? OFFSET ?`,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studies := make([]Study, 0)
	for rows.Next() {
		study, scanErr := scanStudyRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan study cache: %w", ErrUpstreamImpaired, scanErr)
		}

		studies = append(studies, study)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study cache: %w", ErrUpstreamImpaired, err)
	}

	return studies, nil
}

func (c *Client) studyCacheWarm(ctx context.Context, db *sql.DB) (bool, error) {
	var found int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM study_mirror WHERE id_lims = 'SQSCP' LIMIT 1`).Scan(&found)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w: query study cache warmth: %w", ErrUpstreamImpaired, err)
	}

	return c.hasResolverSyncState(ctx, syncTableStudy)
}

func (c *Client) queryAllStudiesSource(ctx context.Context, limit, offset int) ([]Study, error) {
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}
	if c.cache == nil {
		return nil, fmt.Errorf("mlwh: cache client not configured")
	}

	rows, err := c.syncSource.QueryContext(
		ctx,
		`SELECT `+studySelectColumns+`, last_updated FROM study WHERE id_lims = 'SQSCP' ORDER BY id_study_lims LIMIT ? OFFSET ?`,
		limit,
		offset,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: query study source: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	readThroughRows := make([]studySyncRow, 0)
	studies := make([]Study, 0)
	for rows.Next() {
		row, scanErr := scanStudySyncRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan study source: %w", ErrUpstreamImpaired, scanErr)
		}

		readThroughRows = append(readThroughRows, row)
		studies = append(studies, row.Study)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study source: %w", ErrUpstreamImpaired, err)
	}

	if len(readThroughRows) == 0 {
		return studies, nil
	}

	if err = c.upsertAllStudiesReadThrough(ctx, readThroughRows); err != nil {
		return nil, err
	}

	return studies, nil
}

func (c *Client) upsertAllStudiesReadThrough(ctx context.Context, rows []studySyncRow) error {
	tx, err := c.cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin study read-through transaction: %w", ErrUpstreamImpaired, err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, row := range rows {
		exists, existsErr := rowExists(ctx, tx, `SELECT 1 FROM study_mirror WHERE id_study_tmp = ? LIMIT 1`, row.Study.IDStudyTmp)
		if existsErr != nil {
			return fmt.Errorf("%w: query study mirror row existence: %w", ErrUpstreamImpaired, existsErr)
		}
		if exists {
			continue
		}

		if upsertErr := upsertStudyMirror(ctx, tx, c.cache.Dialect(), row); upsertErr != nil {
			return fmt.Errorf("%w: %w", ErrUpstreamImpaired, upsertErr)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit study read-through transaction: %w", ErrUpstreamImpaired, err)
	}

	committed = true

	return nil
}
