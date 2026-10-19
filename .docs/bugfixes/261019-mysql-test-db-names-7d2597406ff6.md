# MySQL cache test databases leak and the tests silently skip

- Branch: mysqltestdb-f8676b0c
- Base: origin/develop @ 5cb948b
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] `openMySQLCacheForTest` (`mlwh/cache_schema_test.go:1508`) creates
      throwaway databases named `wa_mlwh_a3_<unixnano>`. The cache user has
      `CREATE` on `*.*` but full privileges only on `workflow_automation_%`,
      so it can create but not use or drop them: every run leaks one database
      (9843 found on the test server on 2026-10-19) and the MySQL tests that
      use the helper skip with "cache user lacks privileges". Name the
      databases under the `workflow_automation_` prefix, as
      `sync_dependency_mysql_integration_test.go` already does, and stop
      ignoring drop failures.
    - Red command (cache credentials from `.env.development.local`):
      `WA_MLWH_CACHE_PATH=... WA_MLWH_CACHE_PASSWORD=[REDACTED] go test ./mlwh -run TestMySQLSchemaCaseInsensitiveSampleNameEquality -count=1 -v`
      -> `SKIP ... cache user lacks privileges on temporary database ... SELECT command denied ... for table 'schema_version'`,
      and the count of `wa_mlwh_a3_%` databases rose from 9843 to 9844.
    - Fixed in `mlwh/cache_schema_test.go`: the helper names its database with
      `throwawayCacheDBName()` (`workflow_automation_mlwh_dev_it<hex>`) and
      drops it with `dropThrowawayMySQLCacheDB`, which now fails the test
      (`t.Errorf`) when a drop fails (`mlwh/cache_mysql_integration_test.go`).
      The red test now passes and no database is left behind.
- [x] Exposed by the first item (these tests never ran before):
      `TestSearchParityStudyIDSetsAndCountsEqualAcrossDialects`
      (`mlwh/search_parity_test.go:171`) fails on MySQL: `SearchStudies("malar")`
      finds neither "Malaria ..." study. `study_title` and `programme` in
      `mlwh/cache_schema/mysql/study_mirror.sql` lack
      `COLLATE {{MYSQL_TEXT_COLLATION}}`, so case-insensitive search depends on
      the database default collation.
    - Red command: the test above with the cache credentials exported ->
      `Expected []int64{1, 2}, Actual []int64{}`.
    - Fixed in `mlwh/cache_schema/mysql/study_mirror.sql` (COLLATE on
      `study_title` and `programme`) and `mlwh/cache.go`
      (`ensureMySQLStudySearchColumnCollation`, run from `prepareCacheSchema`,
      repairs existing current-version caches in place without a re-sync, as
      `ensureAdditiveCurrentCacheIndexes` does). Tests in
      `mlwh/cache_schema_test.go` cover the reopen repair and a missing
      `study_mirror` still triggering a rebuild.
- [ ] Exposed by the first item:
      `TestClientSyncSparseMySQLProductMirrorsReplaceExistingKeys`
      (`mlwh/sync_test.go:3908`) fails in setup with
      `dropMirrorPrimaryKeyForTest(seq_product_irods_locations_mirror): Error 1091 Can't DROP 'PRIMARY'`;
      the MySQL table has no primary key any more.
    - Red command: the test above with the cache credentials exported.
- [ ] Exposed by the first item:
      `TestClientSyncSampleColdLoadRecreatesIndexesMySQL`
      (`mlwh/sync_test.go:4383`) expects the `sample_mirror_*_idx` indexes after
      a cold MySQL sample sync and gets none, because `finalizeSampleSyncState`
      defers the MySQL index rebuild (`shouldDeferMirrorIndexRebuild`). The
      test and the deferral were added together in 1b61666.
    - Red command: the test above with the cache credentials exported ->
      `Expected map[...9 indexes...], Actual map[]`.
