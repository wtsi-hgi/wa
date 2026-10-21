# Concurrent cache opens fail or race on schema setup

- Branch: cachelock-732babe9
- Base: origin/develop @ fa77143
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] Several processes opening a new SQLite cache at once, such as
      `wa mlwh serve` and `wa mlwh sync` starting in one pod, can fail at
      once with "mlwh: ping sqlite cache: database is locked (5)
      (SQLITE_BUSY)". The container command in `DEVELOPING.md` slept 60
      seconds before the first sync to avoid it.
    - Red command:
      `go test ./mlwh/ -run TestConcurrentProcessesOpenAFreshSQLiteCache -count=1`
      -> helper processes exit with `ping sqlite cache: database is locked`.
    - Cause: connecting runs `PRAGMA journal_mode(WAL)`, and switching a new
      rollback-mode file to WAL returns `SQLITE_BUSY` at once while another
      connection holds it, without calling the busy handler. The reported
      pragma order was not the cause: modernc.org/sqlite always runs
      `busy_timeout` before the other `_pragma` params, so the DSN order
      makes no difference.
    - Fixed in `mlwh/cache.go`: the SQLite cache's first ping retries on
      `SQLITE_BUSY` until `cacheSchemaLockTimeout`. Once the file is in WAL
      mode the pragma is a no-op, so only a new cache retries.
- [x] Cache schema creation and migration are not serialised across
      processes, for SQLite or MySQL. Concurrent opens of a new or older
      cache fail with `UNIQUE constraint failed: schema_version.version`,
      `no such table: main.<mirror>`, or on MySQL
      `Error 1061: Duplicate key name ...`, and an older cache can be
      migrated, dropping its mirrors, more than once.
    - Red commands:
      `go test ./mlwh/ -run 'TestConcurrentOpens' -count=3` and, with
      `WA_MLWH_CACHE_PATH`/`WA_MLWH_CACHE_PASSWORD` set,
      `go test ./mlwh/ -run TestRealMySQLConcurrent -count=1`.
    - Cause: `prepareCacheSchema` checked `schema_version` and then created
      or migrated the schema in separate autocommit statements with no lock.
    - Fixed in `mlwh/cache.go`: a cache already at `CacheSchemaVersion` that
      validates still opens with no lock, so opening never waits for a
      running sync. Otherwise setup runs under a lock and re-checks the
      schema first: SQLite uses `BEGIN IMMEDIATE` on the one-connection
      write pool, so the setup also commits atomically, and MySQL uses
      `GET_LOCK` on a name derived from the cache database. Both wait up to
      `cacheSchemaLockTimeout` (5 minutes).
    - Tests in `mlwh/cache_concurrent_open_test.go`: eight goroutines open a
      new SQLite cache; eight open an older one and exactly one migration is
      logged; eight processes each open ten new caches in step; and real
      MySQL twins of the first two.
    - Docs: `DEVELOPING.md` no longer sleeps 60 seconds before the first
      sync in the container command.
