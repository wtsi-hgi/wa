# Phase 6: frontend

Ref: [spec.md](spec.md) sections A3, A4, E5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills (this repo's frontend is Next.js;
ignore the FastAPI part of the skill name).

Depends on Phase 3 (the `mlwh` server endpoints exist). Independent of
Phase 5. A3 and A4 both touch `frontend/app/(results)/actions.ts`, so
they are done sequentially in one subagent; E5 (file renames) follows.

## Items

### Item 6.1: A3 - frontend Server Actions hit the mlwh server

spec.md section: A3

In `frontend/lib/backend-client.ts`, add `"WA_MLWH_BACKEND_URL"` to
`BackendEnvVar`, drop `"WA_SEQMETA_BACKEND_URL"`, and replace
`seqmetaJson` with `mlwhJson(path, schema)` (service `"mlwh"`,
`WA_MLWH_BACKEND_URL`). In `frontend/app/(results)/actions.ts`, repoint
`validateIdentifier` -> `GET /classify/:id`, `enrichIdentifier`/
`enrichIdentifiers` -> `GET /enrich/:id`, `fetchStudySamples` ->
`GET /study/:id/samples`. In `frontend/lib/studies-cache.ts`,
`GET /studies`. 404 still maps to null; parse the `{code, message}`
envelope. Test files `frontend/tests/actions.test.ts`,
`frontend/tests/backend-client.test.ts`,
`frontend/tests/studies-cache.test.ts`. Covering all 5 acceptance tests
from A3.

- [x] implemented
- [x] reviewed

### Item 6.2: A4 - split study-samples endpoint selection in the frontend

spec.md section: A4

Update `fetchStudyLibrarySamples(studyId, libraryType, {libraryId,
idLibraryLims})` in `frontend/app/(results)/actions.ts` to select:
`idLibraryLims` set -> `GET /library-lims-id/:idLibraryLims/samples`;
else `libraryId` set -> `GET /library-id/:libraryId/samples`; else
`GET /library/:libraryType/study/:studyId/samples`; parsing
`enrichmentSamplesSchema`. Tests in `frontend/tests/actions.test.ts`.
Covering all 3 acceptance tests from A4.

Depends on Item 6.1 (same file).

- [x] implemented
- [x] reviewed

### Item 6.3: E5 - frontend file/component/contract renames

spec.md section: E5

Rename `lib/seqmeta-cache-core.ts` -> `lib/mlwh-cache-core.ts`,
`lib/seqmeta-cache.ts` -> `lib/mlwh-cache.ts`,
`lib/seqmeta-cache-server.ts` -> `lib/mlwh-cache-server.ts`,
`lib/seqmeta-enrichment.ts` -> `lib/mlwh-enrichment.ts`,
`components/seqmeta-badge.tsx` -> `components/mlwh-badge.tsx`; rename the
`SeqmetaCache` class -> `MLWHCache` (cookie name constant value
unchanged, exported symbol renamed). Do NOT rename `seqmeta-keys.ts` or
the `seqmeta_*` metadata-key strings. Update imports repo-wide and the
corresponding vitest suites. Covering all 3 acceptance tests from E5.

Depends on Items 6.1-6.2.

- [x] implemented
- [x] reviewed
