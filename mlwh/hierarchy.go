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
	samplesForStudyCacheSQL        = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForLibraryTypeCacheSQL  = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForLibraryCacheSQL      = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND library_samples.id_study_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	samplesForRunCacheSQL          = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM iseq_product_metrics_mirror INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = iseq_product_metrics_mirror.id_sample_tmp WHERE iseq_product_metrics_mirror.id_run = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	findSamplesBySangerIDSQL       = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesByIDSampleLimsSQL   = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesByAccessionSQL      = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesBySupplierSQL       = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP' ORDER BY id_sample_tmp LIMIT 2`
	findSamplesByLibraryTypeSQL    = `SELECT DISTINCT ` + sampleMirrorSelectColumns + ` FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.pipeline_id_lims = ? AND sample_mirror.id_lims = 'SQSCP' ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT 2`
	searchSamplesBySangerIDSQL     = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE sanger_sample_id = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	searchSamplesByIDSampleLimsSQL = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE id_sample_lims = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	searchSamplesByAccessionSQL    = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE accession_number = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	searchSamplesBySupplierSQL     = `SELECT ` + sampleMirrorSelectColumns + ` FROM sample_mirror WHERE supplier_name = ? AND id_lims = 'SQSCP' ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp LIMIT ? OFFSET ?`
	librariesForStudySQL           = `SELECT pipeline_id_lims, library_id, id_library_lims, COUNT(DISTINCT id_sample_tmp) FROM library_samples WHERE id_study_lims = ? GROUP BY pipeline_id_lims, library_id, id_library_lims ORDER BY pipeline_id_lims, library_id, id_library_lims LIMIT ? OFFSET ?`
	runsForStudyCacheSQL           = `SELECT DISTINCT id_run FROM iseq_product_metrics_mirror WHERE id_study_lims = ? ORDER BY id_run LIMIT ? OFFSET ?`
	lanesForSampleCacheSQL         = `SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? ORDER BY id_run, position, tag_index LIMIT ? OFFSET ?`
	lanesForSampleStudyCacheSQL    = `SELECT DISTINCT id_run, position, tag_index FROM iseq_product_metrics_mirror WHERE id_sample_tmp = ? AND id_study_lims = ? ORDER BY id_run, position, tag_index LIMIT ? OFFSET ?`
	irodsPathsForSampleCacheSQL    = `SELECT DISTINCT id_iseq_product, irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_sample_tmp = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
	irodsPathsForStudyCacheSQL     = `SELECT DISTINCT id_iseq_product, irods_collection, irods_file_name FROM seq_product_irods_locations_mirror WHERE id_study_lims = ? ORDER BY id_iseq_product LIMIT ? OFFSET ?`
	studiesForSampleCacheSQL       = `SELECT DISTINCT study_mirror.id_study_tmp, study_mirror.id_lims, study_mirror.id_study_lims, study_mirror.uuid_study_lims, study_mirror.name, study_mirror.accession_number, study_mirror.study_title, study_mirror.faculty_sponsor, study_mirror.state, study_mirror.data_release_strategy, study_mirror.data_access_group, study_mirror.programme, study_mirror.reference_genome, study_mirror.ethically_approved, study_mirror.study_type, study_mirror.contains_human_dna, study_mirror.contaminated_human_dna, study_mirror.study_visibility, study_mirror.ega_dac_accession_number, study_mirror.ega_policy_accession_number, study_mirror.data_release_timing FROM sample_mirror INNER JOIN library_samples ON library_samples.id_sample_tmp = sample_mirror.id_sample_tmp INNER JOIN study_mirror ON study_mirror.id_study_lims = library_samples.id_study_lims WHERE sample_mirror.name = ? AND sample_mirror.id_lims = 'SQSCP' AND study_mirror.id_lims = 'SQSCP' ORDER BY study_mirror.id_study_lims`
	qualifiedStudyMirrorSelectSQL  = qualifySelectColumns("study_mirror", studyMirrorSelectColumns)
)

var (
	sampleStudyPairsForLibraryID     = `SELECT DISTINCT ` + sampleMirrorSelectColumns + `, library_samples.id_study_lims FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.library_id = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp, library_samples.id_study_lims LIMIT ? OFFSET ?`
	sampleStudyPairsForLibraryLimsID = `SELECT DISTINCT ` + sampleMirrorSelectColumns + `, library_samples.id_study_lims FROM library_samples INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp = library_samples.id_sample_tmp WHERE library_samples.id_library_lims = ? ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp, library_samples.id_study_lims LIMIT ? OFFSET ?`
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

func (c *Client) expandResolvedSampleIdentifiers(ctx context.Context, base TaggedID, samples []Sample) (expandedSearchValues, error) {
	taggedIDSet := make(map[TaggedID]struct{}, len(samples))
	sampleValues := make([]string, 0, len(samples))
	sampleSeen := make(map[string]struct{}, len(samples))
	runValues := []string{}
	runSeen := map[string]struct{}{}
	laneValues := []string{}
	laneSeen := map[string]struct{}{}

	for _, sample := range samples {
		sampleName := strings.TrimSpace(sample.Name)
		if sampleName == "" {
			continue
		}

		taggedIDSet[TaggedID{Kind: KindSangerSampleName, Canonical: sampleName}] = struct{}{}
		sampleValues = appendUniqueString(sampleValues, sampleSeen, sampleName)

		lanes, err := c.LanesForSample(ctx, sampleName, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}

		addRunTags(taggedIDSet, lanes)
		for _, lane := range lanes {
			runValues = appendUniqueString(runValues, runSeen, strconv.Itoa(lane.IDRun))
			laneValues = appendUniqueString(laneValues, laneSeen, laneSearchValue(lane))
		}
	}

	taggedIDs := append([]TaggedID{base}, sortedTaggedIDs(taggedIDSet)...)

	return expandedSearchValues{
		TaggedIDs: taggedIDs,
		Samples:   sampleValues,
		Runs:      runValues,
		Lanes:     laneValues,
	}, nil
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
		`SELECT library_samples.id_sample_tmp, library_samples.pipeline_id_lims, library_samples.library_id, library_samples.id_library_lims, `+qualifiedStudyMirrorSelectSQL+`
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
			scanArgs := make([]any, 0, len(dest)+4)
			scanArgs = append(scanArgs, &sampleID, &library.PipelineIDLims, &library.LibraryID, &library.IDLibraryLims)
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

type sampleStudyPair struct {
	Sample      Sample
	StudyLimsID string
}

type expandedSearchValues struct {
	TaggedIDs []TaggedID
	Samples   []string
	Runs      []string
	Lanes     []string
}

func sortSampleStudyPairs(pairs []sampleStudyPair) {
	slices.SortFunc(pairs, func(left, right sampleStudyPair) int {
		if left.Sample.Name < right.Sample.Name {
			return -1
		}
		if left.Sample.Name > right.Sample.Name {
			return 1
		}
		if left.StudyLimsID < right.StudyLimsID {
			return -1
		}
		if left.StudyLimsID > right.StudyLimsID {
			return 1
		}
		if left.Sample.IDSampleTmp < right.Sample.IDSampleTmp {
			return -1
		}
		if left.Sample.IDSampleTmp > right.Sample.IDSampleTmp {
			return 1
		}

		return 0
	})
}

func appendUniqueString(values []string, seen map[string]struct{}, value string) []string {
	if value == "" {
		return values
	}
	if _, ok := seen[value]; ok {
		return values
	}

	seen[value] = struct{}{}

	return append(values, value)
}

func laneSearchValue(lane Lane) string {
	if lane.Position <= 0 || lane.TagIndex <= 0 {
		return ""
	}

	return strconv.Itoa(lane.IDRun) + "_" + strconv.Itoa(lane.Position) + "#" + strconv.Itoa(lane.TagIndex)
}

func sampleSearchIdentifierQuery(kind IdentifierKind) string {
	switch kind {
	case KindSangerSampleID:
		return searchSamplesBySangerIDSQL
	case KindSampleLimsID:
		return searchSamplesByIDSampleLimsSQL
	case KindSampleAccession:
		return searchSamplesByAccessionSQL
	case KindSupplierName:
		return searchSamplesBySupplierSQL
	default:
		return ""
	}
}

func (c *Client) lanesForSampleStudy(ctx context.Context, sampleID int64, studyLimsID string, limit, offset int) ([]Lane, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, lanesForSampleStudyCacheSQL, sampleID, studyLimsID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: query lanes for sample study: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	lanes := make([]Lane, 0)
	for rows.Next() {
		lane := Lane{}
		if err = rows.Scan(&lane.IDRun, &lane.Position, &lane.TagIndex); err != nil {
			return nil, fmt.Errorf("%w: scan lanes for sample study: %w", ErrUpstreamImpaired, err)
		}

		lanes = append(lanes, lane)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query lanes for sample study: %w", ErrUpstreamImpaired, err)
	}
	if len(lanes) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableIseqProductMetrics); syncErr != nil {
			return nil, syncErr
		}
	}

	return lanes, nil
}

func buildTaggedIDs(base TaggedID, sampleTags []TaggedID, runTagSet map[TaggedID]struct{}) []TaggedID {
	taggedIDs := make([]TaggedID, 0, 1+len(sampleTags)+len(runTagSet))
	taggedIDs = append(taggedIDs, base)
	taggedIDs = append(taggedIDs, sampleTags...)
	taggedIDs = append(taggedIDs, sortedTaggedIDs(runTagSet)...)

	return taggedIDs
}

func (c *Client) expandSampleStudyPairs(ctx context.Context, base TaggedID, pairs []sampleStudyPair) (expandedSearchValues, error) {
	sortSampleStudyPairs(pairs)

	sampleTags := make([]TaggedID, 0, len(pairs))
	sampleValues := make([]string, 0, len(pairs))
	sampleSeen := make(map[string]struct{}, len(pairs))
	runTagSet := make(map[TaggedID]struct{})
	laneValues := make([]string, 0)
	laneSeen := make(map[string]struct{})

	for _, pair := range pairs {
		sampleTags = append(sampleTags, TaggedID{Kind: KindSangerSampleName, Canonical: pair.Sample.Name})
		sampleValues = appendUniqueString(sampleValues, sampleSeen, pair.Sample.Name)

		lanes, err := c.lanesForSampleStudy(ctx, pair.Sample.IDSampleTmp, pair.StudyLimsID, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}

		for _, lane := range lanes {
			runTagSet[TaggedID{Kind: KindRunID, Canonical: strconv.Itoa(lane.IDRun)}] = struct{}{}
			laneValues = appendUniqueString(laneValues, laneSeen, laneSearchValue(lane))
		}
	}

	runTags := sortedTaggedIDs(runTagSet)
	runValues := make([]string, 0, len(runTags))
	for _, runTag := range runTags {
		runValues = append(runValues, runTag.Canonical)
	}

	return expandedSearchValues{
		TaggedIDs: buildTaggedIDs(base, sampleTags, runTagSet),
		Samples:   sampleValues,
		Runs:      runValues,
		Lanes:     laneValues,
	}, nil
}

func (c *Client) sampleStudyPairsForLibraryIdentifier(ctx context.Context, query, identifier string, limit, offset int) ([]sampleStudyPair, error) {
	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	rows, err := db.QueryContext(ctx, query, identifier, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: query library identifier samples cache: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	pairs := make([]sampleStudyPair, 0)
	for rows.Next() {
		sample, studyLimsID, scanErr := scanSampleStudyPairRow(rows.Scan)
		if scanErr != nil {
			return nil, fmt.Errorf("%w: scan library identifier samples cache: %w", ErrUpstreamImpaired, scanErr)
		}

		pairs = append(pairs, sampleStudyPair{Sample: sample, StudyLimsID: studyLimsID})
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query library identifier samples cache: %w", ErrUpstreamImpaired, err)
	}
	if len(pairs) == 0 {
		if err := c.requireResolverSyncState(ctx, syncTableIseqFlowcell); err != nil {
			return nil, err
		}

		return nil, ErrNotFound
	}

	return pairs, nil
}

func scanSampleStudyPairRow(scan func(dest ...any) error) (Sample, string, error) {
	var studyLimsID string

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
		&studyLimsID,
	); err != nil {
		return Sample{}, "", err
	}
	applyNullableHierarchySampleFields(&sample, nullable)

	return sample, studyLimsID, nil
}

func expandRunIdentifier(base TaggedID, canonical string, samples []Sample) expandedSearchValues {
	taggedIDSet := make(map[TaggedID]struct{}, len(samples))
	addSampleTags(taggedIDSet, samples)
	taggedIDs := append([]TaggedID{base}, sortedTaggedIDs(taggedIDSet)...)
	sampleValues := make([]string, 0, len(taggedIDs)-1)
	for _, taggedID := range taggedIDs[1:] {
		sampleValues = append(sampleValues, taggedID.Canonical)
	}

	return expandedSearchValues{
		TaggedIDs: taggedIDs,
		Samples:   sampleValues,
		Runs:      []string{canonical},
	}
}

func expandSampleIdentifier(base TaggedID, canonical string, lanes []Lane) expandedSearchValues {
	runTagSet := make(map[TaggedID]struct{})
	addRunTags(runTagSet, lanes)
	taggedIDs := append([]TaggedID{base}, sortedTaggedIDs(runTagSet)...)
	runValues := make([]string, 0, len(taggedIDs)-1)
	for _, taggedID := range taggedIDs[1:] {
		runValues = append(runValues, taggedID.Canonical)
	}
	laneValues := make([]string, 0, len(lanes))
	laneSeen := make(map[string]struct{}, len(lanes))
	for _, lane := range lanes {
		laneValues = appendUniqueString(laneValues, laneSeen, laneSearchValue(lane))
	}

	return expandedSearchValues{
		TaggedIDs: taggedIDs,
		Samples:   []string{canonical},
		Runs:      runValues,
		Lanes:     laneValues,
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

func (c *Client) findSamplesByQuery(ctx context.Context, query, raw, action string, syncTables ...string) ([]Sample, error) {
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

	if syncErr := c.requireAnySyncState(ctx, syncTables...); syncErr != nil {
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
	return c.findSamplesByQuery(ctx, findSamplesByLibraryTypeSQL, libraryType, "query samples by library type", syncTableSample, syncTableIseqFlowcell)
}

func (c *Client) samplesForSampleSearchIdentifier(ctx context.Context, kind IdentifierKind, canonical string) ([]Sample, error) {
	query := sampleSearchIdentifierQuery(kind)
	if query == "" {
		return nil, ErrUnsupportedIdentifier
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, db, query, "query sample direct metadata search", canonical, MaxSamplesPerHop, 0)
	if err != nil {
		return nil, err
	}
	if len(samples) > 0 {
		if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
			return nil, err
		}

		return samples, nil
	}

	if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
		return nil, err
	}

	return nil, ErrNotFound
}

// ExpandSampleSearchValues expands direct sample metadata to canonical sample names only.
func (c *Client) ExpandSampleSearchValues(ctx context.Context, kind IdentifierKind, canonical string) ([]string, error) {
	query := sampleSearchIdentifierQuery(kind)
	if query == "" {
		return nil, ErrUnsupportedIdentifier
	}

	db := c.readCacheDB()
	if db == nil {
		return nil, fmt.Errorf("mlwh: cache reader not configured")
	}

	samples, err := querySamples(ctx, db, query, "query sample direct metadata search", canonical, MaxSamplesPerHop, 0)
	if err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
			return nil, err
		}

		return nil, ErrNotFound
	}

	return uniqueSampleNames(samples), nil
}

func uniqueSampleNames(samples []Sample) []string {
	names := make([]string, 0, len(samples))
	seen := make(map[string]struct{}, len(samples))

	for _, sample := range samples {
		name := strings.TrimSpace(sample.Name)
		if name == "" {
			continue
		}

		names = appendUniqueString(names, seen, name)
	}

	return names
}

func (c *Client) samplesForLibraryIdentifier(ctx context.Context, query, identifier string, limit, offset int) ([]Sample, error) {
	pairs, err := c.sampleStudyPairsForLibraryIdentifier(ctx, query, identifier, limit, offset)
	if err != nil {
		return nil, err
	}

	samples := make([]Sample, 0, len(pairs))
	seen := make(map[int64]struct{}, len(pairs))
	for _, pair := range pairs {
		if _, ok := seen[pair.Sample.IDSampleTmp]; ok {
			continue
		}

		seen[pair.Sample.IDSampleTmp] = struct{}{}
		samples = append(samples, pair.Sample)
	}
	if err = hydrateSampleFanOut(ctx, c, samples); err != nil {
		return nil, err
	}

	return samples, nil
}

// SamplesForLibraryID returns samples linked to an exact library_id.
func (c *Client) SamplesForLibraryID(ctx context.Context, libraryID string, limit, offset int) ([]Sample, error) {
	return c.samplesForLibraryIdentifier(ctx, sampleStudyPairsForLibraryID, libraryID, limit, offset)
}

// SamplesForLibraryLimsID returns samples linked to an exact id_library_lims.
func (c *Client) SamplesForLibraryLimsID(ctx context.Context, idLibraryLims string, limit, offset int) ([]Sample, error) {
	return c.samplesForLibraryIdentifier(ctx, sampleStudyPairsForLibraryLimsID, idLibraryLims, limit, offset)
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
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return nil, err
		}
		if summary.allAbsent {
			return []Sample{}, neverSyncedReadErr()
		}
		if !summary.allPresent {
			return []Sample{}, neverSyncedReadErr()
		}

		return []Sample{}, nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		if errors.Is(err, ErrCacheNeverSynced) {
			return []Sample{}, err
		}

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
		if errors.Is(err, ErrCacheNeverSynced) {
			return []Sample{}, err
		}

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
		summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
		if err != nil {
			return nil, err
		}
		if summary.allAbsent {
			return []Sample{}, neverSyncedReadErr()
		}
		if !summary.allPresent {
			return []Sample{}, neverSyncedReadErr()
		}

		return []Sample{}, nil
	}

	if err := c.requireAnySyncState(ctx, syncTableStudy); err != nil {
		if errors.Is(err, ErrCacheNeverSynced) {
			return []Sample{}, err
		}

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

	summary, err := c.requiredSyncStateSummary(ctx, syncTableSample, syncTableIseqFlowcell)
	if err != nil {
		return nil, err
	}
	if summary.allAbsent {
		return []Sample{}, neverSyncedReadErr()
	}
	if !summary.allPresent {
		return []Sample{}, neverSyncedReadErr()
	}

	return []Sample{}, nil
}

// LibrariesForStudy returns libraries for a study.
func (c *Client) LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Library, error) {
	if _, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID); err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableStudy); syncErr != nil {
				if errors.Is(syncErr, ErrCacheNeverSynced) {
					return []Library{}, syncErr
				}

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

	rows, err := db.QueryContext(ctx, librariesForStudySQL, studyLimsID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("%w: query libraries for study: %w", ErrUpstreamImpaired, err)
	}
	defer func() { _ = rows.Close() }()

	libraries := make([]Library, 0)
	for rows.Next() {
		library := Library{}
		var sampleCount int
		if err = rows.Scan(&library.PipelineIDLims, &library.LibraryID, &library.IDLibraryLims, &sampleCount); err != nil {
			return nil, fmt.Errorf("%w: scan libraries for study: %w", ErrUpstreamImpaired, err)
		}
		library.IDStudyLims = studyLimsID

		libraries = append(libraries, library)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: query libraries for study: %w", ErrUpstreamImpaired, err)
	}
	if len(libraries) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableIseqFlowcell); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []Library{}, syncErr
			}

			return nil, syncErr
		}
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
		if err = c.requireAnySyncState(ctx, syncTableStudy); err != nil {
			if errors.Is(err, ErrCacheNeverSynced) {
				return []Run{}, err
			}

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
	if len(runs) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableIseqProductMetrics); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []Run{}, syncErr
			}

			return nil, syncErr
		}
	}

	return runs, nil
}

// LanesForSample returns lanes for a sample.
func (c *Client) LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]Lane, error) {
	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableSample); syncErr != nil {
				if errors.Is(syncErr, ErrCacheNeverSynced) {
					return []Lane{}, syncErr
				}

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
	if len(lanes) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableIseqProductMetrics); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []Lane{}, syncErr
			}

			return nil, syncErr
		}
	}

	return lanes, nil
}

// IRODSPathsForSample returns iRODS paths for a sample.
func (c *Client) IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]IRODSPath, error) {
	sample, err := c.resolveSampleFromCache(ctx, `SELECT `+sampleMirrorSelectColumns+` FROM sample_mirror WHERE name = ? AND id_lims = 'SQSCP' LIMIT 1`, sangerName)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableSample); syncErr != nil {
				if errors.Is(syncErr, ErrCacheNeverSynced) {
					return []IRODSPath{}, syncErr
				}

				return nil, syncErr
			}

			return nil, ErrNotFound
		}

		return nil, err
	}

	paths, err := c.queryIRODSPaths(ctx, irodsPathsForSampleCacheSQL, sample.IDSampleTmp, limit, offset, "query irods paths for sample")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []IRODSPath{}, syncErr
			}

			return nil, syncErr
		}
	}

	return paths, nil
}

// IRODSPathsForStudy returns iRODS paths for a study.
func (c *Client) IRODSPathsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]IRODSPath, error) {
	study, err := c.resolveStudyFromCache(ctx, `SELECT `+studyMirrorSelectColumns+` FROM study_mirror WHERE id_study_lims = ? AND id_lims = 'SQSCP' LIMIT 1`, studyLimsID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			if syncErr := c.requireAnySyncState(ctx, syncTableStudy); syncErr != nil {
				if errors.Is(syncErr, ErrCacheNeverSynced) {
					return []IRODSPath{}, syncErr
				}

				return nil, syncErr
			}

			return nil, ErrNotFound
		}

		return nil, err
	}

	paths, err := c.queryIRODSPaths(ctx, irodsPathsForStudyCacheSQL, study.IDStudyLims, limit, offset, "query irods paths for study")
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		if syncErr := c.requireAnySyncState(ctx, syncTableSeqProductIRODSLocations); syncErr != nil {
			if errors.Is(syncErr, ErrCacheNeverSynced) {
				return []IRODSPath{}, syncErr
			}

			return nil, syncErr
		}
	}

	return paths, nil
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
		if err := c.requireAnySyncState(ctx, syncTableStudy, syncTableIseqFlowcell); err != nil {
			return nil, err
		}

		return nil, ErrNotFound
	}

	if err = c.requireAnySyncState(ctx, syncTableSample); err != nil {
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
		path.IRODSPath = strings.TrimRight(path.Collection, "/") + "/" + path.DataObject

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

	values, err := c.expandIdentifierValues(ctx, kind, canonical)
	if err != nil {
		return nil, err
	}

	c.setExpandIdentifierCache(kind, canonical, values.TaggedIDs)

	return values.TaggedIDs, nil
}

// ExpandSearchValues expands a canonical identifier into sample, run, and lane search values.
func (c *Client) ExpandSearchValues(ctx context.Context, kind IdentifierKind, canonical string) (SearchValues, error) {
	values, err := c.expandIdentifierValues(ctx, kind, canonical)
	if err != nil {
		return SearchValues{}, err
	}

	return SearchValues{
		Samples: values.Samples,
		Runs:    values.Runs,
		Lanes:   values.Lanes,
	}, nil
}

func (c *Client) expandIdentifierValues(ctx context.Context, kind IdentifierKind, canonical string) (expandedSearchValues, error) {
	base := TaggedID{Kind: kind, Canonical: canonical}

	switch kind {
	case KindStudyLimsID:
		samples, err := c.SamplesForStudy(ctx, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}
		pairs := make([]sampleStudyPair, 0, len(samples))
		for _, sample := range samples {
			pairs = append(pairs, sampleStudyPair{Sample: sample, StudyLimsID: canonical})
		}

		return c.expandSampleStudyPairs(ctx, base, pairs)
	case KindLibraryType:
		studyLimsIDs, err := c.libraryStudyLimsIDs(ctx, canonical)
		if err != nil {
			return expandedSearchValues{}, err
		}

		pairs := make([]sampleStudyPair, 0)
		for _, studyLimsID := range studyLimsIDs {
			studySamples, sampleErr := c.SamplesForLibrary(ctx, canonical, studyLimsID, MaxSamplesPerHop, 0)
			if sampleErr != nil {
				return expandedSearchValues{}, sampleErr
			}
			for _, sample := range studySamples {
				pairs = append(pairs, sampleStudyPair{Sample: sample, StudyLimsID: studyLimsID})
			}
		}

		return c.expandSampleStudyPairs(ctx, base, pairs)
	case KindLibraryID:
		pairs, err := c.sampleStudyPairsForLibraryIdentifier(ctx, sampleStudyPairsForLibraryID, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}

		return c.expandSampleStudyPairs(ctx, base, pairs)
	case KindLibraryLimsID:
		pairs, err := c.sampleStudyPairsForLibraryIdentifier(ctx, sampleStudyPairsForLibraryLimsID, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}

		return c.expandSampleStudyPairs(ctx, base, pairs)
	case KindSangerSampleName:
		lanes, err := c.LanesForSample(ctx, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}

		return expandSampleIdentifier(base, canonical, lanes), nil
	case KindSampleLimsID, KindSangerSampleID, KindSupplierName, KindSampleAccession:
		samples, err := c.samplesForSampleSearchIdentifier(ctx, kind, canonical)
		if err != nil {
			return expandedSearchValues{}, err
		}

		return c.expandResolvedSampleIdentifiers(ctx, base, samples)
	case KindRunID:
		samples, err := c.SamplesForRun(ctx, canonical, MaxSamplesPerHop, 0)
		if err != nil {
			return expandedSearchValues{}, err
		}

		return expandRunIdentifier(base, canonical, samples), nil
	default:
		return expandedSearchValues{}, ErrUnsupportedIdentifier
	}
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
