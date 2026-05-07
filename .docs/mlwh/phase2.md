# Phase 2: Resolvers

Ref: [spec.md](spec.md) sections A4, B1, B2, B3, B4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 2.1: A4 - Cold-cache lazy sync for resolver-backed tables

spec.md section: A4

Extend `mlwh/sync.go` so `ResolveLibrary` and the `donor_id` step of
`ResolveSample` trigger a full sync of `iseq_flowcell` / `sample`
respectively on a cold cache, blocking until the sync transaction
commits. Warm cache must skip `Sync`. Covers all 3 acceptance tests
from A4. Depends on Phase 1.

- [x] implemented
- [x] reviewed

### Batch 1 (parallel, after item 2.1 is reviewed)

#### Item 2.2: B1 - ResolveSample cascade [parallel with 2.3, 2.4]

spec.md section: B1

Implement `(*Client).ResolveSample` in `mlwh/resolver.go` plus
`mlwh/resolver_reject.go` for the LIMS-provider rejection set. Indexed
cascade: UUID, `id_sample_lims`, `name`, `sanger_sample_id`,
`supplier_name`, `accession_number`, `donor_id` (cache). Negative
cache on miss; `ErrUpstreamImpaired` on non-client errors. Covers all
8 acceptance tests from B1.

- [x] implemented
- [x] reviewed

#### Item 2.3: B2 - ResolveStudy cascade [parallel with 2.2, 2.4]

spec.md section: B2

Implement `(*Client).ResolveStudy` with `ResolveStudyOption` and
`WithCaseInsensitiveStudyName`. Cascade: UUID, `id_study_lims`,
`accession_number`, `name` (case-sensitive by default). Multiple
text matches return `ErrAmbiguous` naming both LIMS IDs. Covers all 7
acceptance tests from B2.

- [x] implemented
- [x] reviewed

#### Item 2.4: B3 - ResolveRun and ResolveLibrary [parallel with 2.2, 2.3]

spec.md section: B3

Implement `(*Client).ResolveRun` (numeric, `iseq_product_metrics`
existence check) and `(*Client).ResolveLibrary` (cache-backed exact
match on `pipeline_id_lims`, with cold-cache lazy sync). Doc comment
on `ResolveLibrary` must mention "first call" and "wa mlwh sync".
Covers all 7 acceptance tests from B3.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).

### Item 2.5: B4 - ClassifyIdentifier dispatch

spec.md section: B4

Implement `(*Client).ClassifyIdentifier` dispatching by input shape
(UUID, integer, text), applying study-before-sample-before-run-
before-library priority within each shape, short-circuiting on the
LIMS-provider rejection set, and propagating `ErrUpstreamImpaired`.
Depends on items 2.2, 2.3, 2.4. Covers all 6 acceptance tests from B4.

- [x] implemented
- [x] reviewed
