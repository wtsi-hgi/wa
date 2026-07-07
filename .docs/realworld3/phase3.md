# Phase 3: D3 search + filters (C1-C4)

Ref: [spec.md](spec.md) sections C1, C2, C3, C4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase makes literal whole-value prefix the SOLE default sample
search, moves word-prefix to opt-in `--words`, and adds the shared exact
filter family (organism / library-type / qc / deliverable). The
literal-prefix machinery already exists in `search.go`
(`sampleFullPrefixFields`, `sampleFullPrefixPageSQL/CountSQL`); today's
default wrongly merges it with the word-token path. `searchTermMinLength=3`
and `sampleSearchCountCap=10000` are unchanged. The filters apply to
SAMPLE search only (not study search) and are each exempt from the 3-char
minimum (which stays on the free-text term / `--words`).

State the QC grain explicitly: on sample SEARCH `--qc` matches the
per-sample ROLL-UP verdict (`qc.go` fail>pending>pass, agreeing with
`SampleProgress.qc`); on the D1 EXPORT (Phase 4) `--qc` matches the raw
PER-PRODUCT `qc`. All four items share `mlwh/search.go`, so they are
sequential. Depends on Phase 1 (A5 `common_name_word_mirror`) and Phase 2
(B2 deliverable). MySQL EXPLAIN/count proofs live in `search_mysql_test.go`
/ `cache_mysql_integration_test.go` (skipped without creds); behavioural
tests are hermetic over the ephemeral SQLite cache.

## Items

### Item 3.1: C1 - default sample search = literal whole-value prefix

spec.md section: C1

Make `SearchSamples(term)` with no mode return exactly the SQSCP samples
where any of `name, supplier_name, common_name, donor_id` starts with the
term (`col LIKE 'term%' ESCAPE '!'`, index range seek) - the ONLY default;
remove the word-token union from the default path. Introduce the
`SampleSearchOptions` struct (`Words`, `Organism`, `LibraryType`, `QC`,
`DeliverablesOnly`) and the `SearchSamples`/`CountSampleSearch`
signatures per the spec. File: `search.go`. Covering all 3 acceptance
tests from C1 (`SearchSamples("hek_r", {})` returns exactly the 4
`Hek_R1..4`, not 55; the 3-char minimum still applies to a bare short
term; MySQL EXPLAIN shows each field predicate is an index range seek, no
full scan).

- [ ] implemented
- [ ] reviewed

### Item 3.2: C2 - --words opt-in word-prefix mode

spec.md section: C2

Retain `sample_search_token` and make `--words` (`SampleSearchOptions.
Words`) select separator-agnostic multi-word matching (`10X Automation
HEK` matches stored `10X_Automation_HEK`; `mus`/`musculus` match `Mus
Musculus`). It is NOT the default; the cross-field AND that today yields
`hek_r`->55 is reached ONLY via `--words`. File: `search.go`. Covering
both acceptance tests from C2 (`SearchSamples("musculus", {Words:true})`
matches `Mus Musculus`; `SearchSamples("hek_r", {Words:true})` returns the
broader word-prefix set, >= the 4 literal matches and distinct from the
default). Depends on 3.1 (shared `search.go`, the options struct).

- [ ] implemented
- [ ] reviewed

### Item 3.3: C3 - --organism word-membership filter over common_name

spec.md section: C3

Add `--organism` (`SampleSearchOptions.Organism`): a `common_name`
matches iff it contains the query as a whole word (all query words for a
multi-word query), resolved via `common_name_word_mirror` (A5), then
samples constrained by the indexed `sample_mirror.common_name`. Includes
subspecies (`Mus musculus castaneus`), excludes mid-word fragments
(`usculus`), is not exact-whole-value, and is exempt from the 3-char
minimum. File: `search.go`. Covering all 4 acceptance tests from C3
(`{Organism:"musculus"}` matches every common_name containing the word
`musculus` incl. subspecies, count exceeds `Mus Musculus`'s 235,447 -
assert the actual word-membership count; `usculus` matches nothing; `mus
musculus` requires both words; EXPLAIN uses the `(word)` index and the
`common_name` index, no full scan). Depends on 3.1 and Phase 1 (A5).

- [ ] implemented
- [ ] reviewed

### Item 3.4: C4 - shared exact filter family (search + export), AND-combined, indexed

spec.md section: C4

Wire `--library-type` (exact `library_samples.pipeline_id_lims`),
`--organism` (C3), `--qc pass|fail|pending`, and `--deliverables-only`
(B2) so they AND-combine with the term and each other, each index-served
(intersection by candidate `id_sample_tmp` set, not a scan), each exempt
from the 3-char minimum. Factor the filter helpers so the SAME family
applies on the export surface (its `export.go` application lands in Phase
4, whose `ExportOptions` already carries these fields). Files: `search.go`,
`count.go` (the `export.go` touchpoint is completed in Phase 4). Covering
all 4 acceptance tests from C4 (`--qc pass` on search = per-sample roll-up
verdict, a sample with any fail excluded; the SAME `--qc pass` on the
export = raw per-product `qc=1` - the grain difference asserted
explicitly in Phase 4; `--library-type <val> --organism musculus` =
indexed intersection, no full scan; the family applied on the EXPORT
surface returns the strict subset matching the sample set - this AT is
exercised via the Phase 4 export). Depends on 3.3 (organism) and Phase 2
(B2 deliverable).

- [ ] implemented
- [ ] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm literal-prefix is the SOLE default
(word-token union removed), `--words` is opt-in, and the QC grain
difference (per-sample roll-up on search vs per-product on export) holds.

## Ordering and dependency notes

- Depends on Phase 1 (A5 `common_name_word_mirror`) and Phase 2 (B2
  `--deliverables-only` tri-state).
- Items are sequential because all four edit `mlwh/search.go`: C1 sets
  the default and the options struct, C2 adds `--words`, C3 adds
  `--organism`, C4 assembles the AND-combined shared filter family.
- C4's shared filter family is reused by the D1 export in Phase 4; its
  `export.go` touchpoint and the export-surface acceptance test (C4 AT4)
  complete together with Phase 4's export layer, since `export.go` is
  created there. Keep the filter helpers factored for that reuse.
- SearchSamples/CountSampleSearch Description updates (new default vs
  `--words` vs filters, QC grain) are consolidated in Phase 10 (J).
