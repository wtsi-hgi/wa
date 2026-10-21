# MySQL runs export fails with a collation mix

- Branch: runscollation-7d73a8b4
- Base: origin/develop @ e943921
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] `wa mlwh export runs <parent-kind> <id>` fails on every MySQL cache
      with MySQL error 1271 (an illegal mix of collations in a UNION),
      reported as 502 `upstream_impaired`. Found while adding MySQL tests for
      multi-sample export; likely `exportRunArmQuery` in `mlwh/export.go`
      (`CAST(... AS CHAR)` takes the connection collation and is mixed with
      mirror column collations across `UNION ALL`).
    - Red command (production, read-only API):
      `WA_MLWH_SERVER_URL=https://apps.hgi.sanger.ac.uk/mlwh-api wa mlwh export runs study 6684`
      -> `Error: export runs of study "6684": mlwh: upstream database impaired: query export run rows: Error 1271 (HY000): Illegal mix of collations for operation 'UNION'`
    - Cause: `runListingIntegerTextExpr` (`mlwh/runs_agg.go`) rendered
      `CAST(expr AS CHAR)`, which takes the connection collation
      (go-sql-driver default `utf8mb4_general_ci`) and clashes with the cache
      text columns (`utf8mb4_0900_ai_ci`) across UNION arms. It also broke the
      global `wa mlwh runs` listing and `--group-by platform` aggregate.
    - Fixed by rendering `CONCAT('', expr)` on MySQL, which is coercible so the
      column collation wins; SQLite unchanged. Regression test
      `TestRunUnionQueriesMixNumericAndTextRunIDsOnMySQLCache` fails on the
      base and passes with the fix.
