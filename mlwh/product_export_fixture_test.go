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
	"database/sql"
	"testing"
	"time"
)

// manifestAllRows is the fetch-all limit used by product export tests that
// exercise the retained product-grain query builders.
const manifestAllRows = 1000

const (
	h4Study7568MergedSamples            = 48
	h4Study7568MergedSingleLaneProducts = h4Study7568MergedSamples * 2
	h4MergedMultilaneReason             = "merged_multilane"
	h4Study7568DirectCramPath           = "/seq/illumina/runs/49/49348/lane3/plex99/49348_3#99.cram"
	h4Study7568MergedCramPathForFixture = "/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram"
)

func setIseqProductMetricsMirrorQC(t *testing.T, db *sql.DB, idIseqProduct int64, qc sql.NullInt64) {
	t.Helper()

	_, err := db.Exec(
		`UPDATE iseq_product_metrics_mirror SET qc = ?, qc_lib = ?, qc_seq = ? WHERE id_iseq_product = ?`,
		qc,
		qc,
		qc,
		idIseqProduct,
	)
	if err != nil {
		t.Fatalf("setIseqProductMetricsMirrorQC(): %v", err)
	}
}

func seedManifestStudy7568MergedCRAMScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 7568, "7568")
	for i := range h4Study7568MergedSamples {
		sampleID := int64(7_568_000 + i)
		tagIndex := i + 1
		seedManifestSampleRow(
			t,
			db,
			sampleID,
			"7568STDY"+formatInt(sampleID),
			"supplier-"+formatInt(sampleID),
			"EGAN"+formatInt(sampleID),
			"sanger-"+formatInt(sampleID),
		)
		seedIseqProductMetricsMirrorRow(t, db, int64(49_348_000+tagIndex*10+1), sampleID, 49348, 1, tagIndex, "7568")
		seedIseqProductMetricsMirrorRow(t, db, int64(49_348_000+tagIndex*10+2), sampleID, 49348, 2, tagIndex, "7568")
		seedIRODSLocationMirrorRow(
			t,
			db,
			"merged-"+formatInt(sampleID),
			"/seq/illumina/runs/49/49348/lane1-2/plex"+formatInt(int64(tagIndex)),
			"49348_1-2#"+formatInt(int64(tagIndex))+".cram",
			sampleID,
			"7568",
		)
		setIRODSLocationMirrorQCAndDeliverableFields(
			t,
			db,
			"merged-"+formatInt(sampleID),
			sql.NullInt64{Int64: 1, Valid: true},
			sql.NullInt64{Int64: 1, Valid: true},
			true,
		)
	}

	directSampleID := int64(7_568_999)
	seedManifestSampleRow(t, db, directSampleID, "7568-direct-cram", "supplier-direct", "EGAN-direct", "sanger-direct")
	seedIseqProductMetricsMirrorRow(t, db, 49_348_999, directSampleID, 49348, 3, 99, "7568")
	seedIRODSLocationMirrorRow(t, db, "49348999", "/seq/illumina/runs/49/49348/lane3/plex99", "49348_3#99.cram", directSampleID, "7568")

	seedManifestSyncState(t, db)
}

// seedManifestS1Scenario seeds study S1 with 3 Illumina products across 2
// samples (run/lane/tag distinct): sample 21 carries products on (52553,1,1)
// and (52553,1,2); sample 22 carries product on (52554,2,3).
func seedManifestS1Scenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedHierarchyStudy(t, db, 211, "S1")
	seedManifestSampleRow(t, db, 21, "S1-sample-alpha", "supplier-alpha", "EGAN-alpha", "sanger-alpha")
	seedManifestSampleRow(t, db, 22, "S1-sample-beta", "supplier-beta", "EGAN-beta", "sanger-beta")

	seedIseqProductMetricsMirrorRow(t, db, 2101, 21, 52553, 1, 1, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 2102, 21, 52553, 1, 2, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 2203, 22, 52554, 2, 3, "S1")

	seedManifestSyncState(t, db)
}

// seedManifestSampleRow inserts a sample_mirror row with caller-controlled
// identity fields for product export fixtures.
func seedManifestSampleRow(t *testing.T, db *sql.DB, id int64, name, supplierName, accession, sangerSampleID string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO sample_mirror(id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims, name, sanger_sample_id, supplier_name, accession_number, donor_id, taxon_id, common_name, description, last_updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		"SQSCP",
		formatInt(id+100),
		"manifest-sample-uuid-"+formatInt(id),
		name,
		sangerSampleID,
		supplierName,
		accession,
		"manifest-donor-"+formatInt(id),
		9606,
		"human",
		"manifest-description",
		formatSyncTime(time.Date(2026, time.May, 6, 12, 5, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("seedManifestSampleRow(): %v", err)
	}
}

// seedManifestSyncState marks the product export feeding tables as synced so
// the never-synced cascade is not triggered in product-grain fixtures.
func seedManifestSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, time.June, 27, 6, 0, 0, 0, time.UTC)
	seedSyncStateRun(t, db, syncTableStudy, highWater, oldest)
	seedSyncStateRun(t, db, syncTableSample, highWater, oldest.Add(time.Hour))
	seedSyncStateRun(t, db, syncTableIseqFlowcell, highWater, oldest.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableIseqProductMetrics, highWater, oldest.Add(2*time.Hour))
	seedSyncStateRun(t, db, syncTableSeqProductIRODSLocations, highWater, oldest.Add(3*time.Hour))
}

func seedManifestSyncStateWithoutIRODS(t *testing.T, db *sql.DB) {
	t.Helper()

	seedManifestIdentitySyncState(t, db)
	seedSyncStateRun(
		t,
		db,
		syncTableIseqProductMetrics,
		time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.June, 27, 8, 0, 0, 0, time.UTC),
	)
}

func seedManifestIdentitySyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, time.June, 27, 6, 0, 0, 0, time.UTC)
	seedSyncStateRun(t, db, syncTableStudy, highWater, oldest)
	seedSyncStateRun(t, db, syncTableSample, highWater, oldest.Add(time.Hour))
	seedSyncStateRun(t, db, syncTableIseqFlowcell, highWater, oldest.Add(2*time.Hour))
}
