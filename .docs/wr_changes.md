# Feature: Push-based job-completion subscription for the wr client

> Copy this file into the `wr` repo (`~/wr`) and run the spec-writer
> workflow from there. The file references concrete `wr` code paths so the
> spec author can locate the existing machinery to reuse.

## Background

External and internal tools submit jobs to the wr manager through the Go
client library (`jobqueue.Connect` / `Client.Add`, grouping related jobs
under a `RepGroup`) and need to react the *moment* those jobs finish —
register output files elsewhere, fire a webhook, or push a "your results
are ready" event to a waiting web UI. The concrete near-term consumer is
the `wa` workflow-automation system's `jobrun` component, which submits a
command on behalf of a web frontend and must surface completion to the
browser with no perceptible lag.

Today the only way a Go client learns that a job finished is to **poll**.
The client API (`jobqueue/client.go`) is request/response only:

- `GetByRepGroup` (`jobqueue/client.go:1868`),
  `GetByRepGroupMatch` (`:1879`), `GetIncomplete` (`:1893`),
  `GetIncompleteByRepGroupMatch` (`:1904`),
  `GetLastCompletionTimeByRepGroup` (`:1918`) — all synchronous, all over
  the `request()` req/rep transport (`:2031`). There is no push, stream,
  or subscribe primitive on `Client`.

Polling has three costs we want to remove: completion latency bounded by
the poll interval, wasted manager round-trips, and poor scaling when many
waiters each poll for their own `RepGroup`.

Crucially, **the manager already knows the instant a job changes state,
and already pushes that out** — just not to Go clients:

- The queue change hook `q.SetChangedCallback(...)`
  (`jobqueue/server.go:1623`) fires on every job state transition.
- It already broadcasts aggregate counts (`jstateCount`) and, for
  *subscribed* connections, per-job `JStatus` updates with
  `IsPushUpdate = true` (`jobqueue/server.go:1741`).
- Subscriptions are tracked per connection by job key in
  `jobSubscriptions` (`jobqueue/server.go:361`), managed by
  `subscribeToJobs` (`:3286`) and `unsubscribeFromJob` (`:3305`).
- This stream is delivered only over the browser web-UI websocket
  `/status_ws` (`jobqueue/server.go:851`,
  `webInterfaceStatusWS` in `jobqueue/serverWebI.go:200`,
  `setupUpdateListener` at `:430`). It is not reachable from
  `jobqueue.Client`.

So the push capability exists end-to-end inside the manager; this feature
is about exposing it to the Go client library through a first-class,
supported API, reusing the same state-change hook and `JStatus` payload
rather than building a parallel mechanism.

## What we want

A push/subscribe capability on the wr Go client so a client that
submitted jobs can be notified promptly (sub-second after the manager
observes the transition) when those jobs reach a terminal state, without
polling.

### Client-facing subscription API

- A method on `jobqueue.Client` to subscribe to completion events for a
  set of jobs identified by **RepGroup** (exact match — the handle the
  submitter already owns) and, ideally, also by explicit job keys /
  `JobEssence`s for callers that want finer scope.
- Delivery via an idiomatic Go push primitive: a receive-only channel of
  typed updates (e.g. `<-chan *JobUpdate`), or a registered callback. The
  update carries at least: job key, `RepGroup`, terminal `JobState`
  (complete / buried / lost), exit code, fail reason, and start/end
  timing — the fields already present on `JStatus`
  (`jobqueue/serverWebI.go`). It need not carry full stdout/stderr; the
  consumer can fetch those via the existing `Get*` methods on receipt.
- **Terminal-state semantics:** deliver one event per subscribed job when
  it transitions to `complete`, `buried`, or `lost`
  (`JobState` constants in `jobqueue/job.go`). Optionally also an
  aggregate signal — "every job currently in RepGroup X has reached a
  terminal state" — since "tell me when my whole batch is done" is the
  common case.
- **Catch-up / late subscribe:** if a subscribed job is already terminal
  at subscribe time (e.g. a fast job finished microseconds before the
  client subscribed), its event must be delivered immediately so the
  caller never hangs forever waiting for an event that already fired. The
  spec must define the catch-up window (currently-live jobs plus
  recently-archived jobs for the RepGroup).
- **Lifecycle:** subscription bound to a `context.Context` and/or an
  explicit `Unsubscribe` + `Close`; clean teardown when the client
  disconnects; bounded buffering with a documented overflow policy
  (drop-oldest vs. block) and a way for the consumer to detect that drops
  occurred.
- **Auth/transport:** subscriptions use the client's existing
  authenticated, TLS-secured connection (CA file + token, as
  `Connect`/`ConnectUsingConfig` already require — `jobqueue/client.go:207`,
  `:288`) and are subject to the same authorization as other client
  calls. The caller must not have to run a browser or hand-assemble
  websocket frames.

### Server side

- Generalize the existing subscription mechanism so subscriptions can be
  keyed by **RepGroup** (not only by individual job key), and so a
  non-browser (Go-client) transport can register and receive them.
- The `SetChangedCallback` hook (`jobqueue/server.go:1623`) and the
  `JStatus` payload must remain the single source of state-change events
  feeding **both** the browser `/status_ws` stream and the new client
  subscriptions — no second, divergent notification path.
- The transport choice is left to the spec/design: either extend the
  existing nanomsg/mangos connection used by `jobqueue.Client` with an
  asynchronous push/streaming channel, or provide a supported Go client
  over the existing `/status_ws` websocket. The hard constraint is that
  it works over the client's existing authenticated connection and is
  exposed as a normal Go method, not as a browser-only feature.

## Acceptance criteria

- Submit N jobs under a `RepGroup`, subscribe, and receive a terminal
  event for each within a small bound of *actual* completion (not
  poll-interval bound) — including at least one job that fails (buried)
  and one that is lost.
- Subscribing *after* a job already completed still yields that job's
  terminal event (catch-up).
- A subscription survives a manager restart / reconnect without silently
  missing terminal events, or surfaces an explicit, detectable gap that
  the client can recover from by re-syncing.
- No regression to the existing `/status_ws` browser updates; both
  consumers are driven by the same `SetChangedCallback` hook.
- An unauthorized / invalid-token client cannot subscribe.

## Out of scope

- Changing how jobs are submitted, scheduled, or run.
- New web-UI features.
- Server-side persistence of subscriptions across manager restarts (the
  client re-subscribes on reconnect).
- Delivering full stdout/stderr in the push payload (identifiers +
  terminal status + exit/fail/timing metadata only; fetch output via the
  existing `Get*` methods).

## Reference points in the wr codebase

- `jobqueue/client.go` — `Connect` (`:207`), `ConnectUsingConfig`
  (`:288`), `Add` (`:418`), `GetByRepGroup` (`:1868`),
  `GetByRepGroupMatch` (`:1879`), `GetIncomplete` (`:1893`),
  `GetLastCompletionTimeByRepGroup` (`:1918`), `request()` (`:2031`),
  `RepGroupMatch` modes. Poll-only today.
- `jobqueue/job.go` — `Job` struct, `JobState` constants
  (`complete` / `buried` / `lost` / …), `RepGroup` / `ReqGroup`.
- `jobqueue/server.go` — `SetChangedCallback` state-change hook (`:1623`);
  `jstateCount`; `jobSubscriptions` (`:361`), `subscribeToJobs` (`:3286`),
  `unsubscribeFromJob` (`:3305`); `statusCaster` (`:354`);
  `/status_ws` route registration (`:851`); `IsPushUpdate` set (`:1741`).
- `jobqueue/serverWebI.go` — `webInterfaceStatusWS` (`:200`),
  `setupUpdateListener` (`:430`), `JStatus` struct with `IsPushUpdate`
  (`:117`).
