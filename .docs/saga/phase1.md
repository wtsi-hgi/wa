# Phase 1: Core HTTP + Client

Ref: [spec.md](spec.md) sections A1, A2, A3, B1, B2, B3, B4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 1.1: A1 - Create client with defaults

spec.md section: A1

Implement `NewClient(apiKey string, opts ...Option) (*Client, error)`
in `saga/client.go` with the `Client` struct, `Option` type,
`ErrNoAPIKey` sentinel, and default configuration (base URL, cache
duration, HTTP timeout). Implement `Close()` stub. Test in
`saga/client_test.go` covering all 2 acceptance tests from A1.

- [x] implemented
- [x] reviewed

### Item 1.2: A2 - Create client with custom options

spec.md section: A2

Implement `WithBaseURL` and `WithCacheDuration` functional options
in `saga/client.go`. Test in `saga/client_test.go` covering all
3 acceptance tests from A2.

- [x] implemented
- [x] reviewed

### Item 1.3: A3 - Client Close

spec.md section: A3

Implement `Close()` to stop the activecache goroutines in
`saga/client.go`. Ensure double-close safety. Test in
`saga/client_test.go` covering all 2 acceptance tests from A3.

- [x] implemented
- [x] reviewed

### Item 1.4: B1 - Request headers

spec.md section: B1

Implement internal HTTP methods `doGet`, `doPost`, `doDelete` in
`saga/http.go` that attach `X-Api-Key` and `User-Agent` headers
to every request. Test with mock `httptest.Server` in
`saga/http_test.go` covering all 2 acceptance tests from B1.

- [x] implemented
- [x] reviewed

### Item 1.5: B2 - API error handling

spec.md section: B2

Implement `APIError` struct with `Error()` and `Unwrap()` methods,
plus `ErrUnauthorized`, `ErrNotFound`, `ErrServerError` sentinels
in `saga/http.go`. Map HTTP status codes to appropriate errors.
Test in `saga/http_test.go` covering all 5 acceptance tests from
B2.

- [x] implemented
- [x] reviewed

### Item 1.6: B3 - Retry with backoff

spec.md section: B3

Integrate `github.com/wtsi-ssg/wr/retry` with
`backoff.Backoff{Min: 250ms, Max: 3s, Factor: 1.5}` and
`UntilLimit{Max: 3}` into the HTTP layer in `saga/http.go`. Only
retry on 5xx and timeouts; do not retry 4xx. Test in
`saga/http_test.go` covering all 3 acceptance tests from B3.

- [x] implemented
- [x] reviewed

### Item 1.7: B4 - Caching

spec.md section: B4

Implement GET response caching with `github.com/wtsi-hgi/activecache`
in `saga/cache.go`. Cache key is method + full URL. POST/DELETE
are never cached and invalidate related entries. Test in
`saga/cache_test.go` covering all 3 acceptance tests from B4.

- [x] implemented
- [x] reviewed
