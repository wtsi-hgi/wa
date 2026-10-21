# Never-synced cache error reads as "identifier not found"

- Branch: neversynced-d7693fef
- Base: origin/develop @ fa77143
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] On a never-synced MLWH cache, `wa mlwh serve` answers 503
      `cache_never_synced`, but the message reads
      `mlwh: identifier not found: mlwh: cache has never been synced; run "wa mlwh sync" first`,
      which suggests the identifier is wrong.
    - Red command:
      `go test -tags netgo -count 1 ./mlwh -run TestServerNeverSyncedMessageIsNotANotFound`
      -> the envelope message starts with `mlwh: identifier not found: `.
    - Cause: `neverSyncedReadErr` (`mlwh/sync.go`) built the error with
      `fmt.Errorf("%w: %w", ErrNotFound, ErrCacheNeverSynced)`, so its text
      carried both sentinel messages. The server writes `err.Error()` as the
      envelope message.
    - Fixed in `mlwh/sync.go`: `neverSyncedReadErr` returns a
      `neverSyncedReadError` whose message is `ErrCacheNeverSynced`'s alone
      and which still unwraps to both `ErrCacheNeverSynced` and `ErrNotFound`.
    - Decision: `errors.Is(err, ErrNotFound)` stays true. The empty-cache
      contract in `.docs/mlwh-sync/spec.md` and `.docs/mlwh-overhaul/spec.md`
      requires it, `RemoteClient` reproduces it for list endpoints, and
      callers such as enrichment rely on it to degrade. Dropping it fails
      about 40 `mlwh` tests, including enrichment behaviour tests. Status
      503 and code `cache_never_synced` are unchanged, because
      `httpStatusAndErrorCode` checks `ErrCacheNeverSynced` first.
    - Test in `mlwh/server_test.go`: a real `Client` over a cold cache,
      served through the real routes, answers 503 `cache_never_synced` with
      exactly the never-synced message.
    - `TestResultsRegisterInvalidSampleWithSparseMLWHCache`
      (`cmd/results_register_test.go`) passed only because of the misleading
      text. Opening its legacy cache recreates the tables and clears
      `sync_state`, so the lookup is genuinely never-synced. It now expects
      "never been synced" and no "not found".
