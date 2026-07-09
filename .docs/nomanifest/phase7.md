# Phase 7: Docs

Ref: [spec.md](spec.md) sections G1, K1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: phase 6 (the registry must be final before the Export
`Description` is edited and `api-reference.md` is regenerated). This is
the last phase.

## Items

Items are sequential. Item 7.1 regenerates `.docs/mcp/api-reference.md`
by running the `mlwh` test package with `WA_REFRESH_DOCS`, which compiles
and executes `mlwh/docs_test.go`; item 7.2 EDITS that same
`mlwh/docs_test.go` glossary-concept test, so the two cannot run
concurrently in a shared tree (a regeneration run would compile the file
while 7.2 is mid-edit). Run 7.1 first, then 7.2. Within 7.1 the
`api-reference.md` regeneration runs AFTER the registry
`Description`/`cursor` edits; within 7.2 the glossary.md "Data manifest"
removal and its `docs_test.go` glossary-test update land together as one
green step, so no step leaves the glossary-concept test RED. The phase
ends GREEN after 7.2.

### Item 7.1: G1.5 - Export Description edit and api-reference regeneration

spec.md section: G1 (acceptance test G1.5)

Append a products clause to the single `Export` registry entry
`Description` (`mlwh/registry.go` ~560): product-grained (one row per
distinct `(id_run, position, tag_index)`, including products with no
iRODS object), keyset-cursor paginated, default response a bounded page
(up to the internal 1000 default) carrying `Total`/`NextCursor`/
`Complete`, `cursor` to continue, `all=true` for the complete set,
`file_type` only restricts the attached `irods_path`; reconcile the
entry's existing "cursor for iRODS keyset pagination" phrase to cover
iRODS/products. Generalise the shared `cursor` query-param description
(~1243) from "a previous iRODS export page" to cover "a previous iRODS
or products export page". Do NOT edit `fetchAllPaginationParams()`'s
`limit` wording (~1296; out of scope, ~20 shared endpoints). Regenerate
`.docs/mcp/api-reference.md` by running `TestWriteEndpointReference` with
`WA_REFRESH_DOCS` set, AFTER the `Description`/`cursor` edits above so the
regenerated reference reflects only this phase's registry changes and
keeps the api-reference no-drift test green; the phase itself ends green
after item 7.2. Phase 6 already regenerated the reference for its manifest
removal, so that manifest-removal drift is not re-handled here.
Covers acceptance test G1.5 (the committed reference is byte-equal to
`EndpointReference()`, contains no `manifest` section, and its Export
section documents the products relationship's keyset/bounded-page
semantics). Test file: `mlwh/docs_test.go`.

- [x] implemented
- [x] reviewed

### Item 7.2: K1 - README and glossary present products, not manifest

spec.md section: K1

Update `README.md`: remove every `wa mlwh manifest` reference and
document `wa mlwh export products study <id>` as product-grained
(includes products with no iRODS path), `export irods` as
file-object-grained, and `export sample-crams` as sample-grained (K1.1).
Update `.docs/mcp/glossary.md`: remove the "Data manifest" concept
section, its anchor, and all `[data manifest](#data-manifest)`
cross-references (no dangling anchors), drop `/study/:id/manifest` from
the listing, and document product export as a new glossary term (K1.2).
In the SAME step as the glossary.md edit, update the glossary-concept
test `TestGlossaryDefinesPeopleAndManifestConceptsG2` in
`mlwh/docs_test.go`: drop the `"data manifest"` term assertion and
instead assert the new product-export term the glossary now defines
(e.g. a `Product export` heading, asserted as `"product export"`),
keeping the `"file-type filter (filename suffix)"`, `"faculty sponsor"`,
`"study_users / role membership"`, `"manual qc"`, and `"data access
group"` assertions; the test may be renamed to reflect product export
(K1.3). Landing the glossary.md removal and this test update together
keeps the suite GREEN - removing the "Data manifest" heading without the
test change would turn the glossary-concept test RED. Covers acceptance
tests K1.1-K1.3. K1.1 and K1.2 are verified by reading/grep over
`README.md` and `.docs/mcp/glossary.md`; K1.3 by the glossary-concept
test in `mlwh/docs_test.go`. Test file: `mlwh/docs_test.go`.

- [x] implemented
- [x] reviewed
