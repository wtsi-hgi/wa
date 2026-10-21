# wa Feature Map

| Feature                                         | Surface                                         | Helper                                                   |
| ----------------------------------------------- | ----------------------------------------------- | -------------------------------------------------------- |
| [MLWH feedback submit](mlwh-feedback-submit.md) | `wa mlwh serve` `POST /feedback`, on and off    | `drive-feedback.mjs`, `drive-feedback-modes.sh`          |
| [Feedback admin page](feedback-admin-page.md)   | Frontend `/feedback`, admin API behind it       | `drive-feedback.mjs`, `drive-menu-after-login.mjs`       |
| [Usage admin page](usage-admin-page.md)         | Frontend `/usage`, `wa mlwh serve` `GET /usage` | Playwright, recipe in file, `drive-menu-after-login.mjs` |
| [MLWH sync admin page](mlwh-sync-admin-page.md) | Frontend `/mlwh-sync`, `GET /freshness`, CLI    | `drive-mlwh-sync.mjs`, recipe in file                    |
| [MLWH query API and CLI](mlwh-query-api.md)     | `wa mlwh serve` read routes, `wa mlwh info` etc | curl / `$RUN/wa`                                         |
| [Results web UI](results-web.md)                | Frontend dashboard, search, result detail       | Playwright, recipe in file                               |
| [Results CLI and API](results-cli.md)           | `wa results register/search/get`                | `$RUN/wa`                                                |

Every feature assumes a stack from the skill's Launch step and a passing
Doctor.
