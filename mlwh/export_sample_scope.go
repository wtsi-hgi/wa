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
	"strconv"
)

// Supplier names and donor ids legitimately name several Sanger samples. A
// multi-sample export scopes rows to every sample these subqueries select; the
// queries mirror the supplier_name and donor_id resolver steps so the scope is
// exactly the set ResolveSample found ambiguous.
var manyToOneSampleScopeQueries = []string{
	`SELECT sample_mirror.id_sample_tmp FROM sample_mirror WHERE sample_mirror.supplier_name = ? AND sample_mirror.id_lims = 'SQSCP'`,
	`SELECT sample_mirror.id_sample_tmp FROM donor_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = donor_samples.id_sample_tmp WHERE donor_samples.donor_id = ?`,
}

// exportSampleScope is the set of samples a sample-parent export covers: one
// resolved sample, or every sample a many-to-one identifier selects.
type exportSampleScope struct {
	subquery string
	args     []any
}

func singleSampleScope(idSampleTmp int64) exportSampleScope {
	return exportSampleScope{args: []any{idSampleTmp}}
}

func (s exportSampleScope) multiple() bool {
	return s.subquery != ""
}

// predicate restricts column (an id_sample_tmp column) to the scope.
func (s exportSampleScope) predicate(column string) (string, []any) {
	args := append([]any(nil), s.args...)
	if !s.multiple() {
		return column + " = ?", args
	}

	return column + " IN (" + s.subquery + ")", args
}

// manyToOneSampleScope reports the multi-sample scope for an ambiguous raw
// identifier when the ambiguity comes only from supplier_name or donor_id. A
// match on a column that must be unique (name, sanger_sample_id, LIMS id,
// accession) means the identifier is genuinely ambiguous, so ok is false.
func (c *Client) manyToOneSampleScope(ctx context.Context, raw string) (exportSampleScope, bool, error) {
	for _, step := range resolveSampleTextSteps {
		if !step.mustBeUnique() {
			continue
		}

		samples, err := c.querySamplesFromCache(ctx, step.query+" LIMIT 1", raw)
		if err != nil {
			return exportSampleScope{}, false, err
		}
		if len(samples) > 0 {
			return exportSampleScope{}, false, nil
		}
	}

	for _, subquery := range manyToOneSampleScopeQueries {
		count, err := c.queryCount(ctx, `SELECT COUNT(*) FROM (`+subquery+`) AS scoped_samples`, "count samples in scope", raw)
		if err != nil {
			return exportSampleScope{}, false, err
		}
		if count > 1 {
			return exportSampleScope{subquery: subquery, args: []any{raw}}, true, nil
		}
	}

	return exportSampleScope{}, false, nil
}

// exportLanesFrom selects the distinct Illumina run/lane/tag products of the
// scoped samples, one row per (product, sample), so rows from different samples
// stay distinguishable and the order is deterministic across samples. With
// withLibraryType the derived table also carries each row's library type from
// the product's flowcell; grouping on the same four keys keeps one row per
// (product, sample).
func exportLanesFrom(scope exportSampleScope, withLibraryType bool) (string, []any) {
	predicate, args := scope.predicate("ipm.id_sample_tmp")
	if withLibraryType {
		return ` FROM (SELECT ipm.id_run, ipm.position, ipm.tag_index, ipm.id_sample_tmp, ` + exportLibraryTypeAggregate +
			` AS library_type FROM iseq_product_metrics_mirror ipm` + exportLibraryTypeJoin + ` WHERE ` + predicate +
			` GROUP BY ipm.id_run, ipm.position, ipm.tag_index, ipm.id_sample_tmp) AS lanes`, args
	}

	return ` FROM (SELECT DISTINCT ipm.id_run, ipm.position, ipm.tag_index, ipm.id_sample_tmp, '' AS library_type FROM iseq_product_metrics_mirror ipm WHERE ` +
		predicate + `) AS lanes`, args
}

// multiSampleStudiesForExport lists the distinct studies linked to any scoped
// sample. A single-sample export keeps sampleStudiesForExport, whose
// no-studies-means-not-found contract predates multi-sample scopes.
func (c *Client) multiSampleStudiesForExport(ctx context.Context, scope exportSampleScope, limit, offset int) ([]Study, int, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	predicate, args := scope.predicate("library_samples.id_sample_tmp")
	from := ` FROM library_samples INNER JOIN study_mirror ON study_mirror.id_study_lims = library_samples.id_study_lims WHERE ` +
		predicate + ` AND study_mirror.id_lims = 'SQSCP'`
	query := `SELECT DISTINCT ` + qualifySelectColumns("study_mirror", studyMirrorSelectColumns) + from +
		` ORDER BY study_mirror.id_study_lims LIMIT ? OFFSET ?`

	studies, err := c.queryStudySearch(ctx, db, query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}

	total, err := c.queryCount(ctx, `SELECT COUNT(*) FROM (SELECT DISTINCT study_mirror.id_study_tmp`+from+`) AS scoped_studies`, "count export studies", args...)
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		if err = c.requireAnySyncState(ctx, syncTableStudy, syncTableIseqFlowcell); err != nil {
			return nil, 0, err
		}
	}

	return studies, total, nil
}

// exportLaneRow is one distinct run/lane/tag product of one sample.
type exportLaneRow struct {
	IDRun, Position, TagIndex                       int
	Name, SupplierName, SangerSampleID, AccessionNo string
	LibraryType                                     string
}

func (row exportLaneRow) cell(column string) string {
	switch column {
	case "id_run":
		return strconv.Itoa(row.IDRun)
	case "lane":
		return strconv.Itoa(row.Position)
	case "tag_index":
		return strconv.Itoa(row.TagIndex)
	case "name":
		return row.Name
	case "supplier_name":
		return row.SupplierName
	case "sanger_sample_id":
		return row.SangerSampleID
	case "accession_number":
		return row.AccessionNo
	case exportColumnLibraryType:
		return row.LibraryType
	default:
		return ""
	}
}

func queryExportLaneRows(ctx context.Context, db *sql.DB, query string, args ...any) ([]exportLaneRow, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query export lanes: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	lanes := make([]exportLaneRow, 0)
	for rows.Next() {
		var lane exportLaneRow
		if err = rows.Scan(&lane.IDRun, &lane.Position, &lane.TagIndex, &lane.Name, &lane.SupplierName, &lane.SangerSampleID, &lane.AccessionNo, &lane.LibraryType); err != nil {
			return nil, fmt.Errorf("%w: scan export lanes: %w", ErrUpstreamImpaired, err)
		}

		lanes = append(lanes, lane)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query export lanes: %w", ErrUpstreamImpaired, err)
	}

	return lanes, nil
}

func projectLanes(lanes []exportLaneRow, columns []exportColumn) [][]string {
	rows := make([][]string, len(lanes))
	for rowIndex, lane := range lanes {
		rows[rowIndex] = make([]string, len(columns))
		for columnIndex, column := range columns {
			rows[rowIndex][columnIndex] = lane.cell(column.Name)
		}
	}

	return rows
}

// mustBeUnique reports whether the step's identifier names at most one sample,
// so several matches mean real ambiguity rather than a many-to-one identifier.
func (step sampleLookupStep) mustBeUnique() bool {
	return step.kind != KindSupplierName
}

func (c *Client) resolveExportSampleParent(ctx context.Context, parentID string, multiSample bool) (exportParent, error) {
	match, err := c.ResolveSample(ctx, parentID)
	if err == nil {
		return exportParent{
			Value:       match.Sample.IDSampleTmp,
			Canonical:   match.Sample.Name,
			Sample:      match.Sample,
			SampleScope: singleSampleScope(match.Sample.IDSampleTmp),
		}, nil
	}
	if !multiSample || !errors.Is(err, ErrAmbiguous) {
		return exportParent{}, err
	}

	scope, ok, scopeErr := c.manyToOneSampleScope(ctx, parentID)
	if scopeErr != nil {
		return exportParent{}, scopeErr
	}
	if !ok {
		return exportParent{}, err
	}

	return exportParent{Canonical: parentID, SampleScope: scope}, nil
}

func (c *Client) exportLanes(ctx context.Context, plan exportPlan, parent exportParent) (ExportResult, error) {
	db := c.readCacheDB()
	if db == nil {
		return ExportResult{}, fmt.Errorf("mlwh: cache reader not configured")
	}

	from, args := exportLanesFrom(parent.SampleScope, exportColumnsNeedLibraryType(plan.columns))
	query := `SELECT lanes.id_run, lanes.position, lanes.tag_index, COALESCE(sm.name, ''), COALESCE(sm.supplier_name, ''), ` +
		`COALESCE(sm.sanger_sample_id, ''), COALESCE(sm.accession_number, ''), lanes.library_type` + from +
		` LEFT JOIN sample_mirror sm ON sm.id_sample_tmp = lanes.id_sample_tmp` +
		` ORDER BY lanes.id_run, lanes.position, lanes.tag_index, lanes.id_sample_tmp LIMIT ? OFFSET ?`

	lanes, err := queryExportLaneRows(ctx, db, query, append(args, plan.limit, plan.offset)...)
	if err != nil {
		return ExportResult{}, err
	}

	countFrom, countArgs := exportLanesFrom(parent.SampleScope, false)
	total, err := c.queryCount(ctx, `SELECT COUNT(*)`+countFrom, "count export lanes", countArgs...)
	if err != nil {
		return ExportResult{}, err
	}
	if total == 0 {
		if err = c.requireAnySyncState(ctx, syncTableIseqProductMetrics); err != nil {
			return ExportResult{}, err
		}
	}

	return buildExportResult(plan, projectLanes(lanes, plan.columns), total), nil
}
