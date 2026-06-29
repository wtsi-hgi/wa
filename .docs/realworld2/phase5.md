# Phase 5: D4 -- People to studies (E1-E3)

Ref: [spec.md](spec.md) sections E1, E2, E3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (the four-step add-a-query recipe, the `id_lims =
'SQSCP'` invariant, the count<->list cross-check, and the people-endpoint
cascade: an EMPTY list + `neverSyncedReadErr()` on a never-synced cache,
an empty list with NO error on a synced cache with no matches).

All new code lives in `mlwh/people.go` (with the `PersonStudy` /
`PersonCandidate` structs in `mlwh/types.go`) plus the counts (reuse
`queryCount`) and the wiring files; tests in `mlwh/people_test.go`,
hermetic over `openSQLiteSyncTestCache`. This phase depends on Phase 1
(`study_users_mirror` and the `study_mirror_faculty_sponsor_idx` index).
The three endpoints are matched case-insensitively (substring) and
paginated with `searchPaginationParams` (default 100, max 1000) and
`X-Total-Count` / `X-Next-Offset`; a whitespace-only path param (after
trim) -> `ErrUnsupportedIdentifier` (400) raised in the handler before
the queryer. The faculty_sponsor endpoint matches `study.faculty_sponsor`
(the named PI, free-text); the user endpoint matches `study_users` role
membership across `name`/`login`/`email` -- they return different sets,
and each Description must state the distinction.

The items live in ONE new file (`mlwh/people.go`); implement them
sequentially to avoid same-file edit conflicts. They are otherwise
logically independent surfaces.

## Items

### Item 5.1: E1 - studies by faculty sponsor

spec.md section: E1

Implement `StudiesForFacultySponsor(ctx, name string, limit, offset int)
([]PersonStudy, error)` and `CountStudiesForFacultySponsor(ctx, name
string) (Count, error)` for `GET /studies/faculty-sponsor/:name` (+
`/count`). Match `study_mirror.faculty_sponsor` containing the term,
case-insensitive substring (`LOWER(faculty_sponsor) LIKE
'%'||LOWER(?)||'%'`), filtered to `id_lims='SQSCP'`. Each row's `Study`
is the full study; `Role` is empty (sponsor is not a `study_users` role).
Ordered by `id_study_lims`; paginated with sizing headers; the count is
the same match with no LIMIT (count == len(list)). A whitespace-only
`:name` -> `ErrUnsupportedIdentifier` (400). The `Description` states it
matches the named PI/SPONSOR (`study.faculty_sponsor`, free-text),
case-insensitive substring; that it is DISTINCT from `/studies/user`
(role membership); and the freshness caveat. Cascade: never-synced ->
empty list + `neverSyncedReadErr()`; synced-no-match -> empty list, no
error. Wire the Registry entry, `Queryer` member, `server.go` case, and
`RemoteClient` method (+ `StudiesForFacultySponsorPage`). Covering all 4
acceptance tests from E1 (`StudiesForFacultySponsor("carl", all)` returns
the 3 Carl studies case-insensitively, each with full `Study` and empty
`Role`, count 3; `limit=2&offset=0` gives 2 rows, `X-Total-Count: 3`,
`X-Next-Offset: 2`; whitespace-only name -> 400; never-synced empty list
+ both sentinels / synced-no-match empty list no error). Depends on Phase
1.

- [x] implemented
- [x] reviewed

### Item 5.2: E2 - studies by user (role-filtered)

spec.md section: E2

Implement `StudiesForUser(ctx, person, role string, limit, offset int)
([]PersonStudy, error)` and `CountStudiesForUser(ctx, person, role
string) (Count, error)` for `GET /studies/user/:person` (+ `/count`).
Match `study_users_mirror` where `person` is a case-insensitive substring
of `name` OR `login` OR `email`, joined to `study_mirror` on
`id_study_tmp` (filtered `study_mirror.id_lims='SQSCP'`). DEFAULT role
filter `role IN ('owner','manager','data_access_contact')`; an optional
`role=` query param (comma-separated, raw, "" = default set) OVERRIDES
the default set, each value matched exactly (case-insensitive). Each
row's `Role` is the matched `study_users` role; the SAME study may appear
under multiple roles -- de-duplicate to one row per `(id_study_lims,
role)`, order by `(id_study_lims, role)`. Paginated with sizing headers;
the count is the distinct `(id_study_lims, role)` matches with no LIMIT.
A whitespace-only `:person` -> 400. The `Description` states it matches
`study_users` ROLE MEMBERSHIP (distinct from faculty_sponsor);
case-insensitive substring across `name`, `login` AND `email` (so an
email/login or a name both resolve); the DEFAULT role set and that
`role=` widens it; that each row surfaces the matched `role`; and the
freshness caveat. Cascade as E1. Wire the Registry entry (with the `role`
`QueryParam`), `Queryer` member, `server.go` case (pass the raw `role`
query param through), and `RemoteClient` method (+ `StudiesForUserPage`).
Covering all 6 acceptance tests from E2 (login "ca3" with the default
roles returns X,Y,Z [owner/manager] but NOT W [follower], each with its
`Role`, count 3; matching by email and by name substring returns the same
3; `role=follower` returns W, count 1; a person who is both owner and
data_access_contact of X yields X twice with the two roles, count 2
[distinct (study, role)]; whitespace-only person -> 400; never-synced
empty list + both sentinels / synced-no-match empty list no error).
Depends on Phase 1; independent of 5.1 (sequence after it only to avoid
same-file edit conflicts).

- [x] implemented
- [x] reviewed

### Item 5.3: E3 - resolve-person directory

spec.md section: E3

Implement `ResolvePerson(ctx, term string, limit, offset int)
([]PersonCandidate, error)` and `CountResolvePerson(ctx, term string)
(Count, error)` for `GET /resolve-person/:term` (+ `/count`). Given a
partial term (case-insensitive), return the DISTINCT candidate people
from BOTH sources: from `study_mirror`, distinct `faculty_sponsor` values
containing the term (`Source="faculty_sponsor"`, `Name`=the sponsor text,
`Login`/`Email`/`Role` empty), with `StudyCount` = distinct SQSCP studies
for that sponsor; and from `study_users_mirror`, distinct `(name, login,
email, role)` where the term is a substring of `name`/`login`/`email`
(`Source="study_users"`), with `StudyCount` = distinct studies for that
`(login, role)`. Bounded/pageable (`searchPaginationParams`) with sizing
headers; ordered by `(source, name, login, role)`; the count is the
distinct candidate count across both sources. A whitespace-only `:term`
-> 400. The `Description` states the stored forms (faculty_sponsor is
free-text full names; study_users identifies a person by `name` AND
`login` (Sanger username) AND `email`); the match-across-name/login/email
behaviour; and the routing guidance to enumerate here when a narrow term
is empty/ambiguous, then use `/studies/faculty-sponsor` or
`/studies/user`. Cascade as E1/E2. Wire the Registry entry, `Queryer`
member, `server.go` case, and `RemoteClient` method (+
`ResolvePersonPage`). Covering all 5 acceptance tests from E3 (a "carl"
term returns a `faculty_sponsor` candidate `Name="Carl Anderson"`
`StudyCount=91` AND a `study_users` candidate `Name="Carl
Anderson"`/`Login="ca3"`/`Email="ca3@sanger.ac.uk"`/`Role="owner"`
`StudyCount=59`; a "ca3" login fragment returns the study_users candidate;
two distinct sponsors "Carl Anderson" and "Carla Anders" both appear and
the count counts every distinct candidate; whitespace-only term -> 400;
never-synced empty list + both sentinels / synced-no-match empty list no
error). Depends on Phase 1; independent of 5.1/5.2 (sequence after them
only to avoid same-file edit conflicts).

CAUTION (Note 2, E3 count basis): the `study_users` candidate
`StudyCount` is "per (login, role)" while the candidates themselves are
grouped "per (name, login, email, role)". Keep the count basis CONSISTENT
with the grouping key as the spec defines: group candidates by `(name,
login, email, role)` and compute each candidate's `StudyCount` as the
distinct studies for its `(login, role)`. Do not silently collapse the
two keys.

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item (or one pass
over the phase) is acceptable; reviewers must confirm the count<->list
identity for each endpoint, the faculty_sponsor-vs-study_users
distinction in the Descriptions, and the E3 count-basis consistency
(Note 2).

## Ordering and dependency notes

- This phase depends on Phase 1 being fully reviewed
  (`study_users_mirror` and `study_mirror_faculty_sponsor_idx`); it is
  independent of Phases 2-4.
- The three items all live in the one new file `mlwh/people.go`, so they
  are sequenced (5.1 -> 5.2 -> 5.3) to avoid same-file edit conflicts;
  they are otherwise logically independent surfaces.
- E3 count-basis consistency (Note 2): group candidates by `(name, login,
  email, role)` but count `StudyCount` per `(login, role)` for the
  study_users source, exactly as the spec states.
- The `/studies/user` query must be served by a `study_users_mirror`
  lookup index (login/email/name or id_study_tmp); the MySQL EXPLAIN
  proof lives in Phase 7 (I1).
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 6 does the final `APIVersion` bump,
  CLI, doc regeneration, and drift-guard verification.
