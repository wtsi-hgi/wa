# MLWH Query API And CLI

## What It Is

`wa mlwh serve` is a read-only REST API over a local MLWH cache: classify an
identifier, resolve studies, samples, runs, and libraries, export iRODS paths
and products, latest data, run aggregates, programmes. The `wa mlwh`
subcommands (`info`, `search`, `export`, `latest`, `runs`, `studies`,
`programmes`, `people`) are clients of that server. The frontend uses it too,
for study-based search.

## How To Reach It

- HTTP: `$MLWH_URL/<route>`. The full contract is `GET /openapi.json`
  (`info.version` is the MLWH API version, 1.9.0 at the time of writing) and
  the generated `MLWH_API_REFERENCE.md`.
- CLI: `WA_MLWH_SERVER_URL=$MLWH_URL $RUN/wa mlwh <subcommand>`, or
  `--server $MLWH_URL`.

## How To Drive It

Fixture data in the test-mode cache: studies `6568` (programme
`Human genetics`) and `7607`, and sample `WTSI_wEMB10524782` (accession
`ERS10524782`) in study 6568. There is no iRODS data, so data-object counts
are 0.

1. HTTP, each 200 with JSON:
   `curl -s $MLWH_URL/health` gives `{"status":"ok"}`. Also try
   `/studies`, `/classify/6568` (`"kind":"study_lims_id"`),
   `/classify/WTSI_wEMB10524782` (`"kind":"sanger_sample_name"`),
   `/resolve/study/6568`, `/programmes`, `/freshness`, and `/openapi.json`.
   Save bodies as `$RUN/evidence/20-mlwh_<route>.json`.
2. CLI: `WA_MLWH_SERVER_URL=$MLWH_URL $RUN/wa mlwh info 6568 --type study`
   exits 0 and prints a `Study  6568` card with name, accession
   `EGAS00001006568`, and 1 sample. Add `--json` for one object.
   `wa mlwh studies --programme "Human genetics"` lists study 6568.
   Evidence: `21-mlwh-info-6568.txt`, `22-mlwh-studies.txt`.
3. Never-synced behaviour: a serve on an empty cache (see
   `drive-feedback-modes.sh`) answers data routes with
   503 `cache_never_synced` and `/freshness` with every table
   `"ever_synced":false`.

## Gotchas

- `wa mlwh info` without `WA_MLWH_SERVER_URL`, `--server`, or
  `WA_MLWH_CACHE_PATH` fails with "must be set". run-dev.sh does not export
  the URL to your shell.
- A real cache needs `wa mlwh sync` against the production MLWH with
  credentials. Do not do that from this skill. Test mode exists so you never
  need to.
- Secured mode (`WA_MLWH_SERVER_CERT/KEY/TOKEN`) serves data only under
  `/rest/v1/auth/...` with a JWT. This map covers plain mode only.
