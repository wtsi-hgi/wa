# Phase 4: seqmeta Repointing

Ref: [spec.md](spec.md) sections D1, D2, D3, D4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 4.1: D1 - Provider interface and types swap

spec.md section: D1

Rewrite `seqmeta/provider.go` and `seqmeta/types.go` so `Provider`
embeds `mlwh.Querier` and the hierarchy/resolver methods seqmeta uses;
swap `saga.Study`/`saga.MLWHSample`/`saga.IRODSFile` for the `mlwh`
equivalents; rename `IdentifierType` constants to mirror
`IdentifierKind`; delete `Project`, `Users`, `HopProject`, `HopUsers`,
`IdentifierProjectName`. Remove every `saga` import in `seqmeta/`.
Covers all 3 acceptance tests from D1.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after item 4.1 is reviewed)

#### Item 4.2: D2 - Validate via mlwh.ClassifyIdentifier [parallel: 4.3, 4.4]

spec.md section: D2

Rewrite `seqmeta/validate.go` so `Validate` invokes
`Provider.ClassifyIdentifier` and maps `Match` to `IdentifierResult`;
preserve the existing 404 mapping for `ErrUnknownIdentifier` and the
502 mapping for `ErrUpstreamImpaired`. Covers all 7 acceptance tests
from D2.

- [ ] implemented
- [ ] reviewed

#### Item 4.3: D3 - Enrichment graph from mlwh details [parallel: 4.2, 4.4]

spec.md section: D3

Rewrite `seqmeta/enrich.go` to build `EnrichmentGraph` directly from
`mlwh.SampleDetail`/`StudyDetail`/`RunDetail`/`LibraryDetail`. Enforce
`MaxSamplesPerHop = 1000` truncation: set `partial = true`, append
`MissingHop{Hop, Reason: ReasonSamplesTruncated}`, truncate samples.
Drop `project`/`users` keys from JSON. Covers all 4 acceptance tests
from D3.

- [ ] implemented
- [ ] reviewed

#### Item 4.4: D4 - Diff routes through cache [parallel: 4.2, 4.3]

spec.md section: D4

Rewrite `seqmeta/diff.go` and `seqmeta/server.go` so
`/diff/study/...` and `/diff/sample/...` route through
`mlwh.AllStudies`, `mlwh.SamplesForStudy`, and
`mlwh.IRODSPathsForSample`. Emit the four-field iRODS shape only
(`id_product`, `collection`, `data_object`, `irods_path`). Preserve
existing watermark/hash logic in `seqmeta/store.go`. Covers all 7
acceptance tests from D4.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
