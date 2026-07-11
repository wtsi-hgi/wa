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
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestRealMySQLWarmDependencyReconciliation(t *testing.T) {
	baseDSN, password := realMySQLCacheDSNOrSkip(t)
	throwawayDSN := createAgentMySQLCacheDB(t, baseDSN, password)
	ctx := context.Background()
	client, err := OpenCacheOnly(ctx, CacheConfig{Path: throwawayDSN, Password: password})
	if err != nil {
		t.Fatalf("open agent MySQL cache: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	source := openRealMLWHSchemaSource(t)
	base := time.Date(2026, time.July, 11, 12, 0, 0, 0, time.UTC)
	seedRealMLWHStudyRow(t, source, 1001, "SQSCP", "study-1001", "uuid-1001", "Study 1001", "acc-1001", base)
	seedRealMLWHFlowcellRow(t, source, 1002, "library", 1003, 1001, base)
	seedRealMLWHFlowcellRow(t, source, 1004, "library", 1003, 1001, base)
	seedRealMLWHProductMetricRow(t, source, 1005, 1002, 1006, 1, 1, 1, 1, 1, base.Add(time.Minute))
	seedRealMLWHProductMetricRow(t, source, 1007, 1004, 1006, 2, 1, 1, 1, 1, base.Add(2*time.Minute))
	seedRealMLWHCompositeProductMetricRow(t, source, 1008, "agent-composite", `{"components":[{"id_run":1006,"position":1,"tag_index":1},{"id_run":1006,"position":2,"tag_index":1}]}`, base.Add(3*time.Minute))

	seedRealMLWHStudyRow(t, source, 1011, "SQSCP", "study-1011", "uuid-1011", "Study 1011", "acc-1011", base)
	seedRealMLWHPacBioRunRow(t, source, 1012, 1013, 1011)
	seedRealMLWHPacBioProductMetricRow(t, source, 1014, 1012, "agent-pacbio", base.Add(time.Minute))
	seedRealMLWHIRODSLocationPlatformRow(t, source, 1015, "agent-pacbio", "PacBio", "/seq/pacbio", "agent.bam", base.Add(2*time.Minute), base.Add(2*time.Minute))
	seedRealMLWHPacBioProductMetricRow(t, source, 1016, 1012, "agent-pacbio-newer", base.Add(9*time.Minute))
	seedRealMLWHIRODSLocationPlatformRow(t, source, 1017, "agent-pacbio-newer", "PacBio", "/seq/pacbio", "newer.bam", base.Add(10*time.Minute), base.Add(10*time.Minute))

	client.syncSource = sqliteJSONTableSource{db: source}
	client.disableSyncLock = true
	tables := []string{syncTableIseqProductMetrics, syncTablePacBioProductMetrics, syncTableSeqProductIRODSLocations}
	started := time.Now()
	if _, err = syncSelectedTablesForTest(ctx, client, tables...); err != nil {
		t.Fatalf("cold sync: %v", err)
	}
	t.Logf("cold sync duration: %s", time.Since(started))

	seedRealMLWHIRODSLocationProductRow(t, source, 1018, "agent-composite", "/seq/illumina", "agent.cram", base.Add(11*time.Minute))
	started = time.Now()
	if _, err = syncSelectedTablesForTest(ctx, client, tables...); err != nil {
		t.Fatalf("composite add warm sync: %v", err)
	}
	t.Logf("composite add warm sync duration: %s", time.Since(started))
	assertMySQLDependencyCount(t, client.cache.DB(), "agent-composite", 1, 1)

	if _, err = source.Exec(`UPDATE pac_bio_product_metrics SET id_pac_bio_tmp = NULL WHERE id_pac_bio_product = 'agent-pacbio'`); err != nil {
		t.Fatal(err)
	}
	if _, err = syncSelectedTablesForTest(ctx, client, tables...); err != nil {
		t.Fatalf("PacBio loss warm sync: %v", err)
	}
	assertMySQLDependencyCount(t, client.cache.DB(), "agent-pacbio", 0, 0)

	if _, err = source.Exec(`UPDATE pac_bio_product_metrics SET id_pac_bio_tmp = 1012 WHERE id_pac_bio_product = 'agent-pacbio'`); err != nil {
		t.Fatal(err)
	}
	if _, err = syncSelectedTablesForTest(ctx, client, tables...); err != nil {
		t.Fatalf("PacBio restoration warm sync: %v", err)
	}
	assertMySQLDependencyCount(t, client.cache.DB(), "agent-pacbio", 1, 1)

	seedRealMLWHStudyRow(t, source, 1021, "SQSCP", "study-1021", "uuid-1021", "Study 1021", "acc-1021", base)
	seedRealMLWHPacBioRunRow(t, source, 1022, 1023, 1021)
	if _, err = source.Exec(`UPDATE pac_bio_product_metrics SET id_pac_bio_tmp = 1022 WHERE id_pac_bio_product = 'agent-pacbio'`); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	if _, err = syncSelectedTablesForTest(ctx, client, tables...); err != nil {
		t.Fatalf("PacBio relink warm sync: %v", err)
	}
	t.Logf("PacBio relink warm sync duration: %s", time.Since(started))
	if got := countRows(t, client.cache.DB(), `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = 'agent-pacbio' AND id_sample_tmp = 1023 AND id_study_lims = 'study-1021'`); got != 1 {
		t.Fatalf("relinked PacBio iRODS rows = %d, want 1", got)
	}

	if _, err = source.Exec(`UPDATE study SET id_lims = 'OTHER' WHERE id_study_tmp = 1001`); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Exec(`UPDATE seq_product_irods_locations SET last_changed = ? WHERE id_product = 'agent-composite'`, formatSyncTime(base.Add(12*time.Minute))); err != nil {
		t.Fatal(err)
	}
	if _, err = syncSelectedTablesForTest(ctx, client, tables...); err != nil {
		t.Fatalf("composite removal warm sync: %v", err)
	}
	assertMySQLDependencyCount(t, client.cache.DB(), "agent-composite", 0, 0)
}

func createAgentMySQLCacheDB(t *testing.T, baseDSN, password string) string {
	t.Helper()
	parsed, err := mysql.ParseDSN(baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	dbName := "workflow_automation_mlwh_agent_" + randomHexToken()
	serverConfig := *parsed
	serverConfig.DBName = ""
	serverConfig.Passwd = password
	serverDSN := serverConfig.FormatDSN()
	server, err := sql.Open("mysql", serverDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = server.Close() }()
	if _, err = server.Exec("DROP DATABASE IF EXISTS `" + dbName + "`"); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Exec("CREATE DATABASE `" + dbName + "`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		dropThrowawayMySQLCacheDB(t, serverDSN, dbName)
		verify, openErr := sql.Open("mysql", serverDSN)
		if openErr != nil {
			t.Errorf("verify agent database cleanup: %v", openErr)

			return
		}
		defer func() { _ = verify.Close() }()
		var count int
		if queryErr := verify.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.SCHEMATA WHERE SCHEMA_NAME = ?`, dbName).Scan(&count); queryErr != nil || count != 0 {
			t.Errorf("agent database cleanup count = %d, err = %v", count, queryErr)
		}
	})
	throwaway := *parsed
	throwaway.DBName = dbName
	throwaway.Passwd = ""

	return throwaway.FormatDSN()
}

func assertMySQLDependencyCount(t *testing.T, db *sql.DB, product string, products, irods int) {
	t.Helper()
	if got := countRows(t, db, `SELECT COUNT(*) FROM iseq_product_metrics_mirror WHERE id_iseq_product = ?`, product) + countRows(t, db, `SELECT COUNT(*) FROM pac_bio_product_metrics_mirror WHERE id_pac_bio_product = ?`, product); got != products {
		t.Fatalf("product mirror rows for %s = %d, want %d", product, got, products)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM seq_product_irods_locations_mirror WHERE id_iseq_product = ?`, product); got != irods {
		t.Fatalf("iRODS mirror rows for %s = %d, want %d", product, got, irods)
	}
}
