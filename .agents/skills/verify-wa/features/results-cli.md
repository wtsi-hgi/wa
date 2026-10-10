# Results CLI And API

## What It Is

`wa results register|search|get|delete` talk to the results API
(`wa results serve`, HTTPS, JWT auth) to register pipeline output
directories with workflow identity, unique run key, and MLWH metadata
resolved server-side, then search and fetch them with their file lists.

## How To Reach It

`$RUN/wa results <cmd> --server $RESULTS_URL --cert $CERT`. Auth uses the
JWT cached in `$XDG_STATE_HOME/.wa-results.jwt`. In test mode the CLI gets it
without a prompt when the results owner token
(`.wa-results-server.token`) is in the same `XDG_STATE_HOME`.

## How To Drive It

```bash
. "$RUN/stack.env"
mkdir -p "$RUN/cli-state" "$RUN/cli-out/sub"
cp "$STATE_HOME/.wa-results-server.token" "$RUN/cli-state/"
echo hello >"$RUN/cli-out/sub/a.txt"; echo x >"$RUN/cli-out/b.csv"
export XDG_STATE_HOME="$RUN/cli-state"
C="--server $RESULTS_URL --cert $CERT"
$RUN/wa results register "$RUN/cli-out" $C --user "$(id -un)" --operator "$(id -un)" \
  --command verify-wa --workflow verify/wa-cli --unique cli-probe \
  --study 6568 --sample WTSI_wEMB10524782 </dev/null
$RUN/wa results search $C --pipeline-name verify/wa-cli --user "$(id -un)" </dev/null
$RUN/wa results get $C --files <id> </dev/null
```

Expected:

- `register` exits 0 and prints the result set JSON: a 64-hex `id`,
  `run_key` `runid=cli-probe`, metadata `seqmeta_id_study_lims: "6568"` and
  `seqmeta_name: "WTSI_wEMB10524782"` (resolved through MLWH), and
  `access.can_view: true`. `.wa-results.jwt` appears in `cli-state`.
- `search` returns a one-element array with that id.
- `get --files` lists `b.csv` and `sub/a.txt`.
- Re-registering the same `--workflow` and `--unique` replaces the set rather
  than adding one.

Evidence: `30-register.txt`, `31-search.json`, `32-get.json`.

## Gotchas

- `results search` filters pipelines with `--pipeline-name` or
  `--pipeline-identifier`; there is no `--pipeline` flag.
- Always redirect stdin from `/dev/null`. Without a cached JWT or owner
  token the CLI prompts `Password:` and would hang.
- Point `--server` at the results API port, never the frontend port.
- Unauthenticated `search` still works, but rows come back
  `access.locked: true`.
