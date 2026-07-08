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
	"net/http"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
)

// F1 acceptance test 1: full-window monthly counts use run grain, not the
// physical well/lane/flowcell rows the platform mirrors may carry.
func TestRunsMonthlyCountsUseRunGrainAndDateBasisF1(t *testing.T) {
	convey.Convey("Given the F1 monthly run aggregation fixture with duplicate wells, flowcells, lanes and products", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedF1MonthlyRunAggregationScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		rows, err := client.MonthlyRunCounts(context.Background(), RunAggregationOptions{
			Since: "2023-12-01",
			Until: "2024-03-01",
		})

		convey.Convey("when MonthlyRunCounts runs over the full window, then every platform counts one run identifier per bucket and states its date basis", func() {
			convey.So(err, convey.ShouldBeNil)

			byKey := monthlyRunCountByMonthPlatform(rows)
			convey.So(byKey["2023-12|Illumina"], convey.ShouldResemble, MonthlyRunCount{
				Month:         "2023-12",
				Manufacturer:  "Illumina",
				Platform:      "Illumina",
				Count:         1,
				DateBasis:     "run complete",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byKey["2023-12|PacBio"], convey.ShouldResemble, MonthlyRunCount{
				Month:         "2023-12",
				Manufacturer:  "PacBio",
				Platform:      "PacBio",
				Count:         1,
				DateBasis:     "run_complete",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byKey["2024-01|ONT"], convey.ShouldResemble, MonthlyRunCount{
				Month:         "2024-01",
				Manufacturer:  "Oxford Nanopore",
				Platform:      "ONT",
				Count:         2,
				DateBasis:     "warehouse load time - not a true sequencing date",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byKey["2024-01|Ultimagen"], convey.ShouldResemble, MonthlyRunCount{
				Month:         "2024-01",
				Manufacturer:  "Ultima Genomics",
				Platform:      "Ultimagen",
				Count:         1,
				DateBasis:     "run archived",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byKey["2024-02|Elembio"], convey.ShouldResemble, MonthlyRunCount{
				Month:         "2024-02",
				Manufacturer:  "Element Biosciences",
				Platform:      "Elembio",
				Count:         1,
				DateBasis:     "run complete",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})

			convey.So(byKey, convey.ShouldNotContainKey, "2023-12|Ultimagen")
		})
	})
}

// F1 acceptance test 2: ONT is included and labelled with the warehouse-load
// caveat rather than dropped for lacking a true sequencing completion date.
func TestRunsMonthlyIncludesONTWarehouseLoadBasisF1(t *testing.T) {
	convey.Convey("Given the F1 monthly run aggregation fixture with ONT flowcells", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedF1MonthlyRunAggregationScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		rows, err := client.MonthlyRunCounts(context.Background(), RunAggregationOptions{
			Since:     "2024-01-01",
			Until:     "2024-02-01",
			Platforms: []string{"ONT"},
		})

		convey.Convey("when the query is restricted to ONT, then the ONT bucket is present with the warehouse-load date_basis", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, []MonthlyRunCount{{
				Month:         "2024-01",
				Manufacturer:  "Oxford Nanopore",
				Platform:      "ONT",
				Count:         2,
				DateBasis:     "warehouse load time - not a true sequencing date",
				CacheSyncedAt: "2026-07-01T08:04:00Z",
			}})
		})
	})
}

func TestRunsMonthlyEndpointReturnsGroupedCountsF1(t *testing.T) {
	convey.Convey("Given the F1 monthly run aggregation fixture served through the MLWH API", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedF1MonthlyRunAggregationScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/runs/monthly?since=2024-01-01&until=2024-02-01&platform=ONT")

		convey.Convey("when GET /runs/monthly is served, then it returns the same monthly ONT bucket as the Client method", func() {
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var rows []MonthlyRunCount
			convey.So(json.Unmarshal(response.Body.Bytes(), &rows), convey.ShouldBeNil)
			convey.So(rows, convey.ShouldResemble, []MonthlyRunCount{{
				Month:         "2024-01",
				Manufacturer:  "Oxford Nanopore",
				Platform:      "ONT",
				Count:         2,
				DateBasis:     "warehouse load time - not a true sequencing date",
				CacheSyncedAt: "2026-07-01T08:04:00Z",
			}})
		})
	})
}

func TestRunListingRowsUseCompositeIDAndDateBasisF2(t *testing.T) {
	convey.Convey("Given the F2 global run listing fixture with one or more runs per platform", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedF1MonthlyRunAggregationScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		opts := RunAggregationOptions{Since: "2023-12-01", Until: "2024-03-01"}
		rows, err := client.RunListing(context.Background(), opts, 100, "")
		count, countErr := client.CountRunListing(context.Background(), opts)

		convey.Convey("when RunListing is read, then each row carries a stable lower-case platform/native-id composite plus the F1 date basis", func() {
			convey.So(err, convey.ShouldBeNil)
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(rows))

			byID := runListingByID(rows)
			convey.So(byID["illumina:52553"], convey.ShouldResemble, RunListingRow{
				ID:            "illumina:52553",
				Platform:      "Illumina",
				NativeID:      "52553",
				Manufacturer:  "Illumina",
				RunDate:       "2023-12-15",
				DateBasis:     "run complete",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byID["pacbio:TRACTION-RUN-1000"], convey.ShouldResemble, RunListingRow{
				ID:            "pacbio:TRACTION-RUN-1000",
				Platform:      "PacBio",
				NativeID:      "TRACTION-RUN-1000",
				Manufacturer:  "PacBio",
				RunDate:       "2023-12-20",
				DateBasis:     "run_complete",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byID["ont:ONTRUN-11"], convey.ShouldResemble, RunListingRow{
				ID:            "ont:ONTRUN-11",
				Platform:      "ONT",
				NativeID:      "ONTRUN-11",
				Manufacturer:  "Oxford Nanopore",
				RunDate:       "2024-01-11",
				DateBasis:     "warehouse load time - not a true sequencing date",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byID["ultimagen:8801"], convey.ShouldResemble, RunListingRow{
				ID:            "ultimagen:8801",
				Platform:      "Ultimagen",
				NativeID:      "8801",
				Manufacturer:  "Ultima Genomics",
				RunDate:       "2024-01-05",
				DateBasis:     "run archived",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byID["elembio:7701"], convey.ShouldResemble, RunListingRow{
				ID:            "elembio:7701",
				Platform:      "Elembio",
				NativeID:      "7701",
				Manufacturer:  "Element Biosciences",
				RunDate:       "2024-02-06",
				DateBasis:     "run complete",
				CacheSyncedAt: "2026-07-01T08:00:00Z",
			})
			convey.So(byID, convey.ShouldNotContainKey, "illumina:52554")
		})
	})
}

func TestRunListingAllPagesByCompositeIDF2(t *testing.T) {
	convey.Convey("Given the F2 global run listing fixture and a small page size", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedF1MonthlyRunAggregationScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		opts := RunAggregationOptions{Since: "2023-12-01", Until: "2024-03-01"}
		count, countErr := client.CountRunListing(context.Background(), opts)
		var (
			cursor string
			ids    []string
		)
		for {
			page, err := client.RunListing(context.Background(), opts, 2, cursor)
			convey.So(err, convey.ShouldBeNil)
			for _, row := range page {
				ids = append(ids, row.ID)
			}
			if len(page) < 2 {
				break
			}
			cursor = page[len(page)-1].ID
		}

		convey.Convey("when pages are continued by the last composite id, then every run is emitted exactly once and count equals the full list", func() {
			convey.So(countErr, convey.ShouldBeNil)
			convey.So(count.Count, convey.ShouldEqual, len(ids))
			convey.So(ids, convey.ShouldResemble, []string{
				"elembio:7701",
				"illumina:52553",
				"ont:ONTRUN-11",
				"ont:ONTRUN-12",
				"pacbio:TRACTION-RUN-1000",
				"pacbio:TRACTION-RUN-1001",
				"ultimagen:8801",
			})
		})
	})
}

func seedF1MonthlyRunAggregationScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedF1MonthlyRunSyncState(t, db)
	seedF1MonthlyIlluminaRuns(t, db)
	seedF1MonthlyPacBioRuns(t, db)
	seedF1MonthlyONTRuns(t, db)
	seedF1MonthlyUltimagenRuns(t, db)
	seedF1MonthlyElembioRuns(t, db)
}

func seedF1MonthlyRunSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.July, 1, 9, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, time.July, 1, 8, 0, 0, 0, time.UTC)
	for offset, table := range []string{
		syncTableIseqRunStatus,
		syncTableIseqRunStatusDict,
		syncTableIseqProductMetrics,
		syncTablePacBioRunWellMetrics,
		syncTableOseqFlowcell,
		syncTableUseqRunMetrics,
		syncTableEseqRunLaneMetrics,
	} {
		seedSyncStateRun(t, db, table, highWater, oldest.Add(time.Duration(offset)*time.Minute))
	}
}

func seedF1MonthlyIlluminaRuns(t *testing.T, db *sql.DB) {
	t.Helper()

	seedIseqRunStatusDictMirrorRow(t, db, 1, "run complete")
	seedIseqRunStatusDictMirrorRow(t, db, 2, "run pending")

	seedIseqProductMetricsMirrorRow(t, db, 91001, 101, 52553, 1, 1, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 91002, 102, 52553, 2, 1, "S1")
	seedIseqProductMetricsMirrorRow(t, db, 91003, 103, 52554, 1, 1, "S1")
	seedIseqRunStatusMirrorRow(t, db, 901, 52553, time.Date(2023, time.December, 15, 10, 0, 0, 0, time.UTC), 1, 0)
	seedIseqRunStatusMirrorRow(t, db, 902, 52554, time.Date(2023, time.December, 16, 10, 0, 0, 0, time.UTC), 2, 0)
}

func seedF1MonthlyPacBioRuns(t *testing.T, db *sql.DB) {
	t.Helper()

	complete := time.Date(2023, time.December, 20, 10, 0, 0, 0, time.UTC)
	for well := range 8 {
		wellLabel := string(rune('A'+well)) + "01"
		_, err := db.Exec(
			`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_complete, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			int64(1000+well),
			"TRACTION-RUN-1000",
			wellLabel,
			1,
			formatSyncTime(complete),
			"Complete",
			"Complete",
			formatSyncTime(complete.Add(time.Hour)),
			formatSyncDate(complete),
		)
		convey.So(err, convey.ShouldBeNil)
	}

	january := time.Date(2024, time.January, 9, 10, 0, 0, 0, time.UTC)
	_, err := db.Exec(
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_complete, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		2001,
		"TRACTION-RUN-1001",
		"A01",
		1,
		formatSyncTime(january),
		"Complete",
		"Complete",
		formatSyncTime(january.Add(time.Hour)),
		formatSyncDate(january),
	)
	convey.So(err, convey.ShouldBeNil)
}

func seedF1MonthlyONTRuns(t *testing.T, db *sql.DB) {
	t.Helper()

	loadTime := time.Date(2024, time.January, 11, 10, 0, 0, 0, time.UTC)
	ontRows := []struct {
		id             int64
		sampleID       int64
		experimentName string
	}{
		{3001, 301, "ONTRUN-11"},
		{3002, 302, "ONTRUN-11"},
		{3003, 303, "ONTRUN-12"},
	}
	for _, row := range ontRows {
		_, err := db.Exec(
			`INSERT INTO oseq_flowcell_mirror(id_oseq_flowcell_tmp, id_sample_tmp, id_study_lims, experiment_name, run_id, run_uuid, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			row.id,
			row.sampleID,
			"S1",
			row.experimentName,
			nil,
			"uuid-"+row.experimentName+"-"+formatInt(row.id),
			formatSyncTime(loadTime),
			formatSyncDate(loadTime),
		)
		convey.So(err, convey.ShouldBeNil)
	}
}

func seedF1MonthlyUltimagenRuns(t *testing.T, db *sql.DB) {
	t.Helper()

	archived := time.Date(2024, time.January, 5, 10, 0, 0, 0, time.UTC)
	_, err := db.Exec(
		`INSERT INTO useq_run_metrics_mirror(id_run, run_name, run_status, run_start, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		8801,
		"ultima-run-8801",
		"run archived",
		formatSyncTime(archived.Add(-time.Hour)),
		formatSyncTime(archived),
		formatSyncTime(archived.Add(time.Hour)),
		formatSyncDate(archived),
	)
	convey.So(err, convey.ShouldBeNil)
}

func seedF1MonthlyElembioRuns(t *testing.T, db *sql.DB) {
	t.Helper()

	complete := time.Date(2024, time.February, 6, 10, 0, 0, 0, time.UTC)
	for lane := 1; lane <= 2; lane++ {
		_, err := db.Exec(
			`INSERT INTO eseq_run_lane_metrics_mirror(id_run, lane, run_started, run_complete, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?)`,
			7701,
			lane,
			formatSyncTime(complete.Add(-time.Hour)),
			formatSyncTime(complete),
			formatSyncTime(complete.Add(time.Hour)),
			formatSyncDate(complete),
		)
		convey.So(err, convey.ShouldBeNil)
	}
}

func runListingByID(rows []RunListingRow) map[string]RunListingRow {
	byID := make(map[string]RunListingRow, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}

	return byID
}

func monthlyRunCountByMonthPlatform(rows []MonthlyRunCount) map[string]MonthlyRunCount {
	byKey := make(map[string]MonthlyRunCount, len(rows))
	for _, row := range rows {
		byKey[row.Month+"|"+row.Platform] = row
	}

	return byKey
}

// G2 acceptance tests 1 and 2: grouped run aggregation keeps the F1 run grain,
// joins study metadata inside one query and counts a cross-programme run once in
// each programme it touches.
func TestSequencingAggregateGroupsPacBioRunsByProgrammeG2(t *testing.T) {
	convey.Convey("Given PacBio run rows linked to studies in two programmes", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedG2PacBioSequencingAggregateScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		rows, err := client.SequencingAggregate(context.Background(), SequencingAggregateOptions{
			GroupBy:   []string{"programme"},
			Unit:      "runs",
			Since:     "2026-02-01",
			Until:     "2026-03-01",
			Platforms: []string{"PacBio"},
		})

		convey.Convey("when grouped by programme, then PacBio runs carry the PacBio date basis and a cross-programme run counts once per programme", func() {
			convey.So(err, convey.ShouldBeNil)

			byProgramme := sequencingAggregateByGroup(rows, "programme")
			convey.So(byProgramme["Cancer"], convey.ShouldResemble, SequencingAggregateRow{
				Group:         map[string]string{"programme": "Cancer"},
				Unit:          "runs",
				Count:         2,
				DateBasis:     "run_complete",
				CacheSyncedAt: "2026-07-02T08:00:00Z",
			})
			convey.So(byProgramme["Malaria"], convey.ShouldResemble, SequencingAggregateRow{
				Group:         map[string]string{"programme": "Malaria"},
				Unit:          "runs",
				Count:         1,
				DateBasis:     "run_complete",
				CacheSyncedAt: "2026-07-02T08:00:00Z",
			})
		})
	})
}

func TestSequencingAggregateCountsOneRunOncePerProgrammeGroupG2(t *testing.T) {
	convey.Convey("Given one PacBio run with several products in one programme and one product in another programme", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedG2CrossProgrammeRunScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		rows, err := client.SequencingAggregate(context.Background(), SequencingAggregateOptions{
			GroupBy:   []string{"programme"},
			Unit:      "runs",
			Since:     "2026-02-01",
			Until:     "2026-03-01",
			Platforms: []string{"PacBio"},
		})

		convey.Convey("when grouped by programme, then duplicate products do not inflate the run count within a programme", func() {
			convey.So(err, convey.ShouldBeNil)

			byProgramme := sequencingAggregateByGroup(rows, "programme")
			convey.So(byProgramme["Cancer"].Count, convey.ShouldEqual, 1)
			convey.So(byProgramme["Malaria"].Count, convey.ShouldEqual, 1)
		})
	})
}

func seedG2CrossProgrammeRunScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedG2SequencingAggregateSyncState(t, db)
	seedStudyMirrorSearchRow(t, db, 7001, "7001", "study-cancer-a", "Cancer A", "Cancer", "Sponsor A")
	seedStudyMirrorSearchRow(t, db, 7002, "7002", "study-malaria", "Malaria", "Malaria", "Sponsor B")
	seedG2PacBioRunWell(t, db, 92001, "TRACTION-G2-CROSS", "A01", "2026-02-10")
	seedG2PacBioRunWell(t, db, 92002, "TRACTION-G2-CROSS", "B01", "2026-02-10")
	seedG2PacBioRunWell(t, db, 92003, "TRACTION-G2-CROSS", "C01", "2026-02-10")
	seedG2PacBioProduct(t, db, "pb-g2-cross-cancer-1", 92001, 8101, "7001")
	seedG2PacBioProduct(t, db, "pb-g2-cross-cancer-2", 92002, 8102, "7001")
	seedG2PacBioProduct(t, db, "pb-g2-cross-malaria", 92003, 8103, "7002")
}

// G2 acceptance tests 4 and 5: samples/products are data-grain aggregates over
// iRODS created, distinct from the run-date path.
func TestSequencingAggregateSamplesAndProductsUseIRODSCreatedWindowG2(t *testing.T) {
	convey.Convey("Given PacBio products delivered to iRODS in a created-time window", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedG2PacBioSequencingAggregateScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}
		opts := SequencingAggregateOptions{
			GroupBy:   []string{"programme"},
			Since:     "2026-02-01T00:00:00Z",
			Until:     "2026-03-01T00:00:00Z",
			Platforms: []string{"PacBio"},
		}

		runRows, runErr := client.SequencingAggregate(context.Background(), SequencingAggregateOptions{
			GroupBy:   opts.GroupBy,
			Unit:      "runs",
			Since:     "2026-02-01",
			Until:     "2026-03-01",
			Platforms: opts.Platforms,
		})
		sampleRows, sampleErr := client.SequencingAggregate(context.Background(), SequencingAggregateOptions{
			GroupBy:   opts.GroupBy,
			Unit:      "samples",
			Since:     opts.Since,
			Until:     opts.Until,
			Platforms: opts.Platforms,
		})
		productRows, productErr := client.SequencingAggregate(context.Background(), SequencingAggregateOptions{
			GroupBy:   opts.GroupBy,
			Unit:      "products",
			Since:     opts.Since,
			Until:     opts.Until,
			Platforms: opts.Platforms,
		})

		convey.Convey("when unit=samples, then it counts distinct samples by iRODS created and differs from unit=runs", func() {
			convey.So(runErr, convey.ShouldBeNil)
			convey.So(sampleErr, convey.ShouldBeNil)

			runByProgramme := sequencingAggregateByGroup(runRows, "programme")
			sampleByProgramme := sequencingAggregateByGroup(sampleRows, "programme")
			convey.So(sampleByProgramme["Cancer"], convey.ShouldResemble, SequencingAggregateRow{
				Group:         map[string]string{"programme": "Cancer"},
				Unit:          "samples",
				Count:         1,
				DateBasis:     "iRODS created",
				CacheSyncedAt: "2026-07-02T08:00:00Z",
			})
			convey.So(sampleByProgramme["Malaria"], convey.ShouldResemble, SequencingAggregateRow{
				Group:         map[string]string{"programme": "Malaria"},
				Unit:          "samples",
				Count:         1,
				DateBasis:     "iRODS created",
				CacheSyncedAt: "2026-07-02T08:00:00Z",
			})
			convey.So(sampleByProgramme["Cancer"].Count, convey.ShouldNotEqual, runByProgramme["Cancer"].Count)
		})

		convey.Convey("when unit=products, then it counts distinct products and can exceed the sample count", func() {
			convey.So(sampleErr, convey.ShouldBeNil)
			convey.So(productErr, convey.ShouldBeNil)

			sampleByProgramme := sequencingAggregateByGroup(sampleRows, "programme")
			productByProgramme := sequencingAggregateByGroup(productRows, "programme")
			convey.So(productByProgramme["Cancer"], convey.ShouldResemble, SequencingAggregateRow{
				Group:         map[string]string{"programme": "Cancer"},
				Unit:          "products",
				Count:         2,
				DateBasis:     "iRODS created",
				CacheSyncedAt: "2026-07-02T08:00:00Z",
			})
			convey.So(productByProgramme["Cancer"].Count, convey.ShouldBeGreaterThan, sampleByProgramme["Cancer"].Count)
		})
	})
}

func TestSequencingAggregateEndpointG2(t *testing.T) {
	convey.Convey("Given the G2 aggregate fixture served through the MLWH API", t, func() {
		cache := openSQLiteSyncTestCache(t)
		defer func() { convey.So(cache.Close(), convey.ShouldBeNil) }()
		seedG2PacBioSequencingAggregateScenario(t, cache.DB())
		client := &Client{cache: cache, cacheReader: cacheReadDB(cache)}

		response := performMLWHRequestForTest(t, client, http.MethodGet, "/sequencing/aggregate?group_by=programme&unit=runs&platform=PacBio&since=2026-02-01&until=2026-03-01")

		convey.Convey("when GET /sequencing/aggregate is served, then it returns the grouped aggregate rows", func() {
			convey.So(response.Code, convey.ShouldEqual, http.StatusOK)

			var rows []SequencingAggregateRow
			convey.So(json.Unmarshal(response.Body.Bytes(), &rows), convey.ShouldBeNil)
			byProgramme := sequencingAggregateByGroup(rows, "programme")
			convey.So(byProgramme["Cancer"].Count, convey.ShouldEqual, 2)
			convey.So(byProgramme["Cancer"].Unit, convey.ShouldEqual, "runs")
			convey.So(byProgramme["Cancer"].DateBasis, convey.ShouldEqual, "run_complete")
		})
	})
}

func seedG2PacBioSequencingAggregateScenario(t *testing.T, db *sql.DB) {
	t.Helper()

	seedG2SequencingAggregateSyncState(t, db)
	seedStudyMirrorSearchRow(t, db, 7001, "7001", "study-cancer-a", "Cancer A", "Cancer", "Sponsor A")
	seedStudyMirrorSearchRow(t, db, 7002, "7002", "study-malaria", "Malaria", "Malaria", "Sponsor B")
	seedStudyMirrorSearchRow(t, db, 7003, "7003", "study-cancer-b", "Cancer B", "Cancer", "Sponsor C")
	seedG2PacBioRunWell(t, db, 91001, "TRACTION-G2-1", "A01", "2026-02-10")
	seedG2PacBioRunWell(t, db, 91002, "TRACTION-G2-1", "B01", "2026-02-10")
	seedG2PacBioRunWell(t, db, 91003, "TRACTION-G2-1", "C01", "2026-02-10")
	seedG2PacBioRunWell(t, db, 91004, "TRACTION-G2-2", "A01", "2026-02-11")
	seedG2PacBioRunWell(t, db, 91005, "TRACTION-G2-OUTSIDE", "A01", "2026-03-05")
	seedG2PacBioProduct(t, db, "pb-g2-cancer-a-1", 91001, 8001, "7001")
	seedG2PacBioProduct(t, db, "pb-g2-cancer-a-2", 91002, 8001, "7001")
	seedG2PacBioProduct(t, db, "pb-g2-malaria-1", 91003, 8002, "7002")
	seedG2PacBioProduct(t, db, "pb-g2-cancer-b-outside-data", 91004, 8003, "7003")
	seedG2PacBioProduct(t, db, "pb-g2-cancer-outside-run", 91005, 8004, "7001")
	for offset := range 1000 {
		wellID := int64(93000 + offset)
		seedG2PacBioRunWell(t, db, wellID, "TRACTION-G2-FILLER-"+formatInt(wellID), "A01", "2026-04-15")
		seedG2PacBioProduct(t, db, "pb-g2-filler-"+formatInt(wellID), wellID, 83000+wellID, "7001")
	}
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "pb-g2-cancer-a-1", "/seq/pacbio", "pb-g2-cancer-a-1.bam", 8001, "7001", time.Date(2026, time.February, 12, 9, 0, 0, 0, time.UTC), "pacbio")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "pb-g2-cancer-a-1", "/seq/pacbio", "pb-g2-cancer-a-1.pbi", 8001, "7001", time.Date(2026, time.February, 12, 9, 1, 0, 0, time.UTC), "pacbio")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "pb-g2-cancer-a-2", "/seq/pacbio", "pb-g2-cancer-a-2.bam", 8001, "7001", time.Date(2026, time.February, 13, 9, 0, 0, 0, time.UTC), "pacbio")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "pb-g2-malaria-1", "/seq/pacbio", "pb-g2-malaria-1.bam", 8002, "7002", time.Date(2026, time.February, 14, 9, 0, 0, 0, time.UTC), "pacbio")
	seedIRODSLocationMirrorRowWithCreatedPlatform(t, db, "pb-g2-cancer-b-outside-data", "/seq/pacbio", "pb-g2-cancer-b-outside-data.bam", 8003, "7003", time.Date(2026, time.March, 2, 9, 0, 0, 0, time.UTC), "pacbio")
}

func seedG2SequencingAggregateSyncState(t *testing.T, db *sql.DB) {
	t.Helper()

	highWater := time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, time.July, 2, 8, 0, 0, 0, time.UTC)
	for offset, table := range []string{
		syncTableStudy,
		syncTableSeqProductIRODSLocations,
		syncTablePacBioRunWellMetrics,
		syncTablePacBioProductMetrics,
		syncTableIseqRunStatus,
		syncTableIseqRunStatusDict,
		syncTableIseqProductMetrics,
		syncTableOseqFlowcell,
		syncTableEseqRunLaneMetrics,
		syncTableEseqProductMetrics,
		syncTableUseqRunMetrics,
		syncTableUseqProductMetrics,
	} {
		seedSyncStateRun(t, db, table, highWater, oldest.Add(time.Duration(offset)*time.Minute))
	}
}

func seedG2PacBioRunWell(t *testing.T, db *sql.DB, id int64, runName, well, normalisedDate string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO pac_bio_run_well_metrics_mirror(id_pac_bio_rw_metrics_tmp, pac_bio_run_name, well_label, plate_number, run_complete, run_status, well_status, last_updated, normalised_date) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		runName,
		well,
		1,
		normalisedDate+"T09:00:00Z",
		"Complete",
		"Complete",
		normalisedDate+"T10:00:00Z",
		normalisedDate,
	)
	if err != nil {
		t.Fatalf("seedG2PacBioRunWell(): %v", err)
	}
}

func seedG2PacBioProduct(t *testing.T, db *sql.DB, idProduct string, wellID, sampleID int64, studyID string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO pac_bio_product_metrics_mirror(id_pac_bio_product, id_pac_bio_rw_metrics_tmp, id_sample_tmp, id_study_lims, qc, last_updated) VALUES (?, ?, ?, ?, ?, ?)`,
		idProduct,
		wellID,
		sampleID,
		studyID,
		1,
		"2026-02-15T09:00:00Z",
	)
	if err != nil {
		t.Fatalf("seedG2PacBioProduct(): %v", err)
	}
}

func sequencingAggregateByGroup(rows []SequencingAggregateRow, key string) map[string]SequencingAggregateRow {
	byKey := make(map[string]SequencingAggregateRow, len(rows))
	for _, row := range rows {
		byKey[row.Group[key]] = row
	}

	return byKey
}
