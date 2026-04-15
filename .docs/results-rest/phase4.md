# Phase 4: REST API Server

Ref: [spec.md](spec.md) sections E1, E2, E3, E4, E5, E6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 4.1: E1 - POST /results

spec.md section: E1

Implement `NewServer(store *Store,
validator *SeqmetaValidator) *Server` and
`(s *Server) Handler() http.Handler` in `results/server.go`.
Wire up chi router with POST /results endpoint. Parses JSON
Registration body, calls validator.ValidateMetadata (if
non-nil), then store.Upsert. Returns 201 on create, 200 on
upsert. Depends on Phase 2 (Store) and Phase 3 (Validator).
Covers all 7 acceptance tests from E1.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after item 4.1 is reviewed)

#### Item 4.2: E2 - GET /results (search) [parallel with 4.3, 4.4, 4.5, 4.6]

spec.md section: E2

Add GET /results handler to chi router in `results/server.go`.
Parses query parameters into SearchParams (including meta_X
and seqmeta_X patterns), calls store.Search, returns JSON
array. Covers all 5 acceptance tests from E2.

- [ ] implemented
- [ ] reviewed

#### Item 4.3: E3 - GET /results/{id} [parallel with 4.2, 4.4, 4.5, 4.6]

spec.md section: E3

Add GET /results/{id} handler in `results/server.go`. Calls
store.Get, returns JSON result set or 404. Covers all 2
acceptance tests from E3.

- [ ] implemented
- [ ] reviewed

#### Item 4.4: E4 - GET /results/{id}/files [parallel with 4.2, 4.3, 4.5, 4.6]

spec.md section: E4

Add GET /results/{id}/files handler in `results/server.go`.
Calls store.GetFiles, returns JSON file array or 404. Covers
all 2 acceptance tests from E4.

- [ ] implemented
- [ ] reviewed

#### Item 4.5: E5 - PUT /results/{id}/files [parallel with 4.2, 4.3, 4.4, 4.6]

spec.md section: E5

Add PUT /results/{id}/files handler in `results/server.go`.
Parses JSON array of FileEntry, calls store.ReplaceOutputFiles.
Returns 200 on success or 404/400 on error. Covers all 3
acceptance tests from E5.

- [ ] implemented
- [ ] reviewed

#### Item 4.6: E6 - DELETE /results/{id} [parallel with 4.2, 4.3, 4.4, 4.5]

spec.md section: E6

Add DELETE /results/{id} handler in `results/server.go`. Calls
store.Delete, returns 204 on success or 404. Covers all 2
acceptance tests from E6.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
