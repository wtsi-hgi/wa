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
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	// RunListingDefaultLimit is the default bounded page size for the global
	// run listing.
	RunListingDefaultLimit = 100
	// RunListingMaxLimit is the largest accepted page size for the global run
	// listing.
	RunListingMaxLimit = 1000

	runDateBasisRunComplete     = "run complete"
	runDateBasisRunArchived     = "run archived"
	runDateBasisPacBioComplete  = "run_complete"
	runDateBasisONTLoadTime     = "warehouse load time - not a true sequencing date"
	runManufacturerElembio      = "Element Biosciences"
	runManufacturerUltimagen    = "Ultima Genomics"
	runManufacturerONT          = "Oxford Nanopore"
	sequencingAggregateRuns     = "runs"
	sequencingAggregateSamples  = "samples"
	sequencingAggregateProducts = "products"
	sequencingAggregateCreated  = "iRODS created"
	monthlyRunIlluminaSQLPrefix = `SELECT SUBSTR(s.normalised_date, 1, 7) AS month, COUNT(DISTINCT s.id_run) AS count ` +
		`FROM iseq_run_status_mirror AS s ` +
		`INNER JOIN iseq_run_status_dict_mirror AS d ON d.id_run_status_dict = s.id_run_status_dict ` +
		`WHERE s.normalised_date <> '' AND d.description = 'run complete' ` +
		`AND EXISTS (SELECT 1 FROM iseq_product_metrics_mirror AS ipm WHERE ipm.id_run = s.id_run)`
	monthlyRunPacBioSQLPrefix = `SELECT SUBSTR(pb.normalised_date, 1, 7) AS month, COUNT(DISTINCT pb.pac_bio_run_name) AS count ` +
		`FROM pac_bio_run_well_metrics_mirror AS pb WHERE pb.normalised_date <> ''`
	monthlyRunONTSQLPrefix = `SELECT SUBSTR(ont.normalised_date, 1, 7) AS month, COUNT(DISTINCT ont.experiment_name) AS count ` +
		`FROM oseq_flowcell_mirror AS ont WHERE ont.normalised_date <> ''`
	monthlyRunUltimagenSQLPrefix = `SELECT SUBSTR(ur.normalised_date, 1, 7) AS month, COUNT(DISTINCT ur.id_run) AS count ` +
		`FROM useq_run_metrics_mirror AS ur WHERE ur.normalised_date <> ''`
	monthlyRunElembioSQLPrefix = `SELECT SUBSTR(er.normalised_date, 1, 7) AS month, COUNT(DISTINCT er.id_run) AS count ` +
		`FROM eseq_run_lane_metrics_mirror AS er WHERE er.normalised_date <> ''`
	monthlyRunSQLSuffix = ` GROUP BY SUBSTR(%s.normalised_date, 1, 7) ORDER BY month`
)

type runAggregationPlatformSpec struct {
	platform     string
	manufacturer string
	dateBasis    string
	alias        string
	indexName    string
	queryPrefix  string
	syncTables   []string
}

var runAggregationPlatformSpecs = []runAggregationPlatformSpec{
	{
		platform:     platformIllumina,
		manufacturer: platformIllumina,
		dateBasis:    runDateBasisRunComplete,
		alias:        "s",
		indexName:    "iseq_run_status_mirror_normalised_date_idx",
		queryPrefix:  monthlyRunIlluminaSQLPrefix,
		syncTables:   []string{syncTableIseqRunStatus, syncTableIseqRunStatusDict, syncTableIseqProductMetrics},
	},
	{
		platform:     platformPacBio,
		manufacturer: platformPacBio,
		dateBasis:    runDateBasisPacBioComplete,
		alias:        "pb",
		indexName:    "pac_bio_run_well_metrics_mirror_normalised_date_idx",
		queryPrefix:  monthlyRunPacBioSQLPrefix,
		syncTables:   []string{syncTablePacBioRunWellMetrics},
	},
	{
		platform:     platformElembio,
		manufacturer: runManufacturerElembio,
		dateBasis:    runDateBasisRunComplete,
		alias:        "er",
		indexName:    "eseq_run_lane_metrics_mirror_normalised_date_idx",
		queryPrefix:  monthlyRunElembioSQLPrefix,
		syncTables:   []string{syncTableEseqRunLaneMetrics},
	},
	{
		platform:     platformUltimagen,
		manufacturer: runManufacturerUltimagen,
		dateBasis:    runDateBasisRunArchived,
		alias:        "ur",
		indexName:    "useq_run_metrics_mirror_normalised_date_idx",
		queryPrefix:  monthlyRunUltimagenSQLPrefix,
		syncTables:   []string{syncTableUseqRunMetrics},
	},
	{
		platform:     platformONT,
		manufacturer: runManufacturerONT,
		dateBasis:    runDateBasisONTLoadTime,
		alias:        "ont",
		indexName:    "oseq_flowcell_mirror_normalised_date_idx",
		queryPrefix:  monthlyRunONTSQLPrefix,
		syncTables:   []string{syncTableOseqFlowcell},
	},
}

func normaliseSequencingAggregateOptions(opts SequencingAggregateOptions) (SequencingAggregateOptions, []runAggregationPlatformSpec, error) {
	unit, err := normaliseSequencingAggregateUnit(opts.Unit)
	if err != nil {
		return SequencingAggregateOptions{}, nil, err
	}
	groupBy, err := normaliseSequencingAggregateGroupBy(opts.GroupBy)
	if err != nil {
		return SequencingAggregateOptions{}, nil, err
	}
	since, until, err := normaliseSequencingAggregateBounds(unit, opts.Since, opts.Until)
	if err != nil {
		return SequencingAggregateOptions{}, nil, err
	}
	specs, err := normaliseRunAggregationPlatforms(opts.Platforms)
	if err != nil {
		return SequencingAggregateOptions{}, nil, err
	}

	return SequencingAggregateOptions{
		GroupBy:   groupBy,
		Unit:      unit,
		Since:     since,
		Until:     until,
		Platforms: canonicalPlatformsForSpecs(specs),
	}, specs, nil
}

func normaliseSequencingAggregateUnit(raw string) (string, error) {
	unit := strings.ToLower(strings.TrimSpace(raw))
	switch unit {
	case sequencingAggregateRuns, sequencingAggregateSamples, sequencingAggregateProducts:
		return unit, nil
	case "":
		return "", fmt.Errorf("%w: unit is required", ErrUnsupportedIdentifier)
	default:
		return "", fmt.Errorf("%w: unsupported sequencing aggregate unit %q", ErrUnsupportedIdentifier, raw)
	}
}

func normaliseSequencingAggregateGroupBy(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	groups := make([]string, 0, len(raw))
	for _, value := range raw {
		for _, part := range strings.Split(value, ",") {
			group := strings.ToLower(strings.TrimSpace(part))
			if group == "" {
				continue
			}
			if err := validateSequencingAggregateGroup(group); err != nil {
				return nil, err
			}
			if _, ok := seen[group]; ok {
				continue
			}
			seen[group] = struct{}{}
			groups = append(groups, group)
		}
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("%w: group_by is required", ErrUnsupportedIdentifier)
	}

	return groups, nil
}

func validateSequencingAggregateGroup(group string) error {
	switch group {
	case "month", "platform", "manufacturer", "programme", "faculty_sponsor":
		return nil
	default:
		return fmt.Errorf("%w: unsupported sequencing aggregate group_by %q", ErrUnsupportedIdentifier, group)
	}
}

func normaliseSequencingAggregateBounds(unit, sinceRaw, untilRaw string) (string, string, error) {
	if unit == sequencingAggregateRuns {
		return normaliseSequencingAggregateRunBounds(sinceRaw, untilRaw)
	}

	return normaliseSequencingAggregateCreatedBounds(sinceRaw, untilRaw)
}

func normaliseSequencingAggregateRunBounds(sinceRaw, untilRaw string) (string, string, error) {
	since, err := normaliseRunDateBound("since", sinceRaw)
	if err != nil {
		return "", "", err
	}
	until, err := normaliseRunDateBound("until", untilRaw)
	if err != nil {
		return "", "", err
	}
	if since != "" && until != "" && until < since {
		return "", "", fmt.Errorf("%w: until must be on or after since", ErrUnsupportedIdentifier)
	}

	return since, until, nil
}

func normaliseSequencingAggregateCreatedBounds(sinceRaw, untilRaw string) (string, string, error) {
	since, err := normaliseCreatedTimestampBound("since", sinceRaw)
	if err != nil {
		return "", "", err
	}
	until, err := normaliseCreatedTimestampBound("until", untilRaw)
	if err != nil {
		return "", "", err
	}
	if since != "" && until != "" && until < since {
		return "", "", fmt.Errorf("%w: until must be on or after since", ErrUnsupportedIdentifier)
	}

	return since, until, nil
}

func normaliseCreatedTimestampBound(name, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if parsed, err := time.Parse(time.DateOnly, trimmed); err == nil {
		return formatSyncTime(parsed.UTC()), nil
	}
	if parsed, err := parseSyncTimeString(trimmed); err == nil {
		return formatSyncTime(parsed.UTC()), nil
	}

	return "", fmt.Errorf("%w: %s must be YYYY-MM-DD or RFC3339", ErrUnsupportedIdentifier, name)
}

func sequencingAggregateSyncTables(opts SequencingAggregateOptions, specs []runAggregationPlatformSpec) []string {
	if opts.Unit == sequencingAggregateSamples || opts.Unit == sequencingAggregateProducts {
		return []string{syncTableStudy, syncTableSeqProductIRODSLocations}
	}

	tables := syncTablesForRunAggregationSpecs(specs)
	if sequencingAggregateNeedsStudyGroup(opts.GroupBy) {
		tables = append(tables, syncTableStudy)
		tables = append(tables, sequencingAggregateProductSyncTables(specs)...)
	}

	return uniqueStrings(tables)
}

func sequencingAggregateNeedsStudyGroup(groupBy []string) bool {
	for _, group := range groupBy {
		if group == "programme" || group == "faculty_sponsor" {
			return true
		}
	}

	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}

	return unique
}

func syncTablesForRunAggregationSpecs(specs []runAggregationPlatformSpec) []string {
	seen := make(map[string]struct{}, len(specs)*2)
	tables := make([]string, 0, len(specs)*2)
	for _, spec := range specs {
		for _, table := range spec.syncTables {
			if _, ok := seen[table]; ok {
				continue
			}
			seen[table] = struct{}{}
			tables = append(tables, table)
		}
	}

	return tables
}

func sequencingAggregateProductSyncTables(specs []runAggregationPlatformSpec) []string {
	tables := make([]string, 0, len(specs))
	for _, spec := range specs {
		switch spec.platform {
		case platformPacBio:
			tables = append(tables, syncTablePacBioProductMetrics)
		case platformElembio:
			tables = append(tables, syncTableEseqProductMetrics)
		case platformUltimagen:
			tables = append(tables, syncTableUseqProductMetrics)
		}
	}

	return tables
}

func sequencingAggregateQuery(opts SequencingAggregateOptions, specs []runAggregationPlatformSpec, dialect string) (string, []any, error) {
	switch opts.Unit {
	case sequencingAggregateRuns:
		return sequencingAggregateRunsQuery(opts, specs, dialect)
	case sequencingAggregateSamples, sequencingAggregateProducts:
		return sequencingAggregateDataQuery(opts, specs, dialect)
	default:
		return "", nil, fmt.Errorf("%w: unsupported sequencing aggregate unit %q", ErrUnsupportedIdentifier, opts.Unit)
	}
}

func sequencingAggregateRunsQuery(opts SequencingAggregateOptions, specs []runAggregationPlatformSpec, dialect string) (string, []any, error) {
	arms := make([]string, 0, len(specs))
	args := make([]any, 0, len(specs)*2)
	needsStudy := sequencingAggregateNeedsStudyGroup(opts.GroupBy)
	for _, spec := range specs {
		arm, armArgs, err := sequencingAggregateRunArmQuery(spec, opts, dialect, needsStudy)
		if err != nil {
			return "", nil, err
		}
		arms = append(arms, arm)
		args = append(args, armArgs...)
	}

	return sequencingAggregateOuterQuery(strings.Join(arms, " UNION ALL "), opts.GroupBy), args, nil
}

func sequencingAggregateOuterQuery(source string, groupBy []string) string {
	selectCols := append([]string{}, groupBy...)
	selectCols = append(selectCols, "COUNT(DISTINCT unit_id) AS count", "date_basis")
	groupCols := append([]string{}, groupBy...)
	groupCols = append(groupCols, "date_basis")

	return `SELECT ` + strings.Join(selectCols, ", ") + ` FROM (` + source + `) AS aggregate_rows GROUP BY ` +
		strings.Join(groupCols, ", ") + ` ORDER BY ` + strings.Join(groupCols, ", ")
}

func sequencingAggregateRunArmQuery(spec runAggregationPlatformSpec, opts SequencingAggregateOptions, dialect string, needsStudy bool) (string, []any, error) {
	args := make([]any, 0, 2)
	numericID := func(expr string) string {
		return runListingIntegerTextExpr(dialect, expr)
	}
	studySelect := sequencingAggregateStudySelectSQL(needsStudy)
	var query string
	switch spec.platform {
	case platformIllumina:
		query = `SELECT SUBSTR(s.normalised_date, 1, 7) AS month, 'Illumina' AS platform, 'Illumina' AS manufacturer, ` + studySelect + `, ` +
			sqlConcat(dialect, "'illumina:'", numericID("s.id_run")) + ` AS unit_id, 'run complete' AS date_basis ` +
			`FROM iseq_run_status_mirror AS s INNER JOIN iseq_run_status_dict_mirror AS d ON d.id_run_status_dict = s.id_run_status_dict `
		query += sequencingAggregateIlluminaStudyJoinSQL(needsStudy)
		query += `WHERE s.normalised_date <> '' AND d.description = 'run complete'`
		if !needsStudy {
			query += ` AND EXISTS (SELECT 1 FROM iseq_product_metrics_mirror AS ipm WHERE ipm.id_run = s.id_run)`
		}
	case platformPacBio:
		query = `SELECT SUBSTR(pb.normalised_date, 1, 7) AS month, 'PacBio' AS platform, 'PacBio' AS manufacturer, ` + studySelect + `, ` +
			sqlConcat(dialect, "'pacbio:'", "pb.pac_bio_run_name") + ` AS unit_id, 'run_complete' AS date_basis ` +
			`FROM pac_bio_run_well_metrics_mirror AS pb `
		query += sequencingAggregatePacBioStudyJoinSQL(needsStudy)
		query += `WHERE pb.normalised_date <> ''`
	case platformElembio:
		query = `SELECT SUBSTR(er.normalised_date, 1, 7) AS month, 'Elembio' AS platform, 'Element Biosciences' AS manufacturer, ` + studySelect + `, ` +
			sqlConcat(dialect, "'elembio:'", numericID("er.id_run")) + ` AS unit_id, 'run complete' AS date_basis ` +
			`FROM eseq_run_lane_metrics_mirror AS er `
		query += sequencingAggregateElembioStudyJoinSQL(needsStudy)
		query += `WHERE er.normalised_date <> ''`
	case platformUltimagen:
		query = `SELECT SUBSTR(ur.normalised_date, 1, 7) AS month, 'Ultimagen' AS platform, 'Ultima Genomics' AS manufacturer, ` + studySelect + `, ` +
			sqlConcat(dialect, "'ultimagen:'", numericID("ur.id_run")) + ` AS unit_id, 'run archived' AS date_basis ` +
			`FROM useq_run_metrics_mirror AS ur `
		query += sequencingAggregateUltimagenStudyJoinSQL(needsStudy)
		query += `WHERE ur.normalised_date <> ''`
	case platformONT:
		query = `SELECT SUBSTR(ont.normalised_date, 1, 7) AS month, 'ONT' AS platform, 'Oxford Nanopore' AS manufacturer, ` + studySelect + `, ` +
			sqlConcat(dialect, "'ont:'", "ont.experiment_name") + ` AS unit_id, 'warehouse load time - not a true sequencing date' AS date_basis ` +
			`FROM oseq_flowcell_mirror AS ont `
		query += sequencingAggregateONTStudyJoinSQL(needsStudy)
		query += `WHERE ont.normalised_date <> ''`
	default:
		return "", nil, fmt.Errorf("%w: unsupported platform %q", ErrUnsupportedIdentifier, spec.platform)
	}
	query += runListingDateFilterSQL(spec.alias, RunAggregationOptions{Since: opts.Since, Until: opts.Until}, &args)

	return query, args, nil
}

func sequencingAggregateStudySelectSQL(needsStudy bool) string {
	if !needsStudy {
		return `'' AS programme, '' AS faculty_sponsor`
	}

	return `COALESCE(sm.programme, '') AS programme, COALESCE(sm.faculty_sponsor, '') AS faculty_sponsor`
}

func sqlConcat(dialect string, parts ...string) string {
	if dialect == "mysql" {
		return "CONCAT(" + strings.Join(parts, ", ") + ")"
	}

	return strings.Join(parts, " || ")
}

func sequencingAggregateIlluminaStudyJoinSQL(needsStudy bool) string {
	if !needsStudy {
		return ""
	}

	return `INNER JOIN iseq_product_metrics_mirror AS ipm ON ipm.id_run = s.id_run ` +
		`INNER JOIN study_mirror AS sm ON sm.id_study_lims = ipm.id_study_lims AND sm.id_lims = 'SQSCP' `
}

func sequencingAggregatePacBioStudyJoinSQL(needsStudy bool) string {
	if !needsStudy {
		return ""
	}

	return `INNER JOIN pac_bio_product_metrics_mirror AS pbm ON pbm.id_pac_bio_rw_metrics_tmp = pb.id_pac_bio_rw_metrics_tmp ` +
		`INNER JOIN study_mirror AS sm ON sm.id_study_lims = pbm.id_study_lims AND sm.id_lims = 'SQSCP' `
}

func sequencingAggregateElembioStudyJoinSQL(needsStudy bool) string {
	if !needsStudy {
		return ""
	}

	return `INNER JOIN eseq_product_metrics_mirror AS epm ON epm.id_run = er.id_run ` +
		`INNER JOIN study_mirror AS sm ON sm.id_study_lims = epm.id_study_lims AND sm.id_lims = 'SQSCP' `
}

func sequencingAggregateUltimagenStudyJoinSQL(needsStudy bool) string {
	if !needsStudy {
		return ""
	}

	return `INNER JOIN useq_product_metrics_mirror AS upm ON upm.id_run = ur.id_run ` +
		`INNER JOIN study_mirror AS sm ON sm.id_study_lims = upm.id_study_lims AND sm.id_lims = 'SQSCP' `
}

func sequencingAggregateONTStudyJoinSQL(needsStudy bool) string {
	if !needsStudy {
		return ""
	}

	return `INNER JOIN study_mirror AS sm ON sm.id_study_lims = ont.id_study_lims AND sm.id_lims = 'SQSCP' `
}

func sequencingAggregateDataQuery(opts SequencingAggregateOptions, specs []runAggregationPlatformSpec, dialect string) (string, []any, error) {
	args := make([]any, 0, 2+len(specs))
	unitExpr := "spi.id_iseq_product"
	if opts.Unit == sequencingAggregateSamples {
		unitExpr = runListingIntegerTextExpr(dialect, "spi.id_sample_tmp")
	}
	query := `SELECT SUBSTR(spi.created, 1, 7) AS month, ` +
		sequencingAggregatePlatformCase("spi.platform") + ` AS platform, ` +
		sequencingAggregateManufacturerCase("spi.platform") + ` AS manufacturer, ` +
		`COALESCE(sm.programme, '') AS programme, COALESCE(sm.faculty_sponsor, '') AS faculty_sponsor, ` +
		unitExpr + ` AS unit_id, '` + sequencingAggregateCreated + `' AS date_basis ` +
		`FROM seq_product_irods_locations_mirror AS spi ` +
		`INNER JOIN study_mirror AS sm ON sm.id_study_lims = spi.id_study_lims AND sm.id_lims = 'SQSCP' ` +
		`WHERE spi.created IS NOT NULL AND spi.created <> ''`
	query += sequencingAggregateDataDateFilterSQL(opts, &args)
	query += sequencingAggregateDataPlatformFilterSQL(specs, &args)

	return sequencingAggregateOuterQuery(query, opts.GroupBy), args, nil
}

func sequencingAggregatePlatformCase(expr string) string {
	return `CASE LOWER(` + expr + `) WHEN 'illumina' THEN 'Illumina' WHEN 'pacbio' THEN 'PacBio' ` +
		`WHEN 'elembio' THEN 'Elembio' WHEN 'ultimagen' THEN 'Ultimagen' WHEN 'ont' THEN 'ONT' ELSE ` + expr + ` END`
}

func sequencingAggregateManufacturerCase(expr string) string {
	return `CASE LOWER(` + expr + `) WHEN 'illumina' THEN 'Illumina' WHEN 'pacbio' THEN 'PacBio' ` +
		`WHEN 'elembio' THEN 'Element Biosciences' WHEN 'ultimagen' THEN 'Ultima Genomics' ` +
		`WHEN 'ont' THEN 'Oxford Nanopore' ELSE ` + expr + ` END`
}

func sequencingAggregateDataDateFilterSQL(opts SequencingAggregateOptions, args *[]any) string {
	var query string
	if opts.Since != "" {
		query += " AND spi.created >= ?"
		*args = append(*args, opts.Since)
	}
	if opts.Until != "" {
		query += " AND spi.created < ?"
		*args = append(*args, opts.Until)
	}

	return query
}

func sequencingAggregateDataPlatformFilterSQL(specs []runAggregationPlatformSpec, args *[]any) string {
	if len(specs) == len(runAggregationPlatformSpecs) {
		return ""
	}
	placeholders := make([]string, 0, len(specs))
	for _, spec := range specs {
		placeholders = append(placeholders, "?")
		*args = append(*args, runListingPlatformKey(spec.platform))
	}

	return " AND LOWER(spi.platform) IN (" + strings.Join(placeholders, ", ") + ")"
}

func (c *Client) monthlyRunCountsForPlatform(ctx context.Context, db *sql.DB, opts RunAggregationOptions, spec runAggregationPlatformSpec, syncedAt string) ([]MonthlyRunCount, error) {
	query, args := monthlyRunCountQuery(spec, opts)
	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query monthly %s run counts: %w", ErrUpstreamImpaired, spec.platform, err)
	}
	defer func() { _ = sqlRows.Close() }()

	rows := make([]MonthlyRunCount, 0)
	for sqlRows.Next() {
		var row MonthlyRunCount
		if err = sqlRows.Scan(&row.Month, &row.Count); err != nil {
			return nil, fmt.Errorf("%w: scan monthly %s run counts: %w", ErrUpstreamImpaired, spec.platform, err)
		}
		row.Manufacturer = spec.manufacturer
		row.Platform = spec.platform
		row.DateBasis = spec.dateBasis
		row.CacheSyncedAt = syncedAt
		rows = append(rows, row)
	}
	if err = sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query monthly %s run counts: %w", ErrUpstreamImpaired, spec.platform, err)
	}

	return rows, nil
}

func monthlyRunCountQuery(spec runAggregationPlatformSpec, opts RunAggregationOptions) (string, []any) {
	query := spec.queryPrefix
	args := make([]any, 0, 2)
	if opts.Since != "" {
		query += " AND " + spec.alias + ".normalised_date >= ?"
		args = append(args, opts.Since)
	}
	if opts.Until != "" {
		query += " AND " + spec.alias + ".normalised_date < ?"
		args = append(args, opts.Until)
	}
	query += fmt.Sprintf(monthlyRunSQLSuffix, spec.alias)

	return query, args
}

func runListingQuery(specs []runAggregationPlatformSpec, opts RunAggregationOptions, dialect string, cursor runListingCursor, limit int) (string, []any) {
	union, args := runListingUnionQuery(specs, opts, dialect)
	query := `SELECT platform_key, platform, native_id, manufacturer, run_date, date_basis FROM (` + union + `) AS runs`
	if cursor.set {
		query += ` WHERE platform_key > ? OR (platform_key = ? AND native_id > ?)`
		args = append(args, cursor.platformKey, cursor.platformKey, cursor.nativeID)
	}
	query += ` ORDER BY platform_key, native_id LIMIT ?`
	args = append(args, limit)

	return query, args
}

func countRunListingQuery(specs []runAggregationPlatformSpec, opts RunAggregationOptions, dialect string) (string, []any) {
	union, args := runListingUnionQuery(specs, opts, dialect)

	return `SELECT COUNT(*) FROM (` + union + `) AS runs`, args
}

func runListingUnionQuery(specs []runAggregationPlatformSpec, opts RunAggregationOptions, dialect string) (string, []any) {
	arms := make([]string, 0, len(specs))
	args := make([]any, 0, len(specs)*2)
	for _, spec := range specs {
		arm, armArgs := runListingArmQuery(spec, opts, dialect)
		arms = append(arms, arm)
		args = append(args, armArgs...)
	}

	return strings.Join(arms, " UNION ALL "), args
}

func runListingArmQuery(spec runAggregationPlatformSpec, opts RunAggregationOptions, dialect string) (string, []any) {
	args := make([]any, 0, 2)
	numericID := func(expr string) string {
		return runListingIntegerTextExpr(dialect, expr)
	}

	var query string
	switch spec.platform {
	case platformIllumina:
		query = `SELECT 'illumina' AS platform_key, 'Illumina' AS platform, ` + numericID("s.id_run") + ` AS native_id, 'Illumina' AS manufacturer, MIN(s.normalised_date) AS run_date, 'run complete' AS date_basis ` +
			`FROM iseq_run_status_mirror AS s ` +
			`INNER JOIN iseq_run_status_dict_mirror AS d ON d.id_run_status_dict = s.id_run_status_dict ` +
			`WHERE s.normalised_date <> '' AND d.description = 'run complete' ` +
			`AND EXISTS (SELECT 1 FROM iseq_product_metrics_mirror AS ipm WHERE ipm.id_run = s.id_run)`
	case platformPacBio:
		query = `SELECT 'pacbio' AS platform_key, 'PacBio' AS platform, pb.pac_bio_run_name AS native_id, 'PacBio' AS manufacturer, MIN(pb.normalised_date) AS run_date, 'run_complete' AS date_basis ` +
			`FROM pac_bio_run_well_metrics_mirror AS pb WHERE pb.normalised_date <> ''`
	case platformElembio:
		query = `SELECT 'elembio' AS platform_key, 'Elembio' AS platform, ` + numericID("er.id_run") + ` AS native_id, 'Element Biosciences' AS manufacturer, MIN(er.normalised_date) AS run_date, 'run complete' AS date_basis ` +
			`FROM eseq_run_lane_metrics_mirror AS er WHERE er.normalised_date <> ''`
	case platformUltimagen:
		query = `SELECT 'ultimagen' AS platform_key, 'Ultimagen' AS platform, ` + numericID("ur.id_run") + ` AS native_id, 'Ultima Genomics' AS manufacturer, MIN(ur.normalised_date) AS run_date, 'run archived' AS date_basis ` +
			`FROM useq_run_metrics_mirror AS ur WHERE ur.normalised_date <> ''`
	case platformONT:
		query = `SELECT 'ont' AS platform_key, 'ONT' AS platform, ont.experiment_name AS native_id, 'Oxford Nanopore' AS manufacturer, MIN(ont.normalised_date) AS run_date, 'warehouse load time - not a true sequencing date' AS date_basis ` +
			`FROM oseq_flowcell_mirror AS ont WHERE ont.normalised_date <> ''`
	}
	query += runListingDateFilterSQL(spec.alias, opts, &args)
	query += runListingGroupBySQL(spec)

	return query, args
}

func runListingIntegerTextExpr(dialect, expr string) string {
	if dialect == "mysql" {
		return "CAST(" + expr + " AS CHAR)"
	}

	return "CAST(" + expr + " AS TEXT)"
}

func runListingDateFilterSQL(alias string, opts RunAggregationOptions, args *[]any) string {
	var query string
	if opts.Since != "" {
		query += " AND " + alias + ".normalised_date >= ?"
		*args = append(*args, opts.Since)
	}
	if opts.Until != "" {
		query += " AND " + alias + ".normalised_date < ?"
		*args = append(*args, opts.Until)
	}

	return query
}

func runListingGroupBySQL(spec runAggregationPlatformSpec) string {
	switch spec.platform {
	case platformIllumina:
		return " GROUP BY s.id_run"
	case platformPacBio:
		return " GROUP BY pb.pac_bio_run_name"
	case platformElembio:
		return " GROUP BY er.id_run"
	case platformUltimagen:
		return " GROUP BY ur.id_run"
	case platformONT:
		return " GROUP BY ont.experiment_name"
	default:
		return ""
	}
}

func normaliseRunAggregationPlatforms(raw []string) ([]runAggregationPlatformSpec, error) {
	if len(raw) == 0 {
		return runAggregationPlatformSpecs, nil
	}

	selected := make(map[string]struct{}, len(raw))
	for _, value := range raw {
		platform, err := normaliseRunAggregationPlatform(value)
		if err != nil {
			return nil, err
		}
		selected[platform] = struct{}{}
	}

	specs := make([]runAggregationPlatformSpec, 0, len(selected))
	for _, spec := range runAggregationPlatformSpecs {
		if _, ok := selected[spec.platform]; ok {
			specs = append(specs, spec)
		}
	}

	return specs, nil
}

func normaliseRunAggregationPlatform(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")

	switch normalized {
	case "illumina":
		return platformIllumina, nil
	case "pacbio":
		return platformPacBio, nil
	case "elembio", "element", "elementbiosciences":
		return platformElembio, nil
	case "ultimagen", "ultima", "ultimagenomics":
		return platformUltimagen, nil
	case "ont", "oxfordnanopore", "nanopore":
		return platformONT, nil
	default:
		return "", fmt.Errorf("%w: unsupported platform %q", ErrUnsupportedIdentifier, raw)
	}
}

func canonicalPlatformsForSpecs(specs []runAggregationPlatformSpec) []string {
	platforms := make([]string, 0, len(specs))
	for _, spec := range specs {
		platforms = append(platforms, spec.platform)
	}

	return platforms
}

type runListingCursor struct {
	platformKey string
	nativeID    string
	set         bool
}

func parseRunListingCursor(raw string) (runListingCursor, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return runListingCursor{}, nil
	}

	rawPlatform, nativeID, ok := strings.Cut(trimmed, ":")
	if !ok || strings.TrimSpace(rawPlatform) == "" || nativeID == "" {
		return runListingCursor{}, fmt.Errorf("%w: invalid run listing cursor", ErrUnsupportedIdentifier)
	}
	platform, err := normaliseRunAggregationPlatform(rawPlatform)
	if err != nil {
		return runListingCursor{}, fmt.Errorf("%w: invalid run listing cursor", ErrUnsupportedIdentifier)
	}

	return runListingCursor{
		platformKey: runListingPlatformKey(platform),
		nativeID:    nativeID,
		set:         true,
	}, nil
}

// RunAggregationOptions carries optional filters for global run aggregation
// queries. Since and Until are normalised-date bounds over the per-platform run
// date basis: since is inclusive and until is exclusive. Platforms uses the
// canonical names Illumina, PacBio, Elembio, Ultimagen and ONT, with a few common
// manufacturer aliases accepted for CLI ergonomics.
type RunAggregationOptions struct {
	Since     string
	Until     string
	Platforms []string
}

func normaliseRunAggregationOptions(opts RunAggregationOptions) (RunAggregationOptions, []runAggregationPlatformSpec, error) {
	since, err := normaliseRunDateBound("since", opts.Since)
	if err != nil {
		return RunAggregationOptions{}, nil, err
	}
	until, err := normaliseRunDateBound("until", opts.Until)
	if err != nil {
		return RunAggregationOptions{}, nil, err
	}
	if since != "" && until != "" && until < since {
		return RunAggregationOptions{}, nil, fmt.Errorf("%w: until must be on or after since", ErrUnsupportedIdentifier)
	}

	specs, err := normaliseRunAggregationPlatforms(opts.Platforms)
	if err != nil {
		return RunAggregationOptions{}, nil, err
	}

	return RunAggregationOptions{Since: since, Until: until, Platforms: canonicalPlatformsForSpecs(specs)}, specs, nil
}

// MonthlyRunCounts returns monthly grouped run counts across the selected
// platforms. The run grain is one platform-native run identifier: Illumina,
// Elembio and Ultimagen count distinct id_run, PacBio counts distinct
// pac_bio_run_name, and ONT counts distinct experiment_name. Every row carries
// its platform's manufacturer, date_basis and cache_synced_at freshness caveat.
func (c *Client) MonthlyRunCounts(ctx context.Context, opts RunAggregationOptions) ([]MonthlyRunCount, error) {
	normalized, specs, err := normaliseRunAggregationOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return []MonthlyRunCount{}, nil
	}

	syncTables := syncTablesForRunAggregationSpecs(specs)
	if err = c.requireAnySyncState(ctx, syncTables...); err != nil {
		return []MonthlyRunCount{}, err
	}

	syncedAt, err := c.oldestFeedingLastRun(ctx, syncTables)
	if err != nil {
		return nil, err
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows := make([]MonthlyRunCount, 0)
	for _, spec := range specs {
		platformRows, queryErr := c.monthlyRunCountsForPlatform(ctx, db, normalized, spec, syncedAt)
		if queryErr != nil {
			return nil, queryErr
		}
		rows = append(rows, platformRows...)
	}

	sortMonthlyRunCounts(rows)

	return rows, nil
}

func sortMonthlyRunCounts(rows []MonthlyRunCount) {
	order := make(map[string]int, len(runAggregationPlatformSpecs))
	for index, spec := range runAggregationPlatformSpecs {
		order[spec.platform] = index
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Month != rows[j].Month {
			return rows[i].Month < rows[j].Month
		}

		return order[rows[i].Platform] < order[rows[j].Platform]
	})
}

// RunListing returns a bounded, keyset-pageable global listing of runs across
// the selected platforms. The run grain matches MonthlyRunCounts: Illumina,
// Elembio and Ultimagen list distinct id_run, PacBio lists distinct
// pac_bio_run_name, and ONT lists distinct experiment_name. The cursor is the
// previous page's final composite id, formatted as "<platform>:<native_id>".
func (c *Client) RunListing(ctx context.Context, opts RunAggregationOptions, limit int, cursor string) ([]RunListingRow, error) {
	normalized, specs, err := normaliseRunAggregationOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return []RunListingRow{}, nil
	}

	limit, err = normaliseRunListingLimit(limit)
	if err != nil {
		return nil, err
	}
	cursorValue, err := parseRunListingCursor(cursor)
	if err != nil {
		return nil, err
	}
	if limit == 0 {
		return []RunListingRow{}, nil
	}

	syncTables := syncTablesForRunAggregationSpecs(specs)
	if err = c.requireAnySyncState(ctx, syncTables...); err != nil {
		return []RunListingRow{}, err
	}
	syncedAt, err := c.oldestFeedingLastRun(ctx, syncTables)
	if err != nil {
		return nil, err
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	query, args := runListingQuery(specs, normalized, c.runListingDialect(), cursorValue, limit)
	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query global run listing: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = sqlRows.Close() }()

	rows := make([]RunListingRow, 0, limit)
	for sqlRows.Next() {
		row, scanErr := scanRunListingRow(sqlRows, syncedAt)
		if scanErr != nil {
			return nil, scanErr
		}
		rows = append(rows, row)
	}
	if err = sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query global run listing: %w", ErrUpstreamImpaired, err)
	}

	return rows, nil
}

func normaliseRunListingLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, fmt.Errorf("%w: limit must not be negative", ErrUnsupportedIdentifier)
	}
	if limit > RunListingMaxLimit {
		return 0, fmt.Errorf("%w: limit must not exceed %d", ErrUnsupportedIdentifier, RunListingMaxLimit)
	}

	return limit, nil
}

func scanRunListingRow(rows *sql.Rows, syncedAt string) (RunListingRow, error) {
	var (
		platformKey string
		row         RunListingRow
	)
	if err := rows.Scan(&platformKey, &row.Platform, &row.NativeID, &row.Manufacturer, &row.RunDate, &row.DateBasis); err != nil {
		return RunListingRow{}, fmt.Errorf("%w: scan global run listing: %w", ErrUpstreamImpaired, err)
	}
	row.ID = platformKey + ":" + row.NativeID
	row.CacheSyncedAt = syncedAt

	return row, nil
}

func runListingPlatformKey(platform string) string {
	switch platform {
	case platformIllumina:
		return "illumina"
	case platformPacBio:
		return "pacbio"
	case platformElembio:
		return "elembio"
	case platformUltimagen:
		return "ultimagen"
	case platformONT:
		return "ont"
	default:
		return strings.ToLower(platform)
	}
}

func normaliseRunDateBound(name, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}

	if parsed, err := time.Parse(time.DateOnly, trimmed); err == nil {
		return parsed.Format(time.DateOnly), nil
	}
	if parsed, err := parseSyncTimeString(trimmed); err == nil {
		return parsed.UTC().Format(time.DateOnly), nil
	}

	return "", fmt.Errorf("%w: %s must be YYYY-MM-DD or RFC3339", ErrUnsupportedIdentifier, name)
}

func scanSequencingAggregateRows(sqlRows *sql.Rows, groupBy []string, unit, syncedAt string) ([]SequencingAggregateRow, error) {
	rows := make([]SequencingAggregateRow, 0)
	for sqlRows.Next() {
		row, err := scanSequencingAggregateRow(sqlRows, groupBy, unit, syncedAt)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sequencing aggregate: %w", ErrUpstreamImpaired, err)
	}

	return rows, nil
}

func scanSequencingAggregateRow(sqlRows *sql.Rows, groupBy []string, unit, syncedAt string) (SequencingAggregateRow, error) {
	groupValues := make([]sql.NullString, len(groupBy))
	scanArgs := make([]any, 0, len(groupBy)+2)
	for i := range groupValues {
		scanArgs = append(scanArgs, &groupValues[i])
	}
	var row SequencingAggregateRow
	scanArgs = append(scanArgs, &row.Count, &row.DateBasis)
	if err := sqlRows.Scan(scanArgs...); err != nil {
		return SequencingAggregateRow{}, fmt.Errorf("%w: scan sequencing aggregate: %w", ErrUpstreamImpaired, err)
	}
	row.Group = make(map[string]string, len(groupBy))
	for i, group := range groupBy {
		row.Group[group] = groupValues[i].String
	}
	row.Unit = unit
	row.CacheSyncedAt = syncedAt

	return row, nil
}

func sortSequencingAggregateRows(rows []SequencingAggregateRow, groupBy []string) {
	sort.SliceStable(rows, func(i, j int) bool {
		for _, group := range groupBy {
			if rows[i].Group[group] != rows[j].Group[group] {
				return rows[i].Group[group] < rows[j].Group[group]
			}
		}
		if rows[i].DateBasis != rows[j].DateBasis {
			return rows[i].DateBasis < rows[j].DateBasis
		}

		return rows[i].Unit < rows[j].Unit
	})
}

// SequencingAggregate returns the general grouped sequencing aggregate. For
// unit=runs it counts platform-native run identifiers using the same per-platform
// date basis as MonthlyRunCounts; when grouped by study metadata, a run counts
// once in each requested study group it touches. For unit=samples or
// unit=products it counts distinct iRODS-delivered samples/products over the
// iRODS created timestamp.
func (c *Client) SequencingAggregate(ctx context.Context, opts SequencingAggregateOptions) ([]SequencingAggregateRow, error) {
	normalized, specs, err := normaliseSequencingAggregateOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return []SequencingAggregateRow{}, nil
	}

	syncTables := sequencingAggregateSyncTables(normalized, specs)
	if err = c.requireAnySyncState(ctx, syncTables...); err != nil {
		return []SequencingAggregateRow{}, err
	}
	syncedAt, err := c.oldestFeedingLastRun(ctx, syncTables)
	if err != nil {
		return nil, err
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	query, args, err := sequencingAggregateQuery(normalized, specs, c.runListingDialect())
	if err != nil {
		return nil, err
	}
	sqlRows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query sequencing aggregate: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = sqlRows.Close() }()

	rows, err := scanSequencingAggregateRows(sqlRows, normalized.GroupBy, normalized.Unit, syncedAt)
	if err != nil {
		return nil, err
	}
	sortSequencingAggregateRows(rows, normalized.GroupBy)

	return rows, nil
}

func (c *Client) runListingDialect() string {
	if c != nil && c.cache != nil {
		return c.cache.Dialect()
	}

	return "sqlite"
}
