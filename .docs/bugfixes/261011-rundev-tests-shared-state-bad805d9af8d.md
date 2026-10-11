# Bugfix: run-dev tests clobber a live verify-wa stack

- Branch: `rundevstate-64ed3f75`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-rundevstate`

## Items

- [ ] Running `go test -run TestRunDev ./cmd/` in a checkout that also has
  a verify-wa stack running breaks that stack: the tests deleted
  `.tmp/state-test/.wa-results-server.token` and rewrote
  `.tmp/wa-dev-key.pem`. The tests share the checkout's `.tmp` state
  directory with real run-dev stacks, and verify-wa's one-stack-per-checkout
  rule does not warn about this.
  - Source: spec-aware PR review of branch `usage`, reported as
    incidental.
  - Evidence: observed on branch `usage` while a verify-wa stack ran in
    the same checkout; not yet reproduced on this base.
  - Fix direction: give the tests an isolated state and key directory, or
    document the conflict in verify-wa.
