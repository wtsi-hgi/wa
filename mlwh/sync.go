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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
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
	syncTablePacBioProductMetrics     = "pac_bio_product_metrics"
	syncTablePacBioRunWellMetrics     = "pac_bio_run_well_metrics"
	syncTableEseqProductMetrics       = "eseq_product_metrics"
	syncTableEseqRun                  = "eseq_run"
	syncTableEseqRunLaneMetrics       = "eseq_run_lane_metrics"
	syncTableUseqProductMetrics       = "useq_product_metrics"
	syncTableUseqRunMetrics           = "useq_run_metrics"
	syncTableOseqFlowcell             = "oseq_flowcell"
	syncTableStudyUsers               = "study_users"
	syncTableIseqRunStatus            = "iseq_run_status"
	syncTableIseqRunStatusDict        = "iseq_run_status_dict"
	syncTableSeqOpsTrackingPerSample  = "seq_ops_tracking_per_sample"
	iseqRunStatusIDResumeMode         = "id_run_status"
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

var iseqProductMetricsCompositeRecoveryChunkSize = syncStatementRowLimit(1)

// sampleSearchTokenReadPageSize is the number of sample_mirror rows the cold-load
// token rebuild reads per id-range page. Each page's rows are fully scanned and
// the result set closed before that page's tokens are inserted, so the
// transaction's single connection is never simultaneously reading a result set
// and writing (which fails on MySQL at scale). It is a var so tests can shrink it
// to force a modest fixture across many pages.
var sampleSearchTokenReadPageSize = 4000

// supportedSyncTables is the set of cache tables Sync() / syncTables() fans out
// over. It must stay consistent with freshnessSyncTables (same table set) so
// freshness never reports a table that the orchestrated sync does not populate.
// The original five come first, then the platform-coverage, run-status and
// tracking mirrors (A4) with their A5 sync strategies (dispatched per table by
// syncTableData).
var supportedSyncTables = []string{
	syncTableSample,
	syncTableStudy,
	syncTableIseqFlowcell,
	syncTableIseqProductMetrics,
	syncTableSeqProductIRODSLocations,
	syncTableIseqRunStatus,
	syncTableIseqRunStatusDict,
	syncTableOseqFlowcell,
	syncTableStudyUsers,
	syncTablePacBioRunWellMetrics,
	syncTableEseqRun,
	syncTableEseqRunLaneMetrics,
	syncTableUseqRunMetrics,
	syncTableSeqOpsTrackingPerSample,
	syncTablePacBioProductMetrics,
	syncTableEseqProductMetrics,
	syncTableUseqProductMetrics,
}

var seqProductIRODSLocationsSyncDependencies = map[string]struct{}{
	syncTableIseqFlowcell:         {},
	syncTableIseqProductMetrics:   {},
	syncTablePacBioProductMetrics: {},
	syncTableEseqProductMetrics:   {},
	syncTableUseqProductMetrics:   {},
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

var iseqFlowcellMirrorColumns = []string{
	"id_iseq_flowcell_tmp",
	"entity_type",
	"pipeline_id_lims",
	"id_sample_tmp",
	"id_study_tmp",
}

var seqProductIRODSLocationsMirrorColumns = []string{
	"id_seq_product_irods_locations_tmp",
	"id_iseq_product",
	"irods_root_collection",
	"irods_data_relative_path",
	"irods_collection",
	"irods_file_name",
	"id_sample_tmp",
	"id_study_lims",
	"last_updated",
	"created",
	"platform",
	"id_run",
	"position",
	"tag_index",
	"qc",
	"is_deliverable",
	"merged",
}

var seqProductIRODSLocationsMirrorKeyColumns = []string{
	"id_seq_product_irods_locations_tmp",
}

var syncStateColumns = []string{"table_name", "high_water", "last_run", "resume_cursor", "indexes_dropped"}

// sampleSearchTokenColumns are the sample_search_token columns written in
// declaration order by the cold-load bulk build and the incremental
// maintenance.
var sampleSearchTokenColumns = []string{"token", "id_sample_tmp"}

var commonNameWordMirrorColumns = []string{"word", "common_name"}

// sampleSearchTokenPageQuery selects one id-range page of sample_mirror rows for
// the cold-load token rebuild, ordered by the primary key so paging is a strict
// keyset scan (no OFFSET, no held-open result set).
const sampleSearchTokenPageQuery = `SELECT id_sample_tmp, name, supplier_name, common_name, donor_id FROM sample_mirror WHERE id_sample_tmp > ? ORDER BY id_sample_tmp LIMIT `

// seqProductIRODSLocationsSelectColumns are the projected iRODS sync columns.
// id_sample_tmp/id_study_lims come from the per-platform recovery UNION; created
// and seq_platform_name ride along from seq_product_irods_locations (spi) itself
// so the mirror can store the iRODS creation time and the authoritative platform.
// irods_data_relative_path is nullable upstream (e.g. the Ultimagen iRODS rows
// store NULL), so it is COALESCEd to ” to keep the string scan target and the
// NOT NULL mirror column happy rather than failing with "converting NULL to
// string is unsupported".
const seqProductIRODSLocationsSelectColumns = `spi.id_seq_product_irods_locations_tmp, spi.id_product, spi.irods_root_collection, COALESCE(spi.irods_data_relative_path, '') AS irods_data_relative_path, recovery.id_sample_tmp, recovery.id_study_lims, spi.last_changed, spi.created, spi.seq_platform_name`

// seqProductIRODSLocationsIlluminaCompositionRecovery is the Illumina
// composition-expansion branch: it expands each product's iseq_composition_tmp
// components and recovers sample/study via iseq_flowcell, unchanged from the
// historical join so existing /study/:id/irods results are preserved.
const seqProductIRODSLocationsIlluminaCompositionRecovery = `SELECT path_ipm.id_iseq_product AS id_product, ifc.id_sample_tmp AS id_sample_tmp, study.id_study_lims AS id_study_lims FROM iseq_product_metrics path_ipm INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE INNER JOIN iseq_product_metrics ipm ON ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP'`

// seqProductIRODSLocationsIlluminaLegacyRecovery is the Illumina direct-join
// branch used when the source lacks JSON_TABLE/composition support. Its
// product-metrics alias is path_ipm (not ipm) so the derived-table FROM clause
// does not collide with the iseq_product_metrics sync-routing marker.
const seqProductIRODSLocationsIlluminaLegacyRecovery = `SELECT path_ipm.id_iseq_product AS id_product, ifc.id_sample_tmp AS id_sample_tmp, study.id_study_lims AS id_study_lims FROM iseq_product_metrics path_ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = path_ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP'`

const seqProductIRODSLocationsPacBioRecovery = `SELECT pbm.id_pac_bio_product AS id_product, pbr.id_sample_tmp AS id_sample_tmp, study.id_study_lims AS id_study_lims FROM pac_bio_product_metrics pbm INNER JOIN pac_bio_run pbr ON pbr.id_pac_bio_tmp = pbm.id_pac_bio_tmp INNER JOIN study ON study.id_study_tmp = pbr.id_study_tmp AND study.id_lims = 'SQSCP'`

const seqProductIRODSLocationsElembioRecovery = `SELECT epm.id_eseq_product AS id_product, efc.id_sample_tmp AS id_sample_tmp, study.id_study_lims AS id_study_lims FROM eseq_product_metrics epm INNER JOIN eseq_flowcell efc ON efc.id_eseq_flowcell_tmp = epm.id_eseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = efc.id_study_tmp AND study.id_lims = 'SQSCP'`

const seqProductIRODSLocationsUltimagenRecovery = `SELECT upm.id_useq_product AS id_product, uw.id_sample_tmp AS id_sample_tmp, study.id_study_lims AS id_study_lims FROM useq_product_metrics upm INNER JOIN useq_wafer uw ON uw.id_useq_wafer_tmp = upm.id_useq_wafer_tmp INNER JOIN study ON study.id_study_tmp = uw.id_study_tmp AND study.id_lims = 'SQSCP'`

const seqProductIRODSLocationsONTRecovery = `SELECT CAST(ofc.id_oseq_flowcell_tmp AS CHAR) AS id_product, ofc.id_sample_tmp AS id_sample_tmp, study.id_study_lims AS id_study_lims FROM oseq_flowcell ofc INNER JOIN study ON study.id_study_tmp = ofc.id_study_tmp AND study.id_lims = 'SQSCP'`

// seqProductIRODSLocationsNonIlluminaRecovery is the PacBio/Elembio/Ultimagen
// and ONT recovery, keyed on each platform's source product/identity id matching
// spi.id_product and recovering only id_sample_tmp/id_study_lims (platform always
// comes from spi.seq_platform_name, never from which metrics table matched).
const seqProductIRODSLocationsNonIlluminaRecovery = seqProductIRODSLocationsPacBioRecovery + ` UNION ALL ` +
	seqProductIRODSLocationsElembioRecovery + ` UNION ALL ` +
	seqProductIRODSLocationsUltimagenRecovery + ` UNION ALL ` +
	seqProductIRODSLocationsONTRecovery

// seqProductIRODSLocationsChangedRecovery preserves every recovery branch while
// restricting its source-side work to products selected by the warm changed_spi
// CTE. The composition JSON_TABLE fragment above remains byte-identical.
const seqProductIRODSLocationsChangedRecovery = seqProductIRODSLocationsIlluminaCompositionRecovery +
	` WHERE path_ipm.id_iseq_product IN (SELECT id_product FROM changed_spi) UNION ALL ` +
	seqProductIRODSLocationsPacBioRecovery +
	` WHERE pbm.id_pac_bio_product IN (SELECT id_product FROM changed_spi) UNION ALL ` +
	seqProductIRODSLocationsElembioRecovery +
	` WHERE epm.id_eseq_product IN (SELECT id_product FROM changed_spi) UNION ALL ` +
	seqProductIRODSLocationsUltimagenRecovery +
	` WHERE upm.id_useq_product IN (SELECT id_product FROM changed_spi) UNION ALL ` +
	seqProductIRODSLocationsONTRecovery +
	` WHERE CAST(ofc.id_oseq_flowcell_tmp AS CHAR) IN (SELECT id_product FROM changed_spi)`

const iseqProductMetricsSelectColumns = `ipm.id_iseq_product, ipm.id_iseq_pr_metrics_tmp, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ipm.id_sample_tmp, ipm.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_changed`

const iseqProductMetricsDirectSingleComponentOnly = `NOT EXISTS (SELECT 1 FROM JSON_TABLE(COALESCE(ipm.iseq_composition_tmp, '{"components":[]}'), '$.components[1]' COLUMNS(component_run INT PATH '$.id_run')) direct_component)`

// sampleSearchTokenIndex is the (token, id_sample_tmp) covering index that backs
// the index-order sample search page. It is dropped before the cold-load bulk
// token build and recreated after, mirroring the secondary-index discipline.
var sampleSearchTokenIndex = syncIndexSpec{Name: "sample_search_token_idx", Column: "token, id_sample_tmp"}

type studySourceColumnSpec struct {
	canonical string
	aliases   []string
}

type syncIndexSpec struct {
	Name   string
	Column string
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

var sampleMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "sample_mirror_id_sample_lims_idx", Column: "id_sample_lims"},
	{Name: "sample_mirror_uuid_sample_lims_idx", Column: "uuid_sample_lims"},
	{Name: "sample_mirror_name_idx", Column: "name"},
	{Name: "sample_mirror_sanger_sample_id_idx", Column: "sanger_sample_id"},
	{Name: "sample_mirror_supplier_name_idx", Column: "supplier_name"},
	{Name: "sample_mirror_accession_number_idx", Column: "accession_number"},
	{Name: "sample_mirror_donor_id_idx", Column: "donor_id"},
	{Name: "sample_mirror_common_name_idx", Column: "common_name"},
	{Name: "sample_mirror_last_updated_idx", Column: "last_updated"},
}

type syncMirrorIndexSet struct {
	Table                 string
	SyncTable             string
	PrimaryKeyColumn      string
	SkipPrimaryKeyRebuild bool
	Indexes               []syncIndexSpec
}

var sampleMirrorIndexSet = syncMirrorIndexSet{Table: "sample_mirror", SyncTable: syncTableSample, Indexes: sampleMirrorSecondaryIndexes}

var iseqFlowcellMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "iseq_flowcell_mirror_entity_type_idx", Column: "entity_type"},
}

var iseqFlowcellMirrorReadIndexes = []syncIndexSpec{
	{Name: "iseq_flowcell_mirror_id_iseq_flowcell_tmp_idx", Column: "id_iseq_flowcell_tmp"},
	{Name: "iseq_flowcell_mirror_entity_type_idx", Column: "entity_type"},
}

var iseqFlowcellMirrorIndexSet = syncMirrorIndexSet{
	Table:            "iseq_flowcell_mirror",
	SyncTable:        syncTableIseqFlowcell,
	PrimaryKeyColumn: "id_iseq_flowcell_tmp",
	Indexes:          iseqFlowcellMirrorSecondaryIndexes,
}

var iseqProductMetricsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "iseq_product_metrics_mirror_id_run_position_tag_index_idx", Column: "id_run, position, tag_index"},
	{Name: "ipm_mirror_sample_run_position_tag_idx", Column: "id_sample_tmp, id_run, position, tag_index"},
	{Name: "ipm_mirror_sample_qc_idx", Column: "id_sample_tmp, qc"},
	{Name: "iseq_product_metrics_mirror_id_iseq_flowcell_tmp_idx", Column: "id_iseq_flowcell_tmp"},
	{Name: "iseq_product_metrics_mirror_id_study_lims_id_run_position_idx", Column: "id_study_lims, id_run, position"},
	{Name: "ipm_mirror_study_sample_product_qc_idx", Column: "id_study_lims, id_sample_tmp, id_iseq_product, qc"},
}

var iseqProductMetricsMirrorIndexSet = syncMirrorIndexSet{
	Table:            "iseq_product_metrics_mirror",
	SyncTable:        syncTableIseqProductMetrics,
	PrimaryKeyColumn: "id_iseq_product",
	Indexes:          iseqProductMetricsMirrorSecondaryIndexes,
}

var seqProductIRODSLocationsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "spi_mirror_source_row_idx", Column: "id_seq_product_irods_locations_tmp"},
	{Name: "seq_product_irods_locations_mirror_id_sample_tmp_idx", Column: "id_sample_tmp"},
	{Name: "spi_mirror_sample_tmp_iseq_product_idx", Column: "id_sample_tmp, id_iseq_product"},
	{Name: "spi_mirror_sample_tmp_created_idx", Column: "id_sample_tmp, created"},
	{Name: "spi_mirror_run_created_idx", Column: "id_run, created"},
	{Name: "spi_mirror_study_lims_sample_tmp_idx", Column: "id_study_lims, id_sample_tmp"},
	{Name: "spi_mirror_study_lims_export_idx", Column: "id_study_lims, id_run, position, tag_index, id_seq_product_irods_locations_tmp"},
	{Name: "spi_mirror_study_lims_iseq_product_idx", Column: "id_study_lims, id_iseq_product"},
	{Name: "spi_mirror_iseq_product_idx", Column: "id_iseq_product"},
}

// iseqProductMetricsMirrorReadIndexes is the subset of the iseq product-metrics
// mirror's secondary indexes that the sparse cold-load path recreates immediately
// (for a mirror too large to rebuild every declared index inline). It MUST include
// the (id_study_lims, id_run, position) index: without it the id_study_lims-scoped
// study read/aggregate queries (RunsForStudy, CountRunsForStudy, the study
// overview, the availability joins) full-scan the ~9M-row mirror (~52s) instead of
// being index-served (~1s). Omitting it here was the root cause of the slow study
// pages, because the large-cold-load schema-shape tolerance then accepted the
// missing index as expected drift. It MUST also include the (id_iseq_product)
// index: during the same cold load, seq_product_irods_locations enriches each
// iRODS batch from this mirror by product id before the deferred primary key is
// rebuilt, so without a sparse product-id index every iRODS batch full-scans the
// ~9M-row product mirror.
var iseqProductMetricsMirrorReadIndexes = []syncIndexSpec{
	{Name: "ipm_mirror_iseq_product_idx", Column: "id_iseq_product"},
	{Name: "iseq_product_metrics_mirror_id_run_position_tag_index_idx", Column: "id_run, position, tag_index"},
	{Name: "ipm_mirror_sample_run_position_tag_idx", Column: "id_sample_tmp, id_run, position, tag_index"},
	{Name: "ipm_mirror_sample_qc_idx", Column: "id_sample_tmp, qc"},
	{Name: "iseq_product_metrics_mirror_id_study_lims_id_run_position_idx", Column: "id_study_lims, id_run, position"},
	{Name: "ipm_mirror_study_sample_product_qc_idx", Column: "id_study_lims, id_sample_tmp, id_iseq_product, qc"},
}

// seqProductIRODSLocationsMirrorReadIndexes is the subset of the iRODS-locations
// mirror's secondary indexes that the sparse cold-load path recreates immediately
// (the mirror is too large -- ~9M rows -- to rebuild every declared index inline).
// It includes the upstream source-row index so warm incremental replacement can
// remove stale cached paths by stable source identity without scanning the mirror.
// It MUST include the (id_study_lims, id_sample_tmp) index: the per-platform
// status-breakdown query intersects each platform's study samples with study-scoped
// iRODS rows by sample, and without this index that linkage full-scans the mirror
// per study. It MUST also include the (id_iseq_product) and (id_study_lims,
// id_iseq_product) indexes: the D1 run-scoped iRODS join and the D2 manifest
// per-product iRODS LEFT JOIN match on product id, so without them those joins
// full-scan the mirror until the full index set is rebuilt. Omitting these here
// would let the large-cold-load schema-shape tolerance accept the missing indexes
// as expected drift, silently recreating slow paths.
var seqProductIRODSLocationsMirrorReadIndexes = []syncIndexSpec{
	{Name: "spi_mirror_source_row_idx", Column: "id_seq_product_irods_locations_tmp"},
	{Name: "seq_product_irods_locations_mirror_id_sample_tmp_idx", Column: "id_sample_tmp"},
	{Name: "spi_mirror_sample_tmp_iseq_product_idx", Column: "id_sample_tmp, id_iseq_product"},
	{Name: "spi_mirror_sample_tmp_created_idx", Column: "id_sample_tmp, created"},
	{Name: "spi_mirror_run_created_idx", Column: "id_run, created"},
	{Name: "spi_mirror_study_lims_sample_tmp_idx", Column: "id_study_lims, id_sample_tmp"},
	{Name: "spi_mirror_study_lims_created_idx", Column: "id_study_lims, created"},
	{Name: "spi_mirror_study_lims_export_idx", Column: "id_study_lims, id_run, position, tag_index, id_seq_product_irods_locations_tmp"},
	{Name: "spi_mirror_study_lims_iseq_product_idx", Column: "id_study_lims, id_iseq_product"},
	{Name: "spi_mirror_iseq_product_idx", Column: "id_iseq_product"},
}

var seqProductIRODSLocationsMirrorIndexSet = syncMirrorIndexSet{
	Table:                 "seq_product_irods_locations_mirror",
	SyncTable:             syncTableSeqProductIRODSLocations,
	SkipPrimaryKeyRebuild: true,
	Indexes:               seqProductIRODSLocationsMirrorSecondaryIndexes,
}

var iseqRunStatusMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "iseq_run_status_mirror_id_run_idx", Column: "id_run"},
	{Name: "iseq_run_status_mirror_id_run_date_idx", Column: "id_run, date"},
	{Name: "iseq_run_status_mirror_normalised_date_idx", Column: "normalised_date, id_run_status_dict, id_run"},
}

var iseqRunStatusMirrorReadIndexes = []syncIndexSpec{
	{Name: "iseq_run_status_mirror_normalised_date_idx", Column: "normalised_date, id_run_status_dict, id_run"},
}

var iseqRunStatusMirrorIndexSet = syncMirrorIndexSet{
	Table:     "iseq_run_status_mirror",
	SyncTable: syncTableIseqRunStatus,
	Indexes:   iseqRunStatusMirrorSecondaryIndexes,
}

var pacBioRunWellMetricsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "pac_bio_run_well_metrics_mirror_run_name_well_label_idx", Column: "pac_bio_run_name, well_label"},
	{Name: "pac_bio_run_well_metrics_mirror_normalised_date_idx", Column: "normalised_date"},
}

var pacBioRunWellMetricsMirrorReadIndexes = []syncIndexSpec{
	{Name: "pac_bio_run_well_metrics_mirror_normalised_date_idx", Column: "normalised_date"},
}

var pacBioRunWellMetricsMirrorIndexSet = syncMirrorIndexSet{
	Table:     "pac_bio_run_well_metrics_mirror",
	SyncTable: syncTablePacBioRunWellMetrics,
	Indexes:   pacBioRunWellMetricsMirrorSecondaryIndexes,
}

var oseqFlowcellMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "oseq_flowcell_mirror_id_sample_tmp_idx", Column: "id_sample_tmp"},
	{Name: "oseq_flowcell_mirror_id_study_lims_idx", Column: "id_study_lims"},
	{Name: "oseq_flowcell_mirror_experiment_name_idx", Column: "experiment_name"},
	{Name: "oseq_flowcell_mirror_last_updated_idx", Column: "last_updated"},
	{Name: "oseq_flowcell_mirror_normalised_date_idx", Column: "normalised_date"},
}

var oseqFlowcellMirrorReadIndexes = []syncIndexSpec{
	{Name: "oseq_flowcell_mirror_last_updated_idx", Column: "last_updated"},
	{Name: "oseq_flowcell_mirror_normalised_date_idx", Column: "normalised_date"},
}

var oseqFlowcellMirrorIndexSet = syncMirrorIndexSet{
	Table:     "oseq_flowcell_mirror",
	SyncTable: syncTableOseqFlowcell,
	Indexes:   oseqFlowcellMirrorSecondaryIndexes,
}

var useqRunMetricsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "useq_run_metrics_mirror_run_name_idx", Column: "run_name"},
	{Name: "useq_run_metrics_mirror_normalised_date_idx", Column: "normalised_date"},
}

var useqRunMetricsMirrorReadIndexes = []syncIndexSpec{
	{Name: "useq_run_metrics_mirror_normalised_date_idx", Column: "normalised_date"},
}

var useqRunMetricsMirrorIndexSet = syncMirrorIndexSet{
	Table:     "useq_run_metrics_mirror",
	SyncTable: syncTableUseqRunMetrics,
	Indexes:   useqRunMetricsMirrorSecondaryIndexes,
}

var eseqRunLaneMetricsMirrorSecondaryIndexes = []syncIndexSpec{
	{Name: "eseq_run_lane_metrics_mirror_id_run_idx", Column: "id_run"},
	{Name: "eseq_run_lane_metrics_mirror_normalised_date_idx", Column: "normalised_date"},
}

var eseqRunLaneMetricsMirrorReadIndexes = []syncIndexSpec{
	{Name: "eseq_run_lane_metrics_mirror_normalised_date_idx", Column: "normalised_date"},
}

var eseqRunLaneMetricsMirrorIndexSet = syncMirrorIndexSet{
	Table:     "eseq_run_lane_metrics_mirror",
	SyncTable: syncTableEseqRunLaneMetrics,
	Indexes:   eseqRunLaneMetricsMirrorSecondaryIndexes,
}

var syncMirrorIndexSets = []syncMirrorIndexSet{
	sampleMirrorIndexSet,
	iseqFlowcellMirrorIndexSet,
	iseqProductMetricsMirrorIndexSet,
	seqProductIRODSLocationsMirrorIndexSet,
	iseqRunStatusMirrorIndexSet,
	pacBioRunWellMetricsMirrorIndexSet,
	oseqFlowcellMirrorIndexSet,
	useqRunMetricsMirrorIndexSet,
	eseqRunLaneMetricsMirrorIndexSet,
}

type sampleSyncMode int

const (
	sampleSyncModeIncremental sampleSyncMode = iota
	sampleSyncModeColdID
)

// sampleSearchTokenRow is one (token, id_sample_tmp) entry of the prefix index.
type sampleSearchTokenRow struct {
	Token       string
	IDSampleTmp int64
}

// insertSampleSearchTokenPage tokenises one page of sample_mirror rows and
// bulk-inserts the resulting (token, id_sample_tmp) rows in chunks bounded by the
// statement parameter limit.
func insertSampleSearchTokenPage(ctx context.Context, tx *sql.Tx, dialect string, page []sampleSearchTokenSource, chunkRowLimit int) error {
	buffer := make([]sampleSearchTokenRow, 0, chunkRowLimit)
	flush := func() error {
		if len(buffer) == 0 {
			return nil
		}

		if err := insertSampleSearchTokenRows(ctx, tx, dialect, buffer); err != nil {
			return err
		}

		buffer = buffer[:0]

		return nil
	}

	for _, source := range page {
		for _, token := range sampleSearchTokens(source.Name, source.SupplierName, source.CommonName, source.DonorID) {
			buffer = append(buffer, sampleSearchTokenRow{Token: token, IDSampleTmp: source.ID})
			if len(buffer) == chunkRowLimit {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}

	return flush()
}

// sampleSearchTokenSource holds one sample_mirror row's id and searchable fields,
// read into memory one page at a time so the result set is closed before that
// page's tokens are inserted.
type sampleSearchTokenSource struct {
	ID           int64
	Name         string
	SupplierName string
	CommonName   string
	DonorID      string
}

// readSampleSearchTokenPage reads one id-range page of sample_mirror rows after
// lastID into a slice and closes the result set before returning, so no result
// set is held open while the caller inserts the page's tokens. It returns the
// page's rows and the maximum id_sample_tmp seen (the next page cursor).
func readSampleSearchTokenPage(ctx context.Context, tx *sql.Tx, pageQuery string, lastID int64) ([]sampleSearchTokenSource, int64, error) {
	rows, err := tx.QueryContext(ctx, pageQuery, lastID)
	if err != nil {
		return nil, 0, fmt.Errorf("mlwh: read sample_mirror for token rebuild: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := make([]sampleSearchTokenSource, 0, sampleSearchTokenReadPageSize)
	maxID := lastID
	for rows.Next() {
		var source sampleSearchTokenSource
		if err = rows.Scan(&source.ID, &source.Name, &source.SupplierName, &source.CommonName, &source.DonorID); err != nil {
			return nil, 0, fmt.Errorf("mlwh: scan sample_mirror for token rebuild: %w", err)
		}

		page = append(page, source)
		if source.ID > maxID {
			maxID = source.ID
		}
	}
	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("mlwh: read sample_mirror for token rebuild: %w", err)
	}

	return page, maxID, nil
}

type commonNameWordMirrorRow struct {
	Word       string
	CommonName string
}

// rebuildCommonNameWordMirror repopulates common_name_word_mirror from the
// distinct common_name vocabulary in sample_mirror. It reuses sampleSearchTokens
// so organism word membership and sample word-prefix search share the exact same
// lowercased [a-z0-9] token boundaries.
func rebuildCommonNameWordMirror(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM `+commonNameWordMirrorTable); err != nil {
		return fmt.Errorf("mlwh: clear common name word mirror before rebuild: %w", err)
	}

	commonNames, err := readDistinctCommonNames(ctx, tx)
	if err != nil {
		return err
	}

	rows := make([]commonNameWordMirrorRow, 0, len(commonNames)*2)
	for _, commonName := range commonNames {
		for _, word := range sampleSearchTokens(commonName) {
			rows = append(rows, commonNameWordMirrorRow{Word: word, CommonName: commonName})
		}
	}

	return insertCommonNameWordMirrorRows(ctx, tx, rows)
}

func readDistinctCommonNames(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT common_name FROM sample_mirror ORDER BY common_name`)
	if err != nil {
		return nil, fmt.Errorf("mlwh: read distinct common names for word mirror: %w", err)
	}
	defer func() { _ = rows.Close() }()

	commonNames := []string{}
	for rows.Next() {
		var commonName string
		if err = rows.Scan(&commonName); err != nil {
			return nil, fmt.Errorf("mlwh: scan common name for word mirror: %w", err)
		}

		commonNames = append(commonNames, commonName)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read distinct common names for word mirror: %w", err)
	}

	return commonNames, nil
}

func insertCommonNameWordMirrorRows(ctx context.Context, tx *sql.Tx, rows []commonNameWordMirrorRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(commonNameWordMirrorColumns)), func(chunk []commonNameWordMirrorRow) error {
		stmt := buildBulkInsertStatement(commonNameWordMirrorTable, commonNameWordMirrorColumns, len(chunk))
		args := make([]any, 0, len(chunk)*len(commonNameWordMirrorColumns))
		for _, row := range chunk {
			args = append(args, row.Word, row.CommonName)
		}

		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: insert common name word mirror batch: %w", err)
		}

		return nil
	})
}

func iseqProductMetricsChangedSourceSelect(whereClause string) string {
	return `SELECT ipm.id_iseq_product, ipm.id_iseq_pr_metrics_tmp, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_changed, COALESCE(ipm.iseq_composition_tmp, '{"components":[]}') FROM iseq_product_metrics ipm LEFT JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp LEFT JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (` + whereClause + `) ORDER BY ipm.last_changed, ipm.id_iseq_pr_metrics_tmp`
}

func iseqProductMetricsSourceQuery(directHint, directWhere, compositeWhere, orderBy string) string {
	return `SELECT ` + iseqProductMetricsSelectColumns + ` FROM (` +
		iseqProductMetricsDirectSourceSelect(directHint, directWhere) +
		` UNION ALL ` +
		iseqProductMetricsCompositeSourceSelect(compositeWhere) +
		`) ipm ORDER BY ` + orderBy
}

func iseqProductMetricsDirectSourceSelect(hint, whereClause string) string {
	return iseqProductMetricsDirectSourceSelectWithWhere(hint, whereClause, iseqProductMetricsDirectSingleComponentOnly)
}

func iseqProductMetricsDirectSourceSelectWithWhere(hint, whereClause, extraWhere string) string {
	where := `(` + whereClause + `)`
	if extraWhere != "" {
		where += ` AND ` + extraWhere
	}

	return `SELECT ` + hint + `ipm.id_iseq_product, ipm.id_iseq_pr_metrics_tmp, ipm.id_iseq_flowcell_tmp, ipm.id_run, ipm.position, ipm.tag_index, ifc.id_sample_tmp, study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq, ipm.last_changed FROM iseq_product_metrics ipm INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE ` + where
}

func iseqProductMetricsCompositeSourceSelect(whereClause string) string {
	return `SELECT path_ipm.id_iseq_product, path_ipm.id_iseq_pr_metrics_tmp, COALESCE(path_ipm.id_iseq_flowcell_tmp, MIN(ipm.id_iseq_flowcell_tmp)) AS id_iseq_flowcell_tmp, CASE WHEN MIN(component.component_run) = MAX(component.component_run) THEN MIN(component.component_run) ELSE 0 END AS id_run, 0 AS position, 0 AS tag_index, MIN(ifc.id_sample_tmp) AS id_sample_tmp, MIN(study.id_study_lims) AS id_study_lims, path_ipm.qc, path_ipm.qc_lib, path_ipm.qc_seq, path_ipm.last_changed FROM iseq_product_metrics path_ipm INNER JOIN JSON_TABLE(path_ipm.iseq_composition_tmp, '$.components[*]' COLUMNS(component_run INT PATH '$.id_run', component_position INT PATH '$.position', component_tag_index INT PATH '$.tag_index')) component ON TRUE INNER JOIN iseq_product_metrics ipm ON ipm.id_run = component.component_run AND ipm.position = component.component_position AND ipm.tag_index = component.component_tag_index INNER JOIN iseq_flowcell ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp INNER JOIN study ON study.id_study_tmp = ifc.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (` + whereClause + `) AND EXISTS (SELECT 1 FROM seq_product_irods_locations spi WHERE spi.id_product = path_ipm.id_iseq_product AND LOWER(spi.seq_platform_name) = 'illumina') GROUP BY path_ipm.id_iseq_product, path_ipm.id_iseq_pr_metrics_tmp, path_ipm.id_iseq_flowcell_tmp, path_ipm.qc, path_ipm.qc_lib, path_ipm.qc_seq, path_ipm.last_changed HAVING COUNT(*) > 1`
}

func iseqProductMetricsCompositeRecoverySourceQuery(candidateCount int) string {
	return iseqProductMetricsCompositeSourceSelect(`path_ipm.id_iseq_product IN (`+sqlPlaceholders(candidateCount)+`)`) +
		` ORDER BY path_ipm.last_changed, path_ipm.id_iseq_pr_metrics_tmp`
}

func iseqProductMetricsLegacySyncSourceQuery() string {
	return iseqProductMetricsLegacyDirectSourceSelect("", `ipm.last_changed >= ?`) + ` ORDER BY ipm.last_changed, ipm.id_iseq_pr_metrics_tmp`
}

func iseqProductMetricsLegacyDirectSourceSelect(hint, whereClause string) string {
	return iseqProductMetricsDirectSourceSelectWithWhere(hint, whereClause, "")
}

func iseqProductMetricsLegacyColdSyncSourceQuery() string {
	return iseqProductMetricsLegacyDirectSourceSelect("/*+ JOIN_FIXED_ORDER() */ ", `ipm.id_iseq_pr_metrics_tmp < ?`) + ` ORDER BY ipm.id_iseq_pr_metrics_tmp DESC`
}

func iseqProductMetricsLegacySyncSourceQueryFromCursor() string {
	return iseqProductMetricsLegacyDirectSourceSelect("", `(ipm.last_changed > ?) OR (ipm.last_changed = ? AND ipm.id_iseq_pr_metrics_tmp > ?)`) + ` ORDER BY ipm.last_changed, ipm.id_iseq_pr_metrics_tmp`
}

func seqProductIRODSLocationsChangedFirstSourceQuery(wherePredicate string) string {
	return `WITH changed_spi AS (SELECT spi.* FROM seq_product_irods_locations spi WHERE ` + wherePredicate +
		`) SELECT ` + seqProductIRODSLocationsSelectColumns +
		` FROM changed_spi spi INNER JOIN (` + seqProductIRODSLocationsChangedRecovery +
		`) recovery ON recovery.id_product = spi.id_product ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`
}

// seqProductIRODSLocationsSourceQuery assembles the full iRODS source SELECT for
// the given Illumina recovery branch (composition or legacy) and WHERE/ORDER
// suffix. The outer FROM stays seq_product_irods_locations spi and the recovery
// UNION spans every platform so non-Illumina data is no longer dropped.
func seqProductIRODSLocationsSourceQuery(illuminaRecovery, whereOrderSuffix string) string {
	return `SELECT ` + seqProductIRODSLocationsSelectColumns +
		` FROM seq_product_irods_locations spi INNER JOIN (` +
		illuminaRecovery + ` UNION ALL ` + seqProductIRODSLocationsNonIlluminaRecovery +
		`) recovery ON recovery.id_product = spi.id_product ` + whereOrderSuffix
}

func iseqProductMetricsLegacySyncQuery(state syncStateRecord) (string, []any, bool, error) {
	if shouldUseAscendingIDColdSync(state, iseqProductMetricsIDResumeMode) {
		id, err := descendingIDColdResumeID(state, iseqProductMetricsIDResumeMode)
		if err != nil {
			return "", nil, true, err
		}

		return iseqProductMetricsLegacyColdSyncSourceQuery(), []any{id}, true, nil
	}

	if state.ResumeCursor == nil {
		return iseqProductMetricsLegacySyncSourceQuery(), []any{formatSyncTime(state.HighWater)}, false, nil
	}

	lastUpdated, idIseqProduct, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, false, fmt.Errorf("mlwh: parse iseq_product_metrics resume cursor: %w", err)
	}

	return iseqProductMetricsLegacySyncSourceQueryFromCursor(), []any{formatSyncTime(lastUpdated), formatSyncTime(lastUpdated), idIseqProduct}, false, nil
}

// insertSampleSearchTokensFromMirror reads every sample's id and searchable
// fields from sample_mirror, tokenises them, and bulk-inserts the resulting
// (token, id_sample_tmp) rows. It reads sample_mirror in id-range pages: each
// page is fully scanned into a bounded slice and its result set closed BEFORE the
// page's tokens are inserted, so the transaction's single connection is never
// reading a result set while writing. This is required on MySQL, where executing
// an INSERT while a streaming SELECT from the same connection is still open fails
// with "driver: bad connection" at scale. Memory stays bounded to one page.
func insertSampleSearchTokensFromMirror(ctx context.Context, tx *sql.Tx, dialect string) error {
	chunkRowLimit := syncStatementRowLimit(len(sampleSearchTokenColumns))
	pageQuery := sampleSearchTokenPageQuery + strconv.Itoa(sampleSearchTokenReadPageSize)
	lastID := int64(0)

	for {
		page, maxID, err := readSampleSearchTokenPage(ctx, tx, pageQuery, lastID)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}

		if err = insertSampleSearchTokenPage(ctx, tx, dialect, page, chunkRowLimit); err != nil {
			return err
		}

		lastID = maxID
	}
}

// sampleSearchTokens returns the distinct lowercased word tokens of the given
// searchable field values. A token is a maximal run of ASCII [a-z0-9] (other
// runes, including non-ASCII letters, split tokens); each rune is lowercased so
// the stored tokens are case-insensitively prefix-searchable. Order is
// stable-first-seen and duplicates across fields are collapsed, so a sample with
// repeated words (e.g. "Homo sapiens" and common_name "homo") stores each token
// once.
func sampleSearchTokens(values ...string) []string {
	seen := make(map[string]struct{})
	tokens := make([]string, 0, len(values)*2)

	var builder strings.Builder
	flush := func() {
		if builder.Len() == 0 {
			return
		}

		token := builder.String()
		builder.Reset()
		if _, ok := seen[token]; ok {
			return
		}

		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}

	for _, value := range values {
		for _, r := range value {
			switch {
			case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
				builder.WriteRune(r)
			case r >= 'A' && r <= 'Z':
				builder.WriteRune(r - 'A' + 'a')
			default:
				flush()
			}
		}

		flush()
	}

	return tokens
}

// replaceSampleSearchTokensForBatch keeps sample_search_token consistent with an
// incremental sample upsert: it deletes the existing token rows for the batch's
// ids (so an updated sample's stale tokens are removed) and inserts the current
// distinct tokens for each sample, making incrementally-synced samples
// searchable. It runs only on the incremental path (the cold-load path rebuilds
// the whole token table at finalize instead).
func replaceSampleSearchTokensForBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []sampleSyncRow) error {
	if len(rows) == 0 {
		return nil
	}

	if err := deleteSampleSearchTokensForKeys(ctx, tx, sampleBatchKeys(rows)); err != nil {
		return err
	}

	tokenRows := make([]sampleSearchTokenRow, 0, len(rows)*4)
	for _, row := range rows {
		for _, token := range sampleSearchTokens(row.Sample.Name, row.Sample.SupplierName, row.Sample.CommonName, row.Sample.DonorID) {
			tokenRows = append(tokenRows, sampleSearchTokenRow{Token: token, IDSampleTmp: row.Sample.IDSampleTmp})
		}
	}

	return forEachRowChunk(tokenRows, syncStatementRowLimit(len(sampleSearchTokenColumns)), func(chunk []sampleSearchTokenRow) error {
		return insertSampleSearchTokenRows(ctx, tx, dialect, chunk)
	})
}

// deleteSampleSearchTokensForKeys removes all token rows owned by the given
// id_sample_tmp keys, in bounded chunks.
func deleteSampleSearchTokensForKeys(ctx context.Context, tx *sql.Tx, keys [][]any) error {
	keyChunkLimit := syncStatementRowLimit(1)
	for start := 0; start < len(keys); start += keyChunkLimit {
		end := min(start+keyChunkLimit, len(keys))
		whereClause, whereArgs := buildKeyInClause([]string{"id_sample_tmp"}, keys[start:end])
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s", sampleSearchTokenTable, whereClause), whereArgs...); err != nil {
			return fmt.Errorf("mlwh: clear sample search token batch: %w", err)
		}
	}

	return nil
}

// insertSampleSearchTokenRows bulk-inserts a chunk of token rows.
func insertSampleSearchTokenRows(ctx context.Context, tx *sql.Tx, dialect string, rows []sampleSearchTokenRow) error {
	if len(rows) == 0 {
		return nil
	}

	stmt := buildBulkInsertStatement(sampleSearchTokenTable, sampleSearchTokenColumns, len(rows))
	args := make([]any, 0, len(rows)*len(sampleSearchTokenColumns))
	for _, row := range rows {
		args = append(args, row.Token, row.IDSampleTmp)
	}

	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("mlwh: insert sample search token batch: %w", err)
	}

	return nil
}

// rebuildSampleSearchTokenIndex repopulates sample_search_token from
// sample_mirror, mirroring the donor_samples cold-load rebuild discipline: it
// drops the covering index, clears the table, streams every SQSCP sample's
// searchable fields and bulk-inserts their distinct word tokens, then recreates
// the index. Runs in both dialects (the prefix index has no engine-maintained
// counterpart).
func rebuildSampleSearchTokenIndex(ctx context.Context, tx *sql.Tx, dialect string) error {
	if err := dropSampleSearchTokenIndex(ctx, tx, dialect); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM `+sampleSearchTokenTable); err != nil {
		return fmt.Errorf("mlwh: clear sample search tokens before rebuild: %w", err)
	}

	if err := insertSampleSearchTokensFromMirror(ctx, tx, dialect); err != nil {
		return err
	}

	return createSampleSearchTokenIndex(ctx, tx, dialect)
}

// dropSampleSearchTokenIndex drops the sample_search_token covering index so the
// cold-load bulk token build inserts without per-row index maintenance,
// mirroring the secondary-index drop-before-bulk-insert discipline. It is
// recreated by createSampleSearchTokenIndex after the build.
func dropSampleSearchTokenIndex(ctx context.Context, tx *sql.Tx, dialect string) error {
	stmt := `DROP INDEX IF EXISTS ` + sampleSearchTokenIndex.Name
	if dialect == "mysql" {
		stmt = `DROP INDEX ` + sampleSearchTokenIndex.Name + ` ON ` + sampleSearchTokenTable
	}

	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("mlwh: drop sample search token index: %w", err)
	}

	return nil
}

// createSampleSearchTokenIndex recreates the sample_search_token covering index
// after the cold-load bulk token build.
func createSampleSearchTokenIndex(ctx context.Context, tx *sql.Tx, dialect string) error {
	stmt := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(%s)`, sampleSearchTokenIndex.Name, sampleSearchTokenTable, sampleSearchTokenIndex.Column)
	if dialect == "mysql" {
		stmt = fmt.Sprintf(`CREATE INDEX %s ON %s(%s)`, sampleSearchTokenIndex.Name, sampleSearchTokenTable, sampleSearchTokenIndex.Column)
	}

	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("mlwh: create sample search token index: %w", err)
	}

	return nil
}

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
	return `SELECT iseq_flowcell.id_iseq_flowcell_tmp, iseq_flowcell.entity_type, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, iseq_flowcell.id_study_tmp, study.id_study_lims, iseq_flowcell.legacy_library_id, iseq_flowcell.id_library_lims, iseq_flowcell.last_updated FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp AND study.id_lims = 'SQSCP' WHERE iseq_flowcell.last_updated >= ? ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims`
}

func flowcellSyncSourceQueryFromCursor() string {
	return `SELECT iseq_flowcell.id_iseq_flowcell_tmp, iseq_flowcell.entity_type, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, iseq_flowcell.id_study_tmp, study.id_study_lims, iseq_flowcell.legacy_library_id, iseq_flowcell.id_library_lims, iseq_flowcell.last_updated FROM iseq_flowcell INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp AND study.id_lims = 'SQSCP' WHERE (iseq_flowcell.last_updated > ?) OR (iseq_flowcell.last_updated = ? AND (iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims) > (?, ?, ?)) ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp, study.id_study_lims`
}

func iseqProductMetricsSyncSourceQuery() string {
	return iseqProductMetricsChangedSourceSelect(`ipm.last_changed >= ?`)
}

func iseqProductMetricsColdSyncSourceQuery() string {
	return iseqProductMetricsSourceQuery("/*+ JOIN_FIXED_ORDER() */ ", `ipm.id_iseq_pr_metrics_tmp < ?`, `path_ipm.id_iseq_pr_metrics_tmp < ?`, `ipm.id_iseq_pr_metrics_tmp DESC`)
}

func iseqProductMetricsSyncSourceQueryFromCursor() string {
	return iseqProductMetricsChangedSourceSelect(`(ipm.last_changed > ?) OR (ipm.last_changed = ? AND ipm.id_iseq_pr_metrics_tmp > ?)`)
}

func seqProductIRODSLocationsSyncSourceQuery() string {
	return seqProductIRODSLocationsChangedFirstSourceQuery(`spi.last_changed >= ?`)
}

func seqProductIRODSLocationsColdSyncSourceQuery() string {
	return seqProductIRODSLocationsSourceQuery(seqProductIRODSLocationsIlluminaCompositionRecovery,
		`WHERE spi.id_seq_product_irods_locations_tmp > ? ORDER BY spi.id_seq_product_irods_locations_tmp`)
}

func seqProductIRODSLocationsSyncSourceQueryFromCursor() string {
	return seqProductIRODSLocationsChangedFirstSourceQuery(`(spi.last_changed > ?) OR (spi.last_changed = ? AND spi.id_seq_product_irods_locations_tmp > ?)`)
}

func seqProductIRODSLocationsLegacySyncSourceQuery() string {
	return seqProductIRODSLocationsSourceQuery(seqProductIRODSLocationsIlluminaLegacyRecovery,
		`WHERE spi.last_changed >= ? ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`)
}

func seqProductIRODSLocationsLegacyColdSyncSourceQuery() string {
	return seqProductIRODSLocationsSourceQuery(seqProductIRODSLocationsIlluminaLegacyRecovery,
		`WHERE spi.id_seq_product_irods_locations_tmp > ? ORDER BY spi.id_seq_product_irods_locations_tmp`)
}

func seqProductIRODSLocationsLegacySyncSourceQueryFromCursor() string {
	return seqProductIRODSLocationsSourceQuery(seqProductIRODSLocationsIlluminaLegacyRecovery,
		`WHERE (spi.last_changed > ?) OR (spi.last_changed = ? AND spi.id_seq_product_irods_locations_tmp > ?) ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`)
}

// SyncSourceQuery names one source SELECT the sync issues against the upstream
// MLWH so it can be validated independently (e.g. prepared / run with LIMIT 0
// against the real source). ArgCount is the number of bound parameters the query
// expects, so a validator can supply that many placeholders.
type SyncSourceQuery struct {
	Name     string
	Query    string
	ArgCount int
}

// AllSyncSourceQueries returns every distinct SELECT the sync issues against the
// upstream MLWH source, across all supported tables and all watermark variants
// (cold / incremental / from-cursor / legacy). It is the single source of truth
// the source-schema integration test runs against the real MLWH so any missing
// column, missing table or wrong schema fails the test generically. New sync
// source queries MUST be added here so they stay covered.
func AllSyncSourceQueries() []SyncSourceQuery {
	queries := []SyncSourceQuery{
		{Name: "sample cold", Query: sampleColdSyncSourceQuery(), ArgCount: 1},
		{Name: "sample incremental", Query: sampleSyncSourceQuery(), ArgCount: 1},
		{Name: "sample from cursor", Query: sampleSyncSourceQueryFromCursor(), ArgCount: 3},
		{Name: "iseq_flowcell incremental", Query: flowcellSyncSourceQuery(), ArgCount: 1},
		{Name: "iseq_flowcell from cursor", Query: flowcellSyncSourceQueryFromCursor(), ArgCount: 5},
		{Name: "iseq_product_metrics incremental", Query: iseqProductMetricsSyncSourceQuery(), ArgCount: 1},
		{Name: "iseq_product_metrics cold", Query: iseqProductMetricsColdSyncSourceQuery(), ArgCount: 2},
		{Name: "iseq_product_metrics from cursor", Query: iseqProductMetricsSyncSourceQueryFromCursor(), ArgCount: 3},
		{Name: "iseq_product_metrics composite recovery", Query: iseqProductMetricsCompositeRecoverySourceQuery(1), ArgCount: 1},
		{Name: "iseq_product_metrics legacy incremental", Query: iseqProductMetricsLegacySyncSourceQuery(), ArgCount: 1},
		{Name: "iseq_product_metrics legacy cold", Query: iseqProductMetricsLegacyColdSyncSourceQuery(), ArgCount: 1},
		{Name: "iseq_product_metrics legacy from cursor", Query: iseqProductMetricsLegacySyncSourceQueryFromCursor(), ArgCount: 3},
		{Name: "seq_product_irods_locations incremental", Query: seqProductIRODSLocationsSyncSourceQuery(), ArgCount: 1},
		{Name: "seq_product_irods_locations cold", Query: seqProductIRODSLocationsColdSyncSourceQuery(), ArgCount: 1},
		{Name: "seq_product_irods_locations from cursor", Query: seqProductIRODSLocationsSyncSourceQueryFromCursor(), ArgCount: 3},
		{Name: "seq_product_irods_locations legacy incremental", Query: seqProductIRODSLocationsLegacySyncSourceQuery(), ArgCount: 1},
		{Name: "seq_product_irods_locations legacy cold", Query: seqProductIRODSLocationsLegacyColdSyncSourceQuery(), ArgCount: 1},
		{Name: "seq_product_irods_locations legacy from cursor", Query: seqProductIRODSLocationsLegacySyncSourceQueryFromCursor(), ArgCount: 3},
		{Name: "iseq_run_status page", Query: iseqRunStatusPageQuery(), ArgCount: 1},
		{Name: "seq_ops_tracking_per_sample", Query: seqOpsTrackingPerSampleSourceQuery, ArgCount: 0},
		{Name: "study probe", Query: `SELECT * FROM study WHERE id_lims = 'SQSCP'`, ArgCount: 0},
	}

	for _, spec := range allProductMetricsMirrorSpecs() {
		query, args := spec.sourceQuery(syncStateRecord{})
		queries = append(queries, SyncSourceQuery{Name: spec.syncTable, Query: query, ArgCount: len(args)})
	}

	for _, table := range wholesaleMirrorTables() {
		spec := wholesaleMirrorSpecFor(table)
		queries = append(queries, SyncSourceQuery{Name: spec.syncTable, Query: spec.sourceQuery, ArgCount: 0})
	}

	return queries
}

type syncStateRecord struct {
	HighWater      time.Time
	ResumeCursor   *string
	IndexesDropped bool
	Exists         bool
}

func dedupeIseqFlowcellMirrorBatch(rows []flowcellSyncRow) []flowcellSyncRow {
	indices := make(map[int64]int, len(rows))
	deduped := make([]flowcellSyncRow, 0, len(rows))
	for _, row := range rows {
		if row.IDIseqFlowcellTmp == 0 {
			continue
		}

		index, ok := indices[row.IDIseqFlowcellTmp]
		if ok {
			deduped[index] = row
			continue
		}

		indices[row.IDIseqFlowcellTmp] = len(deduped)
		deduped = append(deduped, row)
	}

	return deduped
}

func upsertIseqFlowcellMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []flowcellSyncRow) error {
	return forEachRowChunk(rows, syncStatementRowLimit(len(iseqFlowcellMirrorColumns)), func(chunk []flowcellSyncRow) error {
		stmt := buildBulkUpsertStatement(dialect, "iseq_flowcell_mirror", iseqFlowcellMirrorColumns, []string{"id_iseq_flowcell_tmp"}, len(chunk))
		args := make([]any, 0, len(chunk)*len(iseqFlowcellMirrorColumns))
		for _, row := range chunk {
			args = append(args, row.IDIseqFlowcellTmp, row.EntityType, row.PipelineIDLims, row.IDSampleTmp, row.IDStudyTmp)
		}
		if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
			return fmt.Errorf("mlwh: upsert iseq_flowcell_mirror batch: %w", err)
		}

		return nil
	})
}

func syncIseqProductMetricsWarmTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord, query string, args []any) (SyncReport, bool, error) {
	changedRows, err := queryIseqProductMetricsChangedRows(ctx, source, query, args)
	if err != nil {
		if isUnsupportedCompositionQueryError(err) {
			return syncIseqProductMetricsLegacyTable(ctx, cache, source, state)
		}

		return SyncReport{}, false, fmt.Errorf("mlwh: query iseq_product_metrics changed rows: %w", err)
	}

	report := SyncReport{Table: syncTableIseqProductMetrics, HighWater: state.HighWater}
	outputRows, compositeIDs, err := classifyIseqProductMetricsChangedRows(changedRows, &report)
	if err != nil {
		return report, false, err
	}

	compositeRows, err := queryIseqProductMetricsCompositeRows(ctx, source, compositeIDs)
	if err != nil {
		if isUnsupportedCompositionQueryError(err) {
			return syncIseqProductMetricsLegacyTable(ctx, cache, source, state)
		}

		return report, false, err
	}
	outputRows = append(outputRows, compositeRows...)
	slices.SortFunc(outputRows, compareIseqProductMetricsSyncRows)

	if len(outputRows) > 0 {
		resumeCursor := encodeIseqProductMetricsResumeCursor(outputRows[len(outputRows)-1])
		result, writeErr := writeIseqProductMetricsBatch(ctx, cache, outputRows, report.HighWater, &resumeCursor, state.IndexesDropped, false)
		if writeErr != nil {
			return report, false, writeErr
		}
		report.Inserted = result.Inserted
		report.Updated = result.Updated
	}

	sawRows := len(changedRows) > 0
	if sawRows || state.Exists {
		if err = finalizeMirrorSyncState(ctx, cache, iseqProductMetricsMirrorIndexSet, report.HighWater, state.IndexesDropped); err != nil {
			return report, false, err
		}
	}

	return report, sawRows, nil
}

func queryIseqProductMetricsChangedRows(ctx context.Context, source Querier, query string, args []any) ([]iseqProductMetricsChangedRow, error) {
	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("mlwh: inspect iseq_product_metrics changed-row columns: %w", err)
	}

	changedRows := []iseqProductMetricsChangedRow{}
	for rows.Next() {
		row, scanErr := scanIseqProductMetricsChangedRow(rows, len(columns))
		if scanErr != nil {
			return nil, scanErr
		}
		changedRows = append(changedRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("mlwh: read iseq_product_metrics changed rows: %w", err)
	}

	return changedRows, nil
}

func scanIseqProductMetricsChangedRow(rows *sql.Rows, columnCount int) (iseqProductMetricsChangedRow, error) {
	var row iseqProductMetricsChangedRow
	var lastUpdated any
	destinations := []any{
		&row.IDIseqProduct, &row.SourceRowID, &row.IDIseqFlowcellTmp, &row.IDRun,
		&row.Position, &row.TagIndex, &row.IDSampleTmp, &row.IDStudyLims,
		&row.QC, &row.QCLib, &row.QCSeq, &lastUpdated,
	}
	if columnCount == len(destinations)+1 {
		destinations = append(destinations, &row.IseqCompositionTmp)
	} else {
		row.IseqCompositionTmp = `{"components":[]}`
	}
	if err := rows.Scan(destinations...); err != nil {
		return iseqProductMetricsChangedRow{}, fmt.Errorf("mlwh: scan iseq_product_metrics changed row: %w", err)
	}

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return iseqProductMetricsChangedRow{}, fmt.Errorf("mlwh: parse iseq_product_metrics changed-row last_updated: %w", err)
	}
	row.LastUpdated = parsed

	return row, nil
}

func syncIseqProductMetricsLegacyTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, coldIDSync, err := iseqProductMetricsLegacySyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}

	return syncIseqProductMetricsStreamingQuery(ctx, cache, source, state, query, args, coldIDSync)
}

func syncIseqProductMetricsStreamingQuery(ctx context.Context, cache Cache, source Querier, state syncStateRecord, query string, args []any, coldIDSync bool) (SyncReport, bool, error) {
	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		if isUnsupportedCompositionQueryError(err) {
			legacyQuery, legacyArgs, _, legacyErr := iseqProductMetricsLegacySyncQuery(state)
			if legacyErr != nil {
				return SyncReport{}, false, legacyErr
			}

			rows, err = source.QueryContext(ctx, legacyQuery, legacyArgs...)
		}
	}
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

func classifyIseqProductMetricsChangedRows(changedRows []iseqProductMetricsChangedRow, report *SyncReport) ([]iseqProductMetricsSyncRow, []string, error) {
	directRows := make([]iseqProductMetricsSyncRow, 0, len(changedRows))
	compositeIDs := make([]string, 0, len(changedRows))
	for _, row := range changedRows {
		if row.LastUpdated.After(report.HighWater) {
			report.HighWater = row.LastUpdated
		}

		componentCount, err := iseqProductMetricsComponentCount(row.IseqCompositionTmp)
		if err != nil {
			return nil, nil, fmt.Errorf("mlwh: parse iseq_product_metrics composition for %q: %w", row.IDIseqProduct, err)
		}
		if componentCount > 1 {
			compositeIDs = append(compositeIDs, row.IDIseqProduct)

			continue
		}
		if directRow, ok := iseqProductMetricsDirectOutputRow(row); ok {
			directRows = append(directRows, directRow)
		}
	}

	return directRows, compositeIDs, nil
}

func iseqProductMetricsComponentCount(raw string) (int, error) {
	composition := struct {
		Components []json.RawMessage `json:"components"`
	}{}
	if err := json.Unmarshal([]byte(raw), &composition); err != nil {
		return 0, err
	}

	return len(composition.Components), nil
}

func iseqProductMetricsDirectOutputRow(row iseqProductMetricsChangedRow) (iseqProductMetricsSyncRow, bool) {
	if !row.IDIseqFlowcellTmp.Valid || !row.IDSampleTmp.Valid || !row.IDStudyLims.Valid {
		return iseqProductMetricsSyncRow{}, false
	}

	return iseqProductMetricsSyncRow{
		IDIseqProduct: row.IDIseqProduct, SourceRowID: row.SourceRowID,
		IDIseqFlowcellTmp: row.IDIseqFlowcellTmp.Int64, IDRun: nullIntValue(row.IDRun),
		Position: nullIntValue(row.Position), TagIndex: nullIntValue(row.TagIndex),
		IDSampleTmp: row.IDSampleTmp.Int64, IDStudyLims: row.IDStudyLims.String,
		QC: row.QC, QCLib: row.QCLib, QCSeq: row.QCSeq, LastUpdated: row.LastUpdated,
	}, true
}

func queryIseqProductMetricsCompositeRows(ctx context.Context, source Querier, candidateIDs []string) ([]iseqProductMetricsSyncRow, error) {
	compositeRows := []iseqProductMetricsSyncRow{}
	err := forEachRowChunk(candidateIDs, iseqProductMetricsCompositeRecoveryChunkSize, func(chunk []string) error {
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}

		rows, err := source.QueryContext(ctx, iseqProductMetricsCompositeRecoverySourceQuery(len(chunk)), args...)
		if err != nil {
			return fmt.Errorf("mlwh: query iseq_product_metrics composite recovery: %w", err)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			row, scanErr := scanIseqProductMetricsSyncRow(rows)
			if scanErr != nil {
				return scanErr
			}
			compositeRows = append(compositeRows, row)
		}
		if err = rows.Err(); err != nil {
			return fmt.Errorf("mlwh: read iseq_product_metrics composite recovery: %w", err)
		}

		return nil
	})

	return compositeRows, err
}

func enrichSeqProductIRODSLocationsExportFields(ctx context.Context, db *sql.DB, rows []seqProductIRODSLocationsSyncRow) error {
	if len(rows) == 0 {
		return nil
	}

	fieldsByProduct, err := loadSeqProductIRODSLocationsExportFields(ctx, db, rows)
	if err != nil {
		return err
	}

	for i := range rows {
		fields, ok := fieldsByProduct[seqProductIRODSLocationsExportFieldsKey(rows[i])]
		if !ok {
			continue
		}

		rows[i].IDRun = fields.IDRun
		rows[i].Position = fields.Position
		rows[i].TagIndex = fields.TagIndex
		rows[i].QC = fields.QC
		rows[i].IsDeliverable = fields.IsDeliverable
		rows[i].Merged = fields.Merged
	}

	return nil
}

func loadSeqProductIRODSLocationsExportFields(ctx context.Context, db *sql.DB, rows []seqProductIRODSLocationsSyncRow) (map[string]seqProductIRODSLocationsExportFields, error) {
	idsByPlatform := seqProductIRODSLocationsProductIDsByPlatform(rows)
	fieldsByProduct := make(map[string]seqProductIRODSLocationsExportFields, len(rows))

	queries := map[string]string{
		"illumina":  `SELECT ipm.id_iseq_product, ipm.id_run, ipm.position, ipm.tag_index, ipm.qc, CASE WHEN ifc.entity_type IN ('library', 'library_indexed') THEN 1 ELSE 0 END, CASE WHEN ipm.position = 0 AND ipm.tag_index = 0 THEN 1 ELSE 0 END FROM iseq_product_metrics_mirror ipm LEFT JOIN iseq_flowcell_mirror ifc ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp WHERE ipm.id_iseq_product IN (%s)`,
		"elembio":   `SELECT id_eseq_product, id_run, 0, 0, qc, CASE WHEN is_sequencing_control = 0 THEN 1 ELSE 0 END, 0 FROM eseq_product_metrics_mirror WHERE id_eseq_product IN (%s)`,
		"ultimagen": `SELECT id_useq_product, id_run, 0, 0, qc, CASE WHEN is_sequencing_control = 0 THEN 1 ELSE 0 END, 0 FROM useq_product_metrics_mirror WHERE id_useq_product IN (%s)`,
		"pacbio":    `SELECT id_pac_bio_product, 0, 0, 0, qc, NULL, 0 FROM pac_bio_product_metrics_mirror WHERE id_pac_bio_product IN (%s)`,
	}

	for platform, ids := range idsByPlatform {
		query, ok := queries[platform]
		if !ok {
			continue
		}
		if err := loadSeqProductIRODSLocationsExportFieldsForPlatform(ctx, db, fmt.Sprintf(query, sqlPlaceholders(len(ids))), platform, ids, fieldsByProduct); err != nil {
			return nil, err
		}
	}

	return fieldsByProduct, nil
}

func seqProductIRODSLocationsProductIDsByPlatform(rows []seqProductIRODSLocationsSyncRow) map[string][]string {
	seenByPlatform := map[string]map[string]struct{}{}
	for _, row := range rows {
		platform := seqProductIRODSLocationsExportPlatform(row.Platform)
		if platform == "" {
			continue
		}
		if _, ok := seenByPlatform[platform]; !ok {
			seenByPlatform[platform] = map[string]struct{}{}
		}
		seenByPlatform[platform][row.IDIseqProduct] = struct{}{}
	}

	idsByPlatform := make(map[string][]string, len(seenByPlatform))
	for platform, seen := range seenByPlatform {
		ids := make([]string, 0, len(seen))
		for id := range seen {
			ids = append(ids, id)
		}
		idsByPlatform[platform] = ids
	}

	return idsByPlatform
}

func seqProductIRODSLocationsExportPlatform(platform string) string {
	normalized := strings.ToLower(strings.TrimSpace(platform))
	switch normalized {
	case "illumina":
		return "illumina"
	case "elembio", "element":
		return "elembio"
	case "ultimagen", "ultima":
		return "ultimagen"
	case "pacbio", "pac_bio", "pac bio":
		return "pacbio"
	default:
		return ""
	}
}

func loadSeqProductIRODSLocationsExportFieldsForPlatform(ctx context.Context, db *sql.DB, query, platform string, productIDs []string, fieldsByProduct map[string]seqProductIRODSLocationsExportFields) error {
	if len(productIDs) == 0 {
		return nil
	}

	args := make([]any, 0, len(productIDs))
	for _, id := range productIDs {
		args = append(args, id)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mlwh: query %s seq_product_irods_locations export fields: %w", platform, err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var productID string
		var fields seqProductIRODSLocationsExportFields
		if err = rows.Scan(&productID, &fields.IDRun, &fields.Position, &fields.TagIndex, &fields.QC, &fields.IsDeliverable, &fields.Merged); err != nil {
			return fmt.Errorf("mlwh: scan %s seq_product_irods_locations export fields: %w", platform, err)
		}
		fieldsByProduct[platform+"\x00"+productID] = fields
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("mlwh: read %s seq_product_irods_locations export fields: %w", platform, err)
	}

	return nil
}

func sqlPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}

	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func seqProductIRODSLocationsExportFieldsKey(row seqProductIRODSLocationsSyncRow) string {
	platform := seqProductIRODSLocationsExportPlatform(row.Platform)
	if platform == "" {
		return ""
	}

	return platform + "\x00" + row.IDIseqProduct
}

func seqProductIRODSLocationsBatchDedupeKey(row seqProductIRODSLocationsSyncRow) seqProductIRODSLocationsDedupeKey {
	return seqProductIRODSLocationsDedupeKey{
		sourceRowID:     row.SourceRowID,
		idIseqProduct:   row.IDIseqProduct,
		idSampleTmp:     row.IDSampleTmp,
		idStudyLims:     row.IDStudyLims,
		irodsCollection: row.IRODSCollection,
		irodsFileName:   row.IRODSFileName,
	}
}

// formatNullableSyncTime renders an optional sync timestamp as an RFC3339 string
// argument, or nil (SQL NULL) when the value is absent. It keeps a NULL upstream
// created from being stored as the zero time ("0001-01-01T00:00:00Z"), so the
// downstream MIN/MAX(created) aggregates and recency filter ignore it.
func formatNullableSyncTime(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}

	return formatSyncTime(value.Time)
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

// Sync syncs supported cache tables, staging iRODS after the product mirrors it
// reads for A4 export-field enrichment.
func (c *Client) Sync(ctx context.Context) ([]SyncReport, error) {
	return c.syncTables(ctx)
}

func splitSeqProductIRODSLocationsSyncStage(tables []string) ([]string, bool) {
	preIRODS := make([]string, 0, len(tables))
	syncIRODS := false
	for _, table := range tables {
		if table == syncTableSeqProductIRODSLocations {
			syncIRODS = true
			continue
		}

		preIRODS = append(preIRODS, table)
	}

	return preIRODS, syncIRODS
}

func syncResultsContainDependencyError(results []syncResult, dependencies map[string]struct{}) bool {
	for _, result := range results {
		if result.err == nil {
			continue
		}
		if _, ok := dependencies[result.report.Table]; ok {
			return true
		}
	}

	return false
}

func collectSyncResults(results []syncResult, reportCapacity int) ([]SyncReport, []error) {
	reports := make([]SyncReport, 0, reportCapacity)
	var errs []error
	for _, result := range results {
		if result.report.Table != "" && (result.err == nil || syncReportHasObservedState(result.report)) {
			reports = append(reports, result.report)
		}

		if result.err != nil {
			errs = append(errs, result.err)
		}
	}

	return reports, errs
}

type syncResult struct {
	report SyncReport
	err    error
}

func (c *Client) syncTablesInParallel(ctx context.Context, tables []string) []syncResult {
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

	results := make([]syncResult, 0, len(tables))
	for result := range resultCh {
		results = append(results, result)
	}

	return results
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

	var errs []error
	preIRODSTables, syncIRODS := splitSeqProductIRODSLocationsSyncStage(tables)
	preIRODSResults := c.syncTablesInParallel(ctx, preIRODSTables)
	reports, errs = collectSyncResults(preIRODSResults, len(tables))

	if syncIRODS && !syncResultsContainDependencyError(preIRODSResults, seqProductIRODSLocationsSyncDependencies) {
		if repairErr := c.repairSeqProductIRODSLocationsDependencyIndexes(ctx); repairErr != nil {
			errs = append(errs, repairErr)
		} else {
			irodsReports, irodsErrs := collectSyncResults(c.syncTablesInParallel(ctx, []string{syncTableSeqProductIRODSLocations}), 1)
			reports = append(reports, irodsReports...)
			errs = append(errs, irodsErrs...)
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

type seqProductIRODSLocationsDedupeKey struct {
	sourceRowID     int64
	idIseqProduct   string
	idSampleTmp     int64
	idStudyLims     string
	irodsCollection string
	irodsFileName   string
}

type seqProductIRODSLocationsExportFields struct {
	IDRun         int64
	Position      int64
	TagIndex      int64
	QC            sql.NullInt64
	IsDeliverable sql.NullInt64
	Merged        int64
}

type iseqProductMetricsChangedRow struct {
	IDIseqProduct      string
	SourceRowID        int64
	IDIseqFlowcellTmp  sql.NullInt64
	IDRun              sql.NullInt64
	Position           sql.NullInt64
	TagIndex           sql.NullInt64
	IDSampleTmp        sql.NullInt64
	IDStudyLims        sql.NullString
	QC                 sql.NullInt64
	QCLib              sql.NullInt64
	QCSeq              sql.NullInt64
	LastUpdated        time.Time
	IseqCompositionTmp string
}

func compareIseqProductMetricsSyncRows(left, right iseqProductMetricsSyncRow) int {
	if left.LastUpdated.Before(right.LastUpdated) {
		return -1
	}
	if left.LastUpdated.After(right.LastUpdated) {
		return 1
	}

	return int(left.SourceRowID - right.SourceRowID)
}

func normalisedDateFromNullableTime(value sql.NullString) string {
	if !value.Valid || value.String == "" {
		return ""
	}

	parsed, err := parseSyncTimeString(value.String)
	if err != nil {
		return ""
	}

	return formatSyncDate(parsed)
}

func formatSyncDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}

	return value.UTC().Format(time.DateOnly)
}

func (c *Client) repairSeqProductIRODSLocationsDependencyIndexes(ctx context.Context) error {
	if c.cache.Dialect() != "mysql" {
		return nil
	}

	return repairDroppedMirrorIndexSet(ctx, c.cache.DB(), c.cache.Dialect(), iseqProductMetricsMirrorIndexSet)
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
		time.DateOnly,
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
	if !state.Exists {
		return true
	}
	if state.IndexesDropped {
		return state.HighWater.IsZero() || state.ResumeCursor != nil
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

		return iseqProductMetricsColdSyncSourceQuery(), []any{id, id}, true, nil
	}

	if state.ResumeCursor == nil {
		highWater := formatSyncTime(state.HighWater)

		return iseqProductMetricsSyncSourceQuery(), []any{highWater}, false, nil
	}

	lastUpdated, idIseqProduct, err := parseTwoPartResumeCursor(*state.ResumeCursor)
	if err != nil {
		return "", nil, false, fmt.Errorf("mlwh: parse iseq_product_metrics resume cursor: %w", err)
	}

	formattedLastUpdated := formatSyncTime(lastUpdated)

	return iseqProductMetricsSyncSourceQueryFromCursor(), []any{formattedLastUpdated, formattedLastUpdated, idIseqProduct}, false, nil
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
				repaired, _, err := createMirrorDroppedIndexes(ctx, tx, cache.Dialect(), indexSet)
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
			} else {
				repaired, err := rebuildSampleMirrorColdLoadIndexes(ctx, tx, cache.Dialect())
				if err != nil {
					return err
				}
				deferredIndexesDropped = !repaired
			}
		}
		if !indexesDropped {
			if err := rebuildCommonNameWordMirror(ctx, tx); err != nil {
				return err
			}
		}

		return writeSyncStateTx(ctx, tx, cache.Dialect(), syncTableSample, highWater, nil, deferredIndexesDropped)
	})
}

func shouldDeferMirrorIndexRebuild(cache Cache) bool {
	return cache != nil && cache.Dialect() == "mysql"
}

func rebuildSampleMirrorColdLoadIndexes(ctx context.Context, tx *sql.Tx, dialect string) (bool, error) {
	if err := rebuildDonorSampleTable(ctx, tx, dialect); err != nil {
		return false, err
	}
	if err := rebuildSampleSearchTokenIndex(ctx, tx, dialect); err != nil {
		return false, err
	}
	if _, err := createSampleMirrorSecondaryIndexes(ctx, tx, dialect); err != nil {
		return false, err
	}
	if err := rebuildCommonNameWordMirror(ctx, tx); err != nil {
		return false, err
	}

	return true, nil
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

func createSampleMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string) (bool, error) {
	return createMirrorSecondaryIndexes(ctx, tx, dialect, sampleMirrorIndexSet)
}

func createMirrorSecondaryIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) (bool, error) {
	existing, err := mirrorExistingIndexes(ctx, tx, dialect, indexSet)
	if err != nil {
		return false, err
	}

	missing := missingMirrorSecondaryIndexes(existing, indexSet.Indexes)
	if len(missing) == 0 {
		return false, nil
	}
	if dialect == "mysql" {
		if _, err = tx.ExecContext(ctx, buildMySQLCreateMirrorSecondaryIndexesStatement(indexSet.Table, missing)); err != nil {
			return false, fmt.Errorf("mlwh: create %s indexes: %w", indexSet.Table, err)
		}

		return true, nil
	}

	for _, index := range missing {
		stmt := fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(%s)`, index.Name, indexSet.Table, index.Column)

		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			return false, fmt.Errorf("mlwh: create %s index %s: %w", indexSet.Table, index.Name, err)
		}
	}

	return true, nil
}

func createMirrorDroppedIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) (bool, bool, error) {
	if dialect == "mysql" {
		repaired, createdIndexes, err := createMySQLSparseMirrorReadIndexes(ctx, tx, dialect, indexSet)
		if repaired || err != nil {
			return false, createdIndexes, err
		}
	}
	if dialect == "sqlite" {
		rebuildInline, err := shouldRebuildSQLiteMirrorSecondaryIndexesInline(ctx, tx, indexSet)
		if err != nil {
			return false, false, err
		}
		if !rebuildInline {
			if err := createSQLiteSparseMirrorReadIndexes(ctx, tx, dialect, indexSet); err != nil {
				return false, false, err
			}

			return false, false, nil
		}
	}

	if dialect == "mysql" && indexSet.PrimaryKeyColumn != "" {
		rebuildInline, err := shouldRebuildMySQLMirrorSecondaryIndexesInline(ctx, tx, indexSet)
		if err != nil {
			return false, false, err
		}
		if !rebuildInline {
			return false, false, nil
		}
	}

	if err := createMirrorPrimaryKey(ctx, tx, dialect, indexSet); err != nil {
		return false, false, err
	}
	createdIndexes, err := createMirrorSecondaryIndexes(ctx, tx, dialect, indexSet)
	if err != nil {
		return false, false, err
	}

	return true, createdIndexes, nil
}

func createMySQLSparseMirrorReadIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) (bool, bool, error) {
	readIndexSet, ok := mySQLSparseMirrorReadIndexSet(indexSet)
	if !ok {
		return false, false, nil
	}

	createdIndexes, err := createMirrorSecondaryIndexes(ctx, tx, dialect, readIndexSet)
	if err != nil {
		return false, false, err
	}

	return true, createdIndexes, nil
}

func createSQLiteSparseMirrorReadIndexes(ctx context.Context, tx *sql.Tx, dialect string, indexSet syncMirrorIndexSet) error {
	readIndexSet, ok := mySQLSparseMirrorReadIndexSet(indexSet)
	if !ok {
		return nil
	}

	_, err := createMirrorSecondaryIndexes(ctx, tx, dialect, readIndexSet)

	return err
}

func shouldRebuildSQLiteMirrorSecondaryIndexesInline(ctx context.Context, tx *sql.Tx, indexSet syncMirrorIndexSet) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+indexSet.Table).Scan(&count); err != nil {
		return false, fmt.Errorf("mlwh: count %s rows before sqlite index rebuild: %w", indexSet.Table, err)
	}

	return count <= mysqlInlineMirrorIndexRowLimit, nil
}

func mySQLSparseMirrorReadIndexSet(indexSet syncMirrorIndexSet) (syncMirrorIndexSet, bool) {
	switch indexSet.Table {
	case iseqFlowcellMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: iseqFlowcellMirrorReadIndexes}, true
	case iseqProductMetricsMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: iseqProductMetricsMirrorReadIndexes}, true
	case seqProductIRODSLocationsMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: seqProductIRODSLocationsMirrorReadIndexes}, true
	case iseqRunStatusMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: iseqRunStatusMirrorReadIndexes}, true
	case pacBioRunWellMetricsMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: pacBioRunWellMetricsMirrorReadIndexes}, true
	case oseqFlowcellMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: oseqFlowcellMirrorReadIndexes}, true
	case useqRunMetricsMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: useqRunMetricsMirrorReadIndexes}, true
	case eseqRunLaneMetricsMirrorIndexSet.Table:
		return syncMirrorIndexSet{Table: indexSet.Table, Indexes: eseqRunLaneMetricsMirrorReadIndexes}, true
	default:
		return syncMirrorIndexSet{}, false
	}
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
	case syncTablePacBioProductMetrics:
		return syncPacBioProductMetricsTable(ctx, c.cache, source, state)
	case syncTableEseqProductMetrics:
		return syncEseqProductMetricsTable(ctx, c.cache, source, state)
	case syncTableUseqProductMetrics:
		return syncUseqProductMetricsTable(ctx, c.cache, source, state)
	case syncTableIseqRunStatus:
		return syncIseqRunStatusTable(ctx, c.cache, source, state)
	case syncTablePacBioRunWellMetrics, syncTableEseqRun, syncTableEseqRunLaneMetrics,
		syncTableUseqRunMetrics, syncTableOseqFlowcell, syncTableStudyUsers, syncTableIseqRunStatusDict:
		return syncWholesaleMirrorTable(ctx, c.cache, source, state, wholesaleMirrorSpecFor(table))
	case syncTableSeqOpsTrackingPerSample:
		return syncSeqOpsTrackingPerSampleTable(ctx, c.cache, source, state)
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
	QC                sql.NullInt64
	QCLib             sql.NullInt64
	QCSeq             sql.NullInt64
	LastUpdated       time.Time
}

func syncIseqProductMetricsTable(ctx context.Context, cache Cache, source Querier, state syncStateRecord) (SyncReport, bool, error) {
	query, args, coldIDSync, err := iseqProductMetricsSyncQuery(state)
	if err != nil {
		return SyncReport{}, false, err
	}
	if !coldIDSync {
		return syncIseqProductMetricsWarmTable(ctx, cache, source, state, query, args)
	}

	if err = prepareMirrorIndexesForColdSync(ctx, cache, &state, iseqProductMetricsMirrorIndexSet); err != nil {
		return SyncReport{}, false, err
	}

	return syncIseqProductMetricsStreamingQuery(ctx, cache, source, state, query, args, true)
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
	// QC columns are NULL-preserving: a NULL source qc must stay SQL NULL in the
	// mirror so a downstream read maps it to "pending", distinct from a 0 "fail".
	row.QC = qc
	row.QCLib = qcLib
	row.QCSeq = qcSeq

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

func insertIseqProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []iseqProductMetricsSyncRow) error {
	if dialect == "sqlite" {
		return execPreparedInsertRows(ctx, tx, "iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, rows, iseqProductMetricsMirrorRowArgs, "insert iseq_product_metrics mirror batch")
	}

	return forEachRowChunk(rows, syncStatementRowLimit(len(iseqProductMetricsMirrorColumns)), func(chunk []iseqProductMetricsSyncRow) error {
		stmt := buildBulkInsertStatement("iseq_product_metrics_mirror", iseqProductMetricsMirrorColumns, len(chunk))
		if _, err := tx.ExecContext(ctx, stmt, iseqProductMetricsMirrorBatchArgs(chunk)...); err != nil {
			return fmt.Errorf("mlwh: insert iseq_product_metrics mirror batch: %w", err)
		}

		return nil
	})
}

func replaceIseqProductMetricsMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []iseqProductMetricsSyncRow) error {
	if err := deleteExistingKeys(ctx, tx, "iseq_product_metrics_mirror", []string{"id_iseq_product"}, iseqProductMetricsBatchKeys(rows)); err != nil {
		return err
	}

	return insertIseqProductMetricsMirrorBatch(ctx, tx, dialect, rows)
}

func iseqProductMetricsMirrorBatchArgs(rows []iseqProductMetricsSyncRow) []any {
	args := make([]any, 0, len(rows)*len(iseqProductMetricsMirrorColumns))
	for _, row := range rows {
		args = append(args, iseqProductMetricsMirrorRowArgs(row)...)
	}

	return args
}

func iseqProductMetricsMirrorRowArgs(row iseqProductMetricsSyncRow) []any {
	return []any{
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
	}
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
	Created               sql.NullTime
	Platform              string
	IDRun                 int64
	Position              int64
	TagIndex              int64
	QC                    sql.NullInt64
	IsDeliverable         sql.NullInt64
	Merged                int64
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
		if enrichErr := enrichSeqProductIRODSLocationsExportFields(ctx, cache.DB(), batch); enrichErr != nil {
			return enrichErr
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
	var lastUpdated, created any
	if err := rows.Scan(
		&row.SourceRowID,
		&row.IDIseqProduct,
		&row.IRODSRootCollection,
		&row.IRODSDataRelativePath,
		&row.IDSampleTmp,
		&row.IDStudyLims,
		&lastUpdated,
		&created,
		&row.Platform,
	); err != nil {
		return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: scan seq_product_irods_locations sync row: %w", err)
	}
	row.IRODSCollection, row.IRODSFileName = splitIRODSRelativePath(row.IRODSRootCollection, row.IRODSDataRelativePath)

	parsed, err := parseSyncTimeValue(lastUpdated)
	if err != nil {
		return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: parse seq_product_irods_locations last_updated: %w", err)
	}
	row.LastUpdated = parsed

	// created is nullable upstream (default CURRENT_TIMESTAMP); a NULL value is
	// mirrored as NULL (the mirror's created column is nullable) rather than the
	// zero time, so the downstream MIN/MAX(created) aggregates and the
	// created >= since recency filter naturally ignore unknown-creation rows
	// instead of skewing date ranges / delivered_at to year 0001. Existence-based
	// counts (data_objects, samples_with_data) still include the row because they
	// never read created.
	if created != nil {
		parsedCreated, parseErr := parseSyncTimeValue(created)
		if parseErr != nil {
			return seqProductIRODSLocationsSyncRow{}, fmt.Errorf("mlwh: parse seq_product_irods_locations created: %w", parseErr)
		}
		row.Created = sql.NullTime{Time: parsedCreated, Valid: true}
	}

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
	if dialect == "sqlite" {
		return execPreparedInsertRows(ctx, tx, "seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorColumns, rows, seqProductIRODSLocationsMirrorRowArgs, "insert seq_product_irods_locations mirror batch")
	}

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
	if err := deleteExistingKeys(ctx, tx, "seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorKeyColumns, seqProductIRODSLocationsBatchKeys(rows)); err != nil {
		return err
	}

	return insertSeqProductIRODSLocationsMirrorBatch(ctx, tx, dialect, rows)
}

func seqProductIRODSLocationsMirrorBatchArgs(rows []seqProductIRODSLocationsSyncRow) []any {
	args := make([]any, 0, len(rows)*len(seqProductIRODSLocationsMirrorColumns))
	for _, row := range rows {
		args = append(args, seqProductIRODSLocationsMirrorRowArgs(row)...)
	}

	return args
}

func seqProductIRODSLocationsMirrorRowArgs(row seqProductIRODSLocationsSyncRow) []any {
	return []any{
		row.SourceRowID,
		row.IDIseqProduct,
		row.IRODSRootCollection,
		row.IRODSDataRelativePath,
		row.IRODSCollection,
		row.IRODSFileName,
		row.IDSampleTmp,
		row.IDStudyLims,
		formatSyncTime(row.LastUpdated),
		formatNullableSyncTime(row.Created),
		row.Platform,
		row.IDRun,
		row.Position,
		row.TagIndex,
		row.QC,
		row.IsDeliverable,
		row.Merged,
	}
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

func insertSampleMirrorBatch(ctx context.Context, tx *sql.Tx, dialect string, rows []sampleSyncRow) error {
	if dialect == "sqlite" {
		return execPreparedInsertRows(ctx, tx, "sample_mirror", sampleMirrorColumns, rows, sampleMirrorRowArgs, "insert sample mirror batch")
	}

	return forEachRowChunk(rows, syncStatementRowLimit(len(sampleMirrorColumns)), func(chunk []sampleSyncRow) error {
		stmt := buildBulkInsertStatement("sample_mirror", sampleMirrorColumns, len(chunk))
		if _, err := tx.ExecContext(ctx, stmt, sampleMirrorBatchArgs(chunk)...); err != nil {
			return fmt.Errorf("mlwh: insert sample mirror batch: %w", err)
		}

		return nil
	})
}

func execPreparedInsertRows[T any](ctx context.Context, tx *sql.Tx, table string, columns []string, rows []T, rowArgs func(T) []any, label string) error {
	stmt, err := tx.PrepareContext(ctx, buildBulkInsertStatement(table, columns, 1))
	if err != nil {
		return fmt.Errorf("mlwh: prepare %s: %w", label, err)
	}
	defer func() { _ = stmt.Close() }()

	for _, row := range rows {
		if _, err = stmt.ExecContext(ctx, rowArgs(row)...); err != nil {
			return fmt.Errorf("mlwh: %s: %w", label, err)
		}
	}

	return nil
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
	IDIseqFlowcellTmp int64
	EntityType        string
	PipelineIDLims    string
	IDSampleTmp       int64
	IDStudyTmp        int64
	IDStudyLims       string
	LibraryID         string
	IDLibraryLims     string
	LastUpdated       time.Time
}

func scanFlowcellSyncRow(rows *sql.Rows) (flowcellSyncRow, error) {
	var row flowcellSyncRow
	var entityType sql.NullString
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
	case 9:
		err = rows.Scan(&row.IDIseqFlowcellTmp, &entityType, &pipelineIDLims, &row.IDSampleTmp, &row.IDStudyTmp, &studyLims, &libraryID, &idLibraryLims, &lastUpdated)
	default:
		err = fmt.Errorf("unexpected column count %d", len(columns))
	}
	if err != nil {
		return flowcellSyncRow{}, fmt.Errorf("mlwh: scan iseq_flowcell sync row: %w", err)
	}
	row.EntityType = nullStringValue(entityType)
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
		if row.PipelineIDLims == "" {
			continue
		}

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
	indices := make(map[seqProductIRODSLocationsDedupeKey]int, len(rows))
	deduped := make([]seqProductIRODSLocationsSyncRow, 0, len(rows))
	for _, row := range rows {
		key := seqProductIRODSLocationsBatchDedupeKey(row)
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
		keys = append(keys, []any{row.SourceRowID})
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
			if err := insertSampleMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
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

			if err := replaceSampleSearchTokensForBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
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
	libraryRows := dedupeFlowcellBatch(rows)
	mirrorRows := dedupeIseqFlowcellMirrorBatch(rows)
	if err := validateFlowcellBatch(libraryRows); err != nil {
		return syncBatchResult{}, err
	}

	var result syncBatchResult
	err := withSyncWriteTx(ctx, cache, func(tx *sql.Tx) error {
		existing := 0
		if !assumeInserted {
			var err error
			existing, err = countExistingKeys(ctx, tx, "library_samples", []string{"pipeline_id_lims", "id_sample_tmp", "id_study_lims"}, flowcellBatchKeys(libraryRows))
			if err != nil {
				return err
			}
		}
		if err := upsertLibrarySampleBatch(ctx, tx, cache.Dialect(), libraryRows); err != nil {
			return err
		}
		if err := upsertIseqFlowcellMirrorBatch(ctx, tx, cache.Dialect(), mirrorRows); err != nil {
			return err
		}

		result.Updated = existing
		result.Inserted = len(libraryRows) - existing
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
			if err := insertIseqProductMetricsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
				return err
			}
		} else if shouldReplaceSparseMySQLMirrorRows(cache, indexesDropped, assumeInserted) {
			if err := replaceIseqProductMetricsMirrorBatch(ctx, tx, cache.Dialect(), deduped); err != nil {
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
			existing, err = countExistingKeys(ctx, tx, "seq_product_irods_locations_mirror", seqProductIRODSLocationsMirrorKeyColumns, seqProductIRODSLocationsBatchKeys(deduped))
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
