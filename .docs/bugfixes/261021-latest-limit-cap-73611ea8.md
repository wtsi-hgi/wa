# wa mlwh latest sends limits the server rejects with 400

- Branch: latestdata-73611ea8
- Base: origin/develop @ fa77143
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] Production usage data shows a `wa-cli` request to
      `GET /study/:id/latest-data` answered 400 at 2026-10-20T10:50:09Z (no
      error details were recorded then). `wa mlwh latest` is the only CLI
      caller of that endpoint (and of `/latest-data/faculty-sponsor/:name`).
    - Red command: `wa mlwh latest 6568 --limit 1001 --server <url>` against
      a real in-process server -> server answers 400 with "limit must not
      exceed 1000", and the CLI reports it as "mlwh: upstream database
      impaired".
    - Cause: `validateLatestPagination` (`cmd/mlwh_latest.go`) only rejected
      negative values, while `mlwhLatestDataPaginationFromQuery`
      (`mlwh/server.go`) caps `limit` at `SearchMaxLimit` (1000). Every other
      parameter the CLI sends (path-escaped study id, `file_type` with the
      same suffix rule, non-negative offset) passes server validation. The
      cap and the CLI check are unchanged since the endpoint was added
      (v0.10.x), so v0.11.2 and v0.11.3 have the same mismatch.
    - Fixed by rejecting `--limit` above `mlwh.SearchMaxLimit` before opening
      any client, and stating the cap in the flag help. Regression test
      `TestMLWHLatestOverMaxLimitNeverSendsAServerRejectedRequest` fails on
      the base and passes with the fix.
