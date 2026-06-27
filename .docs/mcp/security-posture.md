# wa mlwh API data-exposure and security posture

This document records, as a deliberate design decision rather than an
oversight, the security posture of the cache-backed `wa mlwh serve` REST API:
what it exposes, to whom, and where the access-control boundary actually lies.
It is written for an operator deciding how to deploy the server and for the
external MCP-server implementor consuming the API. The endpoint catalogue is in
`api-reference.md`, the per-field response schemas are in the OpenAPI document
at `GET /openapi.json`, and the domain entities are defined in `glossary.md`.

## Unauthenticated plain HTTP by default

`wa mlwh serve` runs as **unauthenticated plain HTTP by default**. When started
without the TLS/token settings (`WA_MLWH_SERVER_CERT` / `--cert`,
`WA_MLWH_SERVER_KEY` / `--key`, and `WA_MLWH_SERVER_TOKEN` / `--server-token`),
the command binds a plain TCP listener with no TLS and registers every endpoint
on the public router; no credential of any kind is checked. There is no
per-request authentication and no transport encryption in this mode.

Concretely, in `cmd/mlwh.go` the unauthenticated path wires the routes with
`server.RegisterRoutes(authServer.Router(), nil)` and serves over a plain
`net.Listen("tcp", addr)` listener. Every entry in the `Registry`, plus the
plain operational routes `GET /health` and `GET /openapi.json`, is therefore
reachable by any client that can open a TCP connection to the bind address.

This is intentional. The server is a read-only, internal backend that mirrors a
curated subset of MLWH for other `wa` services (and for an external MCP server
in a separate repo) to consume. The expectation is that it is deployed on a
trusted, network-restricted host. The consequence, spelled out below, is that
the network and the consuming layer in front of it constitute the entire
access-control boundary.

## Full-metadata exposure with no per-user authorisation

The API exposes **all mirrored MLWH metadata** with **no per-user
authorisation**. Every read endpoint returns the full mirrored rows of the
underlying types; there is no row-level, field-level, or per-caller access
control anywhere in the request path. The handler maps each `Registry` entry
straight to a cache read and serialises the whole result.

In particular, the directly-served `Study` type (`mlwh/types.go`) includes the
**governance fields** that, in the source LIMS, decide who may see a study's
data. These are returned in full to every caller:

- `data_access_group` - the data-access group governing access to the study
  data.
- `study_visibility` - the visibility of the study.
- `contains_human_dna` - whether the study samples contain human DNA.
- `ega_dac_accession_number` - the EGA Data Access Committee accession number.
- `ega_policy_accession_number` - the EGA policy accession number.

These governance fields are descriptive metadata in this API; they are emitted
verbatim and are **not** consulted to gate access. The server does not restrict
results based on `study_visibility`, `data_access_group`, or any other field,
and it does not identify the caller.

The practical consequence is that **the MCP server / network boundary IS the
access-control boundary** for this data. Any access control that a deployment
requires - restricting who can reach the API, and (if the MCP layer or a
fronting proxy implements one) who may see which studies or samples - must be
enforced by the network placement and by the layer in front of `wa mlwh serve`,
because the server itself enforces none.

## The search surface returns the same full rows

The substring-search endpoints introduce **no additional exposure**: they return
the **same full rows** as the list and find endpoints. `GET /search/study/:term`
returns `[]Study` and `GET /search/sample/:term` returns `[]Sample` - the same
models, with the same governance fields, as `GET /studies`, `GET
/study/:id/samples`, and the `GET /find/sample/...` endpoints. The search count
endpoints (`GET /search/study/:term/count`, `GET /search/sample/:term/count`)
return only a `{count: N}` envelope and transfer no rows, but the searches
themselves expose exactly what the rest of the read API already does. Search is
therefore a more convenient way to reach the same metadata, not a path to extra
metadata, and it sits inside the same access-control boundary as everything
else.

## Data-freshness model

The cache is populated out-of-band by `wa mlwh sync`; `wa mlwh serve` only ever
opens the local cache read-only and never contacts the upstream MLWH database.
Because the served data is therefore a point-in-time mirror, the API exposes its
own freshness so a consuming layer (for example an MCP chat) can say "data
current as of ..." and degrade gracefully rather than presenting stale data as
live.

`GET /freshness` reports, for each of the five mirrored sync tables (`study`,
`sample`, `iseq_flowcell`, `iseq_product_metrics`,
`seq_product_irods_locations`), a `TableFreshness` entry:

- `high_water` - the latest synced `last_updated` value for the table, as a UTC
  RFC3339 timestamp ending in `Z`; empty when the table has never synced.
- `last_run` - the timestamp of the last sync run for the table (UTC RFC3339);
  empty when the table has never synced.
- `ever_synced` - `false` when no `sync_state` row exists for the table.

`/freshness` is a first-class cache-read endpoint, but it is the
graceful-degradation signal, so it **must succeed even on a never-synced
cache**: in that case it returns all five tables with `ever_synced = false` and
empty timestamps, and does **not** error with `cache_never_synced`. (This
contrasts with `GET /health`, which is a cheap plain liveness route that does no
cache read at all.) A consumer reads `/freshness` to decide how much to trust
the data and to surface its age to users.

## Known limitation: secured (gas) mode and the RemoteClient

`wa mlwh serve` can optionally be started in a **secured mode** by supplying the
certificate, key, and server-token settings together. Secured mode is backed by
[go-authserver](https://github.com/wtsi-hgi/go-authserver) (gas) and changes the
wire contract in two ways:

- The data endpoints move **under the `/rest/v1/auth` prefix** (gas
  `EndPointAuth`), because they are registered on the JWT-protected auth router
  group instead of at their root paths. (The plain operational routes `GET
/health` and `GET /openapi.json` stay at the root and remain reachable without
  a token.)
- Reaching those endpoints requires a **JWT** obtained by first logging in at
  `POST /rest/v1/jwt` (gas `EndPointJWT`) and then sending it as a
  `Authorization: Bearer <jwt>` header.

There is a **known limitation, documented here and deliberately not fixed as
part of this work**: the current Go `mlwh.RemoteClient` (`mlwh/remote.go`) does
**not** support secured-mode servers. It builds request URLs by appending each
`Registry` entry's root path directly to the base URL, so it does **not** add
the `/rest/v1/auth` prefix; and while it will attach a static
`Authorization: Bearer` header when a token is preconfigured, it does **not**
perform the `POST /rest/v1/jwt` login flow to mint or refresh that JWT. As a
result, `RemoteClient` is only expected to work against an unauthenticated
`wa mlwh serve`. Pointing it at a secured server would request the wrong paths
and would not authenticate. Adding the `/rest/v1/auth` prefix and a JWT login
flow to `RemoteClient` is out of scope for this change.
