# Phase 6: Manifest surface removal

Ref: [spec.md](spec.md) sections G1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: phases 4 and 5 (the CLI and `wa mlwh info` must already be
rewired off the manifest surface, so deleting it does not break the
compile).

## Items

### Item 6.1: G1 - manifest CLI, routes, registry, and metadata are gone

spec.md section: G1

Remove the manifest surface. Production removals: `cmd/mlwh.go` line 378
`newMLWHManifestCommand()` registration; the whole
`cmd/mlwh_manifest.go`; `mlwh/types.go` `StudyManifest`,
`PagedStudyManifest`, `ManifestRow`, and the `products_without_irods`
field; `mlwh/registry.go` the `StudyManifest` (~433-444) and
`CountStudyManifest` (~445-454) entries and `manifestQueryParams()`
(~1189-1198); `mlwh/server.go` the `StudyManifest`/`CountStudyManifest`
handler cases, `writeMLWHStudyManifest`, and `studyManifestTotal`;
`mlwh/remote.go` `StudyManifest`, `StudyManifestPage`,
`CountStudyManifest`, and `remoteManifestQuery`; `mlwh/queryer.go` the
`StudyManifest` and `CountStudyManifest` interface methods; `mlwh/count.go`
`CountStudyManifest` and `countStudyManifestForEmptyStudy`; and the
residual `StudyManifest` method, envelope helpers, and
`countManifestProductsWithoutIRODS` in `mlwh/manifest.go`. Do NOT delete
the reused builders (product-grain SQL, `scanManifestRow` logic,
`countStudyManifestProducts`, `manifestEmptyRequiredSyncTables`) already
refactored into the products export in phase 1. Tests: delete
`cmd/mlwh_manifest_test.go`; drop the two manifest entries from the
`mlwh/parity_test.go` table; update `mlwh/count_test.go`,
`mlwh/registry_test.go`, `mlwh/server_test.go`, `mlwh/types_test.go`,
`mlwh/cache_mysql_integration_test.go`, and `cmd/mlwh_info_test.go`.
Covers acceptance tests G1.1-G1.4 (CLI unknown-command error;
`/study/S1/manifest` and `/study/S1/manifest/count` return 404; the
registry has no `StudyManifest`/`CountStudyManifest` entry; the OpenAPI
document has no `/study/:id/manifest` paths). Test files:
`cmd/mlwh_test.go`, `mlwh/server_test.go`, `mlwh/registry_test.go`,
`mlwh/openapi_test.go`.

Regenerate `api-reference.md` in THIS phase (green boundary): removing the
`StudyManifest`/`CountStudyManifest` registry entries changes runtime
`EndpointReference()`, so the committed `.docs/mcp/api-reference.md`
(compared byte-for-byte by `TestEndpointReferenceMatchesCommittedDocumentG1`
in `mlwh/docs_test.go`) drifts and that no-drift test goes RED.
go-conventions requires each phase to end GREEN, so AFTER the registry
removals this phase MUST regenerate the fixture: run
`TestWriteEndpointReference` with `WA_REFRESH_DOCS` set, then re-run
`TestEndpointReferenceMatchesCommittedDocumentG1` and confirm it passes. The
regenerated fixture has no `manifest` section; its `Export` section is not
yet edited (that is phase 7), so the diff here is scoped to the two removed
manifest sections.

Note on G1.5: G1.5's full acceptance (the regenerated `api-reference.md`
also documenting the products relationship's keyset/bounded-page semantics)
completes in phase 7, which edits the single `Export` entry `Description`
and the shared `cursor` param and regenerates `api-reference.md` again.
Regenerating in both phases is expected: each registry-mutating phase
regenerates its own fixture so every phase boundary stays GREEN.

- [ ] implemented
- [ ] reviewed
