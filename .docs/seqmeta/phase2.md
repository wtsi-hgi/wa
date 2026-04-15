# Phase 2: Watermark Store

Ref: [spec.md](spec.md) sections B1, B2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 2.1: B1 - Open and create store

spec.md section: B1

Implement `OpenStore` and `Store.Close` in `seqmeta/store.go`.
Auto-create the `watermarks` SQLite table on open using
`modernc.org/sqlite`. Support both file-backed and in-memory
(`:memory:`) paths. Wrap open failures in `ErrStoreOpen`. Covering
all 5 acceptance tests from B1.

- [x] implemented
- [x] reviewed

### Item 2.2: B2 - Save and load entries

spec.md section: B2

Implement `Store.LoadEntries` and `Store.SaveEntries` in
`seqmeta/store.go`. `SaveEntries` upserts entries by
`(query_key, entry_id)` primary key; existing entries for the
same query key not present in the new map are left unchanged.
`LoadEntries` returns entries filtered by query key. Covering all
6 acceptance tests from B2, including tombstone persistence and
cross-key isolation.

- [x] implemented
- [x] reviewed
