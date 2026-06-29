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
	"errors"
	"fmt"
	"strconv"
)

// countStudiesCacheSQL counts the SQSCP studies mirrored in the cache, matching
// the id_lims filter used by AllStudies.
const countStudiesCacheSQL = `SELECT COUNT(*) FROM study_mirror WHERE id_lims = 'SQSCP'`

// countSamplesForStudyCacheSQL counts the distinct samples linked to a study via
// library_samples. It uses the same join and filter as SamplesForStudy
// (samplesForStudyCacheSQL) without a LIMIT, so the count equals the number of
// rows SamplesForStudy returns when fetching all of them.
const countSamplesForStudyCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ?`

// The /count counterparts below each reuse the exact WHERE/JOIN of the list
// they size (samplesForRunCacheSQL, librariesForStudySQL, ... in hierarchy.go)
// with no LIMIT, so each count equals len(list-all) and the two cannot drift.
// COUNT(DISTINCT ...) mirrors a single-column SELECT DISTINCT list; a list whose
// DISTINCT spans several columns is sized by COUNT(*) over the same
// SELECT DISTINCT subquery (SQLite/MySQL have no COUNT(DISTINCT a, b, ...)).
const (
	// countSamplesForRunCacheSQL sizes SamplesForRun (samplesForRunCacheSQL): the
	// distinct samples on a run via iseq_product_metrics_mirror.
	countSamplesForRunCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM iseq_product_metrics_mirror INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = iseq_product_metrics_mirror.id_sample_tmp WHERE iseq_product_metrics_mirror.id_run = ?`

	// countSamplesForLibraryCacheSQL sizes SamplesForLibrary
	// (samplesForLibraryCacheSQL): distinct samples in a library type within a
	// study.
	countSamplesForLibraryCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ?`

	// countSamplesForLibraryTypeCacheSQL sizes SamplesForLibraryType
	// (samplesForLibraryTypeCacheSQL): distinct samples in a library type across
	// all studies.
	countSamplesForLibraryTypeCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ?`

	// countSamplesForLibraryIDCacheSQL sizes SamplesForLibraryID
	// (sampleStudyPairsForLibraryID, de-duplicated by id_sample_tmp): distinct
	// samples linked through an exact library_id.
	countSamplesForLibraryIDCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.library_id = ?`

	// countSamplesForLibraryLimsIDCacheSQL sizes SamplesForLibraryLimsID
	// (sampleStudyPairsForLibraryLimsID, de-duplicated by id_sample_tmp): distinct
	// samples linked through an exact id_library_lims.
	countSamplesForLibraryLimsIDCacheSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_library_lims = ?`

	// countRunsForStudyCacheSQL sizes RunsForStudy (runsForStudyCacheSQL): the
	// distinct runs for a study.
	countRunsForStudyCacheSQL = `SELECT COUNT(DISTINCT id_run) FROM iseq_product_metrics_mirror WHERE id_study_lims = ?`

	// countLibrariesForStudyCacheSQL sizes LibrariesForStudy (librariesForStudySQL,
	// which groups by the (pipeline_id_lims, library_id, id_library_lims) triple):
	// the distinct triples for a study, via COUNT(*) over the same SELECT DISTINCT.
	countLibrariesForStudyCacheSQL = `SELECT COUNT(*) FROM (SELECT DISTINCT pipeline_id_lims, library_id, id_library_lims FROM library_samples WHERE id_study_lims = ?) AS distinct_libraries`

	// countLanesForSampleCacheSQL sizes LanesForSample (lanesForSampleCacheSQL,
	// SELECT DISTINCT id_run, position, tag_index): the distinct run/lane/tag
	// triples for a sample, via COUNT(*) over the same SELECT DISTINCT.
	countLanesForSampleCacheSQL = `SELECT COUNT(*) FROM (SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ?) AS distinct_lanes`

	// countIRODSPathsForSampleCacheSQLPrefix/Suffix size IRODSPathsForSample (the
	// SELECT DISTINCT id_iseq_product, irods_collection, irods_file_name list): the
	// distinct iRODS data objects for a sample, via COUNT(*) over the same SELECT
	// DISTINCT. The file-type filter (B2) is spliced into the inner WHERE between
	// the parent predicate and the closing paren (irodsCountFileTypeQuery), so the
	// count honours the same filter as the list and count == len(list) for every
	// file_type.
	countIRODSPathsForSampleCacheSQLPrefix = `SELECT COUNT(*) FROM (SELECT DISTINCT id_iseq_product, irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_sample_tmp = ?`
	countIRODSPathsForSampleCacheSQLSuffix = `) AS distinct_sample_irods`

	// countIRODSPathsForStudyCacheSQLPrefix/Suffix size IRODSPathsForStudy: the
	// distinct iRODS rows for a study under the same LEFT JOIN to sample_mirror and
	// the same DISTINCT projection, via COUNT(*) over that SELECT DISTINCT. The
	// file-type filter (B2) is spliced into the inner WHERE the same way as the
	// sample count.
	countIRODSPathsForStudyCacheSQLPrefix = `SELECT COUNT(*) FROM (SELECT DISTINCT spi.id_iseq_product, spi.irods_collection, spi.irods_file_name, spi.id_sample_tmp, COALESCE(sample_mirror.name, '') FROM seq_product_irods_locations_mirror spi LEFT JOIN sample_mirror ON sample_mirror.id_sample_tmp = spi.id_sample_tmp WHERE spi.id_study_lims = ?`
	countIRODSPathsForStudyCacheSQLSuffix = `) AS distinct_study_irods`

	// countIRODSPathsForRunCacheSQLPrefix/Suffix size IRODSPathsForRun (B3): the
	// run's iseq_product_metrics_mirror rows joined to the iRODS mirror on
	// id_iseq_product, the same join as irodsPathsForRunCacheSQL with no LIMIT.
	// COUNT(*) over the SELECT DISTINCT of the same iRODS data-object columns the
	// list groups by collapses the product-metrics fan-out identically, so
	// count == len(list) for the run scope. The file-type filter (B2) splices into
	// the inner WHERE between the id_run predicate and the closing paren.
	countIRODSPathsForRunCacheSQLPrefix = `SELECT COUNT(*) FROM (SELECT DISTINCT spi.id_iseq_product, spi.irods_collection, spi.irods_file_name, spi.platform FROM seq_product_irods_locations_mirror spi INNER JOIN iseq_product_metrics_mirror ipm ON ipm.id_iseq_product = spi.id_iseq_product WHERE ipm.id_run = ?`
	countIRODSPathsForRunCacheSQLSuffix = `) AS distinct_run_irods`
)

// countFindSamplesBySangerIDSQL and its siblings size the find/sample/* lists:
// each counts the SQSCP sample_mirror rows matching the exact field the
// corresponding Find method matches (findSamplesBySangerIDSQL, ... in
// hierarchy.go), with no LIMIT, so the count equals len(list) for an unambiguous
// match (the case the cross-check seeds) and reports the true multiplicity when
// the Find list would instead raise ErrAmbiguous.
const (
	countFindSamplesBySangerIDSQL     = `SELECT COUNT(*) FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP'`
	countFindSamplesByIDSampleLimsSQL = `SELECT COUNT(*) FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP'`
	countFindSamplesByAccessionSQL    = `SELECT COUNT(*) FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP'`
	countFindSamplesBySupplierSQL     = `SELECT COUNT(*) FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP'`

	// countFindSamplesByLibraryTypeSQL counts the distinct SQSCP samples linked to
	// a library type, mirroring findSamplesByLibraryTypeSQL's join and filter.
	countFindSamplesByLibraryTypeSQL = `SELECT COUNT(DISTINCT sample_mirror.id_sample_tmp) FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND sample_mirror.id_lims = 'SQSCP'`
)

// irodsCountFileTypeQuery assembles an iRODS count query from its prefix and
// suffix, splicing the file-type filter clause into the inner subquery's WHERE
// (between the parent predicate and the closing paren) when normalised is
// non-empty, and returns the query alongside its bound args (the parent id, then
// the suffix LIKE pattern when filtered). It mirrors irodsFileTypeQuery so the
// count applies exactly the filter the list does and count == len(list) holds.
func irodsCountFileTypeQuery(prefix, suffix, normalised string, parent any) (string, []any) {
	if normalised == "" {
		return prefix + suffix, []any{parent}
	}

	return prefix + irodsFileTypeFilterClause + suffix, []any{parent, irodsFileTypeLikePattern(normalised)}
}

// Count is the bare count envelope returned by the /count endpoints. It
// serialises as {"count": N}.
type Count struct {
	Count int `json:"count" doc:"number of matching rows"`
}

// CountStudies counts the studies mirrored in the cache (study_mirror rows with
// id_lims = 'SQSCP'), the count counterpart of AllStudies. A never-synced cache
// returns Count{} with an error satisfying both ErrCacheNeverSynced and
// ErrNotFound.
func (c *Client) CountStudies(ctx context.Context) (Count, error) {
	count, err := c.queryCount(ctx, countStudiesCacheSQL, "count studies")
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountSamplesForStudy counts the distinct samples linked to a study, the count
// counterpart of SamplesForStudy (same library_samples join and filter, no
// LIMIT), so CountSamplesForStudy(study) equals len(SamplesForStudy(study, all))
// for any study. A synced study with no samples returns Count{Count: 0} and no
// error; a never-synced cache returns Count{} with an error satisfying both
// ErrCacheNeverSynced and ErrNotFound, mirroring SamplesForStudy.
func (c *Client) CountSamplesForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	count, err := c.queryCount(ctx, countSamplesForStudyCacheSQL, "count study samples", studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	return c.countSamplesForEmptyStudy(ctx, studyLimsID)
}

// countSamplesForEmptyStudy resolves the result when no samples were counted for
// a study, mirroring SamplesForStudy's never-synced cascade so the count and the
// list agree on a never-synced cache, an unknown study, and a synced empty study.
func (c *Client) countSamplesForEmptyStudy(ctx context.Context, studyLimsID string) (Count, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return Count{}, err
		}
		if summary.allAbsent || !summary.allPresent {
			return Count{}, neverSyncedReadErr()
		}

		return Count{Count: 0}, nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return Count{}, err
	}

	return Count{}, ErrNotFound
}

// CountSamplesForRun counts the distinct samples on a run, the count
// counterpart of SamplesForRun (same iseq_product_metrics_mirror join and
// id_run filter, no LIMIT), so CountSamplesForRun(run) equals
// len(SamplesForRun(run, all)). A non-numeric run id is an unsupported
// identifier; a run absent from a synced cache returns ErrNotFound; a
// never-synced cache returns Count{} with both ErrCacheNeverSynced and
// ErrNotFound, mirroring SamplesForRun.
func (c *Client) CountSamplesForRun(ctx context.Context, idRun string) (Count, error) {
	runID, err := strconv.Atoi(idRun)
	if err != nil {
		return Count{}, ErrUnsupportedIdentifier
	}

	count, err := c.queryCount(ctx, countSamplesForRunCacheSQL, "count run samples", runID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireResolverSyncState(ctx, syncTableIseqProductMetrics); err != nil {
		return Count{}, err
	}

	return Count{}, ErrNotFound
}

// CountSamplesForLibrary counts the distinct samples in a library type within a
// study, the count counterpart of SamplesForLibrary (same library_samples join
// and pipeline/study filter, no LIMIT), so it equals
// len(SamplesForLibrary(pipeline, study, all)). It shares SamplesForLibrary's
// study-exists / synced-empty / unknown cascade.
func (c *Client) CountSamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string) (Count, error) {
	count, err := c.queryCount(ctx, countSamplesForLibraryCacheSQL, "count library samples", pipelineIDLims, studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	return c.countSamplesForEmptyStudy(ctx, studyLimsID)
}

// CountSamplesForLibraryID counts the distinct samples linked through an exact
// library_id, the count counterpart of SamplesForLibraryID (same
// library_samples.library_id filter, distinct samples, no LIMIT), so it equals
// len(SamplesForLibraryID(libraryID, all)). It shares the library-identifier
// never-synced cascade: an empty result on a synced cache is ErrNotFound.
func (c *Client) CountSamplesForLibraryID(ctx context.Context, libraryID string) (Count, error) {
	return c.countSamplesForLibraryIdentifier(ctx, countSamplesForLibraryIDCacheSQL, "count library-id samples", libraryID)
}

// CountSamplesForLibraryLimsID counts the distinct samples linked through an
// exact id_library_lims, the count counterpart of SamplesForLibraryLimsID, so it
// equals len(SamplesForLibraryLimsID(idLibraryLims, all)). It shares the
// library-identifier never-synced cascade.
func (c *Client) CountSamplesForLibraryLimsID(ctx context.Context, idLibraryLims string) (Count, error) {
	return c.countSamplesForLibraryIdentifier(ctx, countSamplesForLibraryLimsIDCacheSQL, "count library-lims-id samples", idLibraryLims)
}

// countSamplesForLibraryIdentifier is the shared body of the library-identifier
// counts (by library_id and by id_library_lims). It mirrors
// sampleStudyPairsForLibraryIdentifier's never-synced cascade: a zero count on a
// synced cache is ErrNotFound, and a never-synced cache returns Count{} joined
// with ErrCacheNeverSynced and ErrNotFound.
func (c *Client) countSamplesForLibraryIdentifier(ctx context.Context, query, action, identifier string) (Count, error) {
	count, err := c.queryCount(ctx, query, action, identifier)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireResolverSyncState(ctx, syncTableIseqFlowcell); err != nil {
		return Count{}, err
	}

	return Count{}, ErrNotFound
}

// CountSamplesForLibraryType counts the distinct samples in a library type
// across all studies, the count counterpart of SamplesForLibraryType (same
// pipeline_id_lims filter, no LIMIT), so it equals
// len(SamplesForLibraryType(pipeline, all)). It shares SamplesForLibraryType's
// synced-empty cascade: a zero count on a synced cache is Count{0}, and a
// never-synced cache returns Count{} joined with both sentinels.
func (c *Client) CountSamplesForLibraryType(ctx context.Context, pipelineIDLims string) (Count, error) {
	count, err := c.queryCount(ctx, countSamplesForLibraryTypeCacheSQL, "count library-type samples", pipelineIDLims)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
	if err != nil {
		return Count{}, err
	}
	if summary.allAbsent || !summary.allPresent {
		return Count{}, neverSyncedReadErr()
	}

	return Count{Count: 0}, nil
}

// CountRunsForStudy counts the distinct runs for a study, the count counterpart
// of RunsForStudy (same iseq_product_metrics_mirror id_study_lims filter, no
// LIMIT), so it equals len(RunsForStudy(study, all)). It shares RunsForStudy's
// study-exists / synced-empty / unknown cascade.
func (c *Client) CountRunsForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if !studyExists {
		if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
			return Count{}, err
		}

		return Count{}, ErrNotFound
	}

	count, err := c.queryCount(ctx, countRunsForStudyCacheSQL, "count study runs", studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableIseqProductMetrics); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountLibrariesForStudy counts the distinct libraries for a study, the count
// counterpart of LibrariesForStudy (same library_samples grouping by the
// (pipeline_id_lims, library_id, id_library_lims) triple, no LIMIT), so it
// equals len(LibrariesForStudy(study, all)). It shares LibrariesForStudy's
// study-resolve / synced-empty / unknown cascade.
func (c *Client) CountLibrariesForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	if _, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID); err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableStudy); syncErr != nil {
				return Count{}, syncErr
			}

			return Count{}, ErrNotFound
		}

		return Count{}, err
	}

	count, err := c.queryCount(ctx, countLibrariesForStudyCacheSQL, "count study libraries", studyLimsID)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableIseqFlowcell); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountLanesForSample counts the distinct run/lane/tag combinations for a sample
// (by Sanger sample name), the count counterpart of LanesForSample (same
// iseq_product_metrics_mirror SELECT DISTINCT id_run, position, tag_index, no
// LIMIT), so it equals len(LanesForSample(name, all)). It shares LanesForSample's
// sample-resolve / synced-empty / unknown cascade.
func (c *Client) CountLanesForSample(ctx context.Context, sangerName string) (Count, error) {
	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableSample); syncErr != nil {
				return Count{}, syncErr
			}

			return Count{}, ErrNotFound
		}

		return Count{}, err
	}

	count, err := c.queryCount(ctx, countLanesForSampleCacheSQL, "count sample lanes", sample.IDSampleTmp)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableIseqProductMetrics); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountIRODSPathsForSample counts the distinct iRODS data objects for a sample
// (by Sanger sample name), the count counterpart of IRODSPathsForSample (same
// seq_product_irods_locations_mirror SELECT DISTINCT id_iseq_product,
// irods_collection, irods_file_name, no LIMIT), so it equals
// len(IRODSPathsForSample(name, all)). It is the bare, all-file-types variant:
// it delegates to CountIRODSPathsForSampleByFileType with an empty fileType.
func (c *Client) CountIRODSPathsForSample(ctx context.Context, sangerName string) (Count, error) {
	return c.CountIRODSPathsForSampleByFileType(ctx, sangerName, "")
}

// CountIRODSPathsForSampleByFileType counts the distinct iRODS data objects for a
// sample, optionally restricted to data objects whose irods_file_name ends in
// `.<fileType>` (case-insensitive, leading dot stripped), so it equals
// len(IRODSPathsForSampleByFileType(name, fileType, all)) for any fileType. An
// empty fileType counts all objects (the bare behaviour); an invalid fileType is
// rejected with ErrUnsupportedIdentifier. It shares IRODSPathsForSample's
// sample-resolve / synced-empty / unknown cascade.
func (c *Client) CountIRODSPathsForSampleByFileType(ctx context.Context, sangerName, fileType string) (Count, error) {
	normalised, err := normaliseFileType(fileType)
	if err != nil {
		return Count{}, err
	}

	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableSample); syncErr != nil {
				return Count{}, syncErr
			}

			return Count{}, ErrNotFound
		}

		return Count{}, err
	}

	query, args := irodsCountFileTypeQuery(countIRODSPathsForSampleCacheSQLPrefix, countIRODSPathsForSampleCacheSQLSuffix, normalised, sample.IDSampleTmp)
	count, err := c.queryCount(ctx, query, "count sample irods paths", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountIRODSPathsForStudy counts the distinct iRODS data objects for a study, the
// count counterpart of IRODSPathsForStudy (same LEFT JOIN to sample_mirror and
// SELECT DISTINCT projection scoped by id_study_lims, no LIMIT), so it equals
// len(IRODSPathsForStudy(study, all)). It is the bare, all-file-types variant: it
// delegates to CountIRODSPathsForStudyByFileType with an empty fileType.
func (c *Client) CountIRODSPathsForStudy(ctx context.Context, studyLimsID string) (Count, error) {
	return c.CountIRODSPathsForStudyByFileType(ctx, studyLimsID, "")
}

// CountIRODSPathsForStudyByFileType counts the distinct iRODS data objects for a
// study, optionally restricted to data objects whose irods_file_name ends in
// `.<fileType>` (case-insensitive, leading dot stripped), so it equals
// len(IRODSPathsForStudyByFileType(study, fileType, all)) for any fileType. An
// empty fileType counts all objects (the bare behaviour); an invalid fileType is
// rejected with ErrUnsupportedIdentifier. It shares IRODSPathsForStudy's
// study-resolve / synced-empty / unknown cascade.
func (c *Client) CountIRODSPathsForStudyByFileType(ctx context.Context, studyLimsID, fileType string) (Count, error) {
	normalised, err := normaliseFileType(fileType)
	if err != nil {
		return Count{}, err
	}

	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableStudy); syncErr != nil {
				return Count{}, syncErr
			}

			return Count{}, ErrNotFound
		}

		return Count{}, err
	}

	query, args := irodsCountFileTypeQuery(countIRODSPathsForStudyCacheSQLPrefix, countIRODSPathsForStudyCacheSQLSuffix, normalised, study.IDStudyLims)
	count, err := c.queryCount(ctx, query, "count study irods paths", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountIRODSPathsForRun counts the iRODS data objects on a run, the count
// counterpart of IRODSPathsForRun (same iseq_product_metrics_mirror -> iRODS join
// on id_iseq_product, filtered by id_run, with no LIMIT), so
// CountIRODSPathsForRun(run, fileType) equals len(IRODSPathsForRun(run, fileType,
// all)) for any fileType. idRun is the Illumina NPG id_run, resolved via ResolveRun:
// a non-numeric run is ErrUnsupportedIdentifier, a numeric run absent from a synced
// cache is ErrNotFound, and a never-synced cache returns Count{} with both
// ErrCacheNeverSynced and ErrNotFound -- the same run-space cascade as
// CountSamplesForRun. An empty fileType counts all objects; an invalid fileType is
// rejected with ErrUnsupportedIdentifier. A valid but unmatched suffix, or a run
// with no iRODS rows yet, yields Count{0} on a synced cache.
func (c *Client) CountIRODSPathsForRun(ctx context.Context, idRun, fileType string) (Count, error) {
	normalised, err := normaliseFileType(fileType)
	if err != nil {
		return Count{}, err
	}

	match, err := c.ResolveRun(ctx, idRun)
	if err != nil {
		return Count{}, err
	}

	query, args := irodsCountFileTypeQuery(countIRODSPathsForRunCacheSQLPrefix, countIRODSPathsForRunCacheSQLSuffix, normalised, match.Run.IDRun)
	count, err := c.queryCount(ctx, query, "count run irods paths", args...)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); err != nil {
		return Count{}, err
	}

	return Count{Count: 0}, nil
}

// CountFindSamplesBySangerID counts the SQSCP samples whose sanger_sample_id
// matches, the count counterpart of FindSamplesBySangerID, so it equals
// len(FindSamplesBySangerID(id)) for an unambiguous match. It shares the Find
// never-synced cascade: a zero count on a synced cache is ErrNotFound.
func (c *Client) CountFindSamplesBySangerID(ctx context.Context, sangerID string) (Count, error) {
	return c.countFindSamples(ctx, countFindSamplesBySangerIDSQL, "count samples by sanger sample id", sangerID, syncTableSample)
}

// CountFindSamplesByIDSampleLims counts the SQSCP samples whose id_sample_lims
// matches, the count counterpart of FindSamplesByIDSampleLims.
func (c *Client) CountFindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) (Count, error) {
	return c.countFindSamples(ctx, countFindSamplesByIDSampleLimsSQL, "count samples by id_sample_lims", idSampleLims, syncTableSample)
}

// CountFindSamplesByAccessionNumber counts the SQSCP samples whose
// accession_number matches, the count counterpart of
// FindSamplesByAccessionNumber.
func (c *Client) CountFindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) (Count, error) {
	return c.countFindSamples(ctx, countFindSamplesByAccessionSQL, "count samples by accession number", accessionNumber, syncTableSample)
}

// CountFindSamplesBySupplierName counts the SQSCP samples whose supplier_name
// matches, the count counterpart of FindSamplesBySupplierName.
func (c *Client) CountFindSamplesBySupplierName(ctx context.Context, supplierName string) (Count, error) {
	return c.countFindSamples(ctx, countFindSamplesBySupplierSQL, "count samples by supplier name", supplierName, syncTableSample)
}

// CountFindSamplesByLibraryType counts the distinct SQSCP samples linked to a
// library type, the count counterpart of FindSamplesByLibraryType (same
// library_samples join and pipeline_id_lims filter, no LIMIT).
func (c *Client) CountFindSamplesByLibraryType(ctx context.Context, libraryType string) (Count, error) {
	return c.countFindSamples(ctx, countFindSamplesByLibraryTypeSQL, "count samples by library type", libraryType, syncTableSample, syncTableIseqFlowcell)
}

// countFindSamples is the shared body of the find/sample/* counts. It mirrors
// findSamplesByQuery's never-synced cascade: a zero count on a synced cache is
// ErrNotFound, and a never-synced cache returns Count{} joined with
// ErrCacheNeverSynced and ErrNotFound.
func (c *Client) countFindSamples(ctx context.Context, query, action, raw string, syncTables ...string) (Count, error) {
	count, err := c.queryCount(ctx, query, action, raw)
	if err != nil {
		return Count{}, err
	}
	if count > 0 {
		return Count{Count: count}, nil
	}

	if err = c.requireAnySyncState(ctx, syncTables...); err != nil {
		return Count{}, err
	}

	return Count{}, ErrNotFound
}

// queryCount runs a single-row COUNT(...) query against the cache reader and
// returns the scalar result.
func (c *Client) queryCount(ctx context.Context, query, action string, args ...any) (int, error) {
	db := c.readCacheDB()
	if db == nil {
		return 0, fmt.Errorf("mlwh: cache reader not configured")
	}

	var count int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}

	return count, nil
}
