# Phase 6: Wiring + CLI + docs (G1-G2, H1-H4)

Ref: [spec.md](spec.md) sections G1, G2, H1, H2, H3, H4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (the four-step add-a-query recipe,
verbatim-for-MCP Descriptions, the no-drift guards, and the CLI
graceful-degradation pattern: not-found/empty/not-tracked render cleanly
and exit 0).

Per the spec, the Registry/handler/RemoteClient wiring for each endpoint
is done incrementally within Phases 2-5. This phase first verifies that
wiring is complete and consistent across ALL new endpoints and bumps
`APIVersion` (G1), then exposes every new endpoint through the `wa mlwh`
CLI in BOTH local-cache and `--server` modes (H1-H4), and finally
regenerates the docs and extends the glossary with the drift guards green
(G2). Each CLI command opens its client via the existing
`openMLWHInfoConfiguredClient` shape (server URL -> RemoteClient, else
local cache via `resolveMLWHInfoLocalConfig`), supports `--json`, and
follows the `mlwhInfoClient` / `mlwhSearchClient` interface-subset
pattern. Do NOT run a live warehouse; the relevant tests are hermetic
(`registry_test.go`, `server_test.go`, `remote_test.go`, the `cmd/`
tests, `docs_test.go`) and the doc regeneration is a guarded `go test`
run.

## Items

### Batch 1 (sequential foundation)

The Registry/wiring completeness pass must finish (and `APIVersion` must
be bumped) before the CLI commands consume the new `RemoteClient` /
`Client` methods and before the docs are regenerated.

#### Item 6.1: G1 - Registry entries + handler cases + remote methods + Queryer members (verify + complete)

spec.md section: G1

For each new endpoint added across Phases 2-5, confirm the full four-step
wiring exists and is consistent: the `Queryer` member (`mlwh/queryer.go`),
the `Client` method, the `RemoteClient` method (and the `Page[T]`
variants `IRODSPathsForRunPage`, `StudiesForFacultySponsorPage`,
`StudiesForUserPage`, `ResolvePersonPage`; the manifest is an envelope so
it uses the plain `remoteCall`) (`mlwh/remote.go`), the `Registry`
`Endpoint` with a non-empty Summary and a verbatim-for-MCP Description
(`mlwh/registry.go`), and the `server.go` handler `case`. Fill any gaps.
New paginated entries declare integer limit/offset `QueryParams`
(`fetchAllPaginationParams()` for run iRODS / manifest;
`searchPaginationParams()` for the people lists) plus the `file_type` /
`with_irods` / `role` query params. The `file_type` param is added to the
existing `IRODSPathsForStudy`/`IRODSPathsForSample` (+ `/count`) entries
plus `IRODSPathsForRun`; `with_irods`+`file_type` to `StudyManifest`;
`role` to `StudiesForUser` -- the existing entries keep their `Method`
names. Extend the closed-set guard `newAvailabilityRecencyProgressMethods()`
(`registry_test.go`) with the new Method names (preserve the existing
list). Bump `APIVersion` to 1.7.0 (`mlwh/openapi.go`). Covering all 5
acceptance tests from G1 (every new Method has non-empty
Summary/Description, every new paginated entry declares integer
limit/offset QueryParams, and `TestRegistryCoversQueryer` passes; each
new endpoint's handler returns the same typed value as its `Client`
method with no panic and a paginated list sets the sizing headers; each
new endpoint round-trips via `RemoteClient` to the same typed result and
the new `Page[T]` variants return `Total`/`NextOffset` matching the
headers; `APIVersion == "1.7.0"`;
`TestRegistryRecencyDescriptionsCiteCreationTimestampG1` still passes).
Depends on Phases 2-5 (all endpoints implemented).

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after batch 1 is reviewed)

The four CLI items touch mostly disjoint files: H1 edits an existing
file (`cmd/mlwh_info.go`); H2/H3/H4 each add a new command file
(`cmd/mlwh_irods.go` / `cmd/mlwh_manifest.go` / `cmd/mlwh_studies.go`),
each wired in `cmd/mlwh.go`. They share only the small registration
touch-point in `cmd/mlwh.go`, so implement them as parallel subagents but
have the reviewer confirm the `cmd/mlwh.go` registrations do not clash.
All depend on Batch 1 (the finalized `Client`/`RemoteClient` methods).

#### Item 6.2: H1 - extend `wa mlwh info` [parallel with 6.3, 6.4, 6.5]

spec.md section: H1

In `cmd/mlwh_info.go`: on `info <study>`, surface the overview's
`data_access_group`/`faculty_sponsor`/`name`/`accession_number` (D5) and
the breakdown's `qc` (qc_pass/qc_fail/qc_pending) plus derived
received/sequenced/not-sequenced (D3) -- no `mlwhInfoClient` change
beyond the new struct fields (`StudyOverview`/`StatusBreakdown` already
carry them). On `info <run>`, add a run-scoped iRODS section (D1),
summarised/limited (`infoMaxRelated`), via a new `mlwhInfoClient` method
`IRODSPathsForRun(ctx, idRun, "", limit, offset)`, rendering path +
`id_run` + `platform`; an empty result prints "none" (exit 0). Test in
`cmd/mlwh_info_test.go`. Covering all 4 acceptance tests from H1
(`info 7699 --type study` shows received 45277 / sequenced 40795 /
not-sequenced 4482 and qc_pass 40012 / qc_fail 200 / qc_pending 583;
`info <study>` shows `data_access_group="grp-1"`; `info 52553 --type run`
lists the capped run iRODS paths with id_run/platform, and a run with no
objects renders "none" exit 0; a never-synced cache in `--server` mode
degrades gracefully -- neutral message, no sync hint, exit 0). Depends on
Batch 1 and Phases 2/4.

- [x] implemented
- [x] reviewed

#### Item 6.3: H2 - `wa mlwh irods` subcommand [parallel with 6.2, 6.4, 6.5]

spec.md section: H2

Add `cmd/mlwh_irods.go` (wired in `cmd/mlwh.go`): `wa mlwh irods
<study|run|sample> <identifier> [--file-type cram] [--limit N --offset M]
[--server URL] [--json]`. The first positional selects the scope (or
auto-detect by resolving); dispatch to `IRODSPathsForStudyByFileType` /
`IRODSPathsForRun` / `IRODSPathsForSampleByFileType`. Tabular text output
(path, id_run, platform); `--json` emits the `[]IRODSPath`. An
empty/unmatched result prints a "no matching iRODS paths" line and exits
0; a never-synced cache degrades like `info`; an invalid file type
(`--file-type a/b`) yields a bad-request-class error -> a clear message
and a NON-zero exit (input error, not a degradation). Test in
`cmd/mlwh_irods_test.go`. Covering all 4 acceptance tests from H2 (`irods
study S1 --file-type cram` prints 2 cram paths with id_run/platform, exit
0; `irods run 52553 --file-type bam` empty -> "no matching iRODS paths"
exit 0; `--json` emits one `[]IRODSPath` array; `--file-type a/b`
bad-request -> clear message, non-zero exit). Depends on Batch 1 and
Phase 2.

- [x] implemented
- [x] reviewed

#### Item 6.4: H3 - `wa mlwh manifest` subcommand [parallel with 6.2, 6.3, 6.5]

spec.md section: H3

Add `cmd/mlwh_manifest.go` (wired in `cmd/mlwh.go`): `wa mlwh manifest
<study> [--with-irods --file-type cram] [--limit N --offset M] [--server
URL] [--json]`; dispatch to `StudyManifest`. Tabular text: a header line
with the study metadata (name / accession / faculty_sponsor /
data_access_group) once, then one line per row (`name`, `supplier_name`,
`accession_number`, `sanger_sample_id`, `id_run`, `lane`, `tag_index`,
and `irods_path` when `--with-irods`). Honour paging; print the total if
available; empty `rows` prints the header + "no products", exit 0. Test
in `cmd/mlwh_manifest_test.go`. Covering all 4 acceptance tests from H3
(`manifest S1` prints metadata once + 3 row lines with the per-row
fields, exit 0; `--with-irods --file-type cram` includes each row's
`irods_path` with empty rendered as a placeholder like `-`; `--json`
emits one `StudyManifest` object; a synced study with no products prints
the header + "no products" exit 0, and a never-synced cache degrades
gracefully). Depends on Batch 1 and Phase 3.

- [x] implemented
- [x] reviewed

#### Item 6.5: H4 - `wa mlwh studies` and `wa mlwh people` subcommands [parallel with 6.2, 6.3, 6.4]

spec.md section: H4

Add `cmd/mlwh_studies.go` (wired in `cmd/mlwh.go`): `wa mlwh studies
--faculty-sponsor "<name>"` -> `StudiesForFacultySponsor`; `wa mlwh
studies --user <login> [--role owner,manager]` -> `StudiesForUser`;
exactly one of `--faculty-sponsor`/`--user` is required (error if
both/neither). Text output: one line per study (`id_study_lims`, `name`,
`faculty_sponsor`, and `role` in user mode) plus the total count; `--json`
emits `[]PersonStudy`. `wa mlwh people <term>` -> `ResolvePerson`: one
line per candidate (`source`, `name`, `login`, `email`, `role`,
`study_count`); `--json` emits `[]PersonCandidate`; empty results print
"no matches", exit 0. Both follow the `--server`/local +
graceful-degradation pattern. Test in `cmd/mlwh_studies_test.go`.
Covering all 5 acceptance tests from H4 (`studies --faculty-sponsor carl`
prints 3 study lines with the total and faculty_sponsor, exit 0;
`studies --user ca3 --role owner,manager` prints 3 lines each with its
role, exit 0; neither/both of the flags -> a clear usage error and
non-zero exit; `people carl` prints both the faculty_sponsor and the
study_users candidate lines with source / stored form / study_count, exit
0; a never-synced cache via `--server` degrades gracefully -- neutral
"cache not available" message, no sync hint, exit 0). Depends on Batch 1
and Phase 5.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 3 (after batch 2 is reviewed)

The docs regeneration consumes the finalized Registry from Batch 1. It is
technically independent of the CLI (Batch 2) -- it depends only on G1 --
but is placed last so the phase's final state has all wiring, CLI, and
docs consistent in one pass. The orchestrator MAY run 6.6 in parallel
with Batch 2 if it prefers (both depend only on Batch 1).

#### Item 6.6: G2 - regenerate docs; drift guards green; glossary updated

spec.md section: G2

Run `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference` to
rewrite `.docs/mcp/api-reference.md`. Add glossary entries
(`.docs/mcp/glossary.md`) for "data manifest", "file-type filter
(filename suffix)", "faculty sponsor", "study_users / role membership",
"manual QC", and "data access group". Test in `mlwh/docs_test.go`.
Covering all 3 acceptance tests from G2
(`TestEndpointReferenceAndOpenAPICoverSamePathsG1` shows the reference
and OpenAPI cover the same Registry paths; the committed reference matches
`EndpointReference()` after regeneration; the glossary defines "data
manifest" and "file-type filter"). Depends on 6.1 (the Registry must be
complete before regenerating the reference).

- [x] implemented
- [x] reviewed

## Ordering and dependency notes

- This phase depends on Phases 2-5 being fully reviewed (all new
  endpoints implemented and incrementally wired).
- Batch 1 (G1) is the sequential foundation: verify/complete the Registry
  and wiring surface and bump `APIVersion` to 1.7.0. Regenerating docs or
  building the CLI before the Registry is complete would produce drift or
  consume missing methods.
- Batch 2 (H1-H4) is parallel CLI exposure, after G1; the four items
  touch mostly disjoint files (H1 edits `cmd/mlwh_info.go`; H2/H3/H4 add
  new command files), sharing only the registration in `cmd/mlwh.go` --
  reviewers confirm the registrations do not clash.
- Batch 3 (G2) regenerates the docs from the finalized Registry; it
  depends only on G1, so the orchestrator may run it in parallel with
  Batch 2.
- Per the spec, most Registry/handler/RemoteClient wiring was done
  incrementally in Phases 2-5; 6.1 is primarily a completeness/
  consistency pass plus the cross-cutting assertions (Summary/Description
  present, round-trip parity, recency-timestamp wording) and the version
  bump.
