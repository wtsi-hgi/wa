---
name: mlwh-upstream-change
description: "Assess a Unified Warehouse (MLWH) schema release announcement against wa and deliver any change it needs before the release date. Use when the user pastes an MLWH or Unified Warehouse release announcement, or asks whether an upstream MLWH schema change affects wa."
---

# MLWH Upstream Change

DEVELOPING.md, section "Upstream MLWH schema changes", holds how wa reads the
source, the assessment checklist, the compatibility patterns, the tests and
the deploy order. Read it first. [The 2026-10-22
assessment](../../../.docs/upstream/2026-10-22-uwh.md) is a worked example of
the record and the report.

The goal is a wa that works on both the old and the new schema, shipped before
the release date, so that nobody deploys wa at the moment upstream changes. A
plan that needs a deploy at the release time is a **blocker**: report it
instead.

## Steps

1. List every change in the announcement as table, column or index, and old
   to new shape. Done when each announced change has one line.
2. Assess each change with the DEVELOPING.md checklist, citing the queries in
   `AllSyncSourceQueries()` and the cache reads you checked. Done when every
   line has the checklist's class and its evidence.
3. For each change that breaks sync or gives wrong results, write the failing
   hermetic test on the new shape first, then make the change with a
   DEVELOPING.md compatibility pattern. Add tests for compatible-both-ways
   changes that wa's correctness depends on, such as a filter that now matters.
   Done when each new test passes on both shapes and fails when its fix is
   mutated away.
4. Run the gates: `make test-go`, `make lint`, and `wa mlwh check-source`
   against the dev MLWH. For a quicker loop, run
   `go test ./mlwh ./cmd -count=1 -v` with `WA_MLWH_CACHE_PATH` and
   `WA_MLWH_CACHE_PASSWORD` exported from `.env.development.local`, as
   `make test-go` does. Unset, the tests read them from the repo's `.env`
   files, and skip the MySQL cache twins only when no file sets them. Done
   when all pass, no MySQL twin you added skipped, and every source query
   prepares.
5. Write `.docs/upstream/<release date>-uwh.md` in the shape of the worked
   example. Done when it records each change's class and the evidence.

## Report

Return to the user:

- Per change: its class from the DEVELOPING.md checklist, with the evidence.
- Files changed, mutation results, gate results, and the `check-source`
  output.
- Whether a wa release must ship before the release date, and the
  post-release checks from DEVELOPING.md: `check-source` in the pod, then
  `/freshness`.
