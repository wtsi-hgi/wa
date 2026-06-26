# Phase 2: search + count + freshness Client methods

Ref: [spec.md](spec.md) sections A1, A2, A3, F1, F2, D2, B3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

These are pure `mlwh`-package `*Client` methods (no `Queryer`/`Registry`/
handler/`RemoteClient` wiring yet; that is Phase 4). The whole phase
depends on Phase 1 (sample search needs the FTS5/ngram index). Batch 1 is
the independent reads. After Batch 1, three sequential items follow: A3
(Item 2.5) adds the MySQL sample path, then F1 (Item 2.6) adds the search
counts that reuse the search WHERE clauses (including A3's MySQL
narrowing), then B3 (Item 2.7, cross-dialect set-equality) lands last
because it exercises A2/A3/F1 together.

## Items

### Batch 1 (parallel)

#### Item 2.1: A1 - study substring search [parallel with 2.2, 2.3, 2.4]

spec.md section: A1

Implement `(*Client).SearchStudies(ctx, term string, limit, offset int)
([]Study, error)` in `mlwh/search.go`: a single `study_mirror` query with
`id_lims = 'SQSCP'` and `LIKE '%term%'` OR'd across `name`, `study_title`,
`programme`, `faculty_sponsor`, ordered by `id_study_lims` with
`id_study_tmp` tie-breaker, `LIMIT ? OFFSET ?`. Escape `LIKE` wildcards
(`%`, `_`, escape char) before binding. Term length < 3 returns
`[]Study{}` without querying. Never-synced returns an empty slice with
`errors.Join(ErrCacheNeverSynced, ErrNotFound)`. Tests in
`mlwh/search_test.go`. Covers all 6 acceptance tests from A1.

- [ ] implemented
- [ ] reviewed

#### Item 2.2: A2 - sample search, SQLite FTS5 [parallel with 2.1, 2.3, 2.4]

spec.md section: A2

Implement `(*Client).SearchSamples(ctx, term string, limit, offset int)
([]Sample, error)` in `mlwh/search.go`, SQLite path: an FTS5 `MATCH`
against the trigram virtual table to narrow candidate `id_sample_tmp`s,
joined back to `sample_mirror` (`id_lims = 'SQSCP'`), with a
case-insensitive `LIKE '%term%'` post-filter across `name`,
`supplier_name`, `common_name`, `donor_id`, ordered by `id_sample_tmp`,
`LIMIT ? OFFSET ?`. Term length < 3 returns `[]Sample{}` without querying.
Returns full rows (fan-out populated as in `Find*`). Never-synced returns
an empty slice with `errors.Join(ErrCacheNeverSynced, ErrNotFound)`. Tests
in `mlwh/search_test.go`. Covers all 6 acceptance tests from A2.

Dialect dispatch is shared with Item 2.5; coordinate so both the SQLite
and MySQL branches live behind one `c.cache.Dialect()` switch.

- [ ] implemented
- [ ] reviewed

#### Item 2.3: F2 - list-relationship counts [parallel with 2.1, 2.2, 2.4]

spec.md section: F2

Implement `(*Client).CountStudies(ctx) (Count, error)` and
`(*Client).CountSamplesForStudy(ctx, studyLimsID string) (Count, error)`
in `mlwh/count.go`, plus the `Count` envelope type (`Count int
json:"count"`). `CountStudies` counts `study_mirror` rows with
`id_lims = 'SQSCP'`; `CountSamplesForStudy` counts the same distinct-
sample join/filter as `SamplesForStudy` (via `library_samples`), honouring
`id_lims = 'SQSCP'`, no `LIMIT`. Never-synced returns `Count{}` with
`errors.Join(ErrCacheNeverSynced, ErrNotFound)`; a synced study with no
samples returns `Count{Count: 0}` and no error. Tests in
`mlwh/count_test.go`. Covers all 4 acceptance tests from F2.

- [ ] implemented
- [ ] reviewed

#### Item 2.4: D2 - Client.Freshness [parallel with 2.1, 2.2, 2.3]

spec.md section: D2

Implement `(*Client).Freshness(ctx) (Freshness, error)` plus the
`Freshness`/`TableFreshness` types in `mlwh/freshness.go`. Read
`sync_state` for the five tables (`study`, `sample`, `iseq_flowcell`,
`iseq_product_metrics`, `seq_product_irods_locations`), returning one
`TableFreshness` each with `high_water`/`last_run` as UTC RFC3339
(`2006-01-02T15:04:05Z`), `ever_synced=false` and empty timestamps when
the row is absent. A never-synced cache returns all five with
`ever_synced=false` and does NOT error. Add a `sync_state` read that also
selects `last_run` (the existing `readSyncStateFromDB` selects only
`high_water`, `resume_cursor`, `indexes_dropped`).
Tests in `mlwh/freshness_test.go` (the `RemoteClient` half of D2 lands in
Phase 4).
Covers acceptance tests 1, 2, and 3 from D2 (test 4 is the `RemoteClient`
round-trip, deferred to Phase 4).

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Item 2.5: A3 - MySQL ngram query construction

spec.md section: A3

Add the MySQL branch of `SearchSamples` in `mlwh/search.go`: a `FULLTEXT
... MATCH(...) AGAINST(... IN BOOLEAN MODE)` predicate over the four
sample fields plus the same `LIKE '%' || ? || '%'` post-filter,
`id_lims = 'SQSCP'`, `ORDER BY ... id_sample_tmp`, `LIMIT ? OFFSET ?`.
Because `sqlmock` cannot evaluate the full-text predicate, assert query
construction (and dialect dispatch vs the SQLite path). Tests in
`mlwh/search_mysql_test.go`. Covers all 3 acceptance tests from A3.

Runs after Batch 1 is reviewed. Depends on Item 2.2 (extends the same
`SearchSamples` dialect switch).

- [ ] implemented
- [ ] reviewed

### Item 2.6: F1 - search count methods

spec.md section: F1

Implement `(*Client).CountStudySearch(ctx, term string) (Count, error)`
and `(*Client).CountSampleSearch(ctx, term string) (Count, error)` in
`mlwh/search.go`: `SELECT COUNT(*)` with the identical WHERE clause as
`SearchStudies`/`SearchSamples` (same `LIKE` post-filter, same
`id_lims = 'SQSCP'`, same FTS5/ngram index narrowing for samples), no
`LIMIT`. Term length < 3 returns `Count{Count: 0}` without querying.
Never-synced returns `Count{}` with `errors.Join(ErrCacheNeverSynced,
ErrNotFound)`. Tests in `mlwh/search_test.go`. Covers all 4 acceptance
tests from F1.

Runs after Item 2.5. Depends on Items 2.1 and 2.2 (reuses their WHERE
clauses); the sample count also reuses the MySQL narrowing from Item 2.5.

- [ ] implemented
- [ ] reviewed

### Item 2.7: B3 - cross-dialect search set-equality (ASCII fixtures)

spec.md section: B3

Add `mlwh/search_parity_test.go`: seed identical ASCII-only study/sample
rows into a SQLite cache and, under `WA_MLWH_DSN`, a MySQL cache; assert
`SearchStudies`/`SearchSamples` and their counts return identical
`id_*_tmp` sets and equal `Count.Count` across backends. When `WA_MLWH_DSN`
is unset, the MySQL half skips with `t.Skip` and the SQLite assertions
still run. Covers all 3 acceptance tests from B3.

Depends on Items 2.5 and 2.6 (exercises A2/A3 and F1 together).

- [ ] implemented
- [ ] reviewed
