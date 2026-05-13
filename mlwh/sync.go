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
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	syncBatchSize                     = 1000
	syncStatementParamLimit           = 30000
	maxSyncReconnectAttempts          = 5
	sqliteSyncPragmaCleanupTimeout    = 5 * time.Second
	syncTableSample                   = "sample"
	syncTableStudy                    = "study"
	syncTableIseqFlowcell             = "iseq_flowcell"
	syncTableIseqProductMetrics       = "iseq_product_metrics"
	syncTableSeqProductIRODSLocations = "seq_product_irods_locations"
	sqscpIDLims                       = "SQSCP"
	sampleIDDescResumeCursorMode      = "id_sample_tmp_desc"
	sampleLastUpdatedResumeCursorMode = "last_updated"
	iseqProductMetricsIDResumeMode    = "id_iseq_pr_metrics_tmp"
	seqProductIRODSLocationsIDMode    = "id_seq_product_irods_locations_tmp"
	sampleColdInitialID               = int64(1<<63 - 1)
	syncColdInitialAscendingID        = int64(0)
	mysqlInlineSampleIndexRowLimit    = 1000000
	mysqlInlineMirrorIndexRowLimit    = 1000000
)

var syncColdBatchSize = 50000

var supportedSyncTables = []string{
	syncTableSample,
	syncTableStudy,
	syncTableIseqFlowcell,
	syncTableIseqProductMetrics,
	syncTableSeqProductIRODSLocations,
}

var sampleMirrorColumns = []string{
	"id_sample_tmp",
	"id_lims",
	"id_sample_lims",
	"uuid_sample_lims",
	"name",
	"sanger_sample_id",
	"supplier_name",
	"accession_number",
	"donor_id",
	"taxon_id",
	"common_name",
	"description",
	"last_updated",
}

var studyMirrorColumns = []string{
	"id_study_tmp",
	"id_lims",
	"id_study_lims",
	"uuid_study_lims",
	"name",
	"accession_number",
	"study_title",
	"faculty_sponsor",
	"state",
	"data_release_strategy",
	"data_access_group",
	"programme",
	"reference_genome",
	"ethically_approved",
	"study_type",
	"contains_human_dna",
	"contaminated_human_dna",
	"study_visibility",
	"ega_dac_accession_number",
	"ega_policy_accession_number",
	"data_release_timing",
	"last_updated",
}

var iseqProductMetricsMirrorColumns = []string{
	"id_iseq_product",
	"id_iseq_flowcell_tmp",
	"id_run",
	"position",
	"tag_index",
	"id_sample_tmp",
	"id_study_lims",
	"qc",
	"qc_lib",
	"qc_seq",
	"last_updated",
}

var seqProductIRODSLocationsMirrorColumns = []string{
	"id_iseq_product",
	"irods_root_collection",
	"irods_data_relative_path",
	"irods_collection",
	"irods_file_name",
	"id_sample_tmp",
	"id_study_lims",
	"last_updated",
}

type studySourceColumnSpec struct {
	canonical string
	aliases   []string
}

var studySourceColumnSpecs = []studySourceColumnSpec{
	{canonical: "id_study_tmp"},
	{canonical: "id_lims"},
	{canonical: "id_study_lims"},
	{canonical: "uuid_study_lims"},
	{canonical: "name"},
	{canonical: "accession_number"},
	{canonical: "study_title"},
	{canonical: "faculty_sponsor"},
	{canonical: "state"},
	{canonical: "data_release_strategy"},
	{canonical: "data_access_group"},
	{canonical: "programme"},
	{canonical: "reference_genome"},
	{canonical: "ethically_approved"},
	{canonical: "study_type"},
	{canonical: "contains_human_dna"},
	{canonical: "contaminated_human_dna"},
	{canonical: "study_visibility"},
	{canonical: "ega_dac_accession_number", aliases: []string{"egadac_accession_number"}},
	{canonical: "ega_policy_accession_number"},
	{canonical: "data_release_timing"},
}

var syncStateColumns = []string{"table_name", "high_water", "last_run", "resume_cursor", "indexes_dropped"}

type syncIndexSpec struct {
	Name   string
	Column string
}

type syncMirrorIndexSet struct {
	Table                 string
	SyncTable             string
	PrimaryKeyColumn      string
	SkipPrimaryKeyRebuild bool
	Indexes               []syncIndexSpec
}

var sampleMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "sample_mirror_id_sample_lims_idx", Column: "id_sample_lims"},
	{Name: "sample_mirror_uuid_sample_lims_idx", Column: "uuid_sample_lims"},
	{Name: "sample_mirror_name_idx", Column: "name"},
	{Name: "sample_mirror_sanger_sample_id_idx", Column: "sanger_sample_id"},
	{Name: "sample_mirror_supplier_name_idx", Column: "supplier_name"},
	{Name: "sample_mirror_accession_number_idx", Column: "accession_number"},
	{Name: "sample_mirror_donor_id_idx", Column: "donor_id"},
	{Name: "sample_mirror_last_updated_idx", Column: "last_updated"},
}

var sampleMirrorIndexSet = syncMirrorIndexSet{Table: "sample_mirror", SyncTable: syncTableSample, Indexes: sampleMirrorSecondaryIndexes}

var iseqProductMetricsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "iseq_product_metrics_mirror_id_run_position_tag_index_idx", Column: "id_run, position, tag_index"},
	{Name: "ipm_mirror_sample_run_position_tag_idx", Column: "id_sample_tmp, id_run, position, tag_index"},
	{Name: "iseq_product_metrics_mirror_id_iseq_flowcell_tmp_idx", Column: "id_iseq_flowcell_tmp"},
	{Name: "iseq_product_metrics_mirror_id_study_lims_id_run_position_idx", Column: "id_study_lims, id_run, position"},
}

var iseqProductMetricsMirrorIndexSet = syncMirrorIndexSet{
	Table:            "iseq_product_metrics_mirror",
	SyncTable:        syncTableIseqProductMetrics,
	PrimaryKeyColumn: "id_iseq_product",
	Indexes:          iseqProductMetricsMirrorSecondaryIndexes,
}

var seqProductIRODSLocationsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "seq_product_irods_locations_mirror_id_sample_tmp_idx", Column: "id_sample_tmp"},
	{Name: "spi_mirror_study_lims_sample_tmp_idx", Column: "id_study_lims, id_sample_tmp"},
}

var seqProductIRODSLocationsMirrorIndexSet = syncMirrorIndexSet{
	Table:                 "seq_product_irods_locations_mirror",
	SyncTable:             syncTableSeqProductIRODSLocations,
	SkipPrimaryKeyRebuild: true,
	Indexes:               seqProductIRODSLocationsMirrorSecondaryIndexes,
}

var syncMirrorIndexSets = []syncMirrorIndexSet{
	sampleMirrorIndexSet,
	iseqProductMetricsMirrorIndexSet,
	seqProductIRODSLocationsMirrorIndexSet,
}

type sampleSyncMode int

const (
	sampleSyncModeIncremental sampleSyncMode = iota
	sampleSyncModeColdID
)

func sampleColdSyncSourceQuery() string {
	return `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated FROM sample WHERE id_lims = 'SQSCP' AND id_sample_tmp < ? ORDER BY id_sample_tmp DESC`
}

func sampleSyncSourceQuery() string {
	return `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated FROM sample WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_sample_tmp`
}

func sampleSyncSourceQueryFromCursor() string {
	return `SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated FROM sample WHERE id_lims = 'SQSCP' AND ((last_updated > ?) OR (last_updated = ? AND id_sample_tmp > ?)) ORDER BY last_updated, id_sample_tmp`
}

func flowcellSyncSourceQuery() string {
	return `SELECT iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims, iseq_flowcell.legacy_library_id, iseq_flowcell.id_library_lims, iseq_flowcell.last_updated FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp AND study.id_lims = 'SQSCP' WHERE iseq_flowcell.last_updated >= ? ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims`
}

func flowcellSyncSourceQueryFromCursor() string {
	return `SELECT iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims, iseq_flowcell.legacy_library_id, iseq_flowcell.id_library_lims, iseq_flowcell.last_updated FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (iseq_flowcell.last_updated > ?) OR (iseq_flowcell.last_updated = ? AND (iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims) > (?, ?, ?)) ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims`
}

func iseqProductMetricsSyncSourceQuery() string {
	return `SELECT ipm.id_iseq_product, ipm.id_iseq_pr_metrics_tmp, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_changed FROM iseq_product_metrics ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE ipm.last_changed >= ? ORDER BY ipm.last_changed, ipm.id_iseq_pr_metrics_tmp`
}

func iseqProductMetricsColdSyncSourceQuery() string {
	return `SELECT /*+ JOIN_FIXED_ORDER() */ ipm.id_iseq_product, ipm.id_iseq_pr_metrics_tmp, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_changed FROM iseq_product_metrics ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE ipm.id_iseq_pr_metrics_tmp < ? ORDER BY ipm.id_iseq_pr_metrics_tmp DESC`
}

func iseqProductMetricsSyncSourceQueryFromCursor() string {
	return `SELECT ipm.id_iseq_product, ipm.id_iseq_pr_metrics_tmp, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_changed FROM iseq_product_metrics ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (ipm.last_changed > ?) OR (ipm.last_changed = ? AND ipm.id_iseq_pr_metrics_tmp > ?) ORDER BY ipm.last_changed, ipm.id_iseq_pr_metrics_tmp`
}

func seqProductIRODSLocationsSyncSourceQuery() string {
	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp, study.id_study_lims, spi.last_changed FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics path_ipm ON path_ipm.id_iseq_product = spi.id_product INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE INNER JOIN iseq_product_metrics ipm ON ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE spi.last_changed >= ? ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`
}

func seqProductIRODSLocationsColdSyncSourceQuery() string {
	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp, study.id_study_lims, spi.last_changed FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics path_ipm ON path_ipm.id_iseq_product = spi.id_product INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE INNER JOIN iseq_product_metrics ipm ON ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE spi.id_seq_product_irods_locations_tmp > ? ORDER BY spi.id_seq_product_irods_locations_tmp`
}

func seqProductIRODSLocationsSyncSourceQueryFromCursor() string {
	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp, study.id_study_lims, spi.last_changed FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics path_ipm ON path_ipm.id_iseq_product = spi.id_product INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE INNER JOIN iseq_product_metrics ipm ON ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (spi.last_changed > ?) OR (spi.last_changed = ? AND spi.id_seq_product_irods_locations_tmp > ?) ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`
}

func seqProductIRODSLocationsLegacySyncSourceQuery() string {
	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp, study.id_study_lims, spi.last_changed FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_product = spi.id_product INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE spi.last_changed >= ? ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`
}

func seqProductIRODSLocationsLegacyColdSyncSourceQuery() string {
	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp, study.id_study_lims, spi.last_changed FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_product = spi.id_product INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE spi.id_seq_product_irods_locations_tmp > ? ORDER BY spi.id_seq_product_irods_locations_tmp`
}

func seqProductIRODSLocationsLegacySyncSourceQueryFromCursor() string {
	return `SELECT spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp, study.id_study_lims, spi.last_changed FROM seq_product_irods_locations spi INNER JOIN iseq_product_metrics ipm ON ipm.id_iseq_product = spi.id_product INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (spi.last_changed > ?) OR (spi.last_changed = ? AND spi.id_seq_product_irods_locations_tmp > ?) ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`
}

type syncStateRecord struct {
	HighWater      time.Time
	ResumeCursor   *string
	IndexesDropped bool
	Exists         bool
}

// Querier provides the upstream MLWH query surface used by sync.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// SyncReport describes the outcome of syncing one cache table.
type SyncReport struct {
	Table     string
	Inserted  int
	Updated   int
	Duration  time.Duration
	HighWater time.Time
}

func syncSampleTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, mode, err := sampleSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}
	batchSize := sampleSyncBatchSize(mode)
	assumeInserted := sampleSyncCanAssumeInserted(state, mode)

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query sample sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if err = prepareSampleMirrorIndexesForSync(ctx, cache, &state); err != nil {
		return SyncReport{}, false, err
	}

	report := SyncReport{Table: syncTableSample, HighWater: state.HighWater}
	sawRows := false
	batch := make([]sampleSyncRow, 0, batchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		lastRow := batch[len(batch)-1]
		batchHighWater := report.HighWater
		resumeCursor := encodeSampleLastUpdatedResumeCursor(lastRow)
		if mode == sampleSyncModeColdID {
			resumeCursor = encodeSampleIDDescResumeCursor(lastRow)
		}
		result, applyErr := writeSampleBatch(ctx, cache, batch, batchHighWater, &resumeCursor, state.IndexesDropped, assumeInserted)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanSampleSyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}
		if row.Sample.IDLims != sqscpIDLims {
			continue
		}

		sawRows = true
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		batch = append(batch, row)
		if len(batch) == batchSize {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read sample sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSampleSyncState(ctx, cache, report.HighWater, state.IndexesDropped); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func sampleSyncBatchSize(mode sampleSyncMode) int {
	if mode == sampleSyncModeColdID {
		return syncColdBatchSize
	}

	return syncBatchSize
}

func sampleSyncCanAssumeInserted(state syncStateRecord, mode sampleSyncMode) bool {
	if mode != sampleSyncModeColdID {
		return false
	}
	if !state.Exists || state.HighWater.IsZero() {
		return true
	}
	if !state.IndexesDropped || state.ResumeCursor == nil {
		return false
	}

	return strings.HasPrefix(*state.ResumeCursor, sampleIDDescResumeCursorMode+"\t")
}

func syncBatchSizeForState(state syncStateRecord) int {
	if state.HighWater.IsZero() || state.ResumeCursor != nil {
		return syncColdBatchSize
	}

	return syncBatchSize
}

func syncStateCanAssumeInserted(state syncStateRecord) bool {
	return !state.Exists || state.HighWater.IsZero() || state.ResumeCursor != nil
}

func productMirrorSyncCanAssumeInserted(state syncStateRecord, coldIDSync bool, cursorMode string) bool {
	if !state.Exists || state.HighWater.IsZero() {
		return true
	}
	if !coldIDSync || state.ResumeCursor == nil {
		return false
	}

	return strings.HasPrefix(*state.ResumeCursor, cursorMode+"\t")
}

func syncStudyTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := studySyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := queryStudySourceContext(ctx, source, func(columns string) string {
		return strings.Replace(query, studySelectColumns, columns, 1)
	}, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query study sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableStudy, HighWater: state.HighWater}
	sawRows := false
	batchSize := syncBatchSizeForState(state)
	assumeInserted := syncStateCanAssumeInserted(state)
	batch := make([]studySyncRow, 0, batchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeStudyResumeCursor(batch[len(batch)-1])
		result, applyErr := writeStudyBatch(ctx, cache, batch, batchHighWater, &resumeCursor, assumeInserted)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}
	for rows.Next() {
		row, scanErr := scanStudySyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}
		if row.Study.IDLims != sqscpIDLims {
			continue
		}

		sawRows = true
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		batch = append(batch, row)
		if len(batch) == batchSize {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read study sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, syncTableStudy, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func queryStudySourceContext(ctx context.Context, source Querier, queryForColumns func(string) string, args ...any) (*sql.Rows, error) {
	rows, err := source.QueryContext(ctx, queryForColumns(studySelectColumns), args...)
	if err == nil || !isUnknownStudyColumnError(err) {
		return rows, err
	}

	resolvedColumns, resolveErr := resolveStudySourceColumns(ctx, source)
	if resolveErr != nil {
		return nil, errors.Join(err, resolveErr)
	}

	return source.QueryContext(ctx, queryForColumns(resolvedColumns), args...)
}

func resolveStudySourceColumns(ctx context.Context, source Querier) (string, error) {
	rows, err := source.QueryContext(ctx, `SELECT * FROM study WHERE id_lims = 'SQSCP' LIMIT 0`)
	if err != nil {
		return "", fmt.Errorf("mlwh: probe study schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("mlwh: read study schema columns: %w", err)
	}

	available := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		available[column] = struct{}{}
	}

	resolved := make([]string, 0, len(studySourceColumnSpecs))
	for _, spec := range studySourceColumnSpecs {
		column, ok := resolveStudySourceColumn(spec, available)
		if !ok {
			return "", fmt.Errorf("mlwh: study source missing required column %q", spec.canonical)
		}

		if column == spec.canonical {
			resolved = append(resolved, column)

			continue
		}

		resolved = append(resolved, column+` AS `+spec.canonical)
	}

	return strings.Join(resolved, ", "), nil
}

func resolveStudySourceColumn(spec studySourceColumnSpec, available map[string]struct{}) (string, bool) {
	if _, ok := available[spec.canonical]; ok {
		return spec.canonical, true
	}

	for _, alias := range spec.aliases {
		if _, ok := available[alias]; ok {
			return alias, true
		}
	}

	return "", false
}

func isUnknownStudyColumnError(err error) bool {
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "unknown column") || strings.Contains(message, "no such column")
}

func syncFlowcellTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, err := flowcellSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query iseq_flowcell sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableIseqFlowcell, HighWater: state.HighWater}
	sawRows := false
	seen := make(map[string]struct{})
	batchSize := syncBatchSizeForState(state)
	assumeInserted := syncStateCanAssumeInserted(state)
	batch := make([]flowcellSyncRow, 0, batchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeFlowcellResumeCursor(batch[len(batch)-1])
		result, applyErr := writeFlowcellBatch(ctx, cache, batch, batchHighWater, &resumeCursor, assumeInserted)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanFlowcellSyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}

		sawRows = true
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		if row.PipelineIDLims == "" {
			continue
		}

		key := flowcellKey(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		batch = append(batch, row)
		if len(batch) == batchSize {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read iseq_flowcell sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeSyncState(ctx, cache, syncTableIseqFlowcell, report.HighWater); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

// Sync syncs all supported cache tables in parallel.
func (c *Client) Sync(ctx context.Context) ([]SyncReport, error) {
	return c.syncTables(ctx)
}

type sqliteSyncWritePragmaState struct {
	Synchronous int
	CacheSize   int
	TempStore   int
}

func configureSQLiteSyncWritePragmas(ctx context.Context, cache Cache) (func() error, error) {
	if cache == nil || cache.Dialect() != "sqlite" {
		return nil, nil
	}

	state, err := readSQLiteSyncWritePragmaState(ctx, cache.DB())
	if err != nil {
		return nil, err
	}

	for _, statement := range []string{
		`PRAGMA synchronous = OFF`,
		`PRAGMA cache_size = -200000`,
		`PRAGMA temp_store = MEMORY`,
	} {
		if _, err = cache.DB().ExecContext(ctx, statement); err != nil {
			return nil, fmt.Errorf("mlwh: configure sqlite sync write pragma %q: %w", statement, err)
		}
	}

	return func() error {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), sqliteSyncPragmaCleanupTimeout)
		defer cancel()

		if _, restoreErr := cache.DB().ExecContext(cleanupCtx, fmt.Sprintf(`PRAGMA synchronous = %d`, state.Synchronous)); restoreErr != nil {
			return fmt.Errorf("mlwh: restore sqlite sync write pragma synchronous: %w", restoreErr)
		}
		if _, restoreErr := cache.DB().ExecContext(cleanupCtx, fmt.Sprintf(`PRAGMA cache_size = %d`, state.CacheSize)); restoreErr != nil {
			return fmt.Errorf("mlwh: restore sqlite sync write pragma cache_size: %w", restoreErr)
		}
		if _, restoreErr := cache.DB().ExecContext(cleanupCtx, fmt.Sprintf(`PRAGMA temp_store = %d`, state.TempStore)); restoreErr != nil {
			return fmt.Errorf("mlwh: restore sqlite sync write pragma temp_store: %w", restoreErr)
		}

		return nil
	}, nil
}

func readSQLiteSyncWritePragmaState(ctx context.Context, db *sql.DB) (sqliteSyncWritePragmaState, error) {
	var state sqliteSyncWritePragmaState

	if err := db.QueryRowContext(ctx, `PRAGMA synchronous`).Scan(&state.Synchronous); err != nil {
		return sqliteSyncWritePragmaState{}, fmt.Errorf("mlwh: read sqlite sync write pragma synchronous: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA cache_size`).Scan(&state.CacheSize); err != nil {
		return sqliteSyncWritePragmaState{}, fmt.Errorf("mlwh: read sqlite sync write pragma cache_size: %w", err)
	}
	if err := db.QueryRowContext(ctx, `PRAGMA temp_store`).Scan(&state.TempStore); err != nil {
		return sqliteSyncWritePragmaState{}, fmt.Errorf("mlwh: read sqlite sync write pragma temp_store: %w", err)
	}

	return state, nil
}

func (c *Client) syncTables(ctx context.Context) (reports []SyncReport, err error) {
	if c == nil || c.cache == nil {
		return nil, fmt.Errorf("mlwh: cache client not configured")
	}

	tables := append([]string(nil), supportedSyncTables...)

	mu := c.syncMu
	if mu == nil {
		mu = &sync.Mutex{}
		c.syncMu = mu
	}

	mu.Lock()
	defer mu.Unlock()

	releaseLock, err := c.acquireSyncLock(ctx)
	if err != nil {
		return nil, err
	}
	if releaseLock != nil {
		defer func() {
			releaseErr := releaseLock()
			if err == nil && releaseErr != nil {
				err = releaseErr
			}
		}()
	}

	restorePragmas, err := configureSQLiteSyncWritePragmas(ctx, c.cache)
	if err != nil {
		return nil, err
	}
	if restorePragmas != nil {
		defer func() {
			restoreErr := restorePragmas()
			if err == nil && restoreErr != nil {
				err = restoreErr
			}
		}()
	}

	if c.syncRunner != nil {
		return c.runSyncRunner(ctx, tables)
	}

	type syncResult struct {
		report SyncReport
		err    error
	}

	resultCh := make(chan syncResult, len(tables))
	var waitGroup sync.WaitGroup

	for _, table := range tables {
		waitGroup.Add(1)

		go func(table string) {
			defer waitGroup.Done()

			report, syncErr := c.syncTable(ctx, table)
			if syncErr != nil {
				resultCh <- syncResult{report: report, err: fmt.Errorf("%s: %w", table, syncErr)}
				return
			}

			c.emitSyncReport(report)
			resultCh <- syncResult{report: report}
		}(table)
	}

	waitGroup.Wait()
	close(resultCh)

	reports = make([]SyncReport, 0, len(tables))
	var errs []error
	for result := range resultCh {
		if result.report.Table != "" && (result.err == nil || syncReportHasObservedState(result.report)) {
			reports = append(reports, result.report)
		}

		if result.err != nil {
			errs = append(errs, result.err)
		}
	}
	if len(errs) == 0 {
		if repairErr := repairDroppedMirrorIndexes(ctx, c.cache.DB(), c.cache.Dialect()); repairErr != nil {
			errs = append(errs, repairErr)
		}
	}

	return reports, errors.Join(errs...)
}

func readSyncStateFromDB(ctx context.Context, db *sql.DB, table string) (syncStateRecord, error) {
	var highWaterRaw any
	var resumeCursor sql.NullString
	var indexesDropped int
	if err := db.QueryRowContext(ctx, `SELECT high_water, resume_cursor, indexes_dropped FROM sync_state WHERE table_name = ?`, table).Scan(&highWaterRaw, &resumeCursor, &indexesDropped); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return syncStateRecord{}, nil
		}

		return syncStateRecord{}, fmt.Errorf("mlwh: query sync state for %s: %w", table, err)
	}

	highWater, err := parseSyncTimeValue(highWaterRaw)
	if err != nil {
		return syncStateRecord{}, fmt.Errorf("mlwh: parse sync state for %s: %w", table, err)
	}

	state := syncStateRecord{HighWater: highWater, Exists: true}
	if resumeCursor.Valid {
		state.ResumeCursor = &resumeCursor.String
	}
	state.IndexesDropped = indexesDropped == 1

	return state, nil
}

func (c *Client) emitSyncReport(report SyncReport) {
	if c == nil || c.syncReportWriter == nil {
		return
	}

	_, _ = io.WriteString(
		c.syncReportWriter,
		fmt.Sprintf(
			"%s inserted=%d updated=%d high_water=%s\n",
			report.Table,
			report.Inserted,
			report.Updated,
			report.HighWater.UTC().Format("2006-01-02T15:04:05Z"),
		),
	)
}

func (c *Client) runSyncRunner(ctx context.Context, tables []string) (reports []SyncReport, err error) {
	tx, err := c.cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("mlwh: begin cache sync: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err = c.syncRunner(ctx, tx, tables); err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("mlwh: commit cache sync: %w", err)
	}

	committed = true
	c.clearExpandIdentifierCache()

	return nil, nil
}

func (c *Client) syncTable(ctx context.Context, table string) (report SyncReport, err error) {
	report = SyncReport{Table: table}
	started := time.Now()
	defer func() {
		report.Duration = time.Since(started)
	}()
	retryCount := 0

	for {
		state, err := readSyncStateFromDB(ctx, c.cache.DB(), table)
		if err != nil {
			return report, err
		}

		next, _, err := c.syncTableData(ctx, table, state)
		report = mergeSyncReport(report, next)
		if err == nil {
			c.clearExpandIdentifierCache()

			return report, nil
		}
		if !isTransientSyncSourceError(err) {
			return report, err
		}
		if syncReportCommittedProgress(next) {
			retryCount = 0
		}
		if retryCount == maxSyncReconnectAttempts {
			return report, fmt.Errorf("mlwh: sync %s: %w", table, err)
		}

		retryCount++
		backoff := syncReconnectBackoff(retryCount)
		c.emitSyncRetry(table, retryCount, err, backoff)
		if sleepErr := c.sleepSyncRetry(ctx, backoff); sleepErr != nil {
			return report, fmt.Errorf("mlwh: sync %s: %w", table, sleepErr)
		}
	}
}

func syncReportCommittedProgress(report SyncReport) bool {
	return report.Inserted > 0 || report.Updated > 0
}

func syncReportHasObservedState(report SyncReport) bool {
	return syncReportCommittedProgress(report) || !report.HighWater.IsZero()
}

func mergeSyncReport(total, next SyncReport) SyncReport {
	if total.Table == "" {
		total.Table = next.Table
	}
	total.Inserted += next.Inserted
	total.Updated += next.Updated
	total.Duration += next.Duration
	if next.HighWater.After(total.HighWater) {
		total.HighWater = next.HighWater
	}

	return total
}

func isTransientSyncSourceError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	type transientError interface {
		Temporary() bool
		Timeout() bool
	}

	var netErr transientError
	if errors.As(err, &netErr) && (netErr.Temporary() || netErr.Timeout()) {
		return true
	}

	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"invalid connection",
		"unexpected eof",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"timeout awaiting response headers",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

func syncReconnectBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return time.Second
	}

	backoff := time.Second << (attempt - 1)
	if backoff > 30*time.Second {
		return 30 * time.Second
	}

	return backoff
}

func (c *Client) emitSyncRetry(table string, attempt int, retryErr error, backoff time.Duration) {
	var writer io.Writer = os.Stderr
	if c != nil && c.syncRetryWriter != nil {
		writer = c.syncRetryWriter
	}

	_, _ = fmt.Fprintf(writer, "mlwh sync: %s reconnecting attempt %d/%d after %v: backoff %s\n", table, attempt, maxSyncReconnectAttempts, retryErr, backoff)
}

func (c *Client) sleepSyncRetry(ctx context.Context, delay time.Duration) error {
	if c != nil && c.syncRetrySleep != nil {
		return c.syncRetrySleep(ctx, delay)
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withSyncWriteTx(ctx context.Context, cache Cache, apply func(*sql.Tx) error) error {
	if sqliteCache, ok := cache.(*sqliteCache); ok && sqliteCache.writeMu != nil {
		sqliteCache.writeMu.Lock()
		defer sqliteCache.writeMu.Unlock()
	}

	tx, err := cache.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mlwh: begin cache sync: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if cache.Dialect() == "sqlite" {
		if _, err = tx.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
			return fmt.Errorf("mlwh: configure sqlite sync busy timeout: %w", err)
		}
	}

	if err = apply(tx); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("mlwh: commit cache sync: %w", err)
	}

	committed = true

	return nil
}

func parseSyncTimeValue(raw any) (time.Time, error) {
	switch value := raw.(type) {
	case time.Time:
		return value.UTC(), nil
	case string:
		return parseSyncTimeString(value)
	case []byte:
		return parseSyncTimeString(string(value))
	case nil:
		return time.Time{}, fmt.Errorf("nil time value")
	default:
		return time.Time{}, fmt.Errorf("unsupported time value %T", raw)
	}
}

func parseSyncTimeString(raw string) (time.Time, error) {
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return parsed.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time %q", raw)
}

func writeSyncState(ctx context.Context, db *sql.DB, dialect, table string, highWater time.Time, resumeCursor *string, indexesDropped bool) error {
	stmt := buildUpsertStatement(dialect, "sync_state", syncStateColumns, []string{"table_name"})
	_, err := db.ExecContext(ctx, stmt, syncStateArgs(table, highWater, resumeCursor, indexesDropped)...)
	if err != nil {
		return fmt.Errorf("mlwh: write sync state for %s: %w", table, err)
	}

	return nil
}

func writeSyncStateTx(ctx context.Context, tx *sql.Tx, dialect, table string, highWater time.Time, resumeCursor *string, indexesDropped bool) error {
	stmt := buildUpsertStatement(dialect, "sync_state", syncStateColumns, []string{"table_name"})
	_, err := tx.ExecContext(ctx, stmt, syncStateArgs(table, highWater, resumeCursor, indexesDropped)...)
	if err != nil {
		return fmt.Errorf("mlwh: write sync state for %s: %w", table, err)
	}

	return nil
}

func syncStateArgs(table string, highWater time.Time, resumeCursor *string, indexesDropped bool) []any {
	args := []any{table, formatSyncTime(highWater), formatSyncTime(time.Now().UTC())}
	if resumeCursor == nil {
		args = append(args, nil)
	} else {
		args = append(args, *resumeCursor)
	}
	if indexesDropped {
		args = append(args, 1)
	} else {
		args = append(args, 0)
	}

	return args
}

func buildUpsertStatement(dialect, table string, columns, keyColumns []string) string {
	return buildBulkUpsertStatement(dialect, table, columns, keyColumns, 1)
}

func buildBulkUpsertStatement(dialect, table string, columns, keyColumns []string, rowCount int) string {
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + ")"
	values := strings.TrimSuffix(strings.Repeat(rowPlaceholder+", ", rowCount), ", ")
	updateColumns := make([]string, 0, len(columns))
	keySet := make(map[string]struct{}, len(keyColumns))
	for _, key := range keyColumns {
		keySet[key] = struct{}{}
	}
	for _, column := range columns {
		if _, ok := keySet[column]; ok {
			continue
		}
		if dialect == "mysql" {
			updateColumns = append(updateColumns, fmt.Sprintf("%s = VALUES(%s)", column, column))
			continue
		}
		updateColumns = append(updateColumns, fmt.Sprintf("%s = excluded.%s", column, column))
	}
	if len(updateColumns) == 0 {
		if dialect == "mysql" {
			updateColumns = append(updateColumns, fmt.Sprintf("%s = VALUES(%s)", keyColumns[0], keyColumns[0]))
		} else {
			updateColumns = append(updateColumns, fmt.Sprintf("%s = excluded.%s", keyColumns[0], keyColumns[0]))
		}
	}

	insertPrefix := fmt.Sprintf("INSERT INTO %s(%s) VALUES %s", table, strings.Join(columns, ", "), values)
	if dialect == "mysql" {
		return insertPrefix + " ON DUPLICATE KEY UPDATE " + strings.Join(updateColumns, ", ")
	}

	return insertPrefix + " ON CONFLICT(" + strings.Join(keyColumns, ", ") + ") DO UPDATE SET " + strings.Join(updateColumns, ", ")
}

func buildBulkInsertStatement(table string, columns []string, rowCount int) string {
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(columns)), ", ") + ")"
	values := strings.TrimSuffix(strings.Repeat(rowPlaceholder+", ", rowCount), ", ")

	return fmt.Sprintf("INSERT INTO %s(%s) VALUES %s", table, strings.Join(columns, ", "), values)
}

func formatSyncTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func sampleSyncQuery(state syncStateRecord) (string, []any, sampleSyncMode, error) {
	if shouldUseSampleColdIDSync(state) {
		idSampleTmp, err := sampleColdResumeID(state)
		if err != nil {
			return "", nil, sampleSyncModeColdID, err
		}

		return sampleColdSyncSourceQuery(), []any{idSampleTmp}, sampleSyncModeColdID, nil
	}

	if state.ResumeCursor == nil {
		return sampleSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, sampleSyncModeIncremental, nil
	}

	lastUpdated, idSampleTmp, err := parseSampleLastUpdatedResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, sampleSyncModeIncremental, fmt.Errorf("mlwh: parse sample resume cursor: %w", err)
	}

	return sampleSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idSampleTmp}, sampleSyncModeIncremental, nil
}

func shouldUseSampleColdIDSync(state syncStateRecord) bool {
	if !state.Exists || state.IndexesDropped {
		return true
	}
	if state.ResumeCursor == nil {
		return false
	}
	if strings.HasPrefix(*state.ResumeCursor, sampleLastUpdatedResumeCursorMode+"\t") {
		return false
	}

	return true
}

func sampleColdResumeID(state syncStateRecord) (int64, error) {
	if state.ResumeCursor == nil {
		return sampleColdInitialID, nil
	}

	idSampleTmp, ok, err := parseSampleIDDescResumeCursor(*state.ResumeCursor)
	if err != nil {
		return 0, fmt.Errorf("mlwh: parse sample id resume cursor: %w", err)
	}
	if ok {
		return idSampleTmp, nil
	}

	return sampleColdInitialID, nil
}

func studySyncQuery(state syncStateRecord) (string, []any, error) {
	query := `SELECT ` + studySelectColumns + `, last_updated FROM study WHERE id_lims = 'SQSCP' AND last_updated >= ? ORDER BY last_updated, id_study_tmp`
	queryFromCursor := `SELECT ` + studySelectColumns + `, last_updated FROM study WHERE id_lims = 'SQSCP' AND ((last_updated > ?) OR (last_updated = ? AND id_study_tmp > ?)) ORDER BY last_updated, id_study_tmp`
	if state.ResumeCursor == nil {
		return query, []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, idStudyTmp, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse study resume cursor: %w", err)
	}

	return queryFromCursor, []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idStudyTmp}, nil
}

func flowcellSyncQuery(state syncStateRecord) (string, []any, error) {
	if state.ResumeCursor == nil {
		return flowcellSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, nil
	}

	lastUpdated, pipelineIDLims, idSampleTmp, idStudyLims, err := parseFlowcellResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, fmt.Errorf("mlwh: parse iseq_flowcell resume cursor: %w", err)
	}

	return flowcellSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), pipelineIDLims, idSampleTmp, idStudyLims}, nil
}

func iseqProductMetricsSyncQuery(state syncStateRecord) (string, []any, bool, error) {
	if shouldUseAscendingIDColdSync(state, iseqProductMetricsIDResumeMode) {
		id, err := descendingIDColdResumeID(state, iseqProductMetricsIDResumeMode)
		if err != nil {
			return "", nil, true, err
		}

		return iseqProductMetricsColdSyncSourceQuery(), []any{id}, true, nil
	}

	if state.ResumeCursor == nil {
		return iseqProductMetricsSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, false, nil
	}

	lastUpdated, idIseqProduct, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, false, fmt.Errorf("mlwh: parse iseq_product_metrics resume cursor: %w", err)
	}

	return iseqProductMetricsSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idIseqProduct}, false, nil
}

func seqProductIRODSLocationsSyncQuery(state syncStateRecord) (string, []any, bool, error) {
	return seqProductIRODSLocationsSyncQueryForState(state, false)
}

func seqProductIRODSLocationsLegacySyncQuery(state syncStateRecord) (string, []any, bool, error) {
	return seqProductIRODSLocationsSyncQueryForState(state, true)
}

func seqProductIRODSLocationsSyncQueryForState(state syncStateRecord, legacy bool) (string, []any, bool, error) {
	if shouldUseAscendingIDColdSync(state, seqProductIRODSLocationsIDMode) {
		id, err := ascendingIDColdResumeID(state, seqProductIRODSLocationsIDMode)
		if err != nil {
			return "", nil, true, err
		}
		if legacy {
			return seqProductIRODSLocationsLegacyColdSyncSourceQuery(), []any{id}, true, nil
		}

		return seqProductIRODSLocationsColdSyncSourceQuery(), []any{id}, true, nil
	}

	if state.ResumeCursor == nil {
		if legacy {
			return seqProductIRODSLocationsLegacySyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, false, nil
		}

		return seqProductIRODSLocationsSyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, false, nil
	}

	lastUpdated, rowID, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, false, fmt.Errorf("mlwh: parse seq_product_irods_locations resume cursor: %w", err)
	}
	if legacy {
		return seqProductIRODSLocationsLegacySyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), rowID}, false, nil
	}

	return seqProductIRODSLocationsSyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), rowID}, false, nil
}

func shouldUseAscendingIDColdSync(state syncStateRecord, cursorMode string) bool {
	if !state.Exists || state.HighWater.IsZero() {
		return true
	}
	if state.ResumeCursor == nil {
		return false
	}

	return strings.HasPrefix(*state.ResumeCursor, cursorMode+"\t")
}

func ascendingIDColdResumeID(state syncStateRecord, cursorMode string) (int64, error) {
	if state.ResumeCursor == nil {
		return syncColdInitialAscendingID, nil
	}

	id, ok, err := parseAscendingIDResumeCursor(*state.ResumeCursor, cursorMode)
	if err != nil {
		return 0, err
	}
	if ok {
		return id, nil
	}

	return syncColdInitialAscendingID, nil
}

func descendingIDColdResumeID(state syncStateRecord, cursorMode string) (int64, error) {
	if state.ResumeCursor == nil {
		return sampleColdInitialID, nil
	}

	id, ok, err := parseAscendingIDResumeCursor(*state.ResumeCursor, cursorMode)
	if err != nil {
		return 0, err
	}
	if ok {
		return id, nil
	}

	return sampleColdInitialID, nil
}

func finalizeSyncState(ctx context.Context, cache Cache, table string, highWater time.Time) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		return writeSyncStateTx(ctx, tx, cache.Dialect(), table, highWater, nil, false)
	})
}

func finalizeMirrorSyncState(ctx context.Context, cache Cache, indexSet syncMirrorIndexSet, highWater time.Time, indexesDropped bool) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		deferredIndexesDropped := false
		if indexesDropped {
			if shouldDeferMirrorIndexRebuild(cache) {
				deferredIndexesDropped = true
			} else {
				repaired, err := createMirrorDroppedIndexes(ctx, tx, cache.Dialect(), indexSet)
				if err != nil {
					return err
				}
				deferredIndexesDropped = !repaired
			}
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), indexSet.SyncTable, highWater, nil, deferredIndexesDropped)
	})
}

func finalizeSampleSyncState(ctx context.Context, cache Cache, highWater time.Time, indexesDropped bool) error {
	return withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		deferredIndexesDropped := false
		if indexesDropped {
			if shouldDeferMirrorIndexRebuild(cache) {
				deferredIndexesDropped = true
			} else if err := rebuildSampleMirrorColdLoadIndexes(ctx, tx, cache.Dialect()); err != nil {
				return err
			}
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, highWater, nil, deferredIndexesDropped)
	})
}

func shouldDeferMirrorIndexRebuild(cache Cache) bool {
	return cache != nil && cache.Dialect() == "mysql"
}

func rebuildSampleMirrorColdLoadIndexes(ctx context.Context, tx *sql.Tx, dialect string) error {
	if err := rebuildDonorSampleTable(ctx, tx, dialect); err != nil {
		return err
	}
	if dialect == "mysql" {
		rebuildInline, err := shouldRebuildMySQLSampleSecondaryIndexesInline(ctx, tx)
		if err != nil {
			return err
		}
		if !rebuildInline {
			return createMirrorSecondaryIndexes(ctx, tx, dialect, syncMirrorIndexSet{
				Table:   "sample_mirror",
				Indexes: []syncIndexSpec{{Name: "sample_mirror_name_idx", Column: "name"}},
			})
		}
	}
	if err := createSampleMirrorSecondaryIndexes(ctx, tx, dialect); err != nil {
		return err
	}

	return nil
}

func shouldRebuildMySQLSampleSecondaryIndexesInline(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sample_mirror`).Scan(&count); err != nil {
		return false, fmt.Errorf("mlwh: count sample_mirror rows before index rebuild: %w", err)
	}

	return count <= mysqlInlineSampleIndexRowLimit, nil
}

func rebuildDonorSampleTable(ctx context.Context, tx *sql.Tx, dialect string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM donor_samples`); err != nil {
		return fmt.Errorf("mlwh: clear donor samples before rebuild: %w", err)
	}

	insert := `INSERT OR IGNORE INTO donor_samples(donor_id, id_sample_tmp) SELECT donor_id, id_sample_tmp FROM sample_mirror`
	if dialect == "mysql" {
		insert = `INSERT IGNORE INTO donor_samples(donor_id, id_sample_tmp) SELECT donor_id, id_sample_tmp FROM sample_mirror`
	}
	if _, err := tx.ExecContext(ctx, insert); err != nil {
		return fmt.Errorf("mlwh: rebuild donor samples from sample_mirror: %w", err)
	}

	return nil
}

func prepareSampleMirrorIndexesForSync(ctx context.Context, cache Cache, state *syncStateRecord) error {
	if state == nil {
		return fmt.Errorf("mlwh: sample sync state not configured")
	}
	if !shouldDropSampleMirrorIndexes(*state) {
		return nil
	}

	if err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		if err := dropSampleMirrorSecondaryIndexes(ctx, tx, cache.Dialect()); err != nil {
			return err
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, state.HighWater, nil, true)
	}); err != nil {
		return err
	}

	state.Exists = true
	state.IndexesDropped = true

	return nil
}

func shouldDropSampleMirrorIndexes(state syncStateRecord) bool {
	if !state.Exists {
		return true
	}
	if shouldUseSampleColdIDSync(state) && !state.IndexesDropped {
		return true
	}

	return state.HighWater.IsZero() && !state.IndexesDropped
}

func prepareMirrorIndexesForColdSync(ctx context.Context, cache Cache, state *syncStateRecord, indexSet syncMirrorIndexSet) error {
	if state == nil {
		return fmt.Errorf("mlwh: %s sync state not configured", indexSet.SyncTable)
	}
	if state.IndexesDropped {
		return nil
	}

	if err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		if err := dropMirrorSecondaryIndexes(ctx, tx, cache.Dialect(), indexSet); err != nil {
			return err
		}
		if err := dropMirrorPrimaryKey(ctx, tx, cache.Dialect(), indexSet); err != nil {
			return err
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), indexSet.SyncTable, state.HighWater, state.ResumeCursor, true)
	}); err != nil {
		return err
	}

	state.Exists = true
	state.IndexesDropped = true

	return nil
}

func mirrorIndexInventoryQuery(dialect string, table string) string {
	if dialect == "mysql" {
		return fmt.Sprintf(`SELECT DISTINCT INDEX_NAME FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = '%s' AND INDEX_NAME <> 'PRIMARY'`, table)
	}

	return fmt.Sprintf(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = '%s' AND name NOT LIKE 'sqlite_autoindex_%%'`, table)
}

func mirrorExistingIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) (map[string]struct{}, error) {
	rows, err := tx.QueryContext(ctx, mirrorIndexInventoryQuery(dialect, indexSet.Table))
	if err != nil {
		return nil, fmt.Errorf("mlwh: query %s indexes: %w", indexSet.Table, err)
	}
	defer func() { _ = rows.Close() }()

	indexes := make(map[string]struct{}, len(indexSet.Indexes))
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("mlwh: scan %s index: %w", indexSet.Table, err)
		}

		indexes[name] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: iterate %s indexes: %w", indexSet.Table, err)
	}

	return indexes, nil
}

func dropSampleMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string) error {
	return dropMirrorSecondaryIndexes(ctx, tx, dialect, sampleMirrorIndexSet)
}

func dropMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) error {
	existing, err := mirrorExistingIndexes(ctx, tx, dialect, indexSet)
	if err != nil {
		return err
	}

	for _, index := range indexSet.Indexes {
		if _, ok := existing[index.Name]; !ok {
			continue
		}

		stmt := `DROP INDEX IF EXISTS ` + index.Name
		if dialect == "mysql" {
			stmt = `DROP INDEX ` + index.Name + ` ON ` + indexSet.Table
		}

		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: drop %s index %s: %w", indexSet.Table, index.Name, err)
		}
	}

	return nil
}

func createSampleMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string) error {
	return createMirrorSecondaryIndexes(ctx, tx, dialect, sampleMirrorIndexSet)
}

func createMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) error {
	existing, err := mirrorExistingIndexes(ctx, tx, dialect, indexSet)
	if err != nil {
		return err
	}

	missing := missingMirrorSecondaryIndexes(existing, indexSet.Indexes)
	if len(missing) == 0 {
		return nil
	}
	if dialect == "mysql" {
		if _, err = tx.ExecContext(ctx, buildMySQLCreateMirrorSecondaryIndexesStatement(indexSet.Table, missing)); err != nil {
			return fmt.Errorf("mlwh: create %s indexes: %w", indexSet.Table, err)
		}

		return nil
	}

	for _, index := range missing {
		stmt := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(%s)`, index.Name, indexSet.Table, index.Column)

		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("mlwh: create %s index %s: %w", indexSet.Table, index.Name, err)
		}
	}

	return nil
}

func createMirrorDroppedIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) (bool, error) {
	if dialect == "mysql" && indexSet.PrimaryKeyColumn != "" {
		rebuildInline, err := shouldRebuildMySQLMirrorSecondaryIndexesInline(ctx, tx, indexSet)
		if err != nil {
			return false, err
		}
		if !rebuildInline {
			return false, nil
		}
	}

	if err := createMirrorPrimaryKey(ctx, tx, dialect, indexSet); err != nil {
		return false, err
	}
	if err := createMirrorSecondaryIndexes(ctx, tx, dialect, indexSet); err != nil {
		return false, err
	}

	return true, nil
}

func shouldRebuildMySQLMirrorSecondaryIndexesInline(ctx context.Context, tx *sql.Tx, indexSet syncMirrorIndexSet) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+indexSet.Table).Scan(&count); err != nil {
		return false, fmt.Errorf("mlwh: count %s rows before index rebuild: %w", indexSet.Table, err)
	}

	return count <= mysqlInlineMirrorIndexRowLimit, nil
}

func mirrorPrimaryKeyExists(ctx context.Context, tx *sql.Tx, indexSet syncMirrorIndexSet) (bool, error) {
	if indexSet.PrimaryKeyColumn == "" {
		return false, nil
	}

	var count int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = 'PRIMARY'`,
		indexSet.Table,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("mlwh: query %s primary key: %w", indexSet.Table, err)
	}

	return count > 0, nil
}

func dropMirrorPrimaryKey(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) error {
	if dialect != "mysql" || indexSet.PrimaryKeyColumn == "" {
		return nil
	}

	exists, err := mirrorPrimaryKeyExists(ctx, tx, indexSet)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	if _, err = tx.ExecContext(ctx, `ALTER TABLE `+indexSet.Table+` DROP PRIMARY KEY`); err != nil {
		return fmt.Errorf("mlwh: drop %s primary key: %w", indexSet.Table, err)
	}

	return nil
}

func createMirrorPrimaryKey(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) error {
	if dialect != "mysql" || indexSet.PrimaryKeyColumn == "" {
		return nil
	}
	if indexSet.SkipPrimaryKeyRebuild {
		return nil
	}

	exists, err := mirrorPrimaryKeyExists(ctx, tx, indexSet)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if _, err = tx.ExecContext(ctx, `ALTER TABLE `+indexSet.Table+` ADD PRIMARY KEY(`+indexSet.PrimaryKeyColumn+`)`); err != nil {
		return fmt.Errorf("mlwh: create %s primary key: %w", indexSet.Table, err)
	}

	return nil
}

func missingMirrorSecondaryIndexes(existing map[string]struct{}, indexes []syncIndexSpec) []syncIndexSpec {
	missing := make([]syncIndexSpec, 0, len(indexes))
	for _, index := range indexes {
		if _, ok := existing[index.Name]; ok {
			continue
		}

		missing = append(missing, index)
	}

	return missing
}

func buildMySQLCreateSampleMirrorSecondaryIndexesStatement(indexes []syncIndexSpec) string {
	return buildMySQLCreateMirrorSecondaryIndexesStatement("sample_mirror", indexes)
}

func buildMySQLCreateMirrorSecondaryIndexesStatement(table string, indexes []syncIndexSpec) string {
	parts := make([]string, 0, len(indexes))
	for _, index := range indexes {
		parts = append(parts, fmt.Sprintf("ADD INDEX %s(%s)", index.Name, index.Column))
	}

	return "ALTER TABLE " + table + " " + strings.Join(parts, ", ")
}

func parseTwoPartResumeCursor(raw string) (time.Time, int64, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("expected 2 fields, got %d", len(parts))
	}

	lastUpdated, err := parseSyncTimeString(parts[0])
	if err != nil {
		return time.Time{}, 0, err
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse integer field %q: %w", parts[1], err)
	}

	return lastUpdated, id, nil
}

func parseSampleIDDescResumeCursor(raw string) (int64, bool, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 2 || parts[0] != sampleIDDescResumeCursorMode {
		return 0, false, nil
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, true, fmt.Errorf("parse integer field %q: %w", parts[1], err)
	}

	return id, true, nil
}

func parseAscendingIDResumeCursor(raw string, cursorMode string) (int64, bool, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 2 || parts[0] != cursorMode {
		return 0, false, nil
	}

	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, true, fmt.Errorf("parse integer field %q: %w", parts[1], err)
	}

	return id, true, nil
}

func parseSampleLastUpdatedResumeCursor(raw string) (time.Time, int64, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 3 || parts[0] != sampleLastUpdatedResumeCursorMode {
		return time.Time{}, 0, fmt.Errorf("expected %s cursor with 3 fields, got %d", sampleLastUpdatedResumeCursorMode, len(parts))
	}

	lastUpdated, err := parseSyncTimeString(parts[1])
	if err != nil {
		return time.Time{}, 0, err
	}

	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse integer field %q: %w", parts[2], err)
	}

	return lastUpdated, id, nil
}

func parseFlowcellResumeCursor(raw string) (time.Time, string, int64, string, error) {
	parts := strings.Split(raw, "\t")
	if len(parts) != 4 {
		return time.Time{}, "", 0, "", fmt.Errorf("expected 4 fields, got %d", len(parts))
	}

	lastUpdated, err := parseSyncTimeString(parts[0])
	if err != nil {
		return time.Time{}, "", 0, "", err
	}

	idSampleTmp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, "", 0, "", fmt.Errorf("parse integer field %q: %w", parts[2], err)
	}

	return lastUpdated, parts[1], idSampleTmp, parts[3], nil
}

func encodeSampleLastUpdatedResumeCursor(row sampleSyncRow) string {
	return sampleLastUpdatedResumeCursorMode + "\t" + formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.Sample.IDSampleTmp, 10)
}

func encodeSampleIDDescResumeCursor(row sampleSyncRow) string {
	return sampleIDDescResumeCursorMode + "\t" + strconv.FormatInt(row.Sample.IDSampleTmp, 10)
}

func encodeStudyResumeCursor(row studySyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.Study.IDStudyTmp, 10)
}

func encodeFlowcellResumeCursor(row flowcellSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + row.PipelineIDLims + "\t" + strconv.FormatInt(row.IDSampleTmp, 10) + "\t" + row.IDStudyLims
}

func encodeIseqProductMetricsResumeCursor(row iseqProductMetricsSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.SourceRowID, 10)
}

func encodeSeqProductIRODSLocationsResumeCursor(row seqProductIRODSLocationsSyncRow) string {
	return formatSyncTime(row.LastUpdated) + "\t" + strconv.FormatInt(row.SourceRowID, 10)
}

func encodeAscendingIDResumeCursor(cursorMode string, id int64) string {
	return cursorMode + "\t" + strconv.FormatInt(id, 10)
}

func (c *Client) syncTableData(ctx context.Context, table string, state syncStateRecord) (SyncReport, bool, error) {
	source := c.syncSource
	if source == nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: sync source not configured")
	}

	switch table {
	case syncTableSample:
		return syncSampleTable(ctx, c.cache, source, state)
	case syncTableStudy:
		return syncStudyTable(ctx, c.cache, source, state)
	case syncTableIseqFlowcell:
		return syncFlowcellTable(ctx, c.cache, source, state)
	case syncTableIseqProductMetrics:
		return syncIseqProductMetricsTable(ctx, c.cache, source, state)
	case syncTableSeqProductIRODSLocations:
		return syncSeqProductIRODSLocationsTable(ctx, c.cache, source, state)
	default:
		return SyncReport{}, false, fmt.Errorf("mlwh: unsupported sync table %q", table)
	}
}

type iseqProductMetricsSyncRow struct {
	IDIseqProduct     string
	SourceRowID       int64
	IDIseqFlowcellTmp int64
	IDRun             int
	Position          int
	TagIndex          int
	IDSampleTmp       int64
	IDStudyLims       string
	QC                int
	QCLib             int
	QCSeq             int
	LastUpdated       time.Time
}

func syncIseqProductMetricsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, coldIDSync, err := iseqProductMetricsSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}
	if coldIDSync {
		if err = prepareMirrorIndexesForColdSync(ctx, cache, &state, iseqProductMetricsMirrorIndexSet); err != nil {
			return SyncReport{}, false, err
		}
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query iseq_product_metrics sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableIseqProductMetrics, HighWater: state.HighWater}
	sawRows := false
	batchSize := syncBatchSizeForState(state)
	assumeInserted := productMirrorSyncCanAssumeInserted(state, coldIDSync, iseqProductMetricsIDResumeMode)
	batch := make([]iseqProductMetricsSyncRow, 0, batchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeIseqProductMetricsResumeCursor(batch[len(batch)-1])
		if coldIDSync {
			batchHighWater = report.HighWater
			resumeCursor = encodeAscendingIDResumeCursor(iseqProductMetricsIDResumeMode, batch[len(batch)-1].SourceRowID)
		}
		result, applyErr := writeIseqProductMetricsBatch(ctx, cache, batch, batchHighWater, &resumeCursor, state.IndexesDropped, assumeInserted)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanIseqProductMetricsSyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}

		sawRows = true
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		batch = append(batch, row)
		if len(batch) == batchSize {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read iseq_product_metrics sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeMirrorSyncState(ctx, cache, iseqProductMetricsMirrorIndexSet, report.HighWater, state.IndexesDropped); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func scanIseqProductMetricsSyncRow(rows *sql.Rows) (iseqProductMetricsSyncRow, error) {
	var row iseqProductMetricsSyncRow
	var lastUpdated any
	var idRun, position, tagIndex, qc, qcLib, qcSeq sql.NullInt64
	if err := rows.Scan(
		&row.IDIseqProduct,
		&row.SourceRowID,
		&row.IDIseqFlowcellTmp,
		&idRun,
		&position,
		&tagIndex,
		&row.IDSampleTmp,
		&row.IDStudyLims,
		&qc,
		&qcLib,
		&qcSeq,
		&lastUpdated,
	); err != nil {
		return iseqProductMetricsSyncRow{}, fmt.Errorf("mlwh: scan iseq_product_metrics sync row: %w", err)
	}
	row.IDRun = nullIntValue(idRun)
	row.Position = nullIntValue(position)
	row.TagIndex = nullIntValue(tagIndex)
	row.QC = nullIntValue(qc)
	row.QCLib = nullIntValue(qcLib)
	row.QCSeq = nullIntValue(qcSeq)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return iseqProductMetricsSyncRow{}, fmt.Errorf("mlwh: parse iseq_product_metrics last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func nullIntValue(value sql.NullInt64) int {
	if !value.Valid {
		return 0
	}

	return int(value.Int64)
}

func upsertIseqProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []iseqProductMetricsSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(iseqProductMetricsMirrorColumns)), func(chunk []iseqProductMetricsSyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, "iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, []string{"id_iseq_product"}, len(chunk))
		if _, err := tx.ExecContext(ctx, stmt, iseqProductMetricsMirrorBatchArgs(chunk)...); err != nil {
			return fmt.Errorf("mlwh: upsert iseq_product_metrics mirror batch: %w", err)
		}

		return nil
	})
}

func insertIseqProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, rows []iseqProductMetricsSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(iseqProductMetricsMirrorColumns)), func(chunk []iseqProductMetricsSyncRow) error {
		stmt := buildBulkInsertStatement("iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, len(chunk))
		if _, err := tx.ExecContext(ctx, stmt, iseqProductMetricsMirrorBatchArgs(chunk)...); err != nil {
			return fmt.Errorf("mlwh: insert iseq_product_metrics mirror batch: %w", err)
		}

		return nil
	})
}

func replaceIseqProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, rows []iseqProductMetricsSyncRow) error {
	if err := deleteExistingKeys(ctx, tx, "iseq_product_metrics_mirror", []string{"id_iseq_product"}, iseqProductMetricsBatchKeys(rows)); err != nil {
		return err
	}

	return insertIseqProductMetricsMirrorBatch(ctx, tx, rows)
}

func iseqProductMetricsMirrorBatchArgs(rows []iseqProductMetricsSyncRow) []any {
	args := make([]any, 0, len(rows)*len(iseqProductMetricsMirrorColumns))
	for _, row := range rows {
		args = append(args,
			row.IDIseqProduct,
			row.IDIseqFlowcellTmp,
			row.IDRun,
			row.Position,
			row.TagIndex,
			row.IDSampleTmp,
			row.IDStudyLims,
			row.QC,
			row.QCLib,
			row.QCSeq,
			formatSyncTime(row.LastUpdated),
		)
	}

	return args
}

type seqProductIRODSLocationsSyncRow struct {
	SourceRowID           int64
	IDIseqProduct         string
	IRODSRootCollection   string
	IRODSDataRelativePath string
	IRODSCollection       string
	IRODSFileName         string
	IDSampleTmp           int64
	IDStudyLims           string
	LastUpdated           time.Time
}

func syncSeqProductIRODSLocationsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, coldIDSync, err := seqProductIRODSLocationsSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}
	if coldIDSync {
		if err = prepareMirrorIndexesForColdSync(ctx, cache, &state, seqProductIRODSLocationsMirrorIndexSet); err != nil {
			return SyncReport{}, false, err
		}
	}

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		if isUnsupportedCompositionQueryError(err) {
			legacyQuery, legacyArgs, _, legacyErr := seqProductIRODSLocationsLegacySyncQuery(state)
			if legacyErr != nil {
				return SyncReport{}, false, legacyErr
			}

			rows, err = source.QueryContext(ctx, legacyQuery, legacyArgs...)
		}
	}
	if err != nil {
		return SyncReport{}, false, fmt.Errorf("mlwh: query seq_product_irods_locations sync source: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := SyncReport{Table: syncTableSeqProductIRODSLocations, HighWater: state.HighWater}
	sawRows := false
	batchSize := syncBatchSizeForState(state)
	assumeInserted := productMirrorSyncCanAssumeInserted(state, coldIDSync, seqProductIRODSLocationsIDMode)
	batch := make([]seqProductIRODSLocationsSyncRow, 0, batchSize)
	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}

		batchHighWater := batch[len(batch)-1].LastUpdated
		resumeCursor := encodeSeqProductIRODSLocationsResumeCursor(batch[len(batch)-1])
		if coldIDSync {
			batchHighWater = report.HighWater
			resumeCursor = encodeAscendingIDResumeCursor(seqProductIRODSLocationsIDMode, batch[len(batch)-1].SourceRowID)
		}
		result, applyErr := writeSeqProductIRODSLocationsBatch(ctx, cache, batch, batchHighWater, &resumeCursor, state.IndexesDropped, assumeInserted)
		if applyErr != nil {
			return applyErr
		}

		report.Inserted += result.Inserted
		report.Updated += result.Updated
		report.HighWater = batchHighWater
		batch = batch[:0]

		return nil
	}

	for rows.Next() {
		row, scanErr := scanSeqProductIRODSLocationsSyncRow(rows)
		if scanErr != nil {
			return report, false, scanErr
		}

		sawRows = true
		if len(batch) >= batchSize && batch[len(batch)-1].SourceRowID != row.SourceRowID {
			if err = flushBatch(); err != nil {
				return report, false, err
			}
		}

		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		batch = append(batch, row)
	}

	if err = rows.Err(); err != nil {
		return report, false, fmt.Errorf("mlwh: read seq_product_irods_locations sync source: %w", err)
	}
	if err = flushBatch(); err != nil {
		return report, false, err
	}
	if sawRows || state.Exists {
		if err = finalizeMirrorSyncState(ctx, cache, seqProductIRODSLocationsMirrorIndexSet, report.HighWater, state.IndexesDropped); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func isUnsupportedCompositionQueryError(err error) bool {
	message := strings.ToLower(err.Error())

	return strings.Contains(message, "json_table") ||
		strings.Contains(message, "iseq_composition_tmp") ||
		strings.Contains(message, `near "columns"`)
}

func scanSeqProductIRODSLocationsSyncRow(rows *sql.Rows) (seqProductIRODSLocationsSyncRow, error) {
	var row seqProductIRODSLocationsSyncRow
	var lastUpdated any
	if err := rows.Scan(
		&row.SourceRowID,
		&row.IDIseqProduct,
		&row.IRODSRootCollection,
		&row.IRODSDataRelativePath,
		&row.IDSampleTmp,
		&row.IDStudyLims,
		&lastUpdated,
	); err != nil {
		return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: scan seq_product_irods_locations sync row: %w", err)
	}
	row.IRODSCollection, row.IRODSFileName = splitIRODSRelativePath(row.IRODSRootCollection, row.IRODSDataRelativePath)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: parse seq_product_irods_locations last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func splitIRODSRelativePath(rootCollection, relativePath string) (string, string) {
	trimmedRelativePath := strings.TrimSpace(relativePath)
	if trimmedRelativePath == "" {
		return rootCollection, ""
	}

	lastSlash := strings.LastIndex(trimmedRelativePath, "/")
	if lastSlash == -1 {
		return rootCollection, trimmedRelativePath
	}

	directory := trimmedRelativePath[:lastSlash]
	fileName := trimmedRelativePath[lastSlash+1:]
	if directory == "" {
		return rootCollection, fileName
	}

	return strings.TrimRight(rootCollection, "/") + "/" + directory, fileName
}

func insertSeqProductIRODSLocationsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []seqProductIRODSLocationsSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(seqProductIRODSLocationsMirrorColumns)), func(chunk []seqProductIRODSLocationsSyncRow) error {
		stmt := buildBulkInsertStatement("seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorColumns, len(chunk))
		if dialect == "mysql" {
			stmt = strings.Replace(stmt, "INSERT INTO", "INSERT IGNORE INTO", 1)
		}
		if _, err := tx.ExecContext(ctx, stmt, seqProductIRODSLocationsMirrorBatchArgs(chunk)...); err != nil {
			return fmt.Errorf("mlwh: insert seq_product_irods_locations mirror batch: %w", err)
		}

		return nil
	})
}

func replaceSeqProductIRODSLocationsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []seqProductIRODSLocationsSyncRow) error {
	if err := deleteExistingKeys(ctx, tx, "seq_product_irods_locations_mirror", []string{"id_iseq_product"}, seqProductIRODSLocationsBatchKeys(rows)); err != nil {
		return err
	}

	return insertSeqProductIRODSLocationsMirrorBatch(ctx, tx, dialect, rows)
}

func seqProductIRODSLocationsMirrorBatchArgs(rows []seqProductIRODSLocationsSyncRow) []any {
	args := make([]any, 0, len(rows)*len(seqProductIRODSLocationsMirrorColumns))
	for _, row := range rows {
		args = append(args,
			row.IDIseqProduct,
			row.IRODSRootCollection,
			row.IRODSDataRelativePath,
			row.IRODSCollection,
			row.IRODSFileName,
			row.IDSampleTmp,
			row.IDStudyLims,
			formatSyncTime(row.LastUpdated),
		)
	}

	return args
}

type sampleSyncRow struct {
	Sample      Sample
	LastUpdated time.Time
}

type nullableSampleSyncFields struct {
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

func scanSampleSyncRow(rows *sql.Rows) (sampleSyncRow, error) {
	var row sampleSyncRow
	var lastUpdated any
	nullable := &nullableSampleSyncFields{}
	if err := rows.Scan(
		&row.Sample.IDSampleTmp,
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
		&lastUpdated,
	); err != nil {
		return sampleSyncRow{}, fmt.Errorf("mlwh: scan sample sync row: %w", err)
	}
	row.Sample.IDLims = nullStringValue(nullable.idLims)
	row.Sample.IDSampleLims = nullStringValue(nullable.idSampleLims)
	row.Sample.UUIDSampleLims = nullStringValue(nullable.uuidSampleLims)
	row.Sample.Name = nullStringValue(nullable.name)
	row.Sample.SangerSampleID = nullStringValue(nullable.sangerSampleID)
	row.Sample.SupplierName = nullStringValue(nullable.supplierName)
	row.Sample.AccessionNumber = nullStringValue(nullable.accessionNumber)
	row.Sample.DonorID = nullStringValue(nullable.donorID)
	if nullable.taxonID.Valid {
		row.Sample.TaxonID = int(nullable.taxonID.Int64)
	}
	row.Sample.CommonName = nullStringValue(nullable.commonName)
	row.Sample.Description = nullStringValue(nullable.description)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return sampleSyncRow{}, fmt.Errorf("mlwh: parse sample last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertSampleMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []sampleSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(sampleMirrorColumns)), func(chunk []sampleSyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, "sample_mirror", sampleMirrorColumns, []string{"id_sample_tmp"}, len(chunk))
		args := sampleMirrorBatchArgs(chunk)
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: upsert sample mirror batch: %w", err)
		}

		return nil
	})
}

func insertSampleMirrorBatch(ctx context.Context, tx *sql.Tx, rows []sampleSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(sampleMirrorColumns)), func(chunk []sampleSyncRow) error {
		stmt := buildBulkInsertStatement("sample_mirror", sampleMirrorColumns, len(chunk))
		if _, err := tx.ExecContext(ctx, stmt, sampleMirrorBatchArgs(chunk)...); err != nil {
			return fmt.Errorf("mlwh: insert sample mirror batch: %w", err)
		}

		return nil
	})
}

func sampleMirrorBatchArgs(rows []sampleSyncRow) []any {
	args := make([]any, 0, len(rows)*len(sampleMirrorColumns))
	for _, row := range rows {
		args = append(args, sampleMirrorRowArgs(row)...)
	}

	return args
}

func sampleMirrorRowArgs(row sampleSyncRow) []any {
	return []any{
		row.Sample.IDSampleTmp,
		row.Sample.IDLims,
		row.Sample.IDSampleLims,
		row.Sample.UUIDSampleLims,
		row.Sample.Name,
		row.Sample.SangerSampleID,
		row.Sample.SupplierName,
		row.Sample.AccessionNumber,
		row.Sample.DonorID,
		row.Sample.TaxonID,
		row.Sample.CommonName,
		row.Sample.Description,
		formatSyncTime(row.LastUpdated),
	}
}

func replaceDonorSampleBatch(ctx context.Context, tx *sql.Tx, rows []sampleSyncRow) error {
	keyChunkLimit := syncStatementRowLimit(1)
	for start := 0; start < len(rows); start += keyChunkLimit {
		end := min(start+keyChunkLimit, len(rows))
		whereClause, whereArgs := buildKeyInClause([]string{"id_sample_tmp"}, sampleBatchKeys(rows[start:end]))
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM donor_samples WHERE %s", whereClause), whereArgs...); err != nil {
			return fmt.Errorf("mlwh: clear donor sample batch: %w", err)
		}
	}

	return insertDonorSampleBatch(ctx, tx, rows)
}

func insertDonorSampleBatch(ctx context.Context, tx *sql.Tx, rows []sampleSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(2), func(chunk []sampleSyncRow) error {
		insert := buildBulkInsertStatement("donor_samples", []string{"donor_id", "id_sample_tmp"}, len(chunk))
		args := make([]any, 0, len(chunk)*2)
		for _, row := range chunk {
			args = append(args, row.Sample.DonorID, row.Sample.IDSampleTmp)
		}
		if _, err := tx.ExecContext(ctx, insert, args...); err != nil {
			return fmt.Errorf("mlwh: insert donor sample batch: %w", err)
		}

		return nil
	})
}

type studySyncRow struct {
	Study       Study
	LastUpdated time.Time
}

func scanStudySyncRow(rows *sql.Rows) (studySyncRow, error) {
	var row studySyncRow
	var lastUpdated any
	targets, apply := studyScanTargets(&row.Study)
	targets = append(targets, &lastUpdated)
	if err := rows.Scan(targets...); err != nil {
		return studySyncRow{}, fmt.Errorf("mlwh: scan study sync row: %w", err)
	}
	apply()

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return studySyncRow{}, fmt.Errorf("mlwh: parse study last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func upsertStudyMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []studySyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(studyMirrorColumns)), func(chunk []studySyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, "study_mirror", studyMirrorColumns, []string{"id_study_tmp"}, len(chunk))
		args := make([]any, 0, len(chunk)*len(studyMirrorColumns))
		for _, row := range chunk {
			args = append(args,
				row.Study.IDStudyTmp,
				row.Study.IDLims,
				row.Study.IDStudyLims,
				row.Study.UUIDStudyLims,
				row.Study.Name,
				row.Study.AccessionNumber,
				row.Study.StudyTitle,
				row.Study.FacultySponsor,
				row.Study.State,
				row.Study.DataReleaseStrategy,
				row.Study.DataAccessGroup,
				row.Study.Programme,
				row.Study.ReferenceGenome,
				row.Study.EthicallyApproved,
				row.Study.StudyType,
				row.Study.ContainsHumanDNA,
				row.Study.ContaminatedHumanDNA,
				row.Study.StudyVisibility,
				row.Study.EGADACAccessionNumber,
				row.Study.EGAPolicyAccessionNumber,
				row.Study.DataReleaseTiming,
				formatSyncTime(row.LastUpdated),
			)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: upsert study mirror batch: %w", err)
		}

		return nil
	})
}

type flowcellSyncRow struct {
	PipelineIDLims string
	IDSampleTmp    int64
	IDStudyLims    string
	LibraryID      string
	IDLibraryLims  string
	LastUpdated    time.Time
}

func scanFlowcellSyncRow(rows *sql.Rows) (flowcellSyncRow, error) {
	var row flowcellSyncRow
	var pipelineIDLims sql.NullString
	var studyLims sql.NullString
	var libraryID sql.NullString
	var idLibraryLims sql.NullString
	var lastUpdated any
	columns, err := rows.Columns()
	if err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: inspect iseq_flowcell sync columns: %w", err)
	}
	switch len(columns) {
	case 4:
		err = rows.Scan(&pipelineIDLims, &row.IDSampleTmp, &studyLims, &lastUpdated)
	case 6:
		err = rows.Scan(&pipelineIDLims, &row.IDSampleTmp, &studyLims, &libraryID, &idLibraryLims, &lastUpdated)
	default:
		err = fmt.Errorf("unexpected column count %d", len(columns))
	}
	if err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: scan iseq_flowcell sync row: %w", err)
	}
	row.PipelineIDLims = nullStringValue(pipelineIDLims)
	row.IDStudyLims = nullStringValue(studyLims)
	row.LibraryID = nullStringValue(libraryID)
	row.IDLibraryLims = nullStringValue(idLibraryLims)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: parse iseq_flowcell last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func flowcellKey(row flowcellSyncRow) string {
	return fmt.Sprintf("%s\x00%d\x00%s", row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims)
}

func upsertLibrarySampleBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []flowcellSyncRow) error {
	columns := []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims", "library_id", "id_library_lims"}
	return forEachRowChunk(rows, syncStatementRowLimit(len(columns)), func(chunk []flowcellSyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, "library_samples", columns, []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims"}, len(chunk))
		args := make([]any, 0, len(chunk)*len(columns))
		for _, row := range chunk {
			args = append(args, row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims, row.LibraryID, row.IDLibraryLims)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: upsert library sample batch: %w", err)
		}

		return nil
	})
}

type syncBatchResult struct {
	Inserted int
	Updated  int
}

func countExistingKeys(ctx context.Context, tx *sql.Tx, table string, keyColumns []string, keys [][]any) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	total := 0
	chunkLimit := syncStatementRowLimit(len(keyColumns))
	for start := 0; start < len(keys); start += chunkLimit {
		end := min(start+chunkLimit, len(keys))
		whereClause, args := buildKeyInClause(keyColumns, keys[start:end])
		query := fmt.Sprintf("SELECT COUNT(*) FROM (SELECT 1 FROM %s WHERE %s GROUP BY %s) AS existing_keys", table, whereClause, strings.Join(keyColumns, ", "))
		var count int
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			return 0, fmt.Errorf("mlwh: count existing %s batch rows: %w", table, err)
		}

		total += count
	}

	return total, nil
}

func deleteExistingKeys(ctx context.Context, tx *sql.Tx, table string, keyColumns []string, keys [][]any) error {
	if len(keys) == 0 {
		return nil
	}

	chunkLimit := syncStatementRowLimit(len(keyColumns))
	for start := 0; start < len(keys); start += chunkLimit {
		end := min(start+chunkLimit, len(keys))
		whereClause, args := buildKeyInClause(keyColumns, keys[start:end])
		query := fmt.Sprintf("DELETE FROM %s WHERE %s", table, whereClause)
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("mlwh: delete existing %s batch rows: %w", table, err)
		}
	}

	return nil
}

func shouldReplaceSparseMySQLMirrorRows(cache Cache, indexesDropped bool, assumeInserted bool) bool {
	return !assumeInserted && indexesDropped && cache != nil && cache.Dialect() == "mysql"
}

func syncStatementRowLimit(columnCount int) int {
	if columnCount <= 0 {
		return syncBatchSize
	}

	limit := syncStatementParamLimit / columnCount
	if limit < 1 {
		return 1
	}

	return limit
}

func forEachRowChunk[T any](rows []T, limit int, apply func([]T) error) error {
	if limit <= 0 {
		limit = len(rows)
	}

	for start := 0; start < len(rows); start += limit {
		end := min(start+limit, len(rows))
		if err := apply(rows[start:end]); err != nil {
			return err
		}
	}

	return nil
}

func buildKeyInClause(keyColumns []string, keys [][]any) (string, []any) {
	if len(keyColumns) == 1 {
		placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(keys)), ", ")
		args := make([]any, 0, len(keys))
		for _, key := range keys {
			args = append(args, key[0])
		}

		return fmt.Sprintf("%s IN (%s)", keyColumns[0], placeholders), args
	}

	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?, ", len(keyColumns)), ", ") + ")"
	placeholders := strings.TrimSuffix(strings.Repeat(rowPlaceholder+", ", len(keys)), ", ")
	args := make([]any, 0, len(keys)*len(keyColumns))
	for _, key := range keys {
		args = append(args, key...)
	}

	return "(" + strings.Join(keyColumns, ", ") + ") IN (" + placeholders + ")", args
}

func dedupeSampleBatch(rows []sampleSyncRow) []sampleSyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]sampleSyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.Sample.IDSampleTmp]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.Sample.IDSampleTmp] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeStudyBatch(rows []studySyncRow) []studySyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]studySyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.Study.IDStudyTmp]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.Study.IDStudyTmp] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeFlowcellBatch(rows []flowcellSyncRow) []flowcellSyncRow {
	indices := make(map[string]int, len(rows))
	deduped := make([]flowcellSyncRow, 0, len(rows))
	for _, row := range rows {
		key := flowcellKey(row)
		index, ok := indices[key]
		if ok {
			deduped[index] = row
			continue
		}

		indices[key] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeIseqProductMetricsBatch(rows []iseqProductMetricsSyncRow) []iseqProductMetricsSyncRow {
	indices := make(map[string]int, len(rows))
	deduped := make([]iseqProductMetricsSyncRow, 0, len(rows))
	for _, row := range rows {
		index, ok := indices[row.IDIseqProduct]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.IDIseqProduct] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func dedupeSeqProductIRODSLocationsBatch(rows []seqProductIRODSLocationsSyncRow) []seqProductIRODSLocationsSyncRow {
	indices := make(map[string]int, len(rows))
	deduped := make([]seqProductIRODSLocationsSyncRow, 0, len(rows))
	for _, row := range rows {
		key := seqProductIRODSLocationsKey(row)
		index, ok := indices[key]
		if ok {
			deduped[index] = row
			continue
		}

		indices[key] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func seqProductIRODSLocationsKey(row seqProductIRODSLocationsSyncRow) string {
	return fmt.Sprintf("%s\x00%d\x00%s", row.IDIseqProduct, row.IDSampleTmp, row.IDStudyLims)
}

func sampleBatchKeys(rows []sampleSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.Sample.IDSampleTmp})
	}

	return keys
}

func studyBatchKeys(rows []studySyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.Study.IDStudyTmp})
	}

	return keys
}

func flowcellBatchKeys(rows []flowcellSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.PipelineIDLims, row.IDSampleTmp, row.IDStudyLims})
	}

	return keys
}

func iseqProductMetricsBatchKeys(rows []iseqProductMetricsSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.IDIseqProduct})
	}

	return keys
}

func seqProductIRODSLocationsBatchKeys(rows []seqProductIRODSLocationsSyncRow) [][]any {
	keys := make([][]any, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, []any{row.IDIseqProduct})
	}

	return keys
}

func validateFlowcellBatch(rows []flowcellSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: library_samples row (%s, %d) violates constraint: id_study_lims must not be empty", row.PipelineIDLims, row.IDSampleTmp)
		}
	}

	return nil
}

func validateIseqProductMetricsBatch(rows []iseqProductMetricsSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: iseq_product_metrics_mirror row %s violates constraint: id_study_lims must not be empty", row.IDIseqProduct)
		}
	}

	return nil
}

func validateSeqProductIRODSLocationsBatch(rows []seqProductIRODSLocationsSyncRow) error {
	for _, row := range rows {
		if row.IDStudyLims == "" {
			return fmt.Errorf("mlwh: seq_product_irods_locations_mirror row %q violates constraint: id_study_lims must not be empty", row.IDIseqProduct)
		}
	}

	return nil
}

func writeSampleBatch(ctx context.Context, cache Cache, rows []sampleSyncRow, highWater time.Time, resumeCursor *string, indexesDropped bool, assumeInserted bool) (syncBatchResult, error) {
	deduped := dedupeSampleBatch(rows)
	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing := 0
		if !assumeInserted {
			var err error
			existing, err = countExistingKeys(ctx, tx, "sample_mirror", []string{"id_sample_tmp"}, sampleBatchKeys(deduped))
			if err != nil {
				return err
			}
		}
		if assumeInserted {
			if err := insertSampleMirrorBatch(ctx, tx, deduped); err != nil {
				return err
			}
		} else if err := upsertSampleMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		if !indexesDropped {
			if assumeInserted {
				if err := insertDonorSampleBatch(ctx, tx, deduped); err != nil {
					return err
				}
			} else if err := replaceDonorSampleBatch(ctx, tx, deduped); err != nil {
				return err
			}
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err := writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, highWater, resumeCursor, indexesDropped); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeStudyBatch(ctx context.Context, cache Cache, rows []studySyncRow, highWater time.Time, resumeCursor *string, assumeInserted bool) (syncBatchResult, error) {
	deduped := dedupeStudyBatch(rows)
	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing := 0
		if !assumeInserted {
			var err error
			existing, err = countExistingKeys(ctx, tx, "study_mirror", []string{"id_study_tmp"}, studyBatchKeys(deduped))
			if err != nil {
				return err
			}
		}
		if err := upsertStudyMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err := writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableStudy, highWater, resumeCursor, false); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeFlowcellBatch(ctx context.Context, cache Cache, rows []flowcellSyncRow, highWater time.Time, resumeCursor *string, assumeInserted bool) (syncBatchResult, error) {
	deduped := dedupeFlowcellBatch(rows)
	if err := validateFlowcellBatch(deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing := 0
		if !assumeInserted {
			var err error
			existing, err = countExistingKeys(ctx, tx, "library_samples", []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims"}, flowcellBatchKeys(deduped))
			if err != nil {
				return err
			}
		}
		if err := upsertLibrarySampleBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err := writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableIseqFlowcell, highWater, resumeCursor, false); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeIseqProductMetricsBatch(ctx context.Context, cache Cache, rows []iseqProductMetricsSyncRow, highWater time.Time, resumeCursor *string, indexesDropped bool, assumeInserted bool) (syncBatchResult, error) {
	deduped := dedupeIseqProductMetricsBatch(rows)
	if err := validateIseqProductMetricsBatch(deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing := 0
		if !assumeInserted {
			var err error
			existing, err = countExistingKeys(ctx, tx, "iseq_product_metrics_mirror", []string{"id_iseq_product"}, iseqProductMetricsBatchKeys(deduped))
			if err != nil {
				return err
			}
		}
		if assumeInserted {
			if err := insertIseqProductMetricsMirrorBatch(ctx, tx, deduped); err != nil {
				return err
			}
		} else if shouldReplaceSparseMySQLMirrorRows(cache, indexesDropped, assumeInserted) {
			if err := replaceIseqProductMetricsMirrorBatch(ctx, tx, deduped); err != nil {
				return err
			}
		} else if err := upsertIseqProductMetricsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err := writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableIseqProductMetrics, highWater, resumeCursor, indexesDropped); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func writeSeqProductIRODSLocationsBatch(ctx context.Context, cache Cache, rows []seqProductIRODSLocationsSyncRow, highWater time.Time, resumeCursor *string, indexesDropped bool, assumeInserted bool) (syncBatchResult, error) {
	deduped := dedupeSeqProductIRODSLocationsBatch(rows)
	if err := validateSeqProductIRODSLocationsBatch(deduped); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing := 0
		if !assumeInserted {
			var err error
			existing, err = countExistingKeys(ctx, tx, "seq_product_irods_locations_mirror", []string{"id_iseq_product"}, seqProductIRODSLocationsBatchKeys(deduped))
			if err != nil {
				return err
			}
		}
		if assumeInserted {
			if err := insertSeqProductIRODSLocationsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
				return err
			}
		} else {
			if err := replaceSeqProductIRODSLocationsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
				return err
			}
		}

		result.Updated = existing
		result.Inserted = len(deduped) - existing
		if err := writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSeqProductIRODSLocations, highWater, resumeCursor, indexesDropped); err != nil {
			return err
		}

		return nil
	})

	return result, err
}

func (c *Client) requireResolverSyncState(ctx context.Context, table string) error {
	return c.requireAnySyncState(ctx, table)
}

func neverSyncedReadErr() error {
	return fmt.Errorf("%w: %w", ErrNotFound, ErrCacheNeverSynced)
}

func (c *Client) requireAnySyncState(ctx context.Context, tables ...string) error {
	summary, err := c.requiredSyncStateSummary(ctx, tables...)
	if err != nil {
		return err
	}
	if !summary.allPresent {
		return neverSyncedReadErr()
	}

	return nil
}

type requiredSyncStateSummaryResult struct {
	allAbsent  bool
	allPresent bool
}

func (c *Client) requiredSyncStateSummary(ctx context.Context, tables ...string) (requiredSyncStateSummaryResult, error) {
	if len(tables) == 0 {
		return requiredSyncStateSummaryResult{}, fmt.Errorf("mlwh: at least one sync table is required")
	}

	db := c.ReadDB()
	if db == nil {
		if c == nil || c.cache == nil {
			return requiredSyncStateSummaryResult{}, fmt.Errorf("mlwh: cache client not configured")
		}

		db = c.cache.DB()
	}

	seen := make(map[string]struct{}, len(tables))
	summary := requiredSyncStateSummaryResult{allAbsent: true, allPresent: true}

	for _, table := range tables {
		if table == "" {
			return requiredSyncStateSummaryResult{}, fmt.Errorf("mlwh: sync table name must not be empty")
		}
		if _, ok := seen[table]; ok {
			continue
		}
		seen[table] = struct{}{}

		state, err := readSyncStateFromDB(ctx, db, table)
		if err != nil {
			return requiredSyncStateSummaryResult{}, fmt.Errorf("%w: query sync state for %s: %w", ErrUpstreamImpaired, table, err)
		}
		if state.Exists {
			summary.allAbsent = false
		} else {
			summary.allPresent = false
		}
	}

	return summary, nil
}

func (c *Client) hasSyncState(ctx context.Context, table string) (bool, error) {
	if table == "" {
		return false, fmt.Errorf("mlwh: sync table name must not be empty")
	}

	summary, err := c.requiredSyncStateSummary(ctx, table)
	if err != nil {
		return false, err
	}

	return !summary.allAbsent, nil
}
