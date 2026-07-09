# Phase 7: D7 programme + users (G1-G3)

Ref: [spec.md](spec.md) sections G1, G2, G3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase makes `programme` a first-class indexed dimension, generalises
F1 into a grouped sequencing aggregate, and adds the study->users
inverse. State the exact definitions: `programme` grouping/attribution
unit (each product maps to exactly ONE study->programme); a run spanning
multiple studies/programmes is counted ONCE per group it touches; the
study->users direction and role vocabulary (`owner`, `manager`,
`data_access_contact`, `follower`, `slf_manager`, `lab_manager`,
`administrator`) with the DEFAULT (no `role`) returning ALL roles present
(differs from the person->studies default of owner/manager/
data_access_contact); and that `faculty_sponsor` is a `Study` field, NOT
a `study_users` role. Reuse the existing `study_users_mirror` (no new
table).

Depends on Phase 1 (A7 programme index), Phase 6 (F1, which G2
generalises), and Phase 4 (`export.go`, for G3's users relationship). G1
and G2 touch disjoint files, so they form a parallel batch; G3 shares
`people.go`/`count.go` with G1, so it is sequential after. MySQL
EXPLAIN/count proofs live in `cache_mysql_integration_test.go`;
behavioural tests are hermetic over the ephemeral SQLite cache.

## Items

### Batch 1 (parallel)

G1 (programme surfaces) and G2 (grouped aggregate) touch disjoint files
(`availability.go`/`people.go`/`count.go`/`cmd/mlwh_studies.go` vs
`runs_agg.go`/`cmd/mlwh_runs.go`), so they run concurrently.

#### Item 7.1: G1 - programme in overview + studies-by-programme + enumeration [parallel with 7.2]

spec.md section: G1

Add `Programme` to `StudyOverview` (additive) so a per-study pass groups
by programme in one call; add `GET /studies/programme/:name` (+ `/count`)

- exact, indexed (A7) "studies in programme X" (NOT the substring
  `search/study`), backing `export studies programme "X"`; add `GET
/programmes` -> `[]Programme` (distinct programme + study counts). CLI:
  `wa mlwh studies --programme "Human Genetics"`, `wa mlwh programmes`.
  Files: `availability.go`, `people.go`, `count.go`, `cmd/mlwh_studies.go`.
  Covering all 3 acceptance tests from G1 (`StudyOverview.programme` ==
  `study_mirror.programme`; `/studies/programme/"Human Genetics"` returns
  exactly those studies agreeing with `/count`, index-served via EXPLAIN;
  `/programmes` lists distinct values with study counts). Depends on Phase
  1 (A7).

- [x] implemented
- [x] reviewed

#### Item 7.2: G2 - grouped sequencing aggregate (generalises F1) [parallel with 7.1]

spec.md section: G2

Add `GET /sequencing/aggregate?group_by=&unit=&platform=&since=&until=`
-> `[]SequencingAggregateRow`. `group_by` in `{month, platform,
manufacturer, programme, faculty_sponsor}` (combinable); `unit` (explicit,
caller-chosen) `runs` (run grain + per-platform `date_basis` per D5) or
`samples`/`products` (data grain, each product -> exactly ONE
study->programme, windowed by iRODS `created`). Each row states `unit`,
`date_basis`/date field, and `cache_synced_at`; a run spanning multiple
groups is counted once per group. Index-served, no server-side fan-out
over studies. CLI: `wa mlwh runs --monthly --group-by programme
--platform PacBio --since ... --until ...`. Files: `mlwh/runs_agg.go`,
`cmd/mlwh_runs.go`. Covering all 5 acceptance tests from G2 (PacBio
group_by=programme unit=runs per-programme counts stating unit=runs +
PacBio date_basis; a cross-programme run counted once per group; EXPLAIN
index-served no per-study fan-out; unit=samples per-programme
distinct-sample counts windowed by `created` stating the created-based
field, differing from unit=runs; unit=products distinct-product counts
exceeding samples where a sample has multiple products). Depends on Phase
6 (F1).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 2 (sequential, after batch 1 is reviewed)

G3 shares `people.go` and `count.go` with G1, so it runs after Batch 1.

#### Item 7.3: G3 - study->users inverse

spec.md section: G3

Add `GET /study/:id/users?role=` -> `[]StudyUser` `{role, name, login,
email}` from a single indexed `study_users_mirror.id_study_tmp` lookup
joined to `study_mirror`. `role` is an optional comma-separated filter
over the stored vocabulary; DEFAULT (no `role`) returns ALL roles present
(state this; it differs from the person->studies default). Reuse
`study_users_mirror` (no new table); `faculty_sponsor` is a `Study`
field, not a role. Backs `export users study <id> --role
owner,manager,follower`. Files: `people.go`, `count.go`, `mlwh/export.go`.
Covering all 3 acceptance tests from G3 (a study with
owner/manager/follower rows returns all from one `id_study_tmp` lookup,
EXPLAIN uses `study_users_mirror_id_study_tmp_idx`; `?role=owner,manager`
returns only those; `wa mlwh export users study 7568 --role
owner,manager,follower` prints `role,name,login,email` rows). Depends on
7.1 (shared `people.go`/`count.go`) and Phase 4 (`export.go` users
relationship).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after G3 is acceptable;
reviewers must confirm the study->users default returns ALL roles (not
the person->studies subset), it reuses `study_users_mirror` (no new
table), and `faculty_sponsor` is treated as a Study field, not a role.

## Ordering and dependency notes

- Depends on Phase 1 (A7 `study_mirror.programme` index), Phase 6 (F1,
  which G2 generalises), and Phase 4 (`export.go`, for G3's `users`
  relationship and G1's `export studies programme`).
- Batch 1 is parallel over disjoint files: G1 (programme overview/
  studies-by-programme/enumeration) and G2 (the grouped sequencing
  aggregate). Batch 2 is G3 (study->users inverse), sequential because it
  shares `people.go`/`count.go` with G1.
- G2 generalises F1; keep the per-platform `date_basis` and run-grain
  logic shared with `runs_agg.go` rather than duplicated.
- Registry entries and Description text for `/studies/programme/:name`,
  `/programmes`, `/sequencing/aggregate`, and `/study/:id/users` are
  consolidated in Phase 10 (J).
