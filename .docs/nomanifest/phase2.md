# Phase 2: Filters

Ref: [spec.md](spec.md) sections C1, C2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: phase 1 (products export core). Per the spec this phase is
sequential after phase 1 and may run in parallel with phase 3; both
phase 2 and phase 3 edit `mlwh/export.go`, so if run concurrently,
serialize the `export.go` edits.

## Items

Items are sequential (both edit `validateExportFilterSupport` and the
products page + `Total` query). All filters must be applied identically
to the page query and the total count so `Total == len(all matching
rows)`, and none may drop a product for lacking an iRODS object.

### Item 2.1: C1 - qc, organism, library_type reduce Total

spec.md section: C1

Extend `validateExportFilterSupport` so `products` supports `qc`,
`library_type`, and `organism`. `qc` is a `HAVING` over the same grouped
aggregates that feed `qcRollupString` (`fail` -> `MIN(ipm.qc)=0`;
`pending` -> not fail and any NULL qc; `pass` -> `MIN(ipm.qc)=1` with no
NULL qc); when `qc` is set the `Total` count must be `COUNT(*)` over the
grouped `HAVING` subquery, NOT the `SELECT DISTINCT`
`manifestProductGrainDistinctSQL` shape. `library_type` -> a
study-scoped `library_samples` EXISTS on `pipeline_id_lims` (`WHERE`);
`organism` -> `sm.common_name` resolved via `exportOrganismCommonNames`
(`WHERE`). Covers all 4 acceptance tests C1.1-C1.4 (qc fail/pending/pass
each reduce to 1 and sum to the unfiltered 3; organism WHERE reduces
`Total`; library_type reduces `Total`). Reuse `seedManifestS1Scenario`.

- [x] implemented
- [x] reviewed

### Item 2.2: C2 - deliverables_only uses the product entity_type discriminator

spec.md section: C2

Extend `validateExportFilterSupport` and the products page + `Total` so
`deliverables_only` filters by the product's OWN deliverable
discriminator: an EXISTS/join on the product's flowcell
(`iseq_flowcell.entity_type IN ('library', 'library_indexed')`), NOT the
iRODS `is_deliverable` flag, and never dropping an objectless product;
apply it identically to page and count (a predicate that DOES change
`Total`). Add the deliverables-scenario seed fixture per C2.1 (3
Illumina products: 2 deliverable including one with no iRODS object, 1
control). Covers both acceptance tests C2.1-C2.2 (`DeliverablesOnly` nil
-> 3, true -> 2 with the objectless deliverable still present;
`file_type=cram` with the filter still yields `Total` 2 because
`file_type` never changes `Total`).

- [x] implemented
- [x] reviewed
