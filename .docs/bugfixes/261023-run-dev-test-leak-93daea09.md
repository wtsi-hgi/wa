# cmd run-dev tests leave run-dev.sh stacks running

- Branch: testleak-93daea09
- Base: origin/develop @ a9e7866
- Queue owner: llm-knowledge-base session; this checklist owns this branch
  only.

- [x] Stacks started by the run-dev tests (seen from the TLS tests in
      `cmd/run_dev_tls_test.go`) stay alive for hours after `go test` ends:
      `bash run-dev.sh --mode dev ...` with its `wa-run-dev-*` results
      server, seqmeta stub, and frontend stub.
    - Red tests: `TestStartRunDevForTestStackDiesWithTestBinary` and
      `TestStartRunDevForTestCleanupKillsGroupMembersLeftByExitedLeader`
      (`go test ./cmd/ -run '^TestStartRunDevForTest'`) -> the process group
      survives a killed test binary, and a child survives cleanup once
      run-dev.sh has exited. Freezing then killing the binary during
      `TestRunDevSuppliedDevCertificateIsNeverRewritten` left the reported
      stack running.
    - Cause: `startRunDevForTest` puts each stack in its own process group
      and stops it only from `t.Cleanup`. A go test timeout, Ctrl-C, or a
      killed binary skips cleanup, so the signal never reaches the group,
      and a ready run-dev.sh writes nothing, so no SIGPIPE stops it either.
      `Terminate` also returned without signalling the group once
      run-dev.sh had exited, leaving any children still in it.
    - Fixed by a watchdog per stack, in its own process group, that TERMs
      then KILLs the stack's group when the test binary's pipe to it closes
      without a release, and by `Terminate` signalling the group until it
      is empty. `runRunDevExpectingFailureWithinForTest` uses the same
      watchdog.
