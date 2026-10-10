# wa Feature Map

| Feature                                         | Surface                                         | Helper                                          |
| ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| [MLWH feedback submit](mlwh-feedback-submit.md) | `wa mlwh serve` `POST /feedback`, on and off    | `drive-feedback.mjs`, `drive-feedback-modes.sh` |
| [Feedback admin page](feedback-admin-page.md)   | Frontend `/feedback`, admin API behind it       | `drive-feedback.mjs`                            |
| [MLWH query API and CLI](mlwh-query-api.md)     | `wa mlwh serve` read routes, `wa mlwh info` etc | curl / `$RUN/wa`                                |
| [Results web UI](results-web.md)                | Frontend dashboard, search, result detail       | Playwright, recipe in file                      |
| [Results CLI and API](results-cli.md)           | `wa results register/search/get`                | `$RUN/wa`                                       |

Every feature assumes a stack from the skill's Launch step and a passing
Doctor.
