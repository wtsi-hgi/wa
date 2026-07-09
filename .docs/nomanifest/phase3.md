# Phase 3: Pagination completeness

Ref: [spec.md](spec.md) sections D1, D2, D3, F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: phase 1 (products export core). Per the spec this phase is
sequential after phase 1 and may run in parallel with phase 2; both
phase 2 and phase 3 edit `mlwh/export.go`, so if run concurrently,
serialize the `export.go` edits.

## Items

Items are sequential (D1-D3 all edit `mlwh/export.go` and
`mlwh/export_test.go`; F1 verifies the HTTP/remote surface and depends
on D1-D3). Mirror the existing iRODS keyset tests
`TestExportStudyIRODSBoundedPageTotalAndCursorD1a` and
`TestExportStudyIRODSAllUsesKeysetWithoutCappingRowsD1a`.

### Item 3.1: D1 - keyset bounded page returns Total, NextCursor, Complete

spec.md section: D1

Extend `exportProducts` (from phase 1) into a full keyset bounded page:
compute the filtered `Total`, query `limit + 1` rows by keyset, set
`more = len > limit`, trim, `Complete = !more`, and `NextCursor =
encodeExportCursor(lastRow.cursor())` when `more` (mirror `exportIRODS`).
Route the default request (no `Limit`, no `All`) through a bounded page
of `defaultExportAllLimit` (1000) via the existing `exportPaging`. Add
the helper `seedLargeProductExportScenario(t, db, n)`. Note: the cursor
-continuation test D1.2 also needs the cursor-decode plumbing from item
3.3 (D3), so implement them together. Covers all 3 acceptance tests
D1.1-D1.3 (Limit 50 -> 50 rows, Total 97, non-empty NextCursor,
Complete false; continuation -> remaining 47, Complete true, empty
NextCursor; default request over 1500 -> 1000 rows, Total 1500, non
-empty NextCursor, Complete false).

- [ ] implemented
- [ ] reviewed

### Item 3.2: D2 - --all streams the complete set, memory-bounded, no deep OFFSET

spec.md section: D2

Add `exportProductsAll` returning a `streamRows` closure (`Total: -1`,
`Complete: true`, `Rows: nil`) paged by keyset via a new
`streamExportProductsRows` (mirror `streamExportIRODSRows`), advancing
the cursor to the last row with no growing `OFFSET`; route products
through this keyset stream in `exportAll`, NOT through
`streamExportPages`. Covers both acceptance tests D2.1-D2.2 (120000 rows
rendered with `result.Rows` nil, `Total` -1, empty `NextCursor`,
`Complete` true, and heap growth under 20 MiB per the go-conventions
memory-bounded pattern; internal paging advances by
`(id_run, position, tag_index) > last` with no growing OFFSET). Uses
`seedLargeProductExportScenario`.

- [ ] implemented
- [ ] reviewed

### Item 3.3: D3 - cursor accepted for products, still rejected for other bounded kinds

spec.md section: D3

Extend `validateExportContinuationSupport` so `cursor` is accepted for
`products` (currently iRODS-only) and change its rejection message from
"only for iRODS exports" to "only for iRODS and products exports";
extend `newExportPlan` so the cursor is decoded for products
(`if kind == exportRelationshipIRODS || kind ==
exportRelationshipProducts`). Created-date `Sort`/`Since`/`Until` stay
rejected for products via the existing `newExportPlan` guard. Covers all
3 acceptance tests D3.1-D3.3 (products cursor succeeds; `samples of
study` cursor still returns the "only for iRODS and products exports"
`ErrUnsupportedIdentifier`; `Sort`/`Since`/`Until` on products return
`ErrUnsupportedIdentifier`).

- [ ] implemented
- [ ] reviewed

### Item 3.4: F1 - GET /export/products/study/:id serves bounded and complete responses

spec.md section: F1

Verify `GET /export/products/study/:id` serves bounded (`limit`/
`cursor`) and complete (`all=true`) responses through the existing
generic Export handler and `materializeMLWHExportResult` (no server
handler change beyond the export layer). Confirm `remoteExportCanPageAll`
already covers products; add a dedicated products-export local/remote
parity test (keep the existing `irods` Export parity entry for
acceptance test 10); confirm the `RemoteClient` `all=true` stream pages
by `Cursor` (unsorted, `NextCursor` present) with no growing `OFFSET`.
Covers all 5 acceptance tests F1.1-F1.5. Test files:
`mlwh/server_test.go`, `mlwh/remote_test.go`, `mlwh/parity_test.go`.

- [ ] implemented
- [ ] reviewed
