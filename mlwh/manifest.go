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

import "context"

const manifestUnmatchedReasonMergedMultilane = "merged_multilane"

// manifestProductGrainFromWhere is the FROM + WHERE that defines the manifest's
// row grain: the study's sequencing products in iseq_product_metrics_mirror,
// scoped by the product-metrics id_study_lims. It is the single shared building
// block so the C1 list and the C2 count select over the EXACT same product set,
// guaranteeing count == len(rows). The list GROUP BYs the distinct
// (id_run, position, tag_index) product over it; the count does
// COUNT(*) over the same SELECT DISTINCT of that triple.
const manifestProductGrainFromWhere = ` FROM iseq_product_metrics_mirror ipm WHERE ipm.id_study_lims = ?`

// manifestProductGrainDistinctSQL is the DISTINCT (id_run, position, tag_index)
// product set scoped by id_study_lims: the manifest's row grain expressed as a
// SELECT DISTINCT. Product export counts over this exact subquery when no
// filters are applied, so a product with no matching iRODS object is still one
// row.
const manifestProductGrainDistinctSQL = `SELECT DISTINCT ipm.id_run, ipm.position, ipm.tag_index` + manifestProductGrainFromWhere

// The manifest list SQL is split into a prefix (SELECT + base FROM/JOINs/WHERE),
// an optional iRODS LEFT JOIN, and a suffix (GROUP BY/ORDER BY/LIMIT) so the
// optional with_irods iRODS-path column and the optional file-type filter clause
// can be composed without changing the row grain. The grain is ALWAYS one row per
// product (the GROUP BY collapses the iRODS fan-out), so a product with several
// iRODS objects is still one row and count == len(list) holds regardless of
// with_irods / file_type.
const (
	// manifestListSelectPrefix selects the product triple plus the per-product
	// sample identity. The sample identity is taken via LEFT JOIN sample_mirror on
	// the product's id_sample_tmp and aggregated with MIN so the projection is one
	// row per (id_run, position, tag_index) product even if the join were to fan
	// out; a product whose sample is absent from the mirror yields empty identity
	// fields rather than dropping the row. The aggregate QC columns preserve the
	// shared manual_qc roll-up inputs for the grouped product row.
	manifestListSelectPrefix = `SELECT ipm.id_run, ipm.position, ipm.tag_index, ` +
		`MIN(sm.name), MIN(sm.supplier_name), MIN(sm.accession_number), MIN(sm.sanger_sample_id), ` +
		`COUNT(*), SUM(CASE WHEN ipm.qc IS NULL THEN 1 ELSE 0 END), MIN(ipm.qc)`

	// manifestListIRODSSelect adds the per-product iRODS data-object columns
	// (collection + file name) for the with_irods path. They come from spi, the
	// derived table (manifestListIRODSJoin) that has already collapsed each
	// product's iRODS rows to ONE coherent row, so the collection and file name are
	// always from the SAME object (never two independent MINs that could fabricate
	// a non-existent collection/file pair). The full path is assembled in Go (the
	// package avoids dialect-specific SQL string concatenation, cf. the iRODS list
	// helpers).
	manifestListIRODSSelect = `, spi.irods_collection, spi.irods_file_name`

	// manifestListIRODSUnmatchedSelect adds a grouped boolean telling callers that
	// a single-lane product has no direct iRODS match because the same sample has a
	// merged CRAM object in the study. It deliberately reports only the gap; it
	// does not select the composite path and therefore cannot duplicate that path
	// across the sample's single-lane product rows.
	manifestListIRODSUnmatchedExpression = `MAX(CASE WHEN ipm.id_run <> 0 AND ipm.position <> 0 AND spi.id_iseq_product IS NULL AND mcram.id_sample_tmp IS NOT NULL THEN 1 ELSE 0 END)`
	manifestListIRODSUnmatchedSelect     = `, ` + manifestListIRODSUnmatchedExpression

	// manifestListIRODSNoUnmatchedSelect keeps the with_irods scan shape stable
	// when the request is not a CRAM-aware view.
	manifestListIRODSNoUnmatchedSelect = `, 0`

	// manifestListBaseFrom is the base FROM/JOIN: the product-metrics rows LEFT
	// JOINed to sample_mirror for identity. The iRODS join (when with_irods) slots
	// in here, BEFORE the WHERE clause; the scoping predicate is manifestListWhere.
	manifestListBaseFrom = ` FROM iseq_product_metrics_mirror ipm` +
		` LEFT JOIN sample_mirror sm ON sm.id_sample_tmp = ipm.id_sample_tmp`

	// manifestListWhere scopes the products by the product-metrics id_study_lims.
	// It is appended after the FROM/JOIN block (and any iRODS join), so the SQL is
	// always FROM ... JOIN ... WHERE ... regardless of with_irods.
	manifestListWhere = ` WHERE ipm.id_study_lims = ?`

	// manifestListIRODSJoinPrefix begins the set-at-once LEFT JOIN to a DERIVED
	// TABLE (aliased spi) that pre-selects exactly ONE coherent iRODS row per
	// id_iseq_product, ranked by ROW_NUMBER() OVER (PARTITION BY id_iseq_product
	// ORDER BY irods_collection, irods_file_name). Because the chosen collection and
	// file name come from the SAME ranked row, the assembled path is ALWAYS a real
	// object (never two independent MINs that could pair a collection and a file
	// name from different objects). It is a LEFT JOIN, NEVER a per-row correlated
	// subquery (the per-platform-breakdown perf trap) and NOT a DEPENDENT SUBQUERY:
	// the derived table is scoped by id_study_lims (index-served on the iRODS
	// mirror) and joined on the shared id_iseq_product, so a product with no
	// matching iRODS object survives with NULL iRODS columns (irods_path=""). The
	// optional file-type restriction (manifestListIRODSFileTypeClause) slots into
	// the derived table's WHERE before manifestListIRODSJoinSuffix closes it.
	manifestListIRODSJoinPrefix = ` LEFT JOIN (` +
		`SELECT id_iseq_product, id_study_lims, irods_collection, irods_file_name FROM (` +
		`SELECT id_iseq_product, id_study_lims, irods_collection, irods_file_name,` +
		` ROW_NUMBER() OVER (PARTITION BY id_iseq_product` +
		` ORDER BY irods_collection, irods_file_name) AS rn` +
		` FROM seq_product_irods_locations_mirror WHERE id_study_lims = ?`

	// manifestListIRODSJoinSuffix closes the derived table (keeping only the
	// top-ranked row per product, rn = 1) and joins it to the product-metrics rows
	// on the shared id_iseq_product and id_study_lims. It follows
	// manifestListIRODSJoinPrefix and the optional file-type clause.
	manifestListIRODSJoinSuffix = `) ranked WHERE rn = 1) spi` +
		` ON spi.id_iseq_product = ipm.id_iseq_product AND spi.id_study_lims = ipm.id_study_lims`

	// manifestListMergedCRAMJoin marks samples that have a study-scoped merged
	// CRAM object. Product exports still join iRODS paths by id_iseq_product; this
	// sample-scoped join exists only to explain why a single-lane CRAM product has
	// no direct product-grained path.
	manifestListMergedCRAMJoin = ` LEFT JOIN (` +
		`SELECT DISTINCT id_sample_tmp FROM seq_product_irods_locations_mirror` +
		` WHERE id_study_lims = ? AND merged <> 0 AND LOWER(irods_file_name) LIKE ?` +
		`) mcram ON mcram.id_sample_tmp = ipm.id_sample_tmp`

	// manifestListIRODSFileTypeClause restricts the derived table's iRODS rows to
	// data objects whose irods_file_name ends in `.<file-type>`, case-insensitively.
	// It is appended to the derived table's WHERE when a file_type is set, so an
	// unmatched product keeps irods_path="" (the LEFT JOIN finds no ranked row)
	// rather than dropping the product row. The pattern is bound as a ? parameter
	// (irodsFileTypeLikePattern), like irodsFileTypeFilterClause, so one SQL string
	// stays valid on both sqlite and mysql.
	manifestListIRODSFileTypeClause = ` AND LOWER(irods_file_name) LIKE ?`
)

// manifestEmptyRequiredSyncTables are the sync tables that must have ever synced
// before a known study with no manifest products can be reported as a synced empty
// result. sample + iseq_flowcell preserve the existing study/sample empty cascade;
// iseq_product_metrics is the manifest's row-grain source, and the iRODS mirror is
// additionally required when the caller asked for iRODS paths.
func manifestEmptyRequiredSyncTables(withIRODS bool) []string {
	if withIRODS {
		return []string{
			syncTableSample,
			syncTableIseqFlowcell,
			syncTableIseqProductMetrics,
			syncTableSeqProductIRODSLocations,
		}
	}

	return []string{syncTableSample, syncTableIseqFlowcell, syncTableIseqProductMetrics}
}

// manifestIRODSJoin builds the with_irods LEFT JOIN block and its arguments in
// SQL-text order. The returned boolean tells callers whether the joined view is
// CRAM-aware and therefore able to mark merged multi-lane CRAM gaps.
func manifestIRODSJoin(studyLimsID, normalised string) (string, []any, bool) {
	detectMergedCRAMGap := normalised == "" || normalised == "cram"
	join := manifestListIRODSJoinPrefix
	args := []any{studyLimsID}
	if normalised != "" {
		join += manifestListIRODSFileTypeClause
		args = append(args, irodsFileTypeLikePattern(normalised))
	}
	join += manifestListIRODSJoinSuffix
	if detectMergedCRAMGap {
		join += manifestListMergedCRAMJoin
		args = append(args, studyLimsID, irodsFileTypeLikePattern("cram"))
	}

	return join, args, detectMergedCRAMGap
}

// countStudyManifestProducts counts the distinct (id_run, position, tag_index)
// products in the study via COUNT(*) over manifestProductGrainDistinctSQL, the
// EXACT SELECT DISTINCT the list groups by, so the count equals the number of rows
// product export returns when fetching all of them (count == len(rows-all)).
// The with_irods / file_type params do NOT change the count: a product with no
// matching iRODS object is still a row. It is a plain scalar count with no
// cascade; callers handle empty-study resolution after they know there are no
// products.
func (c *Client) countStudyManifestProducts(ctx context.Context, studyLimsID string) (int, error) {
	return c.queryCount(ctx, `SELECT COUNT(*) FROM (`+manifestProductGrainDistinctSQL+`) AS manifest_products`, "count study manifest products", studyLimsID)
}

// requireStudyProductExportEmpty verifies the empty-result cascade for product
// exports: a known study with no products is allowed only after the product
// sources have synced, while an unknown study remains not_found after the study
// table has synced.
func (c *Client) requireStudyProductExportEmpty(ctx context.Context, studyLimsID string, withIRODS bool) error {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, manifestEmptyRequiredSyncTables(withIRODS)...)
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
