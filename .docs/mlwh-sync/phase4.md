# Phase 4: Sample fan-out across studies / libraries

Ref: [spec.md](spec.md) sections C2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 4.1: C2 - Per-pairing sample fan-out across all consumers

spec.md section: C2

Introduce `loadSampleFanOut(ctx, c, []int64) (map[int64][]Library,
map[int64][]Study, error)` walking `library_samples` and use it
from hierarchy methods that populate `Sample.Studies` /
`Sample.Libraries`. Update every consumer per the architecture
audit table:

- `cmd/mlwh_info.go` - per-pairing `library: <pipeline> /
<id_study_lims>` lines.
- `seqmeta/enrich.go` - `buildSampleDetailFromProvider`,
  `distinctLibrariesForSamples`, `libraryLinkForSample`, and the
  study-detail builder consume the slice fields instead of the
  removed scalar fields.
- `seqmeta/diff.go` - hashes one entry per `(library, study)`
  pairing.
- `seqmeta/validate.go` - iterates slices on the resolved sample.
- `results/server.go` and `results/mlwh_search_resolver.go` -
  search expansion emits one tagged-id row per `(sample, study)`
  pairing.

Update every existing seqmeta / results / cmd test that previously
asserted single-`IDStudyLims` / `LibraryType` shape so it asserts
slice contents instead. Covers all 4 acceptance tests from C2.

- [x] implemented
- [x] reviewed
