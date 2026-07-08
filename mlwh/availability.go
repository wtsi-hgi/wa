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
	"slices"
	"strings"
	"sync"
	"time"
)

// Canonical platform names returned in SampleWithData.Platforms (and reused by
// the rest of Phase 2/4). The name derives from which product-metrics mirror the
// sample appears in (never from the iRODS seq_platform_name, which is a source
// string like "illumina"); ONT derives from oseq_flowcell_mirror membership.
const (
	platformIllumina  = "Illumina"
	platformPacBio    = "PacBio"
	platformElembio   = "Elembio"
	platformUltimagen = "Ultimagen"
	platformONT       = "ONT"
)

// studyDataMembershipJoin is the shared "has data for this study" membership the
// whole of Phase 2 reuses: library_samples -> sample_mirror ->
// seq_product_irods_locations_mirror, scoped by the iRODS row's id_study_lims and
// anchored on library_samples membership. A sample has data for the study iff it
// has at least one study-scoped iRODS row. It is expressed as an EXISTS
// correlated on the study-scoped iRODS mirror so the with-data and without-data
// partitions stay exact complements of the study's linked samples (their union
// equals samples_total). The windowed Phase 2 query (C) reuses the same join and
// passes a half-open created filter to studyScopedIRODSExists.
//
// No id_lims = 'SQSCP' filter is applied to sample_mirror here: the canonical
// study membership (SamplesForStudy / CountSamplesForStudy) does not filter on
// it, so adding it would break the with_data + without_data == samples_total
// invariant. The id_lims invariant is honoured by the sample finders/search,
// where it is the established join.
const studyDataMembershipJoin = `library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp`

// studyLinkedSamplesSQL is the distinct linked-sample set shared by the I2
// overview/status aggregate arms. It is the same membership as
// CountSamplesForStudy, but materialised once before joining to data/product sets
// so MySQL can use study-scoped indexes instead of re-running correlated EXISTS
// predicates per linked sample.
const studyLinkedSamplesSQL = `SELECT DISTINCT library_samples.id_sample_tmp FROM ` + studyDataMembershipJoin +
	` WHERE library_samples.id_study_lims = ?`

// samplesWithDataCacheSQL lists the distinct samples that have data for the
// study (the with-data partition), ordered like the other study fan-outs.
var samplesWithDataCacheSQL = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM ` + studyDataMembershipJoin +
	` WHERE library_samples.id_study_lims = ? AND EXISTS (` + studyScopedIRODSExists("") + `)` +
	` ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`

// samplesWithoutDataCacheSQL lists the study's linked samples MINUS the with-data
// partition (sequenced-no-data, registered, and ONT), ordered identically.
var samplesWithoutDataCacheSQL = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM ` + studyDataMembershipJoin +
	` WHERE library_samples.id_study_lims = ? AND NOT EXISTS (` + studyScopedIRODSExists("") + `)` +
	` ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`

// countSamplesWithDataCacheSQL counts the distinct samples that have data for
// the study, the count counterpart of SamplesWithData. It reuses the same
// membership join (studyDataMembershipJoin) and the same study-scoped iRODS
// existence predicate (studyScopedIRODSExists) as samplesWithDataCacheSQL, with
// COUNT(DISTINCT id_sample_tmp) and no LIMIT, so the count equals the number of
// rows SamplesWithData returns when fetching all of them (the count<->list
// cross-check). It counts distinct SAMPLES, never iRODS data objects: a sample
// with many study-scoped iRODS rows contributes exactly one.
var countSamplesWithDataCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM ` + studyDataMembershipJoin +
	` INNER JOIN seq_product_irods_locations_mirror spi ` +
	`ON spi.id_sample_tmp = sample_mirror.id_sample_tmp AND spi.id_study_lims = library_samples.id_study_lims ` +
	`WHERE library_samples.id_study_lims = ?`

// platformCanonicalOrder is the stable order platforms are reported in, so a
// multi-platform sample's Platforms slice is deterministic across calls.
var platformCanonicalOrder = []string{platformIllumina, platformPacBio, platformElembio, platformUltimagen, platformONT}

// studyOverviewFeedingTables are the sync tables whose oldest last_run defines a
// StudyOverview's cache_synced_at: the study + sample identity tables, every
// platform's product-metrics mirror, and the iRODS locations mirror that feed the
// overview's figures. cache_synced_at is the OLDEST last_run among those that have
// ever synced, distinct from any data timestamp (the freshness caveat).
var studyOverviewFeedingTables = []string{
	syncTableStudy,
	syncTableSample,
	syncTableIseqProductMetrics,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
	syncTableSeqProductIRODSLocations,
}

var studyOverviewCountsCacheSQL = studyPhaseCountsSQL(true)

// studyOverviewIRODSAggregateSQL is the single study-scoped iRODS aggregate that
// yields data_objects (row count) and the sequencing date range / newest added
// (MIN/MAX of the mirrored created column) in one indexed pass over the
// (id_study_lims, created) index.
const studyOverviewIRODSAggregateSQL = `SELECT COUNT(*), MIN(created), MAX(created) FROM seq_product_irods_locations_mirror WHERE id_study_lims = ?`

// studyOverviewRunsCacheSQL counts the distinct runs for the study from the same
// source as RunsForStudy (iseq_product_metrics_mirror), so the figure agrees with
// /study/:id/runs.
const studyOverviewRunsCacheSQL = `SELECT COUNT(DISTINCT id_run) FROM iseq_product_metrics_mirror WHERE id_study_lims = ?`

// runOverviewSamplesStudiesCacheSQL yields, for one Illumina NPG run, the distinct
// samples and the distinct studies on the run in a single indexed pass over
// iseq_product_metrics_mirror keyed on id_run: the same run->samples source as
// SamplesForRun (iseq_product_metrics_mirror INNER JOIN sample_mirror on
// id_sample_tmp), and the run's study set by its id_study_lims, so the figures
// agree with /run/:id/samples and /run/:id/detail.
const runOverviewSamplesStudiesCacheSQL = `SELECT COUNT(DISTINCT id_sample_tmp), COUNT(DISTINCT id_study_lims) FROM iseq_product_metrics_mirror WHERE id_run = ?`

// runOverviewIRODSAggregateSQL is the single run-scoped iRODS aggregate that
// yields data_objects (row count) and the sequencing date range (MIN/MAX of the
// mirrored created column) for one run. The run links to its iRODS data objects
// through the shared id_iseq_product: each iseq_product_metrics_mirror row for the
// run joins to the seq_product_irods_locations_mirror rows that carry the same
// id_iseq_product (the run's real data files in iRODS). MIN/MAX created are NULL
// when the run has no iRODS rows, which the caller maps to an absent date range.
const runOverviewIRODSAggregateSQL = `SELECT COUNT(*), MIN(spi.created), MAX(spi.created) FROM seq_product_irods_locations_mirror spi INNER JOIN iseq_product_metrics_mirror ipm ON ipm.id_iseq_product = spi.id_iseq_product WHERE ipm.id_run = ?`

// runOverviewFeedingTables are the sync tables whose oldest last_run defines a
// RunOverview's cache_synced_at: the iseq product-metrics mirror (which supplies
// the run, its samples and its studies) and the iRODS locations mirror (which
// supplies the data objects and the sequencing date range). cache_synced_at is the
// OLDEST last_run among those that have ever synced, distinct from any data
// timestamp (the freshness caveat).
var runOverviewFeedingTables = []string{
	syncTableIseqProductMetrics,
	syncTableSeqProductIRODSLocations,
}

// studyOverviewLibrariesCacheSQL counts the distinct libraries for the study using
// the same (pipeline_id_lims, library_id, id_library_lims) grouping as
// LibrariesForStudy, so the figure agrees with /study/:id/libraries. It reuses
// countLibrariesForStudyCacheSQL verbatim so the two cannot drift, and so the
// derived table keeps its `AS distinct_libraries` alias: MySQL requires every
// derived table to be aliased (Error 1248), and an unaliased subquery here was a
// MySQL-only failure invisible to the SQLite-backed tests.
const studyOverviewLibrariesCacheSQL = countLibrariesForStudyCacheSQL

// studyOverviewLibraryTypesCacheSQL lists the distinct library types (pipeline LIMS
// ids) present in the study, sorted, the same library-type notion as
// FindSamplesByLibraryType / SamplesForLibraryType.
const studyOverviewLibraryTypesCacheSQL = `SELECT DISTINCT pipeline_id_lims FROM library_samples WHERE id_study_lims = ? ORDER BY pipeline_id_lims`

// studyScopedIRODSAddedWindow is the half-open [since, until) filter the overview's
// added_last_7_days appends to studyScopedIRODSExists, comparing the iRODS created
// column (NEVER last_updated/last_run) over the (id_study_lims, created) index.
const studyScopedIRODSAddedWindow = `AND spi.created >= ? AND spi.created < ?`

// countSamplesAddedSinceCacheSQL counts the distinct samples whose study-scoped
// iRODS data was added in a half-open [since, until) window on the created column.
var countSamplesAddedSinceCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM ` + studyDataMembershipJoin +
	` INNER JOIN seq_product_irods_locations_mirror spi ` +
	`ON spi.id_sample_tmp = sample_mirror.id_sample_tmp AND spi.id_study_lims = library_samples.id_study_lims ` +
	`WHERE library_samples.id_study_lims = ? AND spi.created >= ? AND spi.created < ?`

// samplesAddedSinceCacheSQL lists the distinct samples whose study-scoped iRODS
// data was added in a half-open [since, until) window on the created column: the
// windowed variant of samplesWithDataCacheSQL (C2). It is the same membership
// query and the same study-scoped iRODS existence predicate, with the
// [since, until) created filter appended via studyScopedIRODSExists, the same
// ordering, and the same LIMIT/OFFSET pagination, so the in-window list and the
// windowed count (countSamplesAddedSinceCacheSQL) stay the exact list<->count
// cross-check.
var samplesAddedSinceCacheSQL = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM ` + studyDataMembershipJoin +
	` WHERE library_samples.id_study_lims = ? AND EXISTS (` + studyScopedIRODSExists(studyScopedIRODSAddedWindow) + `)` +
	` ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`

const latestDataSelectColumns = `COALESCE(spi.created, '') AS created, spi.id_iseq_product AS id_product, spi.irods_collection AS collection, spi.irods_file_name AS data_object, spi.id_study_lims AS id_study_lims, COALESCE(study_mirror.name, '') AS study_name, COALESCE(sample_mirror.name, '') AS name, COALESCE(sample_mirror.supplier_name, '') AS supplier_name, spi.id_run AS id_run, spi.position AS lane, spi.tag_index AS tag_index, spi.platform AS platform, spi.merged AS merged`

const latestDataFromSQL = ` FROM seq_product_irods_locations_mirror spi INNER JOIN study_mirror ON study_mirror.id_study_lims = spi.id_study_lims AND study_mirror.id_lims = 'SQSCP' LEFT JOIN sample_mirror ON sample_mirror.id_sample_tmp = spi.id_sample_tmp`

const latestDataForStudySQLPrefix = `SELECT ` + latestDataSelectColumns + latestDataFromSQL + ` WHERE spi.id_study_lims = ?`

const latestDataCreatedOrderSQL = ` ORDER BY spi.created DESC, spi.id_run, spi.id_iseq_product`

var latestDataFacultySponsorStudyIDsSQL = `SELECT id_study_lims FROM study_mirror WHERE ` + facultySponsorWhereClause + ` ORDER BY id_study_lims`

// addedWindowOpenEnded is the upper bound used by the [since, until) created
// filter when until is omitted: a sentinel string that sorts after every RFC3339
// UTC created string, so created < until is always satisfied (the window is
// open-ended). It is not a valid timestamp; it is only ever a string-comparison
// ceiling.
const addedWindowOpenEnded = "9999-12-31T23:59:59Z"

// errUntilRequiresSince reports that the added-window since/until methods were
// given an until without a since. until is only the upper bound of a
// [since, until) window, so alone it is meaningless: rather than silently dropping
// it (which would return the all-time superset), SamplesWithDataSince and
// CountSamplesWithDataSince reject it. It is the Client-layer guard mirroring the
// HTTP handler's 400 for the same input; it stays unexported because callers
// satisfy the contract by passing a since with their until rather than branching
// on this error.
var errUntilRequiresSince = errors.New("mlwh: until requires since")

// studyScopedIRODSExists is the correlated existence predicate for "this linked
// sample has >=1 study-scoped iRODS row". An optional half-open [since, until)
// filter on the mirrored created column (over the (id_study_lims, created)
// index) is appended by the windowed Phase 2 query (C); with no window it is the
// all-time membership used by B3.
func studyScopedIRODSExists(window string) string {
	predicate := `SELECT 1 FROM seq_product_irods_locations_mirror spi WHERE spi.id_sample_tmp = sample_mirror.id_sample_tmp AND spi.id_study_lims = library_samples.id_study_lims`
	if window != "" {
		predicate += " " + window
	}

	return predicate
}

type orderedAsyncError struct {
	err   error
	order int
}

func (c *Client) fillPopulatedStudyOverviewIndependentFields(ctx context.Context, studyLimsID string, overview *StudyOverview) error {
	var (
		irods        StudyOverview
		libraries    int
		libraryTypes []string
		metadata     StudyOverview
		runs         int
		syncedAt     string
		wg           sync.WaitGroup
	)
	errCh := make(chan orderedAsyncError, 6)
	run := func(order int, fill func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fill(); err != nil {
				errCh <- orderedAsyncError{order: order, err: err}
			}
		}()
	}

	run(0, func() error {
		return c.fillStudyOverviewMetadata(ctx, studyLimsID, &metadata)
	})
	run(1, func() error {
		return c.fillStudyOverviewIRODS(ctx, studyLimsID, &irods)
	})
	run(2, func() error {
		var err error
		runs, err = c.queryCount(ctx, studyOverviewRunsCacheSQL, "count study runs for overview", studyLimsID)

		return err
	})
	run(3, func() error {
		var err error
		libraries, err = c.queryCount(ctx, studyOverviewLibrariesCacheSQL, "count study libraries for overview", studyLimsID)

		return err
	})
	run(4, func() error {
		var err error
		libraryTypes, err = c.studyOverviewLibraryTypes(ctx, studyLimsID)

		return err
	})
	run(5, func() error {
		var err error
		syncedAt, err = c.oldestFeedingLastRun(ctx, studyOverviewFeedingTables)

		return err
	})

	wg.Wait()
	close(errCh)
	if err := firstOrderedAsyncError(errCh); err != nil {
		return err
	}

	overview.Name = metadata.Name
	overview.AccessionNumber = metadata.AccessionNumber
	overview.FacultySponsor = metadata.FacultySponsor
	overview.Programme = metadata.Programme
	overview.DataAccessGroup = metadata.DataAccessGroup
	overview.DataObjects = irods.DataObjects
	overview.NewestDataAdded = irods.NewestDataAdded
	overview.SequencingDateRange = irods.SequencingDateRange
	overview.Runs = runs
	overview.Libraries = libraries
	overview.LibraryTypes = libraryTypes
	overview.CacheSyncedAt = syncedAt

	return nil
}

func firstOrderedAsyncError(errCh <-chan orderedAsyncError) error {
	var first orderedAsyncError
	for asyncErr := range errCh {
		if first.err == nil || asyncErr.order < first.order {
			first = asyncErr
		}
	}

	return first.err
}

func studyPhaseCountsSQL(includeRecent bool) string {
	selects := []string{
		`COUNT(linked.id_sample_tmp)`,
		`SUM(CASE WHEN data.id_sample_tmp IS NOT NULL THEN 1 ELSE 0 END)`,
		`SUM(CASE WHEN data.id_sample_tmp IS NULL AND products.id_sample_tmp IS NOT NULL THEN 1 ELSE 0 END)`,
	}
	joins := []string{
		`FROM (` + studyLinkedSamplesSQL + `) AS linked`,
		`LEFT JOIN (SELECT DISTINCT id_sample_tmp FROM seq_product_irods_locations_mirror WHERE id_study_lims = ?) AS data ` +
			`ON data.id_sample_tmp = linked.id_sample_tmp`,
	}
	if includeRecent {
		selects = append(selects, `SUM(CASE WHEN recent.id_sample_tmp IS NOT NULL THEN 1 ELSE 0 END)`)
		joins = append(joins,
			`LEFT JOIN (SELECT DISTINCT id_sample_tmp FROM seq_product_irods_locations_mirror `+
				`WHERE id_study_lims = ? AND created >= ? AND created < ?) AS recent `+
				`ON recent.id_sample_tmp = linked.id_sample_tmp`,
		)
	}
	joins = append(joins,
		`LEFT JOIN (`+studyProductSampleSetSQL()+`) AS products ON products.id_sample_tmp = linked.id_sample_tmp`,
	)

	return `SELECT ` + strings.Join(selects, ", ") + " " + strings.Join(joins, " ")
}

// studyProductSampleSetSQL returns the distinct sample ids with product metrics in
// one study across all product-bearing platforms. Each arm is study-scoped so the
// I2 aggregate queries are served by (id_study_lims, id_sample_tmp, product, qc)
// indexes instead of scanning product mirrors or probing them per sample.
func studyProductSampleSetSQL() string {
	arm := func(table string) string {
		return `SELECT id_sample_tmp FROM ` + table + ` WHERE id_study_lims = ?`
	}

	return strings.Join([]string{
		arm("iseq_product_metrics_mirror"),
		arm("pac_bio_product_metrics_mirror"),
		arm("eseq_product_metrics_mirror"),
		arm("useq_product_metrics_mirror"),
	}, " UNION ")
}

func studyOverviewCountsArgs(studyLimsID string, windowArgs []any) []any {
	args := []any{studyLimsID, studyLimsID, studyLimsID}
	args = append(args, windowArgs...)
	for range 4 {
		args = append(args, studyLimsID)
	}

	return args
}

// platformsForStudySamplesSQL returns, for the given sample ids, every (sample,
// platform) pair the sample has within the study. The canonical platform name
// comes from which product-metrics mirror the sample appears in; ONT comes from
// oseq_flowcell_mirror. Membership is study-scoped so a sample shared across
// studies reports only the platforms it actually has products on in this study.
func platformsForStudySamplesSQL(placeholders string) string {
	scoped := func(table, platform string) string {
		return `SELECT id_sample_tmp, '` + platform + `' AS platform FROM ` + table +
			` WHERE id_study_lims = ? AND id_sample_tmp IN (` + placeholders + `)`
	}

	return strings.Join([]string{
		scoped("iseq_product_metrics_mirror", platformIllumina),
		scoped("pac_bio_product_metrics_mirror", platformPacBio),
		scoped("eseq_product_metrics_mirror", platformElembio),
		scoped("useq_product_metrics_mirror", platformUltimagen),
		scoped("oseq_flowcell_mirror", platformONT),
	}, " UNION ALL ")
}

// orderPlatformsBySample projects each sample's platform set onto the canonical
// order so the reported slice is deterministic.
func orderPlatformsBySample(platformSet map[int64]map[string]struct{}) map[int64][]string {
	platformsBySample := make(map[int64][]string, len(platformSet))
	for sampleID, set := range platformSet {
		ordered := make([]string, 0, len(set))
		for _, platform := range platformCanonicalOrder {
			if _, ok := set[platform]; ok {
				ordered = append(ordered, platform)
			}
		}
		platformsBySample[sampleID] = ordered
	}

	return platformsBySample
}

func parseLastRunValue(table string, raw any) (time.Time, error) {
	switch value := raw.(type) {
	case nil:
		return time.Time{}, nil
	case string:
		if strings.TrimSpace(value) == "" {
			return time.Time{}, nil
		}
	case []byte:
		if strings.TrimSpace(string(value)) == "" {
			return time.Time{}, nil
		}
	}

	parsed, err := parseSyncTimeValue(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("mlwh: parse last_run for %s: %w", table, err)
	}
	if parsed.IsZero() {
		return time.Time{}, nil
	}

	return parsed.UTC(), nil
}

// normalizeAddedWindowArgs converts the RFC3339 since/until bounds into the
// normalised-UTC string args the [since, until) created filter binds, formatted
// like the stored iRODS created column (formatSyncTime) so the comparison over
// the (id_study_lims, created) index is a correct lexical comparison. An empty
// until is open-ended, represented by the maximum sentinel so created < until is
// always true. The HTTP handler rejects malformed bounds before this is reached;
// this re-validates defensively so a direct Client caller still gets an error
// rather than a silently wrong count.
func normalizeAddedWindowArgs(since, until string) ([]any, error) {
	sinceUTC, err := parseSyncTimeString(since)
	if err != nil {
		return nil, fmt.Errorf("%w: parse since %q: %w", ErrUpstreamImpaired, since, err)
	}

	untilArg := addedWindowOpenEnded
	if until != "" {
		untilUTC, err := parseSyncTimeString(until)
		if err != nil {
			return nil, fmt.Errorf("%w: parse until %q: %w", ErrUpstreamImpaired, until, err)
		}

		untilArg = formatSyncTime(untilUTC)
	}

	return []any{formatSyncTime(sinceUTC), untilArg}, nil
}

func latestDataForStudyQuery(studyLimsID, fileType string, limit, offset int) (string, []any, error) {
	query, args, err := latestDataFilterQuery(latestDataForStudySQLPrefix, "", fileType, studyLimsID)
	if err != nil {
		return "", nil, err
	}

	return query + latestDataCreatedOrderSQL + ` LIMIT ? OFFSET ?`, append(args, limit, offset), nil
}

func latestDataForFacultySponsorQuery(studyIDs []string, fileType string, limit, offset int) (string, []any, error) {
	perStudyLimit := limit + offset
	parts := make([]string, 0, len(studyIDs))
	args := make([]any, 0, len(studyIDs)*3+2)

	for index, studyID := range studyIDs {
		query, queryArgs, err := latestDataFilterQuery(latestDataForStudySQLPrefix, "", fileType, studyID)
		if err != nil {
			return "", nil, err
		}

		parts = append(parts, fmt.Sprintf("SELECT * FROM (%s%s LIMIT ?) AS latest_%d", query, latestDataCreatedOrderSQL, index))
		args = append(args, queryArgs...)
		args = append(args, perStudyLimit)
	}

	query := `SELECT created, id_product, collection, data_object, id_study_lims, study_name, name, supplier_name, id_run, lane, tag_index, platform, merged FROM (` +
		strings.Join(parts, ` UNION ALL `) +
		`) AS per_study_latest ORDER BY created DESC, id_run, id_product LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	return query, args, nil
}

func scanRecentDataRow(scan func(dest ...any) error) (RecentDataRow, error) {
	var (
		row        RecentDataRow
		idProduct  string
		collection string
		dataObject string
		merged     int
	)
	if err := scan(
		&row.Created,
		&idProduct,
		&collection,
		&dataObject,
		&row.IDStudyLims,
		&row.StudyName,
		&row.Name,
		&row.SupplierName,
		&row.IDRun,
		&row.Position,
		&row.TagIndex,
		&row.Platform,
		&merged,
	); err != nil {
		return RecentDataRow{}, err
	}

	row.IRODSPath = strings.TrimRight(collection, "/") + "/" + dataObject
	row.Merged = merged != 0

	return row, nil
}

// SamplesWithData lists the distinct samples linked to the study that have at
// least one study-scoped iRODS row ("data available for this study"), paginated
// like the other study fan-outs. Each row is platform-qualified (see
// SampleWithData). It shares the study membership join with SamplesWithoutData,
// so the two lists partition the study's linked samples (their union equals
// samples_total). The never-synced / unknown-study / synced-empty cascade
// matches SamplesForStudy.
func (c *Client) SamplesWithData(ctx context.Context, studyLimsID string, limit, offset int) ([]SampleWithData, error) {
	return c.samplesByDataPartition(ctx, samplesWithDataCacheSQL, studyLimsID, "query study samples with data", studyLimsID, limit, offset)
}

// SamplesWithoutData lists the study's linked samples that have no study-scoped
// iRODS row (the complement of SamplesWithData: sequenced-no-data, registered,
// and ONT), paginated and platform-qualified. The cascade matches SamplesForStudy.
func (c *Client) SamplesWithoutData(ctx context.Context, studyLimsID string, limit, offset int) ([]SampleWithData, error) {
	return c.samplesByDataPartition(ctx, samplesWithoutDataCacheSQL, studyLimsID, "query study samples without data", studyLimsID, limit, offset)
}

// SamplesWithDataSince lists the distinct samples linked to the study whose
// study-scoped iRODS data was added to iRODS in the half-open window
// [since, until): it filters on the mirrored created column (the iRODS CREATION
// timestamp, NEVER last_updated/last_run) over the (id_study_lims, created)
// index, with created >= since (since included) AND created < until (until
// excluded); until is optional and the window is open-ended when it is empty.
// Both bounds are compared in normalised UTC. It is the windowed variant of
// SamplesWithData over the same membership join and the same study-scoped iRODS
// existence predicate (studyScopedIRODSExists), paginated and platform-qualified
// identically, so the in-window list and the windowed count
// (CountSamplesWithDataSince) stay the exact list<->count cross-check. WITHOUT a
// since (empty), it reuses the all-time SamplesWithData path, so the two never
// diverge; an until supplied without a since is rejected (until is only the upper
// bound of a window, so it is meaningless alone) rather than silently dropped. The
// never-synced / unknown-study / synced-empty cascade matches SamplesWithData. The
// since/until values are validated as RFC3339 at the HTTP handler, which returns
// 400 before this method is reached, so callers of this method pass them through.
func (c *Client) SamplesWithDataSince(ctx context.Context, studyLimsID, since, until string, limit, offset int) ([]SampleWithData, error) {
	if since == "" {
		if until != "" {
			return nil, errUntilRequiresSince
		}

		return c.SamplesWithData(ctx, studyLimsID, limit, offset)
	}

	window, err := normalizeAddedWindowArgs(since, until)
	if err != nil {
		return nil, err
	}

	queryArgs := append([]any{studyLimsID}, window...)
	queryArgs = append(queryArgs, limit, offset)

	return c.samplesByDataPartition(ctx, samplesAddedSinceCacheSQL, studyLimsID, "query study samples with data since", queryArgs...)
}

// CountSamplesWithData counts the distinct samples linked to the study that have
// at least one study-scoped iRODS row ("data available for this study"), the
// count counterpart of SamplesWithData over the same membership join, so
// CountSamplesWithData(study) equals len(SamplesWithData(study, all)) for any
// study. It counts distinct samples, never iRODS data objects. The
// never-synced / unknown-study / synced-empty cascade matches CountSamplesForStudy:
// a synced study with no samples-with-data returns Count{Count: 0} and no error,
// an unknown study returns ErrNotFound, and a never-synced cache returns Count{}
// with an error satisfying both ErrCacheNeverSynced and ErrNotFound.
func (c *Client) CountSamplesWithData(ctx context.Context, studyLimsID string) (Count, error) {
	count, err := c.queryCount(ctx, countSamplesWithDataCacheSQL, "count study samples with data", studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	return c.countSamplesForEmptyStudy(ctx, studyLimsID)
}

// CountSamplesWithDataSince counts the distinct samples linked to the study whose
// study-scoped iRODS data was added to iRODS in the half-open window
// [since, until): it filters on the mirrored created column (the iRODS CREATION
// timestamp, NEVER last_updated/last_run) over the (id_study_lims, created)
// index, with created >= since (since included) AND created < until (until
// excluded); until is optional and the window is open-ended when it is empty.
// Both bounds are compared in normalised UTC. It is the windowed variant of
// CountSamplesWithData over the same membership join and the same study-scoped
// iRODS existence predicate (studyScopedIRODSExists), so it counts distinct
// SAMPLES, never iRODS data objects. WITHOUT a since (empty), it reuses the
// all-time CountSamplesWithData path, so the two never diverge; an until supplied
// without a since is rejected (until is only the upper bound of a window, so it is
// meaningless alone) rather than silently dropped. The
// never-synced / unknown-study / synced-empty cascade matches CountSamplesWithData:
// a synced study with no in-window samples-with-data returns Count{Count: 0} and
// no error, an unknown study returns ErrNotFound, and a never-synced cache returns
// Count{} with an error satisfying both ErrCacheNeverSynced and ErrNotFound. The
// since/until values are validated as RFC3339 at the HTTP handler, which returns
// 400 before this method is reached, so callers of this method pass them through.
func (c *Client) CountSamplesWithDataSince(ctx context.Context, studyLimsID, since, until string) (Count, error) {
	if since == "" {
		if until != "" {
			return Count{}, errUntilRequiresSince
		}

		return c.CountSamplesWithData(ctx, studyLimsID)
	}

	window, err := normalizeAddedWindowArgs(since, until)
	if err != nil {
		return Count{}, err
	}

	args := append([]any{studyLimsID}, window...)
	count, err := c.queryCount(ctx, countSamplesAddedSinceCacheSQL, "count study samples with data since", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	return c.countSamplesForEmptyStudy(ctx, studyLimsID)
}

// LatestDataForStudy returns one bounded, pageable newest-first page of raw iRODS
// location rows for a study. Membership is the raw
// seq_product_irods_locations_mirror scan scoped by (id_study_lims, created),
// not the manifest/product grain, so the first row's created timestamp reconciles
// with StudyOverview.newest_data_added. Ties are stable by (id_run, id_product).
func (c *Client) LatestDataForStudy(ctx context.Context, studyLimsID, fileType string, limit, offset int) ([]RecentDataRow, error) {
	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableStudy); syncErr != nil {
				if errors.Is(syncErr, ErrCacheNeverSynced) {
					return []RecentDataRow{}, syncErr
				}

				return nil, syncErr
			}

			return nil, ErrNotFound
		}

		return nil, err
	}

	query, args, err := latestDataForStudyQuery(study.IDStudyLims, fileType, limit, offset)
	if err != nil {
		return nil, err
	}

	rows, err := c.queryRecentDataRows(ctx, query, args, "query latest data for study")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []RecentDataRow{}, syncErr
			}

			return nil, syncErr
		}
	}

	return rows, nil
}

// LatestDataForFacultySponsor returns a newest-first page across all SQSCP
// studies whose faculty_sponsor contains name. It fetches each matching study's
// bounded top rows through the study+created access path and merges that bounded
// candidate set, avoiding both one-query-per-study fan-out and an unbounded
// all-files sort.
func (c *Client) LatestDataForFacultySponsor(ctx context.Context, name, fileType string, limit, offset int) ([]RecentDataRow, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("%w: faculty sponsor name is required", ErrUnsupportedIdentifier)
	}
	if _, err := normaliseFileType(fileType); err != nil {
		return nil, err
	}

	studyIDs, err := c.latestDataFacultySponsorStudyIDs(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(studyIDs) == 0 || limit == 0 {
		return []RecentDataRow{}, nil
	}

	query, args, err := latestDataForFacultySponsorQuery(studyIDs, fileType, limit, offset)
	if err != nil {
		return nil, err
	}

	rows, err := c.queryRecentDataRows(ctx, query, args, "query latest data for faculty sponsor")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []RecentDataRow{}, syncErr
			}

			return nil, syncErr
		}
	}

	return rows, nil
}

func (c *Client) latestDataFacultySponsorStudyIDs(ctx context.Context, name string) ([]string, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, latestDataFacultySponsorStudyIDsSQL, likeContainsArgs(escapeLIKEPattern(name), facultySponsorField)...)
	if err != nil {
		return nil, fmt.Errorf("%w: query latest-data faculty sponsor studies: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studyIDs := make([]string, 0)
	for rows.Next() {
		var studyID string
		if err = rows.Scan(&studyID); err != nil {
			return nil, fmt.Errorf("%w: scan latest-data faculty sponsor studies: %w", ErrUpstreamImpaired, err)
		}

		studyIDs = append(studyIDs, studyID)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query latest-data faculty sponsor studies: %w", ErrUpstreamImpaired, err)
	}
	if len(studyIDs) > 0 {
		return studyIDs, nil
	}
	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return []string{}, err
	}

	return []string{}, nil
}

func (c *Client) queryRecentDataRows(ctx context.Context, query string, args []any, action string) ([]RecentDataRow, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}
	defer func() { _ = rows.Close() }()

	recent := make([]RecentDataRow, 0)
	for rows.Next() {
		row, scanErr := scanRecentDataRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, scanErr)
		}

		recent = append(recent, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}

	return recent, nil
}

// StudyOverview returns the fixed-size study aggregate (spec B1): the
// distinct-sample partition (samples_total / with_data / without_data /
// sequenced_no_data, most-advanced-phase precedence with_data > sequenced_no_data
// > registered), the study-scoped iRODS data-object count and sequencing date
// range, the distinct runs / libraries / sorted library types, the recency
// figures (newest_data_added and the half-open [now-7d, now) added_last_7_days
// over the iRODS created column), and cache_synced_at (the oldest last_run across
// the feeding tables). Every figure is a single indexed aggregate. The
// never-synced / unknown-study / synced-empty cascade matches CountSamplesForStudy:
// a never-synced cache returns the zero value with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound, an unknown study returns ErrNotFound, and a
// synced study with no samples returns an all-zero overview with cache_synced_at
// populated.
func (c *Client) StudyOverview(ctx context.Context, studyLimsID string) (StudyOverview, error) {
	overview := StudyOverview{IDStudyLims: studyLimsID}
	total, err := c.fillStudyOverviewCounts(ctx, studyLimsID, &overview)
	if err != nil {
		return StudyOverview{}, err
	}
	if total == 0 {
		return c.studyOverviewForEmptyStudy(ctx, studyLimsID)
	}

	if err = c.fillPopulatedStudyOverviewIndependentFields(ctx, studyLimsID, &overview); err != nil {
		return StudyOverview{}, err
	}

	return overview, nil
}

// fillStudyOverviewMetadata fills the study-level metadata (name,
// accession_number, faculty_sponsor, data_access_group) from study_mirror, read
// in one row via resolveStudyFromCache (the cheapest existing study-resolution
// path). It is shared by the populated and the synced-empty paths so the four
// fields are present whenever the study exists (D5), with the counts at 0 on the
// empty path.
func (c *Client) fillStudyOverviewMetadata(ctx context.Context, studyLimsID string, overview *StudyOverview) error {
	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		return err
	}

	overview.Name = study.Name
	overview.AccessionNumber = study.AccessionNumber
	overview.FacultySponsor = study.FacultySponsor
	overview.Programme = study.Programme
	overview.DataAccessGroup = study.DataAccessGroup

	return nil
}

// fillStudyOverviewCounts fills the distinct-sample partition and the recency
// count in one set-at-once aggregate over the distinct linked-sample set. The
// data/product/recent arms are pre-filtered by study and then joined once, avoiding
// the repeated correlated EXISTS probes that are too slow for study 7699 scale.
func (c *Client) fillStudyOverviewCounts(ctx context.Context, studyLimsID string, overview *StudyOverview) (int, error) {
	db := c.readCacheDB()
	if db == nil {
		return 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	var (
		total           int
		withData        sql.NullInt64
		sequencedNoData sql.NullInt64
		addedLast7Days  sql.NullInt64
	)
	args := studyOverviewCountsArgs(studyLimsID, c.studyOverviewWindowArgs())
	if err := db.QueryRowContext(ctx, studyOverviewCountsCacheSQL, args...).
		Scan(&total, &withData, &sequencedNoData, &addedLast7Days); err != nil {
		return 0, fmt.Errorf("%w: aggregate study overview counts: %w", ErrUpstreamImpaired, err)
	}

	overview.SamplesTotal = total
	overview.SamplesWithData = int(withData.Int64)
	overview.SamplesWithoutData = total - int(withData.Int64)
	overview.SamplesSequencedNoData = int(sequencedNoData.Int64)
	overview.AddedLast7Days = int(addedLast7Days.Int64)

	return total, nil
}

// studyOverviewWindowArgs are the half-open [now-7d, now) bounds for
// added_last_7_days, formatted like the stored iRODS created column so the
// string comparison over the (id_study_lims, created) index is correct. "now" is
// injected via the client clock so tests are deterministic.
func (c *Client) studyOverviewWindowArgs() []any {
	now := c.clockNow().UTC()
	since := now.AddDate(0, 0, -7)

	return []any{formatSyncTime(since), formatSyncTime(now)}
}

// fillStudyOverviewIRODS fills data_objects, sequencing_date_range and
// newest_data_added from one study-scoped iRODS aggregate (COUNT, MIN, MAX
// created). The date range and newest_data_added are omitted when the study has
// no iRODS rows (e.g. mid-sequencing, before delivery to iRODS), so a study with
// linked samples but zero data objects reports data_objects=0 rather than erroring
// on the NULL MIN/MAX.
func (c *Client) fillStudyOverviewIRODS(ctx context.Context, studyLimsID string, overview *StudyOverview) error {
	db := c.readCacheDB()
	if db == nil {
		return fmt.Errorf("mlwh: cache reader not configured")
	}

	var (
		dataObjects int
		minCreated  any
		maxCreated  any
	)
	if err := db.QueryRowContext(ctx, studyOverviewIRODSAggregateSQL, studyLimsID).Scan(&dataObjects, &minCreated, &maxCreated); err != nil {
		return fmt.Errorf("%w: aggregate study irods for overview: %w", ErrUpstreamImpaired, err)
	}
	if dataObjects == 0 {
		overview.DataObjects = 0

		return nil
	}

	earliest, err := formatFreshnessTime(minCreated)
	if err != nil {
		return fmt.Errorf("mlwh: parse study irods created range for overview: %w", err)
	}

	latest, err := formatFreshnessTime(maxCreated)
	if err != nil {
		return fmt.Errorf("mlwh: parse study irods created range for overview: %w", err)
	}

	overview.DataObjects = dataObjects
	overview.NewestDataAdded = latest
	if earliest != "" || latest != "" {
		overview.SequencingDateRange = &DateRange{Earliest: earliest, Latest: latest}
	}

	return nil
}

// studyOverviewLibraryTypes lists the distinct library types present in the study,
// sorted, as a non-nil slice (empty rather than null when the study has none).
func (c *Client) studyOverviewLibraryTypes(ctx context.Context, studyLimsID string) ([]string, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, studyOverviewLibraryTypesCacheSQL, studyLimsID)
	if err != nil {
		return nil, fmt.Errorf("%w: query study library types for overview: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	libraryTypes := make([]string, 0)
	for rows.Next() {
		var libraryType string
		if err = rows.Scan(&libraryType); err != nil {
			return nil, fmt.Errorf("%w: scan study library types for overview: %w", ErrUpstreamImpaired, err)
		}

		libraryTypes = append(libraryTypes, libraryType)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study library types for overview: %w", ErrUpstreamImpaired, err)
	}
	slices.Sort(libraryTypes)

	return libraryTypes, nil
}

// oldestFeedingLastRun returns the oldest last_run (UTC RFC3339) across the given
// feeding tables, considering only tables that have ever synced. A table with no
// sync_state row is skipped rather than dragging the result to empty.
func (c *Client) oldestFeedingLastRun(ctx context.Context, tables []string) (string, error) {
	db := c.readCacheDB()
	if db == nil {
		return "", fmt.Errorf("mlwh: cache reader not configured")
	}
	if len(tables) == 0 {
		return "", nil
	}

	args := make([]any, 0, len(tables))
	for _, table := range tables {
		args = append(args, table)
	}

	rows, err := db.QueryContext(ctx,
		`SELECT table_name, last_run FROM sync_state WHERE table_name IN (`+placeholders(len(tables))+`)`,
		args...,
	)
	if err != nil {
		return "", fmt.Errorf("%w: query feeding sync state last_run: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	var oldest time.Time
	for rows.Next() {
		var (
			table   string
			lastRaw any
		)
		if err = rows.Scan(&table, &lastRaw); err != nil {
			return "", fmt.Errorf("%w: scan feeding sync state last_run: %w", ErrUpstreamImpaired, err)
		}

		lastRun, err := parseLastRunValue(table, lastRaw)
		if err != nil {
			return "", err
		}
		if lastRun.IsZero() {
			continue
		}
		if oldest.IsZero() || lastRun.Before(oldest) {
			oldest = lastRun
		}
	}
	if err = rows.Err(); err != nil {
		return "", fmt.Errorf("%w: query feeding sync state last_run: %w", ErrUpstreamImpaired, err)
	}

	if oldest.IsZero() {
		return "", nil
	}

	return oldest.UTC().Format(utcRFC3339Layout), nil
}

// studyOverviewForEmptyStudy resolves a StudyOverview when no samples are linked to
// the study, mirroring CountSamplesForStudy's cascade: a never-synced cache returns
// the joined sentinel, an unknown study returns ErrNotFound, and a synced study
// with no samples returns an all-zero overview carrying cache_synced_at.
func (c *Client) studyOverviewForEmptyStudy(ctx context.Context, studyLimsID string) (StudyOverview, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return StudyOverview{}, err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return StudyOverview{}, err
		}
		if summary.allAbsent || !summary.allPresent {
			return StudyOverview{}, neverSyncedReadErr()
		}

		syncedAt, err := c.oldestFeedingLastRun(ctx, studyOverviewFeedingTables)
		if err != nil {
			return StudyOverview{}, err
		}

		overview := StudyOverview{IDStudyLims: studyLimsID, LibraryTypes: []string{}, CacheSyncedAt: syncedAt}
		if err := c.fillStudyOverviewMetadata(ctx, studyLimsID, &overview); err != nil {
			return StudyOverview{}, err
		}

		return overview, nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return StudyOverview{}, err
	}

	return StudyOverview{}, ErrNotFound
}

// RunOverview returns the fixed-size run aggregate (spec D1): the distinct samples
// and distinct studies on the run, the run-scoped iRODS data-object count and
// sequencing date range, and cache_synced_at (the oldest last_run across the
// feeding tables). idRun is the Illumina NPG id_run (the existing Run/ResolveRun
// space; no new resolver) -- a non-Illumina or invalid run yields the existing
// not-found / unsupported-identifier error, and a numeric run absent from the
// synced cache yields ErrNotFound. It is a separate small aggregate, NOT folded
// into RunDetail. The run links to its samples and studies through
// iseq_product_metrics_mirror (the SamplesForRun / RunsForStudy source) and to its
// iRODS data objects through the shared id_iseq_product, so every figure is a
// single indexed aggregate. The never-synced cascade matches the run space: a
// never-synced cache returns the zero value with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound (via ResolveRun).
func (c *Client) RunOverview(ctx context.Context, idRun string) (RunOverview, error) {
	match, err := c.ResolveRun(ctx, idRun)
	if err != nil {
		return RunOverview{}, err
	}

	runID := match.Run.IDRun
	overview := RunOverview{IDRun: runID}
	if err = c.fillRunOverviewSamplesStudies(ctx, runID, &overview); err != nil {
		return RunOverview{}, err
	}
	if err = c.fillRunOverviewIRODS(ctx, runID, &overview); err != nil {
		return RunOverview{}, err
	}

	syncedAt, err := c.oldestFeedingLastRun(ctx, runOverviewFeedingTables)
	if err != nil {
		return RunOverview{}, err
	}
	overview.CacheSyncedAt = syncedAt

	return overview, nil
}

// fillRunOverviewSamplesStudies fills the distinct samples and distinct studies on
// the run from one indexed aggregate over iseq_product_metrics_mirror.
func (c *Client) fillRunOverviewSamplesStudies(ctx context.Context, runID int, overview *RunOverview) error {
	db := c.readCacheDB()
	if db == nil {
		return fmt.Errorf("mlwh: cache reader not configured")
	}

	var samples, studies int
	if err := db.QueryRowContext(ctx, runOverviewSamplesStudiesCacheSQL, runID).Scan(&samples, &studies); err != nil {
		return fmt.Errorf("%w: aggregate run samples and studies for overview: %w", ErrUpstreamImpaired, err)
	}

	overview.Samples = samples
	overview.Studies = studies

	return nil
}

// fillRunOverviewIRODS fills data_objects and sequencing_date_range from one
// run-scoped iRODS aggregate (COUNT, MIN, MAX created) joined to the run via the
// shared id_iseq_product. The date range is omitted when the run has no iRODS rows.
func (c *Client) fillRunOverviewIRODS(ctx context.Context, runID int, overview *RunOverview) error {
	db := c.readCacheDB()
	if db == nil {
		return fmt.Errorf("mlwh: cache reader not configured")
	}

	var (
		dataObjects int
		minCreated  any
		maxCreated  any
	)
	if err := db.QueryRowContext(ctx, runOverviewIRODSAggregateSQL, runID).Scan(&dataObjects, &minCreated, &maxCreated); err != nil {
		return fmt.Errorf("%w: aggregate run irods for overview: %w", ErrUpstreamImpaired, err)
	}
	if dataObjects == 0 {
		overview.DataObjects = 0

		return nil
	}

	earliest, err := formatFreshnessTime(minCreated)
	if err != nil {
		return fmt.Errorf("mlwh: parse run irods created range for overview: %w", err)
	}

	latest, err := formatFreshnessTime(maxCreated)
	if err != nil {
		return fmt.Errorf("mlwh: parse run irods created range for overview: %w", err)
	}

	overview.DataObjects = dataObjects
	if earliest != "" || latest != "" {
		overview.SequencingDateRange = &DateRange{Earliest: earliest, Latest: latest}
	}

	return nil
}

// clockNow returns the client's injectable clock, defaulting to time.Now. It is
// the package's testable-now idiom (cf. expandIdentifierNow): tests set c.now to a
// fixed instant so the half-open added_last_7_days window is deterministic.
func (c *Client) clockNow() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}

	return time.Now()
}

// samplesByDataPartition runs one side of the with/without-data partition and
// enriches each sample with its platforms. The query's bind args (queryArgs)
// carry the study id, the optional [since, until) window bounds, and the
// LIMIT/OFFSET, so the all-time list (study, limit, offset) and the windowed list
// (study, since, until, limit, offset) share this one path; studyLimsID is passed
// separately for the enrichment and empty-result cascade. An empty result
// resolves through the same never-synced / unknown-study / synced-empty cascade as
// SamplesForStudy so the partition agrees with the study membership on every edge
// case.
func (c *Client) samplesByDataPartition(ctx context.Context, query, studyLimsID, action string, queryArgs ...any) ([]SampleWithData, error) {
	cacheDB := c.readCacheDB()
	if cacheDB == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, cacheDB, query, action, queryArgs...)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		return c.enrichSamplesWithPlatforms(ctx, studyLimsID, samples)
	}

	if emptyErr := c.studyDataPartitionEmpty(ctx, studyLimsID); emptyErr != nil {
		return []SampleWithData{}, emptyErr
	}

	return []SampleWithData{}, nil
}

// studyDataPartitionEmpty resolves an empty with/without-data partition exactly
// as SamplesForStudy resolves an empty sample list: a never-synced cache returns
// the joined sentinel, an unknown study returns ErrNotFound, and a synced study
// with no linked samples returns nil (an empty list, no error).
func (c *Client) studyDataPartitionEmpty(ctx context.Context, studyLimsID string) error {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return err
		}
		if summary.allAbsent || !summary.allPresent {
			return neverSyncedReadErr()
		}

		return nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return err
	}

	return ErrNotFound
}

// enrichSamplesWithPlatforms wraps each sample with the platforms it has products
// on within the study. A sample with no products and no ONT flowcell (a
// registered-only sample) carries an empty, non-nil Platforms slice.
func (c *Client) enrichSamplesWithPlatforms(ctx context.Context, studyLimsID string, samples []Sample) ([]SampleWithData, error) {
	platformsBySample, err := c.loadStudySamplePlatforms(ctx, studyLimsID, samples)
	if err != nil {
		return nil, err
	}

	enriched := make([]SampleWithData, 0, len(samples))
	for _, sample := range samples {
		platforms := platformsBySample[sample.IDSampleTmp]
		if platforms == nil {
			platforms = []string{}
		}

		enriched = append(enriched, SampleWithData{Sample: sample, Platforms: platforms})
	}

	return enriched, nil
}

// loadStudySamplePlatforms batch-loads the canonical platforms per sample for one
// page of the partition, returning them in platformCanonicalOrder.
func (c *Client) loadStudySamplePlatforms(ctx context.Context, studyLimsID string, samples []Sample) (map[int64][]string, error) {
	if len(samples) == 0 {
		return map[int64][]string{}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	sampleIDs := make([]int64, 0, len(samples))
	seen := make(map[int64]struct{}, len(samples))
	for _, sample := range samples {
		if _, ok := seen[sample.IDSampleTmp]; ok {
			continue
		}

		seen[sample.IDSampleTmp] = struct{}{}
		sampleIDs = append(sampleIDs, sample.IDSampleTmp)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(sampleIDs)), ",")
	args := make([]any, 0, 5*(len(sampleIDs)+1))
	for range platformCanonicalOrder {
		args = append(args, studyLimsID)
		for _, sampleID := range sampleIDs {
			args = append(args, sampleID)
		}
	}

	rows, err := db.QueryContext(ctx, platformsForStudySamplesSQL(placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query sample platforms: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	platformSet := make(map[int64]map[string]struct{}, len(sampleIDs))
	for rows.Next() {
		var (
			sampleID int64
			platform string
		)
		if err = rows.Scan(&sampleID, &platform); err != nil {
			return nil, fmt.Errorf("%w: scan sample platforms: %w", ErrUpstreamImpaired, err)
		}
		if platformSet[sampleID] == nil {
			platformSet[sampleID] = make(map[string]struct{}, len(platformCanonicalOrder))
		}
		platformSet[sampleID][platform] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query sample platforms: %w", ErrUpstreamImpaired, err)
	}

	return orderPlatformsBySample(platformSet), nil
}
