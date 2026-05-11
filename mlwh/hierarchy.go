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
	"strings"
	"sync"
	"time"
)

const expandIdentifierTTL = 5 * time.Minute

var (
	samplesForStudyCacheSQL       = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForLibraryTypeCacheSQL = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForLibraryCacheSQL     = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForRunCacheSQL         = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM iseq_product_metrics_mirror INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = iseq_product_metrics_mirror.id_sample_tmp WHERE iseq_product_metrics_mirror.id_run = ? ORDER BY sample_mirror.name LIMIT ? OFFSET ?`
	findSamplesBySangerIDSQL      = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesByIDSampleLimsSQL  = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesByAccessionSQL     = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesBySupplierSQL      = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesByLibraryTypeSQL   = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND sample_mirror.id_lims = 'SQSCP' ORDER BY sample_mirror.id_sample_tmp LIMIT 2`
	librariesForStudySQL          = `SELECT pipeline_id_lims, COUNT(DISTINCT id_sample_tmp) FROM library_samples WHERE id_study_lims = ? GROUP BY pipeline_id_lims ORDER BY pipeline_id_lims LIMIT ? OFFSET ?`
	runsForStudyCacheSQL          = `SELECT DISTINCT id_run FROM iseq_product_metrics_mirror WHERE id_study_lims = ? ORDER BY id_run LIMIT ? OFFSET ?`
	lanesForSampleCacheSQL        = `SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? ORDER BY id_run, position, tag_index LIMIT ? OFFSET ?`
	irodsPathsForSampleCacheSQL   = `SELECT CAST(id_iseq_product AS TEXT), irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_sample_tmp = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
	irodsPathsForStudyCacheSQL    = `SELECT CAST(id_iseq_product AS TEXT), irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_study_lims = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
	studiesForSampleCacheSQL      = `SELECT DISTINCT study_mirror.id_study_tmp, study_mirror.id_lims, study_mirror.id_study_lims, study_mirror.uuid_study_lims, study_mirror.name, study_mirror.accession_number, study_mirror.study_title, study_mirror.faculty_sponsor, study_mirror.state, study_mirror.data_release_strategy, study_mirror.data_access_group, study_mirror.programme, study_mirror.reference_genome, study_mirror.ethically_approved, study_mirror.study_type, study_mirror.contains_human_dna, study_mirror.contaminated_human_dna, study_mirror.study_visibility, study_mirror.ega_dac_accession_number, study_mirror.ega_policy_accession_number, study_mirror.data_release_timing FROM sample_mirror INNER JOIN library_samples ON library_samples.id_sample_tmp = sample_mirror.id_sample_tmp INNER JOIN study_mirror ON study_mirror.id_study_lims = library_samples.id_study_lims WHERE sample_mirror.name = ? AND sample_mirror.id_lims = 'SQSCP' AND study_mirror.id_lims = 'SQSCP' ORDER BY study_mirror.id_study_lims`
	qualifiedStudyMirrorSelectSQL = qualifySelectColumns("study_mirror", studyMirrorSelectColumns)
)

func qualifySelectColumns(prefix, columns string) string {
	parts := strings.Split(columns, ", ")
	for index, column := range parts {
		parts[index] = prefix + "." + column
	}

	return strings.Join(parts, ", ")
}

type nullableHierarchySampleFields struct {
	idLims          sql.NullString
	idSampleLims    sql.NullString
	uuidSampleLims  sql.NullString
	name            sql.NullString
	sangerSampleID  sql.NullString
	supplierName    sql.NullString
	accessionNumber sql.NullString
	donorID         sql.NullString
	taxonID         sql.NullInt64
	commonName      sql.NullString
	description     sql.NullString
}

func applyNullableHierarchySampleFields(sample *Sample, nullable *nullableHierarchySampleFields) {
	sample.IDLims = nullStringValue(nullable.idLims)
	sample.IDSampleLims = nullStringValue(nullable.idSampleLims)
	sample.UUIDSampleLims = nullStringValue(nullable.uuidSampleLims)
	sample.Name = nullStringValue(nullable.name)
	sample.SangerSampleID = nullStringValue(nullable.sangerSampleID)
	sample.SupplierName = nullStringValue(nullable.supplierName)
	sample.AccessionNumber = nullStringValue(nullable.accessionNumber)
	sample.DonorID = nullStringValue(nullable.donorID)
	if nullable.taxonID.Valid {
		sample.TaxonID = int(nullable.taxonID.Int64)
	}
	sample.CommonName = nullStringValue(nullable.commonName)
	sample.Description = nullStringValue(nullable.description)
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

func querySampleMatches(ctx context.Context, querier Querier, query, action string, args ...any) ([]Sample, error) {
	rows, err := querier.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrUpstreamImpaired, action, err)
	}
	defer func() { _ = rows.Close() }()

	samples := make([]Sample, 0, 2)
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
	nullable := &nullableHierarchySampleFields{}
	if err := scan(
		&sample.IDSampleTmp,
		&nullable.idLims,
		&nullable.idSampleLims,
		&nullable.uuidSampleLims,
		&nullable.name,
		&nullable.sangerSampleID,
		&nullable.supplierName,
		&nullable.accessionNumber,
		&nullable.donorID,
		&nullable.taxonID,
		&nullable.commonName,
		&nullable.description,
	); err != nil {
		return Sample{}, err
	}
	applyNullableHierarchySampleFields(&sample, nullable)

	return sample, nil
}

func loadSampleFanOut(ctx context.Context, c *Client, sampleIDs []int64) (map[int64][]Library, map[int64][]Study, error) {
	if len(sampleIDs) == 0 {
		return map[int64][]Library{}, map[int64][]Study{}, nil
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	uniqueIDs := make([]int64, 0, len(sampleIDs))
	seenIDs := make(map[int64]struct{}, len(sampleIDs))
	for _, sampleID := range sampleIDs {
		if _, seen := seenIDs[sampleID]; seen {
			continue
		}

		seenIDs[sampleID] = struct{}{}
		uniqueIDs = append(uniqueIDs, sampleID)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(uniqueIDs)), ",")
	args := make([]any, 0, len(uniqueIDs))
	for _, sampleID := range uniqueIDs {
		args = append(args, sampleID)
	}

	rows, err := db.QueryContext(
		ctx,
		`SELECT library_samples.id_sample_tmp, library_samples.pipeline_id_lims, `+qualifiedStudyMirrorSelectSQL+`
		 FROM library_samples
		 INNER JOIN study_mirror ON study_mirror.id_study_lims = library_samples.id_study_lims
		 WHERE study_mirror.id_lims = 'SQSCP' AND library_samples.id_sample_tmp IN (`+placeholders+`)
		 ORDER BY library_samples.id_sample_tmp, study_mirror.id_study_lims, library_samples.pipeline_id_lims`,
		args...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: query sample fan-out: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	librariesBySample := make(map[int64][]Library, len(uniqueIDs))
	studiesBySample := make(map[int64][]Study, len(uniqueIDs))
	studySeen := make(map[int64]map[string]struct{}, len(uniqueIDs))

	for rows.Next() {
		var (
			sampleID int64
			library  Library
		)

		study, scanErr := scanStudyRow(func(dest ...any) error {
			scanArgs := make([]any, 0, len(dest)+2)
			scanArgs = append(scanArgs, &sampleID, &library.PipelineIDLims)
			scanArgs = append(scanArgs, dest...)

			return rows.Scan(scanArgs...)
		})
		if scanErr != nil {
			return nil, nil, fmt.Errorf("%w: query sample fan-out: %w", ErrUpstreamImpaired, scanErr)
		}

		library.IDStudyLims = study.IDStudyLims
		librariesBySample[sampleID] = append(librariesBySample[sampleID], library)

		if _, ok := studySeen[sampleID]; !ok {
			studySeen[sampleID] = make(map[string]struct{})
		}
		if _, ok := studySeen[sampleID][study.IDStudyLims]; ok {
			continue
		}

		studySeen[sampleID][study.IDStudyLims] = struct{}{}
		studiesBySample[sampleID] = append(studiesBySample[sampleID], study)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("%w: query sample fan-out: %w", ErrUpstreamImpaired, err)
	}

	return librariesBySample, studiesBySample, nil
}

func hydrateSampleFanOut(ctx context.Context, c *Client, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}

	sampleIDs := make([]int64, 0, len(samples))
	for _, sample := range samples {
		sampleIDs = append(sampleIDs, sample.IDSampleTmp)
	}

	librariesBySample, studiesBySample, err := loadSampleFanOut(ctx, c, sampleIDs)
	if err != nil {
		return err
	}

	for index := range samples {
		sampleID := samples[index].IDSampleTmp
		if libraries := librariesBySample[sampleID]; len(libraries) > 0 {
			samples[index].Libraries = append([]Library(nil), libraries...)
		}
		if studies := studiesBySample[sampleID]; len(studies) > 0 {
			samples[index].Studies = append([]Study(nil), studies...)
		}
	}

	return nil
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

func sampleMatchResult(raw string, samples []Sample) ([]Sample, error) {
	switch len(samples) {
	case 0:
		return nil, ErrNotFound
	case 1:
		return samples, nil
	default:
		return nil, fmt.Errorf("%w: %q ambiguous between %d and %d", ErrAmbiguous, raw, samples[0].IDSampleTmp, samples[1].IDSampleTmp)
	}
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

func (c *Client) cacheSampleExists(ctx context.Context, sangerName string) (bool, error) {
	db := c.readCacheDB()
	if db == nil {
		return false, fmt.Errorf("mlwh: cache reader not configured")
	}

	return queryExists(ctx, db, `SELECT 1 FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, "query sample mirror existence", sangerName)
}

func (c *Client) findSamplesByQuery(ctx context.Context, query, raw, action, syncTable string) ([]Sample, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySampleMatches(ctx, db, query, action, raw)
	if err != nil {
		return nil, err
	}

	matched, err := sampleMatchResult(raw, samples)
	if err == nil {
		if hydrateErr := hydrateSampleFanOut(ctx, c, matched); hydrateErr != nil {
			return nil, hydrateErr
		}

		return matched, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	if syncErr := c.requireResolverSyncState(ctx, syncTable); syncErr != nil {
		return nil, syncErr
	}

	return nil, ErrNotFound
}

// FindSamplesBySangerID returns the unique sample matching sanger_sample_id.
func (c *Client) FindSamplesBySangerID(ctx context.Context, sangerID string) ([]Sample, error) {
	return c.findSamplesByQuery(ctx, findSamplesBySangerIDSQL, sangerID, "query samples by sanger sample id", syncTableSample)
}

// FindSamplesByIDSampleLims returns the unique sample matching id_sample_lims.
func (c *Client) FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]Sample, error) {
	return c.findSamplesByQuery(ctx, findSamplesByIDSampleLimsSQL, idSampleLims, "query samples by id_sample_lims", syncTableSample)
}

// FindSamplesByAccessionNumber returns the unique sample matching accession_number.
func (c *Client) FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]Sample, error) {
	return c.findSamplesByQuery(ctx, findSamplesByAccessionSQL, accessionNumber, "query samples by accession number", syncTableSample)
}

// FindSamplesBySupplierName returns the unique sample matching supplier_name.
func (c *Client) FindSamplesBySupplierName(ctx context.Context, supplierName string) ([]Sample, error) {
	return c.findSamplesByQuery(ctx, findSamplesBySupplierSQL, supplierName, "query samples by supplier name", syncTableSample)
}

// FindSamplesByLibraryType returns the unique sample matching pipeline_id_lims.
func (c *Client) FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]Sample, error) {
	return c.findSamplesByQuery(ctx, findSamplesByLibraryTypeSQL, libraryType, "query samples by library type", syncTableIseqFlowcell)
}

// SamplesForStudy returns samples linked to a study.
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
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}
	if studyExists {
		return []Sample{}, nil
	}

	if err := c.requireResolverSyncState(ctx, syncTableStudy); err != nil {
		return nil, err
	}

	return nil, ErrNotFound
}

// SamplesForRun returns distinct samples linked to a run.
func (c *Client) SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]Sample, error) {
	runID, err := strconv.Atoi(idRun)
	if err != nil {
		return nil, ErrUnsupportedIdentifier
	}
	cacheDB := c.readCacheDB()
	if cacheDB == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, cacheDB, samplesForRunCacheSQL, "query run samples cache", runID, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	if err = c.requireResolverSyncState(ctx, syncTableIseqProductMetrics); err != nil {
		return nil, err
	}

	return nil, ErrNotFound
}

// SamplesForLibrary returns study-scoped samples linked to a library type.
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
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}
	if studyExists {
		return []Sample{}, nil
	}

	if err := c.requireResolverSyncState(ctx, syncTableStudy); err != nil {
		return nil, err
	}

	return nil, ErrNotFound
}

// SamplesForLibraryType returns samples linked to a library type across all studies.
func (c *Client) SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]Sample, error) {
	cacheDB := c.readCacheDB()
	if cacheDB == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, cacheDB, samplesForLibraryTypeCacheSQL, "query library-type samples cache", pipelineIDLims, limit, offset)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	if err := c.requireResolverSyncState(ctx, syncTableIseqFlowcell); err != nil {
		return nil, err
	}

	return []Sample{}, nil
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
		var sampleCount int
		if err = rows.Scan(&library.PipelineIDLims, &sampleCount); err != nil {
			return nil, fmt.Errorf("%w: scan libraries for study: %w", ErrUpstreamImpaired, err)
		}
		library.IDStudyLims = studyLimsID

		libraries = append(libraries, library)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query libraries for study: %w", ErrUpstreamImpaired, err)
	}

	return libraries, nil
}

// RunsForStudy returns runs for a study.
func (c *Client) RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Run, error) {
	studyExists, err := c.cacheStudyExists(ctx, studyLimsID)
	if err != nil {
		return nil, err
	}
	if !studyExists {
		if err = c.requireResolverSyncState(ctx, syncTableStudy); err != nil {
			return nil, err
		}

		return nil, ErrNotFound
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, runsForStudyCacheSQL, studyLimsID, limit, offset)
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
			if syncErr := c.requireResolverSyncState(ctx, syncTableSample); syncErr != nil {
				return nil, syncErr
			}

			return nil, ErrNotFound
		}

		return nil, err
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, lanesForSampleCacheSQL, sample.IDSampleTmp, limit, offset)
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

	return c.queryIRODSPaths(ctx, irodsPathsForSampleCacheSQL, sample.IDSampleTmp, limit, offset, "query irods paths for sample")
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

	return c.queryIRODSPaths(ctx, irodsPathsForStudyCacheSQL, study.IDStudyLims, limit, offset, "query irods paths for study")
}

// StudiesForSample returns the studies linked to a sample.
func (c *Client) StudiesForSample(ctx context.Context, sangerName string) ([]Study, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, studiesForSampleCacheSQL, sangerName)
	if err != nil {
		return nil, fmt.Errorf("%w: query studies for sample: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	studies := make([]Study, 0)
	for rows.Next() {
		study, scanErr := scanStudyRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: query studies for sample: %w", ErrUpstreamImpaired, scanErr)
		}

		studies = append(studies, study)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query studies for sample: %w", ErrUpstreamImpaired, err)
	}
	if len(studies) > 0 {
		return studies, nil
	}

	sampleExists, err := c.cacheSampleExists(ctx, sangerName)
	if err != nil {
		return nil, err
	}
	if sampleExists {
		return nil, ErrNotFound
	}

	if err = c.requireResolverSyncState(ctx, syncTableSample); err != nil {
		return nil, err
	}

	return nil, ErrNotFound
}

func (c *Client) queryIRODSPaths(ctx context.Context, query string, parent any, limit, offset int, action string) ([]IRODSPath, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, query, parent, limit, offset)
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
	if err := c.requireResolverSyncState(ctx, syncTableIseqFlowcell); err != nil {
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
