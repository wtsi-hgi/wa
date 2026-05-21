# Results Auth Specification

## Overview

Add LDAP-backed authentication and result access control to `wa results`.
The Go results server moves from Chi/plain HTTP to go-authserver/Gin/HTTPS.
Public listing, search, stats, and metadata-key endpoints remain visible to
all users, but result rows include access state so locked rows can be greyed
out. Result detail and file handlers protect content, but no-JWT direct reads
must reach handlers so they can return locked JSON. Mutations require auth.

Access to a result set is granted only when the stored output-directory GID is
non-null and the authenticated canonical username is the requester or operator,
or one of the authenticated user's Unix group IDs equals that GID. Legacy rows
with null GID are inaccessible through normal user authorization.

The CLI uses go-authserver's server-token-file flow. The user who started the
backend can register without a password via a private server token. Other users
can register after LDAP password login; their registration operator is forced
to their authenticated username. Non-registration mutations remain
server-owner-token-only.

## Architecture

### Dependencies

- Remove `github.com/go-chi/chi/v5`.
- Add `github.com/wtsi-hgi/go-authserver`, `github.com/gin-gonic/gin`,
  `github.com/go-ldap/ldap/v3`, and transitive authserver dependencies.
- Keep GoConvey tests. Use `httptest` with Gin handlers for route tests.

### Upstream behaviours to follow

- go-authserver constants:
    - `gas.EndPointREST == "/rest/v1"`
    - `gas.EndPointJWT == "/rest/v1/jwt"`
    - `gas.EndPointAuth == "/rest/v1/auth"`
- `POST /rest/v1/jwt` accepts form or JSON `username` and `password` and
  returns a JSON string JWT.
- `GET /rest/v1/jwt` refreshes a valid JWT.
- Auth middleware reads `cookie: jwt` or `Authorization: Bearer <jwt>`.
- JWTs contain `Username` and `UID`; `gas.User.GIDs()` returns Unix group IDs.
- JWTs do not contain login method or owner-token state.
- go-authserver has no logout/revocation endpoint.
- `EnableAuthWithServerToken(cert, key, tokenBasename, authCB)` stores a
  private token in `gas.TokenDir()/tokenBasename`; `TokenDir()` is
  `$XDG_STATE_HOME`, falling back to the user's home directory.
- Token files must have mode `0600`; loose permissions return
  `gas.JWTPermissionsError`.
- `gas.NewClientCLI(jwtBasename, serverTokenBasename, addr, cert, oktaMode)`
  refreshes a stored JWT, then logs in using the server token if readable,
  otherwise prompts for password on a terminal.
- `gas.NewClientRequest(addr, cert)` and authenticated variants always use
  HTTPS. `cert` is an optional root certificate path.

### Routes

Use go-authserver/Gin route groups. Keep result paths under `/results`, but
prefix them with go-authserver's route bases.

| Method | Path                              | Auth  | Action                   |
| ------ | --------------------------------- | ----- | ------------------------ |
| GET    | `/rest/v1/results`                | opt   | search/list              |
| GET    | `/rest/v1/results/stats`          | opt   | public stats/latest      |
| GET    | `/rest/v1/results/meta-keys`      | no    | metadata keys            |
| GET    | `/rest/v1/results/:id`            | opt   | locked detail check      |
| GET    | `/rest/v1/results/:id/files`      | opt   | locked file list check   |
| GET    | `/rest/v1/results/:id/file`       | opt   | locked file check        |
| GET    | `/rest/v1/auth/session`           | yes   | current user             |
| POST   | `/rest/v1/auth/logout`            | yes   | clear owner marker       |
| GET    | `/rest/v1/auth/results`           | yes   | search/list with access  |
| GET    | `/rest/v1/auth/results/stats`     | yes   | stats/latest with access |
| POST   | `/rest/v1/auth/results`           | yes   | register/upsert          |
| GET    | `/rest/v1/auth/results/:id`       | yes   | result detail            |
| GET    | `/rest/v1/auth/results/:id/files` | yes   | result file list         |
| GET    | `/rest/v1/auth/results/:id/file`  | yes   | file content             |
| PUT    | `/rest/v1/auth/results/:id/files` | owner | rescan                   |
| DELETE | `/rest/v1/auth/results/:id`       | owner | delete                   |

`opt` means unauthenticated callers are allowed. Public detail/file routes must
not sit behind auth middleware: they load the result, evaluate access with nil
user, and return 403 locked JSON for existing rows. Authenticated frontend
callers should use `/rest/v1/auth/...` where available so row access is
calculated for the logged-in user. Public callers receive rows with
`locked=true` unless no user is required for that endpoint.

### WA owner sessions

go-authserver server-token and LDAP logins for the same username mint
indistinguishable JWTs. WA must not derive owner privileges from username.

Install WA middleware around `POST` and `GET /rest/v1/jwt`:

- `POST`: parse username/password without consuming the body. If username is
  the server-starting user and `gas.TokenMatches(password, serverToken)` is
  true, capture the successful JSON string JWT response and mark its hash as an
  owner session until the JWT `exp`.
- `GET`: if the incoming JWT is already an owner session, capture the refreshed
  JSON string JWT response and mark the new token owner until its `exp`.
- `CurrentUser.IsOwner` is true only when the raw request JWT hash is currently
  in the WA owner-session store.
- Server restart clears the in-memory store; the owner must log in with the
  server token again.
- `POST /rest/v1/auth/logout` deletes the owner marker for the current JWT and
  returns 204. It cannot revoke a go-authserver JWT.

### Types

```go
// AccessState describes access for the current authenticated user.
type AccessState struct {
    CanView bool   `json:"can_view"`
    Locked  bool   `json:"locked"`
    Reason  string `json:"reason,omitempty"`
}

// ResultSet is the core domain object returned by queries.
type ResultSet struct {
    ID                 string            `json:"id"`
    PipelineIdentifier string            `json:"pipeline_identifier"`
    RunKey             string            `json:"run_key"`
    Requester          string            `json:"requester"`
    Operator           string            `json:"operator"`
    Command            string            `json:"command"`
    PipelineName       string            `json:"pipeline_name"`
    PipelineVersion    string            `json:"pipeline_version"`
    OutputDirectory    string            `json:"output_directory"`
    OutputDirectoryGID *int64            `json:"output_directory_gid"`
    Metadata           map[string]string `json:"metadata"`
    Access             AccessState       `json:"access"`
    CreatedAt          time.Time         `json:"created_at"`
    UpdatedAt          time.Time         `json:"updated_at"`
}

// AuthenticatedUser is the subset needed from gas.User.
type AuthenticatedUser interface {
    GIDs() ([]string, error)
}

type CurrentUser struct {
    Username string
    User     AuthenticatedUser
    IsOwner  bool
}

type SessionResponse struct {
    Authenticated bool   `json:"authenticated"`
    Username      string `json:"username"`
    IsOwner       bool   `json:"is_owner"`
}

type OwnerSessionStore interface {
    MarkOwner(jwt string, expiresAt time.Time)
    IsOwner(jwt string) bool
    Delete(jwt string)
}

var ErrLocked = errors.New("results: locked")

type LockedResponse struct {
    Error    string `json:"error"`
    Locked   bool   `json:"locked"`
    ResultID string `json:"result_id,omitempty"`
    Message  string `json:"message"`
}
```

`SearchResult` and `StatsResult.Recent` keep their existing shapes; access is
stored in each nested `ResultSet`.

### SQL

Add nullable `output_directory_gid` to `result_sets`.

```sql
ALTER TABLE result_sets ADD COLUMN output_directory_gid BIGINT NULL;
```

`NewStore` must create the column for new SQLite/MySQL databases and migrate
old databases idempotently. No backfill is attempted. Null means inaccessible
to all normal users, even when requester or operator matches.

### CLI and server constants

```go
const (
    resultsServerTokenBasename = ".wa-results-server.token"
    resultsJWTBasename         = ".wa-results.jwt"
)
```

`wa results serve` flags:

| Flag                  | Default                                       | Meaning                                       |
| --------------------- | --------------------------------------------- | --------------------------------------------- |
| `--url`               | `WA_RESULTS_SERVER_URL` or `127.0.0.1:<port>` | HTTPS bind addr                               |
| `--port`              | `8080`                                        | Deprecated alias used only when `--url` unset |
| `--cert`              | `WA_RESULTS_SERVER_CERT`                      | TLS cert/root cert path                       |
| `--key`, `-k`         | `WA_RESULTS_SERVER_KEY`                       | TLS key path                                  |
| `--acme`, `-a`        | `WA_RESULTS_SERVER_ACME`                      | ACME directory URL                            |
| `--cache`, `-c`       | `WA_RESULTS_SERVER_CACHE`                     | ACME cert cache dir                           |
| `--ldap_server`, `-s` | `WA_RESULTS_LDAP_SERVER`                      | LDAP FQDN                                     |
| `--ldap_dn`, `-l`     | `WA_RESULTS_LDAP_DN`                          | bind DN with `%s`                             |
| `--server-token`      | basename const                                | server token basename or abs path             |

Real served modes must reject startup unless:

- `--cert` and `--key`, or `--acme` and `--cache`, are supplied.
- `--ldap_server` and `--ldap_dn` are supplied.

Tests may inject fake auth and use in-memory Gin handlers. No production or
development mode may fall back to passwordless auth. LDAP checks use
`ldaps://<ldap_server>:636` and bind as `fmt.Sprintf(ldap_dn, username)`,
returning the UID from `gas.UserNameToUID(username)` on success.

## A. Gin, HTTPS, and Auth Server

### A1: Gin route migration

As a backend maintainer, I want the results API served by go-authserver/Gin, so
that auth middleware and JWT refresh are shared with the upstream library.

Replace Chi URL params with `gin.Context.Param`, JSON helpers with Gin JSON
responses, and route registration with `gas.Server.Router()` and
`gas.Server.AuthRouter()`.

**Package:** `results/`
**File:** `results/server.go`, `results/server_file.go`
**Test file:** `results/server_test.go`, `results/server_file_test.go`

```go
type Server struct {
    store     *Store
    validator *SeqmetaValidator
    resolver  SearchResolver
}

func NewServer(
    store *Store,
    validator *SeqmetaValidator,
    resolver SearchResolver,
    opts ...ServerOption,
) *Server

func (s *Server) RegisterRoutes(router *gin.Engine, auth *gin.RouterGroup)
```

**Acceptance tests:**

1. Given a Gin router with `RegisterRoutes`, when `GET /rest/v1/results` is
   called, then status 200 and body is a JSON array of all matching rows.
2. Given a registered result, when
   `GET /rest/v1/auth/results/<id>` is called with a fake authorized user,
   then status 200 and the JSON `id` equals the stored ID.
3. Given a valid JWT for user `alice`, when
   `GET /rest/v1/auth/session` is called, then status is 200 and body is
   `{"authenticated":true,"username":"alice","is_owner":false}`.
4. Given all existing result route tests, when migrated to Gin, then every
   previous success/error status and JSON body is preserved except paths now
   include `/rest/v1` or `/rest/v1/auth`.

### A2: HTTPS-only serving

As an operator, I want `wa results serve` to require TLS and LDAP, so that
browser and CLI credentials are never sent over plain HTTP.

Use `gas.New(logWriter)`, `EnableAuthWithServerToken`, `Start`, `StartACME`,
or `StartACMETLSOnly` as appropriate. `--port` alone is no longer an HTTP
server.

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_serve_test.go`

**Acceptance tests:**

1. Given `wa results serve --db db.sqlite --url 127.0.0.1:8443` without certs,
   when command validation runs, then it returns
   `you must supply --cert and --key, or --acme and --cache`.
2. Given cert/key but no LDAP flags, when validation runs in non-test mode,
   then it returns `--ldap_server and --ldap_dn are required`.
3. Given `--cert cert.pem --key key.pem --ldap_server ldap.example.org
--ldap_dn 'uid=%s,ou=people,dc=example,dc=org'`, when the server is wired
   with fakes, then `EnableAuthWithServerToken` receives those cert/key paths
   and `resultsServerTokenBasename`.
4. Given `--acme https://acme.example/dir --cache .tmp/certs`, when cache dir
   exists with permissions other than `0700`, then startup fails with
   `cert cache directory must only be readable by the server user`.
5. Given legacy `--port 9443` and cert/key/LDAP flags, when validation runs,
   then the bind addr is `127.0.0.1:9443` and the scheme remains HTTPS.

### A3: LDAP authentication callback

As a user, I want my LDAP password checked by the results server, so that only
real Unix identities receive JWTs.

**Package:** `internal/authldap/`
**File:** `internal/authldap/ldap.go`
**Test file:** `internal/authldap/ldap_test.go`

```go
type Dialer interface {
    Bind(username, password string) error
    Close()
}

type DialFunc func(address string) (Dialer, error)
type UIDLookup func(username string) (string, error)

func CheckPassword(
    dial DialFunc,
    lookup UIDLookup,
    ldapServer string,
    bindDN string,
    username string,
    password string,
) (bool, string)
```

**Acceptance tests:**

1. Given username `alice`, password `secret`, LDAP server `ldap.example.org`,
   bind DN `uid=%s,ou=people,dc=example,dc=org`, and fake UID lookup returns
   `1001`, when the fake dialer succeeds, then address is
   `ldaps://ldap.example.org:636`, bind username is
   `uid=alice,ou=people,dc=example,dc=org`, result is `true`, and UID is
   `1001`.
2. Given UID lookup fails, when `CheckPassword` runs, then it returns
   `false, ""` and does not dial LDAP.
3. Given LDAP bind fails, when `CheckPassword` runs, then it returns
   `false, ""`.

### A4: Owner session tracking and logout

As an administrator, I want owner-only requests tied to server-token login, so
that LDAP login as the same username never grants owner privileges.

**Package:** `results/`
**File:** `results/auth.go`
**Test file:** `results/auth_test.go`, `results/server_test.go`

```go
type OwnerSessionConfig struct {
    ServerUsername string
    ServerToken    []byte
    Store          OwnerSessionStore
}

func OwnerSessionMiddleware(cfg OwnerSessionConfig) gin.HandlerFunc
func CurrentUserFromContext(
    c *gin.Context,
    store OwnerSessionStore,
) (*CurrentUser, error)
```

**Acceptance tests:**

1. Given server user `svc` and the server token, when
   `POST /rest/v1/jwt` logs in as `svc` with that token, then the returned
   JWT is marked owner and `/rest/v1/auth/session` returns
   `{"authenticated":true,"username":"svc","is_owner":true}`.
2. Given the server-starting username is `svc`, when `svc` logs in with an
   LDAP/password credential instead of the server token, then the returned JWT
   is not marked owner and session returns `"is_owner":false`.
3. Given an owner JWT, when `GET /rest/v1/jwt` refreshes it, then the refreshed
   JWT is marked owner and the old token may be removed from the store.
4. Given an LDAP/password JWT for server username `svc`, when it is refreshed,
   then the refreshed JWT is not marked owner.
5. Given any authenticated JWT, when `POST /rest/v1/auth/logout` is called,
   then status is 204 and any owner marker for that JWT is deleted.
6. Given an unknown JWT with `Username=="svc"`, when `CurrentUserFromContext`
   builds the user, then `IsOwner=false`.

## B. Stored GID and Access State

### B1: Persist output directory GID

As a backend maintainer, I want to store the output directory's Unix GID at
registration, so that access checks never stat the filesystem during search or
page loads.

`POST /rest/v1/auth/results` ignores any client-supplied GID and stats
`registration.OutputDirectory` on the server before validation/upsert. Failure
to determine the GID rejects the registration.

**Package:** `results/`
**File:** `results/store.go`, `results/types.go`, `results/server.go`
**Test file:** `results/store_test.go`, `results/server_test.go`

```go
func (s *Store) Upsert(
    ctx context.Context,
    reg *Registration,
) (*ResultSet, error)
func OutputDirectoryGID(path string) (*int64, error)
```

**Acceptance tests:**

1. Given a directory with Unix GID `1234`, when it is registered, then
   `result_sets.output_directory_gid` is `1234` and JSON includes
   `"output_directory_gid":1234`.
2. Given a registration JSON containing `"output_directory_gid":9999`, when
   the actual directory GID is `1234`, then the stored value is `1234`.
3. Given the output directory cannot be read with stat, when registration is
   called,
   then status is 400 and body contains `determine output directory gid`.
4. Given an old database without `output_directory_gid`, when `NewStore`
   opens it, then the column exists and existing rows contain NULL.
5. Given a legacy row with NULL GID and requester `alice`, when access is
   checked for user `alice`, then `CanView=false` and `Locked=true`.

### B2: Evaluate access

As a backend maintainer, I want one access evaluator, so that
requester/operator/group rules are consistent.

**Package:** `results/`
**File:** `results/auth.go`
**Test file:** `results/auth_test.go`

```go
func AccessForResult(result ResultSet, user *CurrentUser) (AccessState, error)
func RequireResultAccess(result ResultSet, user *CurrentUser) error
func RequireServerOwner(user *CurrentUser) error
```

**Acceptance tests:**

1. Given result GID `200`, requester `alice`, and user `alice` with no groups,
   when access is evaluated, then `CanView=true`, `Locked=false`.
2. Given result GID `200`, operator `alice`, and user `alice`, then
   `CanView=true`.
3. Given result GID `200` and user `bob` with `GIDs()==[]string{"100","200"}`,
   then `CanView=true`.
4. Given result GID `200` and user `bob` with `GIDs()==[]string{"100"}`, then
   `CanView=false`, `Locked=true`, `Reason=="forbidden"`.
5. Given nil user, then `CanView=false`, `Locked=true`,
   `Reason=="login_required"`.
6. Given `GIDs()` returns an error, then access returns that error and no row
   is marked accessible.
7. Given `user.IsOwner=true`, when `RequireServerOwner` runs, then it returns
   nil; for `IsOwner=false`, it returns `ErrLocked`.

### B3: Annotate public results

As a viewer, I want search and latest rows to show whether I can open them, so
that locked rows are visible but not clickable.

Public endpoints annotate with nil user. Auth endpoints annotate with the
current go-authserver user. Both return all rows matching the query.

**Package:** `results/`
**File:** `results/server.go`
**Test file:** `results/server_test.go`

```go
func AnnotateAccess(results []ResultSet, user *CurrentUser) ([]ResultSet, error)
```

**Acceptance tests:**

1. Given two stored results, when anonymous `GET /rest/v1/results` is called,
   then both rows are returned and each has
   `"access":{"can_view":false,"locked":true,"reason":"login_required"}`.
2. Given user `alice` can access one of two results, when
   `GET /rest/v1/auth/results` is called, then both rows are returned, the
   accessible row has `can_view=true`, and the other has `locked=true`.
3. Given a study search that returns `SearchResult`, when the authenticated
   route is called, then each nested `result_set.access` is populated.
4. Given anonymous study or library search returns `SearchResult`, when
   `GET /rest/v1/results` is called, then all wrapped rows are returned and
   each nested `result_set.access` equals
   `{"can_view":false,"locked":true,"reason":"login_required"}`.
5. Given stats recent contains one accessible and one inaccessible row, when
   `GET /rest/v1/auth/results/stats` is called, then both recent rows are
   returned with correct `access` values and aggregate counts are unchanged.

## C. Protected APIs and Mutations

### C1: Protect detail and file APIs

As a viewer, I want inaccessible direct URLs to return a stable locked JSON, so
that the frontend can render a locked state.

Protected detail, file list, and file content endpoints exist in public and
auth route groups. Public routes are for no-JWT direct URL fetches: they first
load the result, then call `RequireResultAccess` with nil user. Auth routes use
the current go-authserver user. Not-found remains 404. Locked is 403. Public
routes never return result detail, file lists, or file bytes.

**Package:** `results/`
**File:** `results/server.go`, `results/server_file.go`
**Test file:** `results/server_test.go`, `results/server_file_test.go`

```go
func WriteLocked(c *gin.Context, resultID string)
```

Locked response:

```json
{
    "error": "locked",
    "locked": true,
    "result_id": "abc",
    "message": "You do not have access to this result set"
}
```

**Acceptance tests:**

1. Given no JWT and existing result `abc`, when `GET /rest/v1/results/abc` is
   called, then status is 403 and body equals the locked JSON shape above.
2. Given no JWT and existing result `abc`, when
   `GET /rest/v1/results/abc/files` is called, then status is 403 and no file
   list is returned.
3. Given no JWT and existing result `abc`, when
   `GET /rest/v1/results/abc/file?path=<registered>` is called, then status is
   403 and no file bytes are returned.
4. Given no JWT and missing result `missing`, when
   `GET /rest/v1/results/missing` is called, then status is 404.
5. Given user `bob` lacks access to result `abc`, when
   `GET /rest/v1/auth/results/abc` is called, then status is 403 and body
   equals the locked JSON shape above.
6. Given the same user, when `GET /rest/v1/auth/results/abc/files` is called,
   then status is 403 and no file list is returned.
7. Given the same user, when
   `GET /rest/v1/auth/results/abc/file?path=<registered>` is called, then
   status is 403 and no file bytes are returned.
8. Given user `alice` has access, when each auth endpoint is called, then
   existing 200 behaviours and content headers are preserved.

### C2: Registration authorization

As a pipeline operator, I want to register after LDAP login, and as the server
owner I want passwordless registration, so that CLI registration is secure and
ergonomic.

Server-owner-token users may set any requester/operator. LDAP/password users
may set requester, but server forces operator to authenticated username before
validation and upsert.

**Package:** `results/`, `cmd/`
**File:** `results/server.go`, `cmd/results.go`
**Test file:** `results/server_test.go`, `cmd/results_register_test.go`

**Acceptance tests:**

1. Given a JWT marked owner by server-token login and registration requester
   `alice`, operator `carol`, when POST runs, then stored requester is `alice`
   and operator is `carol`.
2. Given LDAP user `bob` and registration requester `alice`, operator `carol`,
   when POST runs, then stored requester is `alice` and operator is `bob`.
3. Given the server-starting username is `svc` but the JWT came from
   LDAP/password login, when registration requester is `alice` and operator is
   `carol`, then stored requester is `alice` and operator is `svc`.
4. Given LDAP user `bob` and `--json` registration with operator `carol`, when
   CLI register posts it, then backend response contains `"operator":"bob"`.
5. Given unauthenticated POST to `/rest/v1/results`, when called, then status
   is 404 or 405; registration is only available under `/rest/v1/auth`.

### C3: Server-owner-only mutations

As an administrator, I want delete and rescan to remain owner-only, so that
LDAP registration does not grant broad mutation power.

Apply `RequireServerOwner` to delete, rescan, and any future non-registration
mutation endpoint in this feature.

**Package:** `results/`, `cmd/`
**File:** `results/server.go`, `cmd/results.go`
**Test file:** `results/server_test.go`, `cmd/results_delete_test.go`

**Acceptance tests:**

1. Given LDAP user `alice` with result access, when
   `DELETE /rest/v1/auth/results/<id>` is called, then status is 403 locked
   and the row still exists.
2. Given server-owner-token user, when the same delete is called, then status
   is 204 and the row is removed.
3. Given LDAP user `alice`, when
   `PUT /rest/v1/auth/results/<id>/files` is called, then status is 403.
4. Given server-owner-token user, when rescan is called with valid output
   files, then status is 200 and files are replaced.
5. Given the server-starting username is `svc` but the JWT came from
   LDAP/password login, when delete or rescan is called, then status is 403 and
   no mutation occurs.

## D. CLI Auth Flow

### D1: Authenticated CLI client

As a CLI user, I want `wa results` commands to use stored JWTs, server tokens,
or LDAP password prompts, so that I do not pass credentials on the command
line.

Use go-authserver `ClientCLI`. `--server` remains a full HTTPS URL for WA
users, but auth wiring passes `host[:port]` to gas. `--cert` is the backend
CA/cert path used to trust self-signed dev certificates.

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_register_test.go`, `cmd/results_get_test.go`

```go
type resultsAuthClient interface {
    AuthenticatedRequest() (*resty.Request, error)
    CanReadServerToken() bool
}

func newResultsAuthClient(
    serverURL string,
    certPath string,
    username ...string,
) (resultsAuthClient, error)
```

Add persistent flags to `wa results`:

| Flag       | Default                           | Meaning            |
| ---------- | --------------------------------- | ------------------ |
| `--server` | `https://127.0.0.1:<active port>` | results server URL |
| `--cert`   | `WA_RESULTS_SERVER_CERT`          | CA/cert to trust   |

**Acceptance tests:**

1. Given `$XDG_STATE_HOME/.wa-results-server.token` exists with mode `0600`
   and no JWT exists, when `wa results register` runs as the server owner,
   then no password prompt is emitted and the request has
   `Authorization: Bearer <jwt>`.
2. Given no token files and stdin is a terminal, when `wa results register`
   runs, then it prompts exactly `Password: ` and stores
   `$XDG_STATE_HOME/.wa-results.jwt` with mode `0600`.
3. Given stored JWT permissions are `0777`, when any authenticated CLI command
   runs, then it returns the go-authserver permissions error and does not
   prompt for a password.
4. Given `--server http://127.0.0.1:8080`, when an authenticated command runs,
   then it fails with `results server URL must use https`.
5. Given `--server https://host:8443/api`, when parsed for gas auth, then it
   returns an error because go-authserver CLI auth endpoints require an origin
   URL with no path.

### D2: CLI endpoint permissions

As a CLI user, I want read commands to authenticate when they call protected
endpoints, while public search remains available.

`search` may call public `/rest/v1/results`. `get`, `get --files`, file
downloads if present, `register`, `delete`, and `rescan` use auth.

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_search_test.go`, `cmd/results_get_test.go`

**Acceptance tests:**

1. Given no token and no terminal, when `wa results search` runs, then it
   calls `/rest/v1/results` and succeeds without auth.
2. Given no token and no terminal, when `wa results get <id>` runs, then it
   fails with `authentication failed`.
3. Given an authenticated user lacking access, when `wa results get <id>` gets
   a 403 locked body, then stderr/error contains
   `results server returned 403: locked`.
4. Given an authenticated user with access, when `wa results get --files <id>`
   runs, then it fetches `/rest/v1/auth/results/<id>` and
   `/rest/v1/auth/results/<id>/files` and prints valid JSON.

## E. Next.js Auth and Locked UI

### E1: Server-side auth handlers

As a browser user, I want login/logout through Next.js server-side handlers, so
that JWTs stay in HTTP-only secure cookies.

Add server actions or route handlers that proxy to the Go backend and manage
browser cookies. Do not expose LDAP passwords or JWTs to client components.

**Package:** `frontend/`
**File:** `frontend/app/(results)/auth/actions.ts`,
`frontend/app/api/auth/login/route.ts`,
`frontend/app/api/auth/logout/route.ts`,
`frontend/app/api/auth/refresh/route.ts`
**Test file:** `frontend/tests/auth-actions.test.ts`,
`frontend/tests/integration/auth-api.test.ts`

Cookies:

- `wa_results_jwt`: HTTP-only, Secure, SameSite=Lax, Path=/.
- Delete cookie on logout by setting max age 0.

```ts
export type CurrentSession = {
    authenticated: boolean;
    username: string | null;
};

export async function loginAction(input: {
    username: string;
    password: string;
}): Promise<CurrentSession>;

export async function logoutAction(): Promise<CurrentSession>;
export async function refreshAction(): Promise<CurrentSession>;
export async function currentSession(): Promise<CurrentSession>;
```

**Acceptance tests:**

1. Given backend `POST /rest/v1/jwt` returns JSON string `"jwt-1"`, when
   `loginAction({username:"alice", password:"secret"})` runs, then it sends
   those credentials to the backend, sets `wa_results_jwt=jwt-1` with
   HTTP-only Secure SameSite=Lax, and returns username `alice`.
2. Given backend login returns 401, when `loginAction` runs, then no JWT cookie
   is set and the returned/raised error message is `authentication failed`.
3. Given a JWT cookie exists, when `refreshAction` runs, then it calls
   `GET /rest/v1/jwt` with `Authorization: Bearer <jwt>`, replaces the cookie
   with the refreshed token, and preserves `Secure`.
4. Given a JWT cookie exists, when logout runs, then it calls
   `POST /rest/v1/auth/logout` with `Authorization: Bearer <jwt>`, expires
   `wa_results_jwt`, and session becomes
   `{authenticated:false, username:null}`.
5. Given a JWT cookie exists, when `currentSession` runs, then it calls
   `GET /rest/v1/auth/session` with `Authorization: Bearer <jwt>` and returns
   the backend username.
6. Given the backend logout route returns 404 or 501, when logout runs, then
   the cookie is still expired because go-authserver has no JWT revocation
   endpoint.

### E2: Secure backend client

As a frontend server component, I want backend calls to trust dev certificates
explicitly, so that TLS verification is never globally disabled.

`WA_RESULTS_BACKEND_URL` must be HTTPS for frontend runtime. In development,
`WA_RESULTS_BACKEND_CA_CERT` points to the backend certificate or CA PEM. The
Node fetch/agent path uses that CA only for WA backend requests. `fetchResult`
uses `/rest/v1/auth/results/<id>` when `wa_results_jwt` exists and public
`/rest/v1/results/<id>` when it does not.

**Package:** `frontend/`
**File:** `frontend/lib/backend-client.ts`,
`frontend/app/(results)/actions.ts`, `frontend/app/api/file/route.ts`
**Test file:** `frontend/tests/backend-client.test.ts`,
`frontend/tests/actions-auth.test.ts`, `frontend/tests/file-proxy.test.ts`

**Acceptance tests:**

1. Given `WA_RESULTS_BACKEND_URL=http://host`, when `resultsJson` is called,
   then it throws `results backend URL must use https`.
2. Given `WA_RESULTS_BACKEND_CA_CERT=/tmp/ca.pem`, when `resultsJson` calls
   fetch, then the request uses a TLS agent/root CA derived from that file.
3. Given no CA env var in production, when fetching `https://host`, then the
   default trust store is used.
4. Given a JWT cookie exists, when `fetchResult` runs, then it calls
   `/rest/v1/auth/results/<id>` with `Authorization: Bearer <jwt>`.
5. Given no JWT cookie, when `fetchResult("abc")` runs, then it calls
   `/rest/v1/results/abc` without `Authorization` and preserves the backend
   403 locked JSON response.
6. Given no JWT cookie, when landing page data loads, then search/stats use
   public `/rest/v1/results` and `/rest/v1/results/stats`.
7. Given `wa_results_jwt=<jwt>` exists, when `fetchFiles("abc")` runs, then
   it reads the HTTP-only cookie and calls `/rest/v1/auth/results/abc/files`
   with `Authorization: Bearer <jwt>`.
8. Given `wa_results_jwt=<jwt>` exists, when
   `fetchFileContent("abc", "/out/a.txt")` runs, then it reads the HTTP-only
   cookie and calls `/rest/v1/auth/results/abc/file?path=%2Fout%2Fa.txt` with
   `Authorization: Bearer <jwt>`.
9. Given `wa_results_jwt=<jwt>` exists, when
   `GET /api/file?id=abc&path=%2Fout%2Fa.txt` runs, then
   `frontend/app/api/file/route.ts` reads the HTTP-only cookie and proxies to
   `/rest/v1/auth/results/abc/file?path=%2Fout%2Fa.txt` with
   `Authorization: Bearer <jwt>`.
10. Given any of `fetchResult`, `fetchFiles`, `fetchFileContent`, or
    `/api/file` receives status 403 with locked JSON, then the locked response
    is preserved for the detail locked page or returned by the proxy with
    status 403 and body containing `"error":"locked"`, `"locked":true`,
    `"result_id":"abc"`, and message
    `You do not have access to this result set`.

### E3: Compact login/logout tool

As a browser user, I want a compact login/logout control in the top right, so
that I can see who I am and switch auth state without leaving the page.

Use shadcn/ui components:

- `Button` for login and icon triggers.
- existing `DropdownMenu` for the account/logout menu.
- add `Avatar` for the compact signed-in trigger.
- add `Badge` for username/access state labels where needed.
- add `Tooltip` for lock and icon-only controls.
- add `Alert` for the direct locked-state page.

Use lucide `LogIn`, `LogOut`, `User`, and `LockKeyhole` icons.

**Package:** `frontend/`
**File:** `frontend/components/auth-menu.tsx`,
`frontend/app/(results)/layout.tsx`
**Test file:** `frontend/tests/auth-menu.test.tsx`,
`frontend/e2e/results-auth.spec.ts`

**Acceptance tests:**

1. Given anonymous session, when the landing page renders, then the top-right
   tool shows a login button with accessible name `Log in`.
2. Given user `alice`, when the landing page renders, then the top-right tool
   shows `alice` and a menu item with accessible name `Log out`.
3. Given failed login, when the form submits, then focus remains in the login
   control and an error message `Authentication failed` is announced.
4. Given successful logout, when the menu item is clicked, then the username is
   removed and the login button is shown.

### E4: Locked rows and direct locked page

As any user, I want inaccessible result rows to be visible but disabled, so
that I know the result exists without opening protected content.

Rows with `result_set.access.locked === true` or
`result.access.locked === true` are greyed out, show a lock icon with tooltip,
and are not links. Direct locked detail responses render only a lock symbol and
a link back to `/` or `returnTo`.

**Package:** `frontend/`
**File:** `frontend/components/results-columns.tsx`,
`frontend/components/results-table.tsx`,
`frontend/app/(results)/results/[id]/page-content.tsx`,
`frontend/lib/contracts.ts`
**Test file:** `frontend/tests/results-table.test.tsx`,
`frontend/tests/result-detail-locked.test.tsx`,
`frontend/e2e/results-auth.spec.ts`

**Acceptance tests:**

1. Given a row with `access.locked=true`, when the table renders, then the row
   has `aria-disabled="true"`, opacity is reduced, a lock icon is present, and
   there is no `<a href="/results/<id>">`.
2. Given a row with `access.can_view=true`, when the table renders, then the
   pipeline, unique, requester, date, and output directory cells link to the
   detail page as before.
3. Given `fetchResult` receives 403 locked JSON, when the detail page renders,
   then it shows only the lock icon, text `You do not have access to this
result set`, and a link with accessible name `Back to dashboard`.
4. Given no `wa_results_jwt` cookie and direct navigation to `/results/abc`,
   when the backend public detail route returns 403 locked JSON, then the page
   renders only the lock state and a link with accessible name
   `Back to dashboard`.
5. Given anonymous latest data, when rows render, then every row is locked and
   no row is clickable.

## F. Development HTTPS

### F1: Dev stack certificates

As a developer, I want both backend and frontend dev servers on HTTPS with
explicit trust, so that Secure cookies behave like production.

`run-dev.sh` creates or reuses self-signed dev certificates under `.tmp/`.
The backend is started with `--cert`, `--key`, fake test-only LDAP in test
mode only, and `WA_RESULTS_BACKEND_CA_CERT` is exported for Next. The frontend
uses `next dev --experimental-https --experimental-https-key <key>
--experimental-https-cert <cert>`.

**Package:** repo scripts
**File:** `run-dev.sh`, `.env.development`, `.env.test`, `README.md`
**Test file:** `cmd/run_dev_test.go`, `frontend/tests/next-config.test.ts`

**Acceptance tests:**

1. Given `run-dev.sh --mode dev`, when it starts services, then
   `WA_RESULTS_BACKEND_URL` begins with `https://` and
   `WA_RESULTS_BACKEND_CA_CERT` points to an existing PEM file.
2. Given test mode, when results serve is started, then explicit fake auth is
   used only under test hooks and no LDAP network call is attempted.
3. Given development mode without LDAP flags/env, when results serve starts,
   then it exits with `--ldap_server and --ldap_dn are required`.
4. Given frontend dev command is assembled, then it includes
   `--experimental-https`, `--experimental-https-key`, and
   `--experimental-https-cert`.

## Implementation Order

1. Schema and access core: add GID migration, registration GID capture, access
   evaluator, and store tests.
2. Gin migration: convert server route handlers and tests to Gin while keeping
   auth faked.
3. go-authserver serving: wire HTTPS, LDAP flags, JWT/server-token auth, and
   owner detection in `wa results serve`.
4. Protected API policy: annotate public/auth lists, enforce detail/file/mutate
   authorization, add locked JSON.
5. CLI auth: add `--cert`, gas `ClientCLI`, token basenames, HTTPS URL
   validation, and protected command routing.
6. Next auth proxy: add login/logout/refresh/current-session handlers, secure
   cookies, backend CA trust, and auth/public endpoint selection.
7. Frontend UI: add shadcn components, compact account menu, locked rows, and
   direct locked page.
8. Dev/docs: update `run-dev.sh`, env files, README, and HTTPS health checks.

Phases 1-5 are sequential. Phase 6 can start after route paths and response
contracts from phases 2 and 4 are fixed. Phase 7 follows phase 6. Phase 8 can
run alongside phase 6 after the server flags are stable.

## Appendix: Key Decisions

- Public search/list/stats never filter out inaccessible rows. They only mark
  access state.
- Null output-directory GID always denies normal result viewing, even for
  requester/operator matches.
- `requester` remains user-supplied for LDAP registration; only `operator` is
  forced.
- Server-owner-token auth is tracked by WA owner sessions because
  go-authserver JWTs do not encode login method.
- Server-owner-token auth is the only authority for delete/rescan in this
  feature; matching the server-starting username is not enough.
- Browser logout calls WA backend logout to clear owner markers, then expires
  the cookie. It cannot revoke ordinary go-authserver JWTs.
- Okta routes are not enabled in this feature; go-authserver adoption leaves
  that path available later.
- Development must trust self-signed certs via configured CA/cert paths, never
  by disabling TLS verification globally.
- Acceptance tests map to GoConvey for Go, Vitest for frontend units, and
  Playwright for browser flows. Implementors should follow `go-implementor` and
  `nextjs-fastapi-implementor` style TDD where applicable, while respecting
  this repository's existing package layout.
