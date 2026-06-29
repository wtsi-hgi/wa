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
	"strings"
)

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
// SELECT DISTINCT. The C2 count sizes the list by COUNT(*) over this exact
// subquery, so a product with no matching iRODS object is still one row and
// count == len(StudyManifest(...).Rows) for every study.
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
	// fields rather than dropping the row.
	manifestListSelectPrefix = `SELECT ipm.id_run, ipm.position, ipm.tag_index, ` +
		`MIN(sm.name), MIN(sm.supplier_name), MIN(sm.accession_number), MIN(sm.sanger_sample_id)`

	// manifestListIRODSSelect adds the per-product iRODS data-object columns
	// (collection + file name) for the with_irods path. They come from spi, the
	// derived table (manifestListIRODSJoin) that has already collapsed each
	// product's iRODS rows to ONE coherent row, so the collection and file name are
	// always from the SAME object (never two independent MINs that could fabricate
	// a non-existent collection/file pair). The full path is assembled in Go (the
	// package avoids dialect-specific SQL string concatenation, cf. the iRODS list
	// helpers).
	manifestListIRODSSelect = `, spi.irods_collection, spi.irods_file_name`

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
		`SELECT id_iseq_product, irods_collection, irods_file_name FROM (` +
		`SELECT id_iseq_product, irods_collection, irods_file_name,` +
		` ROW_NUMBER() OVER (PARTITION BY id_iseq_product` +
		` ORDER BY irods_collection, irods_file_name) AS rn` +
		` FROM seq_product_irods_locations_mirror WHERE id_study_lims = ?`

	// manifestListIRODSJoinSuffix closes the derived table (keeping only the
	// top-ranked row per product, rn = 1) and joins it to the product-metrics rows
	// on the shared id_iseq_product. It follows manifestListIRODSJoinPrefix and the
	// optional file-type clause.
	manifestListIRODSJoinSuffix = `) ranked WHERE rn = 1) spi` +
		` ON spi.id_iseq_product = ipm.id_iseq_product`

	// manifestListIRODSFileTypeClause restricts the derived table's iRODS rows to
	// data objects whose irods_file_name ends in `.<file-type>`, case-insensitively.
	// It is appended to the derived table's WHERE when a file_type is set, so an
	// unmatched product keeps irods_path="" (the LEFT JOIN finds no ranked row)
	// rather than dropping the product row. The pattern is bound as a ? parameter
	// (irodsFileTypeLikePattern), like irodsFileTypeFilterClause, so one SQL string
	// stays valid on both sqlite and mysql.
	manifestListIRODSFileTypeClause = ` AND LOWER(irods_file_name) LIKE ?`

	// manifestListSuffix groups by the product triple (collapsing the sample and
	// iRODS fan-out to one row per product) and orders by (id_run, position,
	// tag_index, name) for determinism, then paginates by LIMIT/OFFSET.
	manifestListSuffix = ` GROUP BY ipm.id_run, ipm.position, ipm.tag_index` +
		` ORDER BY ipm.id_run, ipm.position, ipm.tag_index, MIN(sm.name) LIMIT ? OFFSET ?`
)

// manifestFeedingTables are the sync tables whose oldest last_run defines a
// StudyManifest's cache_synced_at: the study + sample identity tables, the
// Illumina product-metrics mirror (the manifest's row grain) and the iRODS
// locations mirror (the optional path column). cache_synced_at is the OLDEST
// last_run among those that have ever synced, distinct from any data timestamp
// (the freshness caveat), the same idiom StudyOverview uses.
var manifestFeedingTables = []string{
	syncTableStudy,
	syncTableSample,
	syncTableIseqProductMetrics,
	syncTableSeqProductIRODSLocations,
}

// manifestListQuery assembles the manifest list query and its bound args for the
// given study, with_irods flag and (validated) normalised file type. Without
// with_irods it is the bare product+sample query (no iRODS join at all, so
// irods_path stays empty), bound as id_study_lims, limit, offset. With with_irods
// it adds the set-at-once iRODS LEFT JOIN to the derived table that picks one
// coherent object per product and, when a file_type is set, appends the filter
// clause to the derived table's WHERE and binds the LIKE pattern. The args are
// bound in SQL-text order, matching the placeholders: the derived table's
// id_study_lims (in its WHERE), [file-type pattern (also in that WHERE)], the
// outer WHERE's id_study_lims, limit, offset.
func manifestListQuery(studyLimsID string, withIRODS bool, normalised string, limit, offset int) (string, []any) {
	if !withIRODS {
		query := manifestListSelectPrefix + manifestListBaseFrom + manifestListWhere + manifestListSuffix

		return query, []any{studyLimsID, limit, offset}
	}

	// Bind args in SQL-text order: the derived table's id_study_lims (in its
	// WHERE), then the file-type LIKE pattern when filtered (also in that WHERE),
	// then the outer WHERE's id_study_lims, then limit/offset.
	join := manifestListIRODSJoinPrefix
	args := []any{studyLimsID}
	if normalised != "" {
		join += manifestListIRODSFileTypeClause
		args = append(args, irodsFileTypeLikePattern(normalised))
	}
	join += manifestListIRODSJoinSuffix
	args = append(args, studyLimsID, limit, offset)

	query := manifestListSelectPrefix + manifestListIRODSSelect + manifestListBaseFrom + join + manifestListWhere + manifestListSuffix

	return query, args
}

// scanManifestRow scans one manifest list row into a ManifestRow, applying the
// nullable sample identity and (when withIRODS) assembling irods_path in Go from
// the collection/file-name of the single coherent iRODS row the derived table
// picked for the product (the package builds iRODS paths in Go rather than with
// dialect-specific SQL concatenation), so the path is always a real object. A NULL
// collection or file name (a product with no matching iRODS object) leaves
// irods_path empty.
func scanManifestRow(scan func(dest ...any) error, withIRODS bool) (ManifestRow, error) {
	var (
		row             ManifestRow
		name            sql.NullString
		supplierName    sql.NullString
		accessionNumber sql.NullString
		sangerSampleID  sql.NullString
		collection      sql.NullString
		fileName        sql.NullString
	)

	dest := []any{&row.IDRun, &row.Position, &row.TagIndex, &name, &supplierName, &accessionNumber, &sangerSampleID}
	if withIRODS {
		dest = append(dest, &collection, &fileName)
	}
	if err := scan(dest...); err != nil {
		return ManifestRow{}, err
	}

	row.Name = nullStringValue(name)
	row.SupplierName = nullStringValue(supplierName)
	row.AccessionNumber = nullStringValue(accessionNumber)
	row.SangerSampleID = nullStringValue(sangerSampleID)
	if withIRODS && collection.Valid && fileName.Valid {
		row.IRODSPath = strings.TrimRight(collection.String, "/") + "/" + fileName.String
	}

	return row, nil
}

// StudyManifest returns one bounded, pageable manifest of a study's sequencing
// products (spec D2/C1): the StudyManifest envelope carries the study
// id_study_lims / name / accession_number / faculty_sponsor / data_access_group
// once (from study_mirror) plus cache_synced_at, and Rows is a page of
// ManifestRow, ONE row per sequencing product (a distinct (id_run, position,
// tag_index) from iseq_product_metrics_mirror scoped by the product-metrics
// id_study_lims, joined to its sample's identity in sample_mirror), ordered by
// (id_run, position, tag_index, name). When withIRODS is true each row also
// carries irods_path via a set-at-once LEFT JOIN to seq_product_irods_locations_mirror
// on the shared id_iseq_product (and id_study_lims) with GROUP BY product, so the
// row count stays product-grained regardless of how many iRODS objects a product
// has; with a fileType it restricts the joined objects to that filename suffix
// (with_irods and no fileType returns any one object for the product, NOT a cram
// default), and a product with no matching iRODS object has irods_path="". An
// invalid fileType (empty/whitespace or containing '%', '_' or '/') is rejected
// with ErrUnsupportedIdentifier (the HTTP handler 400s first; this re-validates
// defensively). The never-synced / unknown-study / synced-empty cascade matches
// CountSamplesForStudy: a never-synced cache returns the zero value with an error
// satisfying both ErrCacheNeverSynced and ErrNotFound, an unknown study returns
// ErrNotFound, and a synced study with no products returns an envelope with the
// study metadata, an empty Rows and a populated cache_synced_at. Every value is
// read from the cache mirrors, so the manifest is complete only up to the feeding
// tables' last sync (see /freshness).
func (c *Client) StudyManifest(ctx context.Context, studyLimsID, fileType string, withIRODS bool, limit, offset int) (StudyManifest, error) {
	normalised, err := normaliseFileType(fileType)
	if err != nil {
		return StudyManifest{}, err
	}

	query, args := manifestListQuery(studyLimsID, withIRODS, normalised, limit, offset)
	rows, err := c.queryManifestRows(ctx, query, args, withIRODS)
	if err != nil {
		return StudyManifest{}, err
	}
	if len(rows) == 0 {
		return c.studyManifestForEmptyStudy(ctx, studyLimsID)
	}

	manifest, err := c.studyManifestEnvelope(ctx, studyLimsID)
	if err != nil {
		return StudyManifest{}, err
	}
	manifest.Rows = rows

	return manifest, nil
}

// countStudyManifestProducts counts the distinct (id_run, position, tag_index)
// products in the study via COUNT(*) over manifestProductGrainDistinctSQL, the
// EXACT SELECT DISTINCT the list groups by, so the count equals the number of rows
// StudyManifest returns when fetching all of them (count == len(rows-all)). It is
// the private sizing helper the HTTP handler uses for X-Total-Count; the public
// CountStudyManifest (spec C2) counts over the same grain, so the standalone count
// and the list's sizing total cannot drift. The with_irods / file_type params do
// NOT change the count (the manifest is product-grained: a product with no
// matching iRODS object is still a row). It is a plain scalar count with no
// cascade: the handler only sizes a manifest that already resolved.
func (c *Client) countStudyManifestProducts(ctx context.Context, studyLimsID string) (int, error) {
	return c.queryCount(ctx, `SELECT COUNT(*) FROM (`+manifestProductGrainDistinctSQL+`) AS manifest_products`, "count study manifest products", studyLimsID)
}

// queryManifestRows runs the manifest list query and scans the product rows. The
// sample-identity columns are LEFT-JOINed (and so nullable for a product whose
// sample is absent from the mirror); the iRODS collection/file-name columns are
// present only for the with_irods query and come from the LEFT-JOINed derived
// table's single coherent row per product, so they are NULL for a product with no
// matching iRODS object, which yields irods_path="".
func (c *Client) queryManifestRows(ctx context.Context, query string, args []any, withIRODS bool) ([]ManifestRow, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: query study manifest: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	manifestRows := make([]ManifestRow, 0)
	for rows.Next() {
		row, scanErr := scanManifestRow(rows.Scan, withIRODS)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan study manifest: %w", ErrUpstreamImpaired, scanErr)
		}

		manifestRows = append(manifestRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query study manifest: %w", ErrUpstreamImpaired, err)
	}

	return manifestRows, nil
}

// studyManifestEnvelope builds the manifest envelope's study-level metadata (read
// ONCE from study_mirror) and cache_synced_at (the oldest last_run across the
// feeding tables), shared by the populated and the synced-empty paths so the two
// carry identical study fields.
func (c *Client) studyManifestEnvelope(ctx context.Context, studyLimsID string) (StudyManifest, error) {
	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		return StudyManifest{}, err
	}

	syncedAt, err := c.oldestFeedingLastRun(ctx, manifestFeedingTables)
	if err != nil {
		return StudyManifest{}, err
	}

	return StudyManifest{
		IDStudyLims:     study.IDStudyLims,
		Name:            study.Name,
		AccessionNumber: study.AccessionNumber,
		FacultySponsor:  study.FacultySponsor,
		DataAccessGroup: study.DataAccessGroup,
		Rows:            []ManifestRow{},
		CacheSyncedAt:   syncedAt,
	}, nil
}

// studyManifestForEmptyStudy resolves the result when no products were found for a
// study, mirroring CountSamplesForStudy's cascade so the manifest and the count
// agree on a never-synced cache, an unknown study and a synced empty study: a
// known study on a fully-synced cache returns an envelope with the study metadata,
// an empty Rows and a populated cache_synced_at; a known study on a never-synced
// cache returns the zero value joined with both sentinels; an unknown study
// returns ErrNotFound.
func (c *Client) studyManifestForEmptyStudy(ctx context.Context, studyLimsID string) (StudyManifest, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return StudyManifest{}, err
	}
	if studyExists {
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return StudyManifest{}, err
		}
		if summary.allAbsent || !summary.allPresent {
			return StudyManifest{}, neverSyncedReadErr()
		}

		return c.studyManifestEnvelope(ctx, studyLimsID)
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		return StudyManifest{}, err
	}

	return StudyManifest{}, ErrNotFound
}
