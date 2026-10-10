# Phase 5: Docs

Ref: [spec.md](spec.md) sections F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 5.1: F1 - Operator documentation

spec.md section: F1

Cover every README topic listed in spec.md F1 in the `mlwh serve` section
of `README.md`. Add the "Usage metrics" section to
`.docs/mcp/security-posture.md`, add the usage DB to its statement about
the server's only write, and name `GET /usage` in the "No route checks a
credential except the feedback admin routes" sentence. Add commented
`WA_MLWH_USAGE_PATH=` (dev: `.tmp/mlwh-usage.sqlite`) and
`WA_MLWH_USAGE_RETENTION_DAYS=` entries to `.env.development` and
`.env.production`. For verify-wa, add
`.agents/skills/verify-wa/features/usage-admin-page.md` in the format of
`feedback-admin-page.md`, its row in `features/README.md`, the `/usage`
page in the `SKILL.md` description, and a `scripts/doctor.sh` check that
unauthenticated `GET /usage` is 401. Tests go in `mlwh/docs_test.go`.
Depends on phases 3 and 4. Covering all 3 acceptance tests from F1.

- [ ] implemented
- [ ] reviewed
