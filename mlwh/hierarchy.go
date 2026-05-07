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
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"
)

const expandIdentifierTTL = 5 * time.Minute

var (
	samplesForStudyCacheSQL    = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ? ORDER BY sample_mirror.name LIMIT ? OFFSET ?`
	samplesForStudySourceSQL   = `SELECT DISTINCT iseq_flowcell.pipeline_id_lims, sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, iseq_flowcell.id_study_lims, sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description, sample.last_updated FROM iseq_flowcell INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_flowcell.id_study_lims = ? AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`
	samplesForLibraryCacheSQL  = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ? ORDER BY sample_mirror.name LIMIT ? OFFSET ?`
	samplesForLibrarySourceSQL = `SELECT DISTINCT iseq_flowcell.pipeline_id_lims, sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, iseq_flowcell.id_study_lims, sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description, sample.last_updated FROM iseq_flowcell INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_flowcell.pipeline_id_lims = ? AND iseq_flowcell.id_study_lims = ? AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`
	samplesForRunSourceSQL     = `SELECT DISTINCT sample.id_sample_tmp, sample.id_lims, sample.id_sample_lims, sample.uuid_sample_lims, iseq_flowcell.id_study_lims, sample.name, sample.name AS sanger_id, sample.sanger_sample_id, sample.supplier_name, sample.accession_number, sample.donor_id, iseq_flowcell.pipeline_id_lims AS library_type, sample.taxon_id, sample.common_name, sample.description FROM iseq_product_metrics INNER JOIN iseq_flowcell ON iseq_flowcell.id_iseq_flowcell_tmp = iseq_product_metrics.id_iseq_flowcell_tmp INNER JOIN sample ON sample.id_sample_tmp = iseq_flowcell.id_sample_tmp WHERE iseq_product_metrics.id_run = ? AND sample.id_lims = 'SQSCP' ORDER BY sample.name LIMIT ? OFFSET ?`
	librariesForStudySQL       = `SELECT pipeline_id_lims, COUNT(DISTINCT id_sample_tmp) FROM library_samples WHERE id_study_lims = ? GROUP BY pipeline_id_lims ORDER BY pipeline_id_lims LIMIT ? OFFSET ?`
	runsForStudySQL            = `SELECT DISTINCT ipm.id_run FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp WHERE ifc.id_study_tmp = ? ORDER BY ipm.id_run LIMIT ? OFFSET ?`
	lanesForSampleSQL          = `SELECT DISTINCT ipm.id_run, ipm.position, ipm.tag_index FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp WHERE ifc.id_sample_tmp = ? ORDER BY ipm.id_run, ipm.position, ipm.tag_index LIMIT ? OFFSET ?`
	irodsPathsForSampleSQL     = `SELECT spi.id_product, spi.collection, spi.data_object FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp INNER JOIN seq_product_irods_locations spi ON spi.id_product = ipm.id_iseq_product WHERE ifc.id_sample_tmp = ? ORDER BY spi.id_product LIMIT ? OFFSET ?`
	irodsPathsForStudySQL      = `SELECT spi.id_product, spi.collection, spi.data_object FROM iseq_flowcell ifc INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_flowcell_tmp = ifc.id_iseq_flowcell_tmp INNER JOIN seq_product_irods_locations spi ON spi.id_product = ipm.id_iseq_product WHERE ifc.id_study_tmp = ? ORDER BY spi.id_product LIMIT ? OFFSET ?`
	studyForSampleSQL          = `SELECT study_mirror.id_study_tmp, study_mirror.id_lims, study_mirror.id_study_lims, study_mirror.uuid_study_lims, study_mirror.name, study_mirror.accession_number, study_mirror.study_title, study_mirror.faculty_sponsor, study_mirror.state, study_mirror.abstract, study_mirror.abbreviation, study_mirror.description, study_mirror.data_release_strategy, study_mirror.data_access_group, study_mirror.hmdmc_number, study_mirror.programme, study_mirror.created, study_mirror.reference_genome, study_mirror.ethically_approved, study_mirror.study_type, study_mirror.contains_human_dna, study_mirror.contaminated_human_dna, study_mirror.study_visibility, study_mirror.egadac_accession_number, study_mirror.ega_policy_accession_number, study_mirror.data_release_timing FROM donor_samples INNER JOIN study_mirror ON study_mirror.id_study_lims = donor_samples.id_study_lims WHERE donor_samples.id_sample_tmp = ? AND study_mirror.id_lims = 'SQSCP' ORDER BY study_mirror.id_study_tmp LIMIT 1`
)

type hierarchyReadThroughRow struct {
	PipelineIDLims string
	Sample         Sample
	LastUpdated    time.Time
}

func scanHierarchyReadThroughRow(rows *sql.Rows) (hierarchyReadThroughRow, error) {
	row := hierarchyReadThroughRow{}
	var lastUpdated any
	if err := rows.Scan(
		&row.PipelineIDLims,
		&row.Sample.IDSampleTmp,
		&row.Sample.IDLims,
		&row.Sample.IDSampleLims,
		&row.Sample.UUIDSampleLims,
		&row.Sample.IDStudyLims,
		&row.Sample.Name,
		&row.Sample.SangerID,
		&row.Sample.SangerSampleID,
		&row.Sample.SupplierName,
		&row.Sample.AccessionNumber,
		&row.Sample.DonorID,
		&row.Sample.LibraryType,
		&row.Sample.TaxonID,
		&row.Sample.CommonName,
		&row.Sample.Description,
		&lastUpdated,
	); err != nil {
		return hierarchyReadThroughRow{}, err
	}

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return hierarchyReadThroughRow{}, err
	}
	row.LastUpdated = parsed

	return row, nil
}

func (c *Client) queryHierarchyReadThroughRows(ctx context.Context, query, action string, args ...any) ([]hierarchyReadThroughRow, error) {
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}
	defer func() { _ = rows.Close() }()

	readThroughRows := make([]hierarchyReadThroughRow, 0)
	for rows.Next() {
		row, scanErr := scanHierarchyReadThroughRow(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, scanErr)
		}

		readThroughRows = append(readThroughRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}

	return readThroughRows, nil
}

func (c *Client) upsertHierarchyReadThrough(ctx context.Context, studyLimsID string, rows []hierarchyReadThroughRow) error {
	if len(rows) == 0 {
		return nil
	}
	if c == nil || c.cache == nil {
		return fmt.Errorf("mlwh: cache client not configured")
	}

	tx, err := c.cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin hierarchy read-through transaction: %w", ErrUpstreamImpaired, err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, row := range rows {
		if row.Sample.IDStudyLims == "" {
			row.Sample.IDStudyLims = studyLimsID
		}
		if row.Sample.SangerID == "" {
			row.Sample.SangerID = row.Sample.Name
		}
		if row.Sample.LibraryType == "" {
			row.Sample.LibraryType = row.PipelineIDLims
		}

		if upsertErr := upsertSampleMirror(ctx, tx, c.cache.Dialect(), sampleSyncRow{Sample: row.Sample, IDStudyLims: studyLimsID, LastUpdated: row.LastUpdated}); upsertErr != nil {
			return fmt.Errorf("%w: %w", ErrUpstreamImpaired, upsertErr)
		}

		libraryExists, existsErr := rowExists(
			ctx,
			tx,
			`SELECT 1 FROM library_samples WHERE pipeline_id_lims = ? AND id_sample_tmp = ? AND id_study_lims = ? LIMIT 1`,
			row.PipelineIDLims,
			row.Sample.IDSampleTmp,
			studyLimsID,
		)
		if existsErr != nil {
			return fmt.Errorf("%w: query library_samples row existence: %w", ErrUpstreamImpaired, existsErr)
		}
		if !libraryExists {
			if replaceErr := replaceLibrarySample(ctx, tx, flowcellSyncRow{PipelineIDLims: row.PipelineIDLims, IDSampleTmp: row.Sample.IDSampleTmp, IDStudyLims: studyLimsID, LastUpdated: row.LastUpdated}); replaceErr != nil {
				return fmt.Errorf("%w: %w", ErrUpstreamImpaired, replaceErr)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit hierarchy read-through transaction: %w", ErrUpstreamImpaired, err)
	}

	committed = true

	return nil
}

func querySamples(ctx context.Context, querier Querier, query, action string, args ...any) ([]Sample, error) {
	rows, err := querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}
	defer func() { _ = rows.Close() }()

	samples := make([]Sample, 0)
	for rows.Next() {
		sample, scanErr := scanSampleRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, scanErr)
		}

		samples = append(samples, sample)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}

	return samples, nil
}

func scanSampleRow(scan func(dest ...any) error) (Sample, error) {
	sample := Sample{}
	if err := scan(
		&sample.IDSampleTmp,
		&sample.IDLims,
		&sample.IDSampleLims,
		&sample.UUIDSampleLims,
		&sample.IDStudyLims,
		&sample.Name,
		&sample.SangerID,
		&sample.SangerSampleID,
		&sample.SupplierName,
		&sample.AccessionNumber,
		&sample.DonorID,
		&sample.LibraryType,
		&sample.TaxonID,
		&sample.CommonName,
		&sample.Description,
	); err != nil {
		return Sample{}, err
	}

	return sample, nil
}

func queryExists(ctx context.Context, querier Querier, query, action string, args ...any) (bool, error) {
	rows, err := querier.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return false, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
		}

		return false, nil
	}

	return true, nil
}

func addSampleRunTags(ctx context.Context, client *Client, taggedIDSet map[TaggedID]struct{}, samples []Sample) error {
	slices.SortFunc(samples, func(left, right Sample) int {
		if left.Name < right.Name {
			return -1
		}
		if left.Name > right.Name {
			return 1
		}

		return 0
	})

	addSampleTags(taggedIDSet, samples)

	for _, sample := range samples {
		lanes, err := client.LanesForSample(ctx, sample.Name, MaxSamplesPerHop, 0)
		if err != nil {
			return err
		}

		addRunTags(taggedIDSet, lanes)
	}

	return nil
}

func addSampleTags(taggedIDSet map[TaggedID]struct{}, samples []Sample) {
	for _, sample := range samples {
		taggedIDSet[TaggedID{Kind: KindSangerSampleName, Canonical: sample.Name}] = struct{}{}
	}
}

func addRunTags(taggedIDSet map[TaggedID]struct{}, lanes []Lane) {
	for _, lane := range lanes {
		taggedIDSet[TaggedID{Kind: KindRunID, Canonical: strconv.Itoa(lane.IDRun)}] = struct{}{}
	}
}

func sortedTaggedIDs(taggedIDSet map[TaggedID]struct{}) []TaggedID {
	taggedIDs := slices.Collect(maps.Keys(taggedIDSet))
	slices.SortFunc(taggedIDs, compareTaggedIDs)

	return taggedIDs
}

func compareTaggedIDs(left, right TaggedID) int {
	leftRank := taggedIDKindRank(left.Kind)
	rightRank := taggedIDKindRank(right.Kind)
	if leftRank != rightRank {
		return leftRank - rightRank
	}

	if left.Kind == KindRunID && right.Kind == KindRunID {
		leftRun, leftErr := strconv.Atoi(left.Canonical)
		rightRun, rightErr := strconv.Atoi(right.Canonical)
		if leftErr == nil && rightErr == nil && leftRun != rightRun {
			return leftRun - rightRun
		}
	}

	if left.Canonical < right.Canonical {
		return -1
	}
	if left.Canonical > right.Canonical {
		return 1
	}

	return 0
}

func taggedIDKindRank(kind IdentifierKind) int {
	switch kind {
	case KindSangerSampleName:
		return 0
	case KindRunID:
		return 1
	default:
		return 2
	}
}

func (c *Client) cacheStudyExists(ctx context.Context, studyLimsID string) (bool, error) {
	db := c.readCacheDB()
	if db == nil {
		return false, fmt.Errorf("mlwh: cache reader not configured")
	}

	return queryExists(ctx, db, `SELECT 1 FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, "query study mirror existence", studyLimsID)
}

func (c *Client) querySourceStudyParent(ctx context.Context, studyLimsID string) (Study, error) {
	if c == nil || c.syncSource == nil {
		return Study{}, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(
		ctx,
		`SELECT `+studyMirrorSelectColumns+` FROM study WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`,
		studyLimsID,
	)
	if err != nil {
		return Study{}, fmt.Errorf("%w: query study source parent: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return Study{}, fmt.Errorf("%w: query study source parent: %w", ErrUpstreamImpaired, err)
		}

		return Study{}, ErrNotFound
	}

	study, err := scanStudyRow(rows.Scan)
	if err != nil {
		return Study{}, fmt.Errorf("%w: scan study source parent: %w", ErrUpstreamImpaired, err)
	}

	return study, nil
}

// SamplesForStudy returns samples linked to a study, reading through to MLWH on a cold cache.
func (c *Client) SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Sample, error) {
	cacheDB := c.readCacheDB()
	if cacheDB == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, cacheDB, samplesForStudyCacheSQL, "query study samples cache", studyLimsID, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		return samples, nil
	}

	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}
	if studyExists {
		return []Sample{}, nil
	}

	studyWarm, err := c.hasResolverSyncState(ctx, syncTableStudy)
	if err != nil {
		return nil, err
	}
	if studyWarm {
		return nil, ErrNotFound
	}

	if _, err = c.querySourceStudyParent(ctx, studyLimsID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	readThroughRows, err := c.queryHierarchyReadThroughRows(ctx, samplesForStudySourceSQL, "query study samples source", studyLimsID, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(readThroughRows) == 0 {
		return []Sample{}, nil
	}
	if err = c.upsertHierarchyReadThrough(ctx, studyLimsID, readThroughRows); err != nil {
		return nil, err
	}

	result := make([]Sample, 0, len(readThroughRows))
	for _, row := range readThroughRows {
		result = append(result, row.Sample)
	}

	return result, nil
}

// SamplesForRun returns distinct samples linked to a run.
func (c *Client) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]Sample, error) {
	runID, err := strconv.Atoi(idRun)
	if err != nil {
		return nil, ErrUnsupportedIdentifier
	}
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}

	samples, err := querySamples(ctx, c.syncSource, samplesForRunSourceSQL, "query run samples source", runID, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		return samples, nil
	}

	runExists, err := queryExists(ctx, c.syncSource, `SELECT id_run FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`, "query run existence", runID)
	if err != nil {
		return nil, err
	}
	if !runExists {
		return nil, ErrNotFound
	}

	return []Sample{}, nil
}

// SamplesForLibrary returns study-scoped samples linked to a library type, reading through on a cold cache.
func (c *Client) SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]Sample, error) {
	cacheDB := c.readCacheDB()
	if cacheDB == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, cacheDB, samplesForLibraryCacheSQL, "query library samples cache", pipelineIDLims, studyLimsID, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		return samples, nil
	}

	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}
	if studyExists {
		return []Sample{}, nil
	}

	studyWarm, err := c.hasResolverSyncState(ctx, syncTableStudy)
	if err != nil {
		return nil, err
	}
	if studyWarm {
		return nil, ErrNotFound
	}

	if _, err = c.querySourceStudyParent(ctx, studyLimsID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	readThroughRows, err := c.queryHierarchyReadThroughRows(ctx, samplesForLibrarySourceSQL, "query library samples source", pipelineIDLims, studyLimsID, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(readThroughRows) == 0 {
		return []Sample{}, nil
	}
	if err = c.upsertHierarchyReadThrough(ctx, studyLimsID, readThroughRows); err != nil {
		return nil, err
	}

	result := make([]Sample, 0, len(readThroughRows))
	for _, row := range readThroughRows {
		result = append(result, row.Sample)
	}

	return result, nil
}

// LibrariesForStudy returns libraries for a study.
func (c *Client) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Library, error) {
	if _, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, librariesForStudySQL, studyLimsID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: query libraries for study: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	libraries := make([]Library, 0)
	for rows.Next() {
		library := Library{}
		if err = rows.Scan(&library.PipelineIDLims, &library.SampleCount); err != nil {
			return nil, fmt.Errorf("%w: scan libraries for study: %w", ErrUpstreamImpaired, err)
		}

		libraries = append(libraries, library)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query libraries for study: %w", ErrUpstreamImpaired, err)
	}

	return libraries, nil
}

// RunsForStudy returns runs for a study.
func (c *Client) RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Run, error) {
	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(ctx, runsForStudySQL, study.IDStudyTmp, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: query runs for study: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	runs := make([]Run, 0)
	for rows.Next() {
		run := Run{}
		if err = rows.Scan(&run.IDRun); err != nil {
			return nil, fmt.Errorf("%w: scan runs for study: %w", ErrUpstreamImpaired, err)
		}

		runs = append(runs, run)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query runs for study: %w", ErrUpstreamImpaired, err)
	}

	return runs, nil
}

// LanesForSample returns lanes for a sample.
func (c *Client) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]Lane, error) {
	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(ctx, lanesForSampleSQL, sample.IDSampleTmp, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: query lanes for sample: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	lanes := make([]Lane, 0)
	for rows.Next() {
		lane := Lane{}
		if err = rows.Scan(&lane.IDRun, &lane.Position, &lane.TagIndex); err != nil {
			return nil, fmt.Errorf("%w: scan lanes for sample: %w", ErrUpstreamImpaired, err)
		}

		lanes = append(lanes, lane)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query lanes for sample: %w", ErrUpstreamImpaired, err)
	}

	return lanes, nil
}

// IRODSPathsForSample returns iRODS paths for a sample.
func (c *Client) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]IRODSPath, error) {
	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return c.queryIRODSPaths(ctx, irodsPathsForSampleSQL, sample.IDSampleTmp, limit, offset, "query irods paths for sample")
}

// IRODSPathsForStudy returns iRODS paths for a study.
func (c *Client) IRODSPathsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]IRODSPath, error) {
	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return c.queryIRODSPaths(ctx, irodsPathsForStudySQL, study.IDStudyTmp, limit, offset, "query irods paths for study")
}

// StudyForSample returns the study linked to a sample.
func (c *Client) StudyForSample(ctx context.Context, sangerName string) (*Study, error) {
	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	study, err := c.resolveStudyBySampleID(ctx, sample.IDSampleTmp)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	return study, nil
}

func (c *Client) queryIRODSPaths(ctx context.Context, query string, parent any, limit, offset int, action string) ([]IRODSPath, error) {
	if c == nil || c.syncSource == nil {
		return nil, fmt.Errorf("mlwh: sync source not configured")
	}

	rows, err := c.syncSource.QueryContext(ctx, query, parent, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}
	defer func() { _ = rows.Close() }()

	paths := make([]IRODSPath, 0)
	for rows.Next() {
		path := IRODSPath{}
		if err = rows.Scan(&path.IDProduct, &path.Collection, &path.DataObject); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
		}
		path.IRODSPath = path.Collection + "/" + path.DataObject

		paths = append(paths, path)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}

	return paths, nil
}

func (c *Client) resolveStudyBySampleID(ctx context.Context, idSampleTmp int64) (*Study, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	study := &Study{}
	err := db.QueryRowContext(ctx, studyForSampleSQL, idSampleTmp).Scan(
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
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: query study for sample: %w", ErrUpstreamImpaired, err)
	}

	return study, nil
}

// ExpandIdentifier expands a canonical identifier into related identifiers used by results queries.
func (c *Client) ExpandIdentifier(ctx context.Context, kind IdentifierKind, canonical string) ([]TaggedID, error) {
	if taggedIDs, ok := c.getExpandIdentifierCache(kind, canonical); ok {
		return taggedIDs, nil
	}

	taggedIDs, err := c.expandIdentifier(ctx, kind, canonical)
	if err != nil {
		return nil, err
	}

	c.setExpandIdentifierCache(kind, canonical, taggedIDs)

	return taggedIDs, nil
}

func (c *Client) expandIdentifier(ctx context.Context, kind IdentifierKind, canonical string) ([]TaggedID, error) {
	base := []TaggedID{{Kind: kind, Canonical: canonical}}
	extra := make(map[TaggedID]struct{})

	switch kind {
	case KindStudyLimsID:
		samples, err := c.SamplesForStudy(ctx, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return nil, err
		}

		if err = addSampleRunTags(ctx, c, extra, samples); err != nil {
			return nil, err
		}
	case KindLibraryType:
		studyLimsIDs, err := c.libraryStudyLimsIDs(ctx, canonical)
		if err != nil {
			return nil, err
		}

		samples := make([]Sample, 0)
		for _, studyLimsID := range studyLimsIDs {
			studySamples, sampleErr := c.SamplesForLibrary(ctx, canonical, studyLimsID, MaxSamplesPerHop, 0)
			if sampleErr != nil {
				return nil, sampleErr
			}

			samples = append(samples, studySamples...)
		}

		if err = addSampleRunTags(ctx, c, extra, samples); err != nil {
			return nil, err
		}
	case KindSangerSampleName:
		lanes, err := c.LanesForSample(ctx, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return nil, err
		}

		addRunTags(extra, lanes)
	case KindRunID:
		samples, err := c.SamplesForRun(ctx, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return nil, err
		}

		addSampleTags(extra, samples)
	default:
		return nil, ErrUnsupportedIdentifier
	}

	return append(base, sortedTaggedIDs(extra)...), nil
}

func (c *Client) libraryStudyLimsIDs(ctx context.Context, pipelineIDLims string) ([]string, error) {
	if err := c.ensureResolverTableSynced(ctx, syncTableIseqFlowcell); err != nil {
		return nil, err
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, `SELECT DISTINCT id_study_lims FROM library_samples WHERE pipeline_id_lims = ? ORDER BY id_study_lims`, pipelineIDLims)
	if err != nil {
		return nil, fmt.Errorf("%w: query library study ids: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studyLimsIDs := make([]string, 0)
	for rows.Next() {
		var studyLimsID string
		if err = rows.Scan(&studyLimsID); err != nil {
			return nil, fmt.Errorf("%w: scan library study ids: %w", ErrUpstreamImpaired, err)
		}

		studyLimsIDs = append(studyLimsIDs, studyLimsID)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query library study ids: %w", ErrUpstreamImpaired, err)
	}
	if len(studyLimsIDs) == 0 {
		return nil, ErrNotFound
	}

	return studyLimsIDs, nil
}

type expandIdentifierCacheKey struct {
	Kind      IdentifierKind
	Canonical string
}

func (c *Client) getExpandIdentifierCache(kind IdentifierKind, canonical string) ([]TaggedID, bool) {
	if c == nil {
		return nil, false
	}

	mu := c.ensureExpandIdentifierCache()
	mu.RLock()
	defer mu.RUnlock()

	entry, ok := c.expandCache[expandIdentifierCacheKey{Kind: kind, Canonical: canonical}]
	if !ok || !entry.ExpiresAt.After(c.expandIdentifierNow()) {
		return nil, false
	}

	return append([]TaggedID(nil), entry.TaggedIDs...), true
}

type expandIdentifierCacheEntry struct {
	TaggedIDs []TaggedID
	ExpiresAt time.Time
}

func (c *Client) setExpandIdentifierCache(kind IdentifierKind, canonical string, taggedIDs []TaggedID) {
	if c == nil {
		return
	}

	mu := c.ensureExpandIdentifierCache()
	mu.Lock()
	defer mu.Unlock()

	c.expandCache[expandIdentifierCacheKey{Kind: kind, Canonical: canonical}] = expandIdentifierCacheEntry{
		TaggedIDs: append([]TaggedID(nil), taggedIDs...),
		ExpiresAt: c.expandIdentifierNow().Add(expandIdentifierTTL),
	}
}

func (c *Client) clearExpandIdentifierCache() {
	if c == nil || c.expandCacheMu == nil {
		return
	}

	c.expandCacheMu.Lock()
	defer c.expandCacheMu.Unlock()

	clear(c.expandCache)
}

func (c *Client) ensureExpandIdentifierCache() *sync.RWMutex {
	if c.expandCacheMu == nil {
		c.expandCacheMu = &sync.RWMutex{}
	}
	if c.expandCache == nil {
		c.expandCache = make(map[expandIdentifierCacheKey]expandIdentifierCacheEntry)
	}

	return c.expandCacheMu
}

func (c *Client) expandIdentifierNow() time.Time {
	if c != nil && c.now != nil {
		return c.now()
	}

	return time.Now()
}
