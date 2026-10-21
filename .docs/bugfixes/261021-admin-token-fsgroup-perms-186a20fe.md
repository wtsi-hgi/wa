# MLWH admin token is replaced on every pod restart

- Branch: tokenperms-186a20fe
- Base: origin/develop @ 5d4e72d
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] In production, `wa mlwh serve` writes a new
      `/data/.wa-mlwh-server.token` on every pod restart, so the farm
      website's copy stops working ("The MLWH server rejected the admin
      token"; `/usage` and `/feedback` admin requests get 401). The token's
      mtime matches the rollout time, and other files in `/data` are
      `-rw-rw----`: kubelet applies the pod's `securityContext.fsGroup`
      (10001) on each mount and adds g+rw to every file.
    - Red command:
      `go test ./cmd/ -run TestMLWHServeAdminTokenSurvivesGroupPermissionChanges -count=1`
      -> a valid token written at mode 0660 comes back as a different token,
      and in secured mode the admin route rejects the original token with 401.
    - Cause: `resultsServeServerToken` (`cmd/results.go`) treated every
      `gas.GetStoredToken` error as "no token". go-authserver returns
      `JWTPermissionsError` when the mode is not exactly 0600, so a valid
      token at 0660 was overwritten. In secured mode, go-authserver's own
      `EnableAuthWithServerToken` did the same before wa read the file.
    - Fixed in `cmd/results.go` and `cmd/mlwh.go`: a token file whose mode
      grants nothing to other users but is not exactly 0600 has 0600 restored
      and is reused, with a warning on stderr. `mlwh serve` repairs the
      `--server-token` file before secured auth setup. A new token is still
      generated when the file is absent, too short to hold a token, or has any
      "other" permission bits set. A file that cannot be read, is not a
      regular file, or whose mode cannot be restored is now an error rather
      than being replaced.
    - Decision: loosened group bits are repaired, not treated as a leak. On
      Kubernetes the group is the pod's own fsGroup, and rotating would break
      every client holding a copy. World-accessible files are still rotated,
      which keeps the results server's existing rotation of a 0644 token.
    - Tests in `cmd/mlwh_test.go`: 0660, 0640 and 0400 tokens are reused at
      0600 with a warning; an absent file is generated at 0600; a 0600 token
      is reused; a 0664 token is replaced; a short file is replaced; secured
      serve with a 0660 `--server-token` file keeps the token and its admin
      route accepts it.
