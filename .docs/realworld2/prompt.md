# Feature: study metadata, data manifests, run-scoped iRODS by file type, study QC counts, and people→studies (MLWH realworld2)

## Summary

A second wave of real user questions must become **fast (web-responsive, ~1s) and
cheap (one call, small/bounded response)** against the MLWH cache. These extend the
"realworld" work already merged (MLWH API 1.6.0). The questions:

1. **"What are the data access groups for study X?"** (`study.data_access_group`)
2. **"What is the iRODS path for the cram files from run X?"** (run-scoped iRODS,
   filtered to a file type)
3. **"List study details, run_id, sample name, supplier_name and sample accession
   for study X."** (a per-sample/per-product tabular **manifest**)
4. **As (3) plus the iRODS path to the cram files.**
5. **"Tell me the study ID for `<name>`."** (study-name search; may be ambiguous —
   the caller must be able to see all matches)
6. **"How many samples for study X have NOT got sequence data? How many have
   completed sequencing? How many passed manual QC?"** (study-level counts across a
   QC dimension)
7. **"List all studies for `<person>`."** (the named faculty sponsor) and the
   related **"list my studies"** (a person's role-based study membership)

This is the **upstream half**. A companion downstream feature in the `mlwh-mcp-server`
repo wraps these endpoints as MCP tools; keep the registry `Description`/`Summary`
text accurate because that is what the MCP layer surfaces verbatim. **Additionally,
the results of each new endpoint must be exposed through the `wa mlwh` CLI** (see
"CLI exposure" — extend `wa mlwh info <identifier>` where the result is "info about
that identifier", otherwise add a new `wa mlwh <subcommand>`).

**Scope rule: everything below is firm and in scope to build.** The "Design
decisions" section settles _how_, never _whether_.

## Authority, and what already landed (reuse — do NOT rebuild)

The only authority is **this repo's Go code** (`mlwh/registry.go`, `mlwh/types.go`,
`mlwh/availability.go`, `mlwh/progress.go`, `mlwh/sync.go`,
`mlwh/cache_schema/{sqlite,mysql}/*.sql`, `mlwh/remote.go`, `mlwh/server.go`,
`cmd/mlwh_info.go`). The `.docs/realworld*` specs are background only — verify
against code. Current MLWH API version: **1.6.0** (`mlwh/openapi.go` `APIVersion`).

Already merged and **reused as-is** (confirm in code before relying on it):

- **`Study`** (`mlwh/types.go`) already carries and mirrors `id_study_lims`, `name`,
  `accession_number`, `study_title`, `faculty_sponsor`, `data_access_group`,
  `programme`, etc. `study_mirror` has these columns; `mlwh/sync.go`
  `studyMirrorColumns` already selects `faculty_sponsor` and `data_access_group`.
  **Indexed:** `id_study_lims`, `uuid_study_lims`, `accession_number`, `name`
  (NOT `faculty_sponsor`/`data_access_group`).
- **`Sample`** already carries/mirrors `name`, `sanger_sample_id`, `supplier_name`,
  `accession_number`, `donor_id`, etc. (indexed incl. `supplier_name`,
  `accession_number`, `sanger_sample_id`).
- **`/search/study/:term`** (+ `/count`) already matches `name`, `study_title`,
  `programme`, **`faculty_sponsor`** (case-insensitive substring); `/search/sample/:term`
  is word-prefix over `name`/`supplier_name`/`common_name`/`donor_id`; the
  `/find/sample/{sanger-id,lims-id,accession,supplier-name,library-type}` exact-match
  endpoints exist.
- **`/resolve/study/:id`** resolves UUID / LIMS id / accession / **name** → `Study`.
- **`StudyOverview`** (`/study/:id/overview`) gives `samples_total`,
  `samples_with_data`, `samples_without_data`, `samples_sequenced_no_data`,
  `data_objects`, `runs`, `libraries`, recency fields, `cache_synced_at`.
  **`StatusBreakdown`** (`/study/:id/status-breakdown`) gives the
  `with_data`/`sequenced_no_data`/`registered` ladder, per-platform, and
  `with_detailed_timeline`. Neither has a **QC** dimension.
- **`iseq_product_metrics_mirror`** already mirrors `qc`, `qc_lib`, `qc_seq` (and the
  other platforms' product-metrics carry their own qc). `progress.go` already rolls
  these up per sample into `SampleProgress.qc` (`pass`/`fail`/`pending`/`not_tracked`,
  precedence fail > pending > pass).
- **`/study/:id/irods`** and **`/sample/:id/irods`** return `IRODSPath`
  (now including `id_sample_tmp` + `name`). The iRODS mirror
  (`seq_product_irods_locations_mirror`) has `id_iseq_product`, `irods_collection`,
  `irods_file_name`, `id_sample_tmp`, `id_study_lims`, `created`, `platform`.

## Verified source-schema facts (checked against the live source `mlwarehouse`)

These were confirmed directly against the read-only source DB (so the sync work is
grounded, not guessed):

- `study` has `data_access_group`, `faculty_sponsor`, `name`, `accession_number`.
- **`study_users`** exists: `id_study_users_tmp`, `id_study_tmp`, `last_updated`,
  `role`, `login`, `email`, `name`. **Semantics:** Sequencescape per-study people
  with a role; observed roles and scale: `follower` (~17k rows, 1046 people),
  `manager` (~16k), `owner` (~11k), `data_access_contact` (412), `slf_manager`,
  `lab_manager`, `administrator`. It is keyed to a person by `login`/`email`/`name`
  and linked to a study by `id_study_tmp`. **This is distinct from
  `study.faculty_sponsor`** (the named PI/sponsor text field). Worked example —
  "Carl Anderson": `faculty_sponsor LIKE '%Carl Anderson%'` → **91 studies**;
  via `study_users` he is `owner` of 59, `data_access_contact` of 5, `follower` of 5.
- `sample` has `supplier_name`, `accession_number`, `sanger_sample_id`, `name`.
- `iseq_product_metrics` has `qc`, `qc_lib`, `qc_seq`, `qc_user`; **`qc` is the
  manual-QC verdict** (1 = pass, 0 = fail, NULL = pending) — this is the
  "manual_qc" the users mean.
- `seq_product_irods_locations` has **no `id_run`** and **no file-type column**
  (columns: `id_product`, `seq_platform_name`, `pipeline_name`,
  `irods_root_collection`, `irods_data_relative_path`,
  `irods_secondary_data_relative_path`, `created`, `last_changed`). So **run-scope**
  must be obtained by joining `id_product` → the platform product-metrics
  (`id_iseq_product` → `id_run`), and **file type** ("cram") must be matched on the
  iRODS filename suffix (`irods_file_name`/`irods_data_relative_path` ending in
  `.cram`).

## Per-question verdict

| Q   | Question                                                | Verdict             | What's needed                                                                                   |
| --- | ------------------------------------------------------- | ------------------- | ----------------------------------------------------------------------------------------------- |
| 1   | data access groups for a study                          | **Handled**         | `Study.data_access_group` is mirrored/exposed; just ensure a **cheap** path surfaces it (D5)    |
| 2   | iRODS cram path for a run                               | **GAP**             | D1: run-scoped iRODS + file-type filter                                                         |
| 3   | study manifest (run_id, name, supplier_name, accession) | **GAP**             | D2: study data-manifest listing (paginated + count)                                             |
| 4   | manifest + iRODS cram path                              | **GAP**             | D2 with the iRODS path column (builds on D1's file-type filter)                                 |
| 5   | study id for `<name>`                                   | **Handled**         | `/search/study/:term` (+`/count`); ensure results carry disambiguation fields (D5/descriptions) |
| 6   | not-sequenced / sequenced / QC-passed                   | **GAP** (aggregate) | D3: study QC-dimension counts (the `qc` data is already mirrored)                               |
| 7   | studies for `<person>` / my studies                     | **Partial**         | faculty-sponsor case already searchable; D4: people→studies (faculty_sponsor + `study_users`)   |

## Deliverables (all firm)

### D1 — Run-scoped iRODS, with a file-type filter

- **New endpoint `GET /run/:id/irods`** (+ `GET /run/:id/irods/count`) returning the
  run's iRODS data objects as `IRODSPath`, scoped by joining
  `seq_product_irods_locations_mirror.id_iseq_product` →
  `iseq_product_metrics_mirror` on `id_run = :id` (Illumina NPG run id, as
  everywhere else). Paginated like the other iRODS lists (`limit`/`offset`,
  `X-Total-Count`/`X-Next-Offset`).
- **A file-type filter** query param (e.g. `file_type=cram` or `ext=cram`) on this
  endpoint **and on `/study/:id/irods` and `/sample/:id/irods`** (so "cram files for
  a study/sample" works too), matching the iRODS filename suffix (case-insensitive,
  e.g. `irods_file_name LIKE '%.cram'`). Define accepted values and behaviour for an
  unknown value (empty result vs 400 — settle in the spec; be consistent with the
  recency-param 400 conventions).
- **Add `id_run` to the `IRODSPath` result** (or a run-aware variant) so a row's run
  is visible — needed by D2/Q4. Be explicit where `id_run` is unknown (non-Illumina
  products) — see platform note.

### D2 — Study data manifest (the tabular listing for Q3/Q4)

- **New endpoint** (e.g. `GET /study/:id/manifest`) returning a **paginated** table
  of one row per sequencing product (run × lane × tag) for the study, each row
  carrying: sample `name`, `supplier_name`, `accession_number`, `sanger_sample_id`,
  `id_run` (and position/tag if cheap), and — when requested — the iRODS path to the
  data object, restricted by the **file-type filter** from D1 (so "+ cram path"
  is one switch). The study-level details (`data_access_group`, `faculty_sponsor`,
  `name`, `accession_number`) are returned **once** (header/envelope or via D5), not
  repeated per row.
- Provide a **`/count`** counterpart and the standard sizing headers. This list can
  be large (a study has thousands of products), so it MUST be bounded-by-default and
  pageable, and MUST be web-responsive via appropriate indexing (this is the same
  class as the per-platform-breakdown perf bug fixed earlier — avoid per-row
  correlated subqueries and full scans of the 9M-row mirrors; verify with EXPLAIN).
- Decide the row grain (per product vs per sample with a run list) in the spec; the
  user examples read as per (sample, run/product) rows. Keep it a single server-side
  join, not N calls.

### D3 — Study QC-dimension counts (Q6)

- Make "received / sequenced / passed manual QC" answerable in **one cheap call**.
  Add study-level counts across the QC dimension — e.g. extend `StudyOverview`
  and/or `StatusBreakdown` (or a new `GET /study/:id/qc-breakdown`) with:
  **received** (= `samples_total`), **sequenced** (distinct samples with ≥1
  product-metrics row in the study, any platform — i.e. `samples_total` − the
  `registered` bucket), **not-sequenced** (the `registered` bucket), and the QC
  split of the sequenced samples: **qc_pass** / **qc_fail** / **qc_pending** from the
  mirrored `qc` column. The historical worked example (MoBa, study 7699) had
  received ≈ 45277, sequenced ≈ 40795, manual-QC-passed ≈ 40012.
- The per-sample QC verdict MUST use the **same roll-up** `progress.go` already
  applies (precedence fail > pending > pass; `not_tracked` when no products) so the
  study counts and `SampleProgress.qc` cannot disagree. Counts must partition
  cleanly (document exactly what each bucket includes; ONT/registered-only samples
  are `not_tracked`/not-sequenced, never silently dropped). Per-platform QC counts
  are in scope if cheap.

### D4 — People → studies (Q7), with correct routing

- **"Studies for `<person name>`" (the named sponsor)** is already served by the
  `faculty_sponsor` substring match in `/search/study/:term`. Make this explicit and
  correct: ensure the description states faculty-sponsor is matched, and that the
  result/`/count` lets the caller see the total (≈91 for Carl Anderson). Consider a
  dedicated **`GET /studies/faculty-sponsor/:name`** (exact/substring) if it makes
  the intent and indexing clearer; add an index on `study.faculty_sponsor` in the
  mirror if it backs a first-class lookup.
- **"My studies" / "studies for person `<login/email>`"** needs the role-based
  membership: **mirror `study_users`** (new mirror table:
  `id_study_users_tmp`, `id_study_tmp`, `role`, `login`, `email`, `name`,
  `last_updated`; link to studies via `id_study_tmp` → `study_mirror`) and add a
  **studies-by-person endpoint** keyed on `login`/`email`/`name`, **role-filtered**
  by default to the meaningful roles (`owner`, `manager`; optionally
  `data_access_contact`) and excluding the noisy `follower` unless explicitly asked —
  with the role surfaced in results. Index the mirror on the lookup keys
  (`login`, `email`, `name`, `id_study_tmp`).
- **Document the routing** in the endpoint descriptions: a person-name query maps to
  `faculty_sponsor` (the sponsor/PI); a "my studies"/login query maps to
  `study_users` (role membership). They return different sets — make the distinction
  explicit so the MCP layer/agent can choose correctly.
- **Person resolution / directory (so a name can be translated to what's stored).**
  Names in the source are stored in forms a user won't type exactly:
  `faculty_sponsor` is free-text full names (e.g. "Carl Anderson"), and `study_users`
  identifies a person by `name` AND `login` (Sanger username, e.g. "ca3") AND
  `email` (e.g. "ca3@sanger.ac.uk"). So:
    - The studies-by-person lookups MUST match **case-insensitively** and **across
      `name`, `login`, and `email`** (substring), so an email/login query and a
      name query both work — a caller given only an email must not get a false empty
      result, and vice-versa.
    - Provide a **people-directory / resolve-person endpoint** that, given a partial
      term, returns the **distinct candidate people** with their canonical stored
      forms — distinct `faculty_sponsor` values, and distinct `study_users`
      `(name, login, email, role)` — plus a study count per candidate. This lets a
      caller translate a spoken/partial name ("Carl", "Anderson") into the exact stored
      value before (or instead of) running the studies query, and disambiguate when
      several people match. It must be cheap and bounded (+`/count`).
    - The descriptions MUST state these stored forms and the "match across
      name/login/email; if a narrow term yields nothing or is ambiguous, enumerate
      candidates rather than dead-ending" behaviour, since the downstream MCP layer
      relies on this text to guide the agent.

### D5 — Cheap study-metadata exposure (Q1, and disambiguation for Q5)

- Ensure `data_access_group`, `faculty_sponsor`, `name`, `accession_number` are
  available from a **cheap** study call (they are on the `Study` object via
  `/resolve/study` already; do NOT force callers to the giant `/study/:id/detail`).
  If `StudyOverview` is the natural cheap study call, consider surfacing these
  study-metadata fields there too so "data access groups for study X" and the study
  overview are one small response. Make the `/search/study` result rows carry enough
  to disambiguate (`id_study_lims`, `name`, `faculty_sponsor`) for Q5.

## CLI exposure (REQUIRED — part of every deliverable)

Each new endpoint's results MUST be reachable from the `wa mlwh` CLI
(`cmd/mlwh_info.go` and siblings), following the pattern established when
`wa mlwh info` was extended for the first realworld wave:

- **Extend `wa mlwh info <identifier>`** where the result is naturally "info about
  that identifier": e.g. the **study QC counts (D3)** and **study metadata /
  data-access groups (D1/D5)** as new sections of `wa mlwh info <study>`; run-scoped
  iRODS (D1) as a section of `wa mlwh info <run>` (respecting size — summarise/limit).
- **Add a new `wa mlwh <subcommand>`** where the result is NOT info about a single
  identifier:
    - iRODS paths filtered by file type → e.g. `wa mlwh irods <study|run|sample> [--file-type cram] [--limit/--offset]`.
    - the study manifest (D2) → e.g. `wa mlwh manifest <study> [--with-irods --file-type cram] [paging]` (tabular output; honour budget/paging).
    - people→studies (D4) → e.g. `wa mlwh studies --faculty-sponsor "<name>"` and `wa mlwh studies --user <login> [--role owner,manager]` (not an identifier-info call).
- The agent building this decides the exact CLI shape, but every new endpoint must be
  demonstrable via the CLI in both local-cache and `--server` modes, with graceful
  degradation (not-found/empty/not-tracked render cleanly, exit 0), matching the
  existing `wa mlwh info` behaviour.

## HARD REQUIREMENTS

1. **One call, small/bounded response; web-responsive (~1s).** Every count/overview/
   QC/data-access answer is a single call independent of study/run size; every list
   (manifest, run iRODS, studies-by-person) is bounded-by-default, pageable, and has
   a `/count` + sizing headers. No per-row correlated subqueries or full scans of the
   large mirrors — add the indexes the new query paths need and prove it with EXPLAIN
   (this is the same perf class as the per-platform-breakdown fix; do not regress it).
2. **Cache correctness.** New mirror columns/tables (`study_users`; any new indexes
   on `study_mirror`/iRODS/product-metrics; `id_run` linkage for run-scoped iRODS)
   must be added to the schema (both `sqlite` and `mysql` dialects, kept in parity),
   to `mlwh/sync.go`'s source selection, AND to the cold-load **sparse read-index
   set** where the mirror is large (the documented trap from prior fixes), with a
   `CacheSchemaVersion` bump or additive-index path as appropriate.
3. **Source-true semantics.** `qc` (manual QC) is `iseq_product_metrics.qc`
   (1/0/NULL → pass/fail/pending), rolled up identically to `progress.go`.
   `faculty_sponsor` (named sponsor) and `study_users` (role membership) are
   different and must not be conflated; "cram" is a filename-suffix match (no file-type
   column exists); run-scope for iRODS is via `id_product → product_metrics → id_run`.
4. **Platform-aware; never a false "no data".** Keep the uniform multi-platform
   treatment (Illumina/PacBio/Elembio/Ultimagen via product-metrics; ONT via
   `oseq_flowcell`). Where a platform lacks a capability (e.g. ONT has no
   product/iRODS/run, so no QC and no cram path), say so explicitly (the existing
   `not_tracked` / empty-platform conventions) — never collapse to a bare "no data"
   or a false zero.
5. **Timestamps not conflated** (carry the realworld discipline forward): iRODS
   `created` = data added; `last_changed` = sync key (not surfaced); `cache_synced_at`
   / `/freshness` = freshness caveat. Manifest/iRODS results are complete only up to
   the last sync.
6. **CLI exposure delivered** (above), in both local and `--server` modes, with
   graceful degradation.
7. **Tests.** TDD with behavioural tests; preserve all existing regression tests.
   Add **real-MySQL integration tests** (the `mlwh/cache_mysql_integration_test.go`
   pattern: unique throwaway DB, dropped on cleanup incl. failure, skipped when creds
   absent) asserting the new query paths execute on MySQL, are index-served
   (EXPLAIN), and return correct counts/rows; and a **source integration test** (the
   `mlwh/sync_source_integration_test.go` pattern) covering the new source columns/
   tables (`study_users`, any new study/sample/qc columns) so the schema the other
   tests assume stays true.
8. **Registry descriptions are the contract.** Each new endpoint's
   `Description`/`Summary`/`Query` in `mlwh/registry.go` must precisely state the
   definition used (what "sequenced"/"passed manual QC" mean, the faculty_sponsor
   vs study_users routing, the file-type-suffix semantics, run-scope linkage, and the
   freshness caveat), because the downstream MCP server surfaces this text verbatim.

## Design decisions for the spec to settle (HOW, not WHETHER)

- Exact new endpoint paths/names and whether D3 extends `StudyOverview`/
  `StatusBreakdown` or adds `/study/:id/qc-breakdown`; the manifest path/grain.
- The file-type filter param name and accepted values, and unknown-value behaviour.
- Whether people→studies is one endpoint with a mode/param or separate
  faculty-sponsor vs study-users endpoints; the default role filter for "my studies".
- The exact `wa mlwh` CLI surface (which results extend `info`, which become new
  subcommands) per the rule above.
- Index choices for each new query path (and the `CacheSchemaVersion`/additive
  decision).

## Pointers / prior art

- Endpoints + descriptions: `mlwh/registry.go`. Result types/json tags:
  `mlwh/types.go`. Availability/QC/breakdown logic: `mlwh/availability.go`,
  `mlwh/progress.go`. iRODS queries: `mlwh/hierarchy.go`. Sync source selection +
  cold-load read-index sets: `mlwh/sync.go`, `mlwh/cache.go`. Schema:
  `mlwh/cache_schema/{sqlite,mysql}/*.sql`. Client + paging: `mlwh/remote.go`,
  `mlwh/client.go`. CLI: `cmd/mlwh_info.go`. Integration-test patterns:
  `mlwh/cache_mysql_integration_test.go`, `mlwh/sync_source_integration_test.go`.
  Checklists for the bugfix workflow: `.docs/bugfixes/`.
- The first wave (reused, not rebuilt): `.docs/realworld/` (background only — code is
  authority).

## Notes

These decisions are settled and govern the spec. They resolve the open "HOW"
choices above; they are direct instructions, not open questions.

### Cache schema migration — bump CacheSchemaVersion (full resync)

- Bump `CacheSchemaVersion` from 10 to 11. The new `study_users_mirror` table and all
  new indexes (e.g. on `study_mirror.faculty_sponsor`, plus the iRODS/product-metrics
  indexes backing the new query paths) are created by the existing recreate-tables
  migration, which performs a full resync. A full resync is acceptable; do NOT take the
  additive `IF NOT EXISTS` no-version-bump path.

### D3 QC counts — extend StatusBreakdown (no new endpoint; defer per-platform)

- Do NOT add a `/study/:id/qc-breakdown` endpoint. Instead extend the existing
  `StatusBreakdown` (`/study/:id/status-breakdown`) response with study-level
  `qc_pass` / `qc_fail` / `qc_pending` counts computed over the **sequenced (distinct)
  samples**, so a single call returns received (= `samples_total`), sequenced
  (= `samples_total` − the `registered` bucket), not-sequenced (= the `registered`
  bucket) AND the QC split.
- The per-sample QC verdict MUST use the **same roll-up** `progress.go` /
  `SampleProgress.qc` already applies (precedence fail > pending > pass; `not_tracked`
  when no products), so study counts and per-sample QC cannot disagree. Reuse
  `StatusBreakdown`'s existing feeding-tables list and `cache_synced_at` / freshness
  machinery.
- **Per-platform QC counts are deferred** (not part of the Q6 ask, not clearly cheap).
  Leave the design so they can be added later without breaking the response shape.

### D4 people→studies — two endpoints + resolve-person directory

- Provide **two separate endpoints**, not one endpoint with a mode/source param, so each
  carries its own description and the faculty_sponsor-vs-study_users routing is
  self-documenting (the MCP layer cannot mis-set a param):
    - `GET /studies/faculty-sponsor/:name` (+ `/count`) — the named PI/sponsor; free-text
      case-insensitive substring match on `study.faculty_sponsor`.
    - `GET /studies/user/:person` (+ `/count`) — `study_users` role membership; matches
      case-insensitively across `name`, `login`, AND `email` (substring).
- **Default role filter for `/studies/user`** is `owner`, `manager`, and
  `data_access_contact` (the substantive "responsible-for" roles). **Exclude** `follower`
  and the operational roles `slf_manager`, `lab_manager`, `administrator` by default. A
  `role=` query param overrides the default set. Each result row MUST surface the matched
  `role`. The endpoint description MUST state the default role set so the agent can widen
  via `role=` when a user expects more.
- Keep the **resolve-person / people-directory endpoint** as its own endpoint that feeds
  both: given a partial term it returns the distinct candidate people with their canonical
  stored forms (distinct `faculty_sponsor` values; distinct `study_users`
  `(name, login, email, role)`) plus a study count per candidate, cheap and bounded
  (+ `/count`).

### File-type filter — open suffix, empty-result-not-error

- The file-type filter accepts **any token** (open vocabulary; no closed allow-list).
  Normalise the token: strip a single leading `.` and match case-insensitively as
  `irods_file_name LIKE '%.<token>'`.
- A **valid-but-unmatched** suffix returns an **empty result, not an error**. Return
  **400** only when the value is empty/whitespace or contains the SQL-wildcard / path
  characters `%`, `_`, or `/`.
- Endpoint descriptions MUST state it is a **filename-suffix** filter. The `/count`
  counterparts MUST honour the same filter so an empty result is distinguishable from
  "no data".

### D1 iRODS id_run representation

- Add `id_run` to `IRODSPath` as an **`int`**, populated via LEFT JOIN
  (`id_iseq_product` → `iseq_product_metrics_mirror.id_run`), with **`0` = not derivable**
  (non-Illumina or unmatched), matching the existing `RunStatusTimeline.IDRun` /
  `RunOverview.IDRun` "0 for non-Illumina" convention so the API stays internally
  consistent. Apply this on the study, sample, AND run iRODS rows.
- Document that `0` means unknown / non-Illumina, and also expose each row's **platform**
  so a `0` reads as "ONT / non-Illumina" rather than ambiguous.
