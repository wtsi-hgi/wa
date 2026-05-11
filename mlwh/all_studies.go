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

type nullableStudyScanFields struct {
	idLims                   sql.NullString
	idStudyLims              sql.NullString
	uuidStudyLims            sql.NullString
	name                     sql.NullString
	accessionNumber          sql.NullString
	studyTitle               sql.NullString
	facultySponsor           sql.NullString
	state                    sql.NullString
	dataReleaseStrategy      sql.NullString
	dataAccessGroup          sql.NullString
	programme                sql.NullString
	referenceGenome          sql.NullString
	ethicallyApproved        sql.NullBool
	studyType                sql.NullString
	containsHumanDNA         sql.NullBool
	contaminatedHumanDNA     sql.NullBool
	studyVisibility          sql.NullString
	egaDACAccessionNumber    sql.NullString
	egaPolicyAccessionNumber sql.NullString
	dataReleaseTiming        sql.NullString
}

func studyScanTargets(study *Study) ([]any, func()) {
	nullable := &nullableStudyScanFields{}

	return []any{
			&study.IDStudyTmp,
			&nullable.idLims,
			&nullable.idStudyLims,
			&nullable.uuidStudyLims,
			&nullable.name,
			&nullable.accessionNumber,
			&nullable.studyTitle,
			&nullable.facultySponsor,
			&nullable.state,
			&nullable.dataReleaseStrategy,
			&nullable.dataAccessGroup,
			&nullable.programme,
			&nullable.referenceGenome,
			&nullable.ethicallyApproved,
			&nullable.studyType,
			&nullable.containsHumanDNA,
			&nullable.contaminatedHumanDNA,
			&nullable.studyVisibility,
			&nullable.egaDACAccessionNumber,
			&nullable.egaPolicyAccessionNumber,
			&nullable.dataReleaseTiming,
		}, func() {
			study.IDLims = nullStringValue(nullable.idLims)
			study.IDStudyLims = nullStringValue(nullable.idStudyLims)
			study.UUIDStudyLims = nullStringValue(nullable.uuidStudyLims)
			study.Name = nullStringValue(nullable.name)
			study.AccessionNumber = nullStringValue(nullable.accessionNumber)
			study.StudyTitle = nullStringValue(nullable.studyTitle)
			study.FacultySponsor = nullStringValue(nullable.facultySponsor)
			study.State = nullStringValue(nullable.state)
			study.DataReleaseStrategy = nullStringValue(nullable.dataReleaseStrategy)
			study.DataAccessGroup = nullStringValue(nullable.dataAccessGroup)
			study.Programme = nullStringValue(nullable.programme)
			study.ReferenceGenome = nullStringValue(nullable.referenceGenome)
			study.EthicallyApproved = nullable.ethicallyApproved.Valid && nullable.ethicallyApproved.Bool
			study.StudyType = nullStringValue(nullable.studyType)
			study.ContainsHumanDNA = nullable.containsHumanDNA.Valid && nullable.containsHumanDNA.Bool
			study.ContaminatedHumanDNA = nullable.contaminatedHumanDNA.Valid && nullable.contaminatedHumanDNA.Bool
			study.StudyVisibility = nullStringValue(nullable.studyVisibility)
			study.EGADACAccessionNumber = nullStringValue(nullable.egaDACAccessionNumber)
			study.EGAPolicyAccessionNumber = nullStringValue(nullable.egaPolicyAccessionNumber)
			study.DataReleaseTiming = nullStringValue(nullable.dataReleaseTiming)
		}
}

func nullStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}

	return ""
}

func scanStudyRow(scan func(dest ...any) error) (Study, error) {
	study := Study{}
	targets, apply := studyScanTargets(&study)
	if err := scan(targets...); err != nil {
		return Study{}, err
	}
	apply()

	return study, nil
}

// AllStudies returns a paged ordered study list.
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
		return []Study{}, nil
	}

	return []Study{}, errors.Join(ErrNotFound, ErrCacheNeverSynced)
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

	return c.hasSyncState(ctx, syncTableStudy)
}
