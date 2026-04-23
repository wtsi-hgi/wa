# Results REST API Specification

## Overview

`results` package providing a REST API and CLI for registering,
searching, and managing pipeline result sets. A result set groups
output files from a single pipeline run with metadata: requester,
operator, command line, pipeline identity, input files, and
arbitrary key-value pairs.

Key behaviours:

- Deterministic SHA256 ID from composite natural key
  (pipeline_identifier + run_key). Upsert on collision.
- Client-side directory scanning with symlink following, cycle
  detection, and hidden-file exclusion.
- Optional server-side seqmeta validation for `seqmeta_*`
  metadata fields. Strict: invalid -> 422, unreachable -> 502.
- Cross-dialect SQL (SQLite + MySQL) via `database/sql`.
- Unified `wa` Cobra binary merging saga inspect, seqmeta,
  and results subcommand trees.

## Architecture

**Package:** `results/`
**CLI:** `cmd/` (package `cmd`, unified `wa` binary)

**Dependencies:** `database/sql`, `modernc.org/sqlite` (tests),
`github.com/go-chi/chi/v5`, `github.com/spf13/cobra`,
`github.com/go-sql-driver/mysql` (production optional)

### Types

```go
// FileEntry represents one tracked file.
type FileEntry struct {
    Path  string    `json:"path"`
    Mtime time.Time `json:"mtime"`
    Size  int64     `json:"size"`
    Kind  string    `json:"kind"`
}
```

`Kind` values: `"output"`, `"input"`, `"pipeline"`.

```go
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
    Metadata           map[string]string `json:"metadata"`
    CreatedAt          time.Time         `json:"created_at"`
    UpdatedAt          time.Time         `json:"updated_at"`
}

// Registration is the POST /results request body.
type Registration struct {
    PipelineIdentifier string            `json:"pipeline_identifier"`
    RunKey             string            `json:"run_key"`
    Requester          string            `json:"requester"`
    Operator           string            `json:"operator"`
    Command            string            `json:"command"`
    PipelineName       string            `json:"pipeline_name"`
    PipelineVersion    string            `json:"pipeline_version"`
    OutputDirectory    string            `json:"output_directory"`
    Files              []FileEntry       `json:"files"`
    Metadata           map[string]string `json:"metadata"`
}

// SearchParams holds parsed query parameters for filtering.
type SearchParams struct {
    Requester          string
    Operator           string
    PipelineName       string
    PipelineVersion    string
    PipelineIdentifier string
    RunKey             string
    OutputDirPrefix    string
    Meta               map[string]string
}

// Store persists result sets in SQL.
type Store struct{ db *sql.DB }

// Server serves the results REST API.
type Server struct {
    store     *Store
    validator *SeqmetaValidator
    handler   http.Handler
}

// SeqmetaValidator validates seqmeta_* metadata fields
// against a remote seqmeta service.
type SeqmetaValidator struct {
    baseURL string
    client  *http.Client
}
```

Sentinel errors:

```go
var (
    ErrNotFound        = errors.New("results: not found")
    ErrInvalidInput    = errors.New("results: invalid input")
    ErrSeqmetaFailed   = errors.New(
        "results: seqmeta unavailable")
    ErrSeqmetaRejected = errors.New(
        "results: seqmeta validation failed")
)
```

### SQL Schema

Compatible subset for SQLite and MySQL. `NewStore` calls
`PRAGMA foreign_keys = ON` (silently ignored by MySQL).
Timestamps stored as RFC 3339 strings.

```sql
CREATE TABLE IF NOT EXISTS result_sets (
    id                  VARCHAR(64)  NOT NULL PRIMARY KEY,
    pipeline_identifier VARCHAR(512) NOT NULL,
    run_key             VARCHAR(512) NOT NULL,
    requester           VARCHAR(255) NOT NULL,
    operator            VARCHAR(255) NOT NULL,
    command             TEXT         NOT NULL,
    pipeline_name       VARCHAR(255) NOT NULL,
    pipeline_version    VARCHAR(255) NOT NULL,
    output_directory    TEXT         NOT NULL,
    created_at          VARCHAR(30)  NOT NULL,
    updated_at          VARCHAR(30)  NOT NULL
);

CREATE TABLE IF NOT EXISTS result_files (
    result_id VARCHAR(64)  NOT NULL,
    path      TEXT         NOT NULL,
    mtime     VARCHAR(30)  NOT NULL,
    size      BIGINT       NOT NULL,
    kind      VARCHAR(10)  NOT NULL,
    FOREIGN KEY (result_id)
        REFERENCES result_sets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS result_metadata (
    result_id VARCHAR(64)  NOT NULL,
    meta_key  VARCHAR(255) NOT NULL,
    value     TEXT         NOT NULL,
    PRIMARY KEY (result_id, meta_key),
    FOREIGN KEY (result_id)
        REFERENCES result_sets(id) ON DELETE CASCADE
);
```

ID is deterministic `SHA256(pipeline_identifier + "\x00" +
run_key)`, so the PRIMARY KEY on `id` implicitly enforces
uniqueness of the natural key pair.

### REST API Routes (chi)

| Method | Path                   | Action                 |
|--------|------------------------|------------------------|
| POST   | /results               | Upsert result set      |
| GET    | /results               | Search (query params)  |
| GET    | /results/{id}          | Get one result set     |
| GET    | /results/{id}/files    | Get file list          |
| PUT    | /results/{id}/files    | Replace output files   |
| DELETE | /results/{id}          | Hard delete            |

Responses: `application/json`.
Error body: `{"error":"<message>"}`.
Status codes: 201 created, 200 upsert/query, 204 delete,
400 bad request, 404 not found, 422 seqmeta rejected,
502 seqmeta unreachable, 500 internal.

### Seqmeta Validation

Server-side during POST. Each metadata key with `seqmeta_`
prefix triggers `GET <seqmeta-url>/validate/<value>`. The
suffix maps to an expected `IdentifierType`:

| Metadata key        | Suffix       | Expected type        |
|---------------------|--------------|----------------------|
| seqmeta_runid       | runid        | run_id               |
| seqmeta_studyid     | studyid      | study_id             |
| seqmeta_sampleid    | sampleid     | sanger_sample_id     |
| seqmeta_librarytype | librarytype  | library_type         |

Unknown `seqmeta_*` suffixes not in the map -> 400.
Type mismatch -> 422. Seqmeta unreachable/timeout -> 502.
Empty seqmeta URL -> skip all validation.

```go
// SeqmetaFieldTypes maps metadata key suffixes (after
// stripping "seqmeta_" prefix) to expected identifier types.
var SeqmetaFieldTypes = map[string]string{
    "runid":       "run_id",
    "studyid":     "study_id",
    "sampleid":    "sanger_sample_id",
    "librarytype": "library_type",
}
```

### Key Building

- `CompositeKeyID(pipelineIdentifier, runKey)`:
   for inputs without embedded NUL bytes,
   `hex(SHA256(pipelineIdentifier + "\x00" + runKey))`.
   If either side contains `"\x00"`, use an unambiguous
   length-prefixed byte serialization before hashing so
   distinct natural-key pairs cannot collide.
- `BuildRunKey(runID, additionalUnique)`: URL-query-encoded
  string from sorted key=value pairs. E.g.
  `BuildRunKey("48522", "random_exon")` ->
  `"runid=48522&unique=random_exon"`. Empty additionalUnique
  -> `"runid=48522"`.
- `DetectPipeline(workflowPath)`: if file is in a git
   checkout, returns `(workflowScopedIdentifier, repoName,
   commitHash)`, where the identifier combines the git remote
   (or repo root path when no remote exists) with the
   workflow-relative path so multiple workflows in one repo do
   not collide. If not in git, returns `(cleanedAbsPath,
  parentDirName, hex(SHA256(fileContents)))`. Never fails
  unless file is unreadable.

### Search Query Parameters

Pre-defined fields (exact match on result_sets columns):
`user` (-> requester), `operator`, `pipeline_name`,
`pipeline_version`, `pipeline_identifier`, `run_key`.

Special: `output_dir_prefix` -> `WHERE output_directory LIKE
?` with pattern built as `prefix + "%"` in Go.

Metadata fields (exact match on result_metadata):
- `meta_X=V` -> strips `meta_` prefix, searches
  `meta_key='X'` AND `value='V'`.
- `seqmeta_X=V` -> no prefix stripping, searches
  `meta_key='seqmeta_X'` AND `value='V'`.

Multiple filters are ANDed. Multiple metadata filters require
the result set to have ALL matching entries.

### Unified wa Binary

Root `main.go` creates a Cobra root command from package
`cmd`. Subcommand trees:

```text
wa saga inspect <identifier>
wa seqmeta diff|validate|serve
wa results register|search|get|delete|rescan|serve
```

Package `cmd/` (one file per tree):

- `cmd/root.go` -> `NewRootCommand() *cobra.Command`
- `cmd/saga.go` -> saga inspect (migrated from `main.go`)
- `cmd/seqmeta.go` -> seqmeta subcommands (migrated from
  `cmd/seqmeta/main.go`)
- `cmd/results.go` -> results subcommands (including serve)

Migration: `cmd/seqmeta/` directory removed; its code moves
to `cmd/seqmeta.go`. Root `main.go` saga inspector logic
moves to `cmd/saga.go`. Root `main.go` becomes a thin entry
point. `cmd/seqmeta/main_test.go` moves to
`cmd/seqmeta_test.go`. `main_test.go` saga helper tests move
to `cmd/saga_test.go`.

---

## A. Types and Key Building

### A1: CompositeKeyID

As a developer, I want a deterministic ID from the natural key
pair, so that the same pipeline+run always produces the same
result set ID.

**Package:** `results/`
**File:** `results/types.go`
**Test file:** `results/types_test.go`

```go
func CompositeKeyID(
    pipelineIdentifier, runKey string,
) string
```

Returns a lowercase hex SHA256 digest of an unambiguous byte
serialization of the two key parts. For ordinary inputs
without embedded NUL bytes, that serialization is exactly
`pipelineIdentifier + "\x00" + runKey`.

**Acceptance tests:**

1. Given `pipelineIdentifier="https://github.com/org/nf"` and
   `runKey="runid=48522&unique=random_exon"`, when
   `CompositeKeyID` is called, then the result is a 64-char
   lowercase hex string equal to the SHA256 of those inputs
   joined by `"\x00"`.
2. Given identical inputs called twice, then both calls return
   the same string.
3. Given `pipelineIdentifier="a\x00b"` and `runKey="c"`, vs
   `pipelineIdentifier="a"` and `runKey="b\x00c"`, then the
   two IDs differ because the serialization remains
   unambiguous even when embedded NUL bytes are present.

### A2: BuildRunKey

As a developer, I want to construct a canonical run_key from
operator-supplied parts, so that key building is deterministic.

```go
func BuildRunKey(
    runID, additionalUnique string,
) string
```

Produces a URL-query-encoded string with keys sorted
alphabetically. Only includes non-empty values.

**Acceptance tests:**

1. Given `runID="48522"` and `additionalUnique="random_exon"`,
   then result is `"runid=48522&unique=random_exon"`.
2. Given `runID="48522"` and `additionalUnique=""`, then result
   is `"runid=48522"`.
3. Given `runID=""` and `additionalUnique="random_exon"`, then
   result is `"unique=random_exon"`.
4. Given both empty, then result is `""`.
5. Given `runID="a&b"` and `additionalUnique="c=d"`, then
   special characters are percent-encoded.

### A3: DetectPipeline

As a developer, I want to auto-detect pipeline identity from
a workflow file path, so that operators don't manually supply
redundant information.

**Package:** `results/`
**File:** `results/types.go`
**Test file:** `results/types_test.go`

```go
func DetectPipeline(workflowPath string) (
    identifier, name, version string, err error,
)
```

**Acceptance tests:**

1. Given a file inside a git repo with remote origin
   `https://github.com/org/nf_splicing.git` and HEAD at
   commit `abc123`, when `DetectPipeline("/repo/main.nf")`
   is called, then `identifier ==
   "https://github.com/org/nf_splicing.git"`,
   `name == "nf_splicing"`, `version == "abc123"`.
   (Test creates a real git repo in `t.TempDir()`.)
2. Given a git repo with no remote, when called, then
   `identifier` is the cleaned absolute repo root path,
   `name` is the repo directory name.
3. Given a file NOT in a git repo, when called, then
   `identifier` is the cleaned absolute path of the file,
   `name` is the parent directory name, and `version` is the
   lowercase hex SHA256 of the file's content.
4. Given an unreadable file path, then `err != nil`.
5. Given a relative path `"./main.nf"` that exists, then
   `identifier` is the resolved absolute path.

---

## B. Directory Scanner

### B1: ScanDirectory

As a developer, I want to recursively list files in a directory
with their mtime and size, so that the CLI can build the output
file list for registration.

**Package:** `results/`
**File:** `results/scanner.go`
**Test file:** `results/scanner_test.go`

```go
func ScanDirectory(dir string, includeHidden bool) (
    []FileEntry, int, error,
)
```

Returns file entries (Kind `"output"` for all), a warning
count (number of skipped cyclic symlinks), and an error only
for unreadable root dir. Each entry has absolute path, mtime
from `os.Stat` (follows symlinks), and size in bytes. Skips
directories themselves (files only). Logs a warning to stderr
if file count exceeds 10,000 (no hard limit).

**Acceptance tests:**

1. Given a `t.TempDir()` containing `a.txt` (10 bytes) and
   `sub/b.txt` (20 bytes), when `ScanDirectory(dir, false)`
   is called, then `len(entries) == 2`, all Kind is
   `"output"`, sizes are 10 and 20, mtimes are non-zero,
   and paths are absolute.
2. Given a directory with `.hidden` file and `visible.txt`,
   when `ScanDirectory(dir, false)` is called, then
   `len(entries) == 1` (hidden excluded). When
   `ScanDirectory(dir, true)`, then `len(entries) == 2`.
3. Given a directory with `.hidden_dir/file.txt`, when
   `ScanDirectory(dir, false)`, then `len(entries) == 0`
   (hidden directory and its contents excluded).
4. Given a symlink `link.txt -> real.txt` where `real.txt`
   is 50 bytes, when scanned, then entry for `link.txt` has
   `Size == 50` (target's stat).
5. Given a cyclic symlink (`a -> b`, `b -> a`), when scanned,
   then the cyclic links are skipped, warning count >= 1, and
   no error is returned.
6. Given an empty directory, when scanned, then
   `len(entries) == 0` with no error.
7. Given a non-existent directory, when scanned, then
   `err != nil`.

---

## C. Database Store

### C1: NewStore and schema creation

As a developer, I want to open a `*sql.DB` and auto-create
tables, so that setup requires no manual migration.

**Package:** `results/`
**File:** `results/store.go`
**Test file:** `results/store_test.go`

```go
func NewStore(db *sql.DB) (*Store, error)
func (s *Store) Close() error
```

`NewStore` executes `PRAGMA foreign_keys = ON` (no-op for
MySQL), then runs all three `CREATE TABLE IF NOT EXISTS`
statements. Returns error if any DDL fails.

**Acceptance tests:**

1. Given an in-memory SQLite `*sql.DB`, when `NewStore(db)` is
   called, then error is nil and store is non-nil.
2. Given a valid store, when `Close()` is called, then no
   error.
3. Given `NewStore` called twice on the same DB, then second
   call succeeds (IF NOT EXISTS).

### C2: Upsert result set

As a developer, I want to insert or update a result set
atomically (with files and metadata), so that re-registration
with the same key updates the record.

```go
func (s *Store) Upsert(
    ctx context.Context, reg *Registration,
) (*ResultSet, error)
```

Computes ID via `CompositeKeyID`. Within a transaction:
if ID exists, preserves `created_at` and sets new
`updated_at`; otherwise sets both to now. Deletes old
files and metadata, inserts new ones. Returns the full
`ResultSet` (without files, those are fetched separately).

**Acceptance tests:**

1. Given an empty store and a valid Registration with
   `PipelineIdentifier="pipe"`, `RunKey="run"`,
   `Requester="alice"`, `Operator="bob"`, 2 files, and
   metadata `{"library":"exon"}`, when `Upsert` is called,
   then returned ResultSet has `ID ==
   CompositeKeyID("pipe","run")`, `Requester == "alice"`,
   `CreatedAt` is recent, and `Metadata["library"] == "exon"`.
2. Given a result set already exists with `CreatedAt == T1`,
   when `Upsert` is called again with same key but
   `Requester="charlie"`, then returned `Requester ==
   "charlie"`, `CreatedAt == T1` (preserved), and
   `UpdatedAt > T1`.
3. Given upsert with 3 files, then a second upsert with 1
   file for the same key, when files are retrieved, then
   only 1 file exists (old files replaced).
4. Given registration with `PipelineIdentifier=""`, when
   `Upsert` is called, then error wraps `ErrInvalidInput`.
5. Given registration with `RunKey=""`, when `Upsert` is
   called, then error wraps `ErrInvalidInput`.

### C3: Get result set by ID

As a developer, I want to retrieve a single result set by its
SHA256 ID.

```go
func (s *Store) Get(
    ctx context.Context, id string,
) (*ResultSet, error)
```

Returns metadata (from result_metadata) populated in the
`Metadata` map. Does NOT include files.

**Acceptance tests:**

1. Given a stored result set with `metadata={"k":"v"}`, when
   `Get(ctx, id)` is called, then `Metadata["k"] == "v"` and
   all scalar fields match.
2. Given a non-existent ID, when `Get` is called, then error
   wraps `ErrNotFound`.

### C4: Search result sets

As a developer, I want to filter result sets by pre-defined
fields and metadata, so that users can find relevant results.

```go
func (s *Store) Search(
    ctx context.Context, params SearchParams,
) ([]ResultSet, error)
```

Returns all matching result sets (with metadata, without
files). Empty params returns all. Multiple filters ANDed.
Metadata filters join against result_metadata.
`OutputDirPrefix` uses `LIKE` with pattern `prefix + "%"`.

**Acceptance tests:**

1. Given 3 result sets with requesters "alice", "alice",
   "bob", when `Search(ctx, SearchParams{Requester:"alice"})`
   is called, then `len(results) == 2`.
2. Given a result set with `metadata={"library":"exon"}` and
   another with `metadata={"library":"intron"}`, when
   `Search(ctx, SearchParams{Meta:map{"library":"exon"}})`
   is called, then `len(results) == 1`.
3. Given result sets with output directories `/a/b/c` and
   `/a/d/e`, when `Search(ctx,
   SearchParams{OutputDirPrefix:"/a/b"})` is called, then
   `len(results) == 1`.
4. Given empty `SearchParams{}`, when called, then all stored
   result sets are returned.
5. Given `SearchParams{Requester:"alice",
   PipelineName:"nf"}`, when called, then only result sets
   matching BOTH filters are returned.
6. Given no matching results, when searched, then returned
   slice is empty (not nil) with no error.

### C5: Get files for result set

As a developer, I want to retrieve all tracked files for a
result set.

```go
func (s *Store) GetFiles(
    ctx context.Context, resultID string,
) ([]FileEntry, error)
```

**Acceptance tests:**

1. Given a result set with 2 output files and 1 input file,
   when `GetFiles(ctx, id)` is called, then
   `len(files) == 3` with correct paths, sizes, and kinds.
2. Given a non-existent result ID, when called, then returns
   empty slice with no error.

### C6: Replace output files

As a developer, I want to replace all output files for a result
set (rescan), preserving input and pipeline files.

```go
func (s *Store) ReplaceOutputFiles(
    ctx context.Context,
    resultID string,
    files []FileEntry,
) error
```

Within a transaction: deletes all rows with `kind='output'`
for the result ID, inserts the new files (all must have
`Kind="output"`). Updates `updated_at` on the result set.

**Acceptance tests:**

1. Given a result set with 3 output files and 1 input file,
   when `ReplaceOutputFiles(ctx, id, [2 new files])` is
   called, then `GetFiles` returns 3 total (2 new output +
   1 original input).
2. Given `ReplaceOutputFiles` with an empty slice, then all
   output files are removed but input files remain.
3. Given a non-existent result ID, when called, then error
   wraps `ErrNotFound`.

### C7: Delete result set

As a developer, I want to permanently remove a result set and
all associated files and metadata.

```go
func (s *Store) Delete(
    ctx context.Context, id string,
) error
```

Relies on `ON DELETE CASCADE` for files and metadata. SQLite
requires `PRAGMA foreign_keys = ON` (set by `NewStore`).

**Acceptance tests:**

1. Given a stored result set with files and metadata, when
   `Delete(ctx, id)` is called, then `Get(ctx, id)` returns
   `ErrNotFound` and `GetFiles(ctx, id)` returns empty.
2. Given a non-existent ID, when `Delete` is called, then
   error wraps `ErrNotFound`.

---

## D. Seqmeta Validation

### D1: SeqmetaValidator

As a developer, I want to validate `seqmeta_*` metadata
fields against a remote seqmeta service, so that operator
typos are caught at registration time.

**Package:** `results/`
**File:** `results/validate.go`
**Test file:** `results/validate_test.go`

```go
func NewSeqmetaValidator(
    baseURL string, timeout time.Duration,
) *SeqmetaValidator

// ValidateMetadata checks all seqmeta_* fields in metadata.
// Skips validation if v is nil or baseURL is empty.
// Returns nil if metadata has no seqmeta_* fields.
func (v *SeqmetaValidator) ValidateMetadata(
    ctx context.Context, metadata map[string]string,
) error
```

For each `seqmeta_X` key: look up suffix `X` in
`SeqmetaFieldTypes`. If suffix unknown, return
`ErrInvalidInput`. Call
`GET <baseURL>/validate/<value>`, parse JSON response,
check `type` field matches expected. If mismatch, return
`ErrSeqmetaRejected`. If HTTP error or timeout, return
`ErrSeqmetaFailed`.

**Acceptance tests:**

1. Given an `httptest.Server` returning
   `{"identifier":"48522","type":"run_id","object":{}}` for
   `/validate/48522`, and metadata
   `{"seqmeta_runid":"48522"}`, when `ValidateMetadata` is
   called, then no error.
2. Given seqmeta returns `{"type":"sanger_sample_id"}` for
   value `"48522"` but expected type is `run_id`, when called
   with `{"seqmeta_runid":"48522"}`, then error wraps
   `ErrSeqmetaRejected`.
3. Given metadata `{"seqmeta_unknown":"val"}` (suffix
   `"unknown"` not in `SeqmetaFieldTypes`), when called, then
   error wraps `ErrInvalidInput`.
4. Given an unreachable seqmeta URL, when called with
   `{"seqmeta_runid":"48522"}`, then error wraps
   `ErrSeqmetaFailed`.
5. Given a nil `*SeqmetaValidator`, when `ValidateMetadata`
   is called, then no error (validation skipped).
6. Given metadata with no `seqmeta_*` keys (e.g.
   `{"library":"exon"}`), when called, then no error.
7. Given seqmeta server returns 404 for the identifier, when
   called, then error wraps `ErrSeqmetaRejected`.

---

## E. REST API Server

### E1: POST /results

As a consumer, I want to register a result set via POST, with
optional seqmeta validation, so that pipeline outputs are
tracked.

**Package:** `results/`
**File:** `results/server.go`
**Test file:** `results/server_test.go`

```go
func NewServer(
    store *Store, validator *SeqmetaValidator,
) *Server
func (s *Server) Handler() http.Handler
```

POST body is JSON `Registration`. Server computes ID, calls
`validator.ValidateMetadata` (if non-nil), then calls
`store.Upsert`. Returns 201 on first insert, 200 on upsert.

**Acceptance tests:**

1. Given empty store and valid Registration JSON, when
   `POST /results` is called, then status is 201, response
   body is JSON with `"id"` (64-char hex), `"requester"`,
   `"created_at"`, and `Content-Type` is
   `application/json`.
2. Given same Registration POST'd twice, then second response
   status is 200 and `"created_at"` matches the first.
3. Given Registration with `"seqmeta_runid":"48522"` and a
   validator backed by an `httptest.Server` returning correct
   type, when POST'd, then status is 201.
4. Given seqmeta returns wrong type, then status is 422 and
   body has `"error"` key.
5. Given seqmeta is unreachable, then status is 502.
6. Given Registration missing `pipeline_identifier`, then
   status is 400.
7. Given malformed JSON body, then status is 400.

### E2: GET /results (search)

As a consumer, I want to search result sets with query
parameters, so that I can find relevant pipeline runs.

**Acceptance tests:**

1. Given 2 stored result sets (requesters "alice" and "bob"),
   when `GET /results?user=alice`, then status 200 and JSON
   array has 1 element with `"requester":"alice"`.
2. Given `GET /results?meta_library=exon`, then only result
   sets with metadata key `"library"` = `"exon"` are
   returned.
3. Given `GET /results?output_dir_prefix=/lustre/scratch`,
   then only result sets whose `output_directory` starts
   with that prefix are returned.
4. Given `GET /results` with no params, then all result sets
   are returned.
5. Given no matches, then status 200 and body is `[]`.

### E3: GET /results/{id}

As a consumer, I want to fetch a single result set by ID.

**Acceptance tests:**

1. Given a stored result set, when
   `GET /results/<valid-id>`, then status 200 and body
   matches the stored data including metadata.
2. Given non-existent ID, then status 404 with `"error"` key.

### E4: GET /results/{id}/files

As a consumer, I want to fetch the file list separately from
result set metadata.

**Acceptance tests:**

1. Given a result set with 5 files (3 output, 1 input,
   1 pipeline), when `GET /results/{id}/files`, then status
   200 and JSON array has 5 entries with correct `"kind"`
   values.
2. Given non-existent ID, then status 404.

### E5: PUT /results/{id}/files

As a consumer, I want to replace output files for a result
set (client-side rescan), preserving input and pipeline files.

Request body: JSON array of `FileEntry` (all kind `"output"`).

**Acceptance tests:**

1. Given a result set with 2 output files and 1 input file,
   when `PUT /results/{id}/files` with 3 new output files,
   then status 200 and subsequent `GET /results/{id}/files`
   returns 4 files (3 output + 1 input).
2. Given non-existent ID, then status 404.
3. Given malformed JSON, then status 400.

### E6: DELETE /results/{id}

As a consumer, I want to permanently delete a result set.

**Acceptance tests:**

1. Given a stored result set, when `DELETE /results/{id}`,
   then status 204 and subsequent `GET /results/{id}` returns
   404.
2. Given non-existent ID, then status 404.

---

## F. Unified wa Binary

### F1: Root command and migration

As a developer, I want a single `wa` binary with saga,
seqmeta, and results subcommand trees, so that the repo
produces one deployable artifact.

**Package:** `cmd/`
**File:** `cmd/root.go`
**Test file:** `cmd/root_test.go`

```go
// package cmd

func NewRootCommand() *cobra.Command
```

Root command `wa` with subcommands:
- `saga` (with `inspect` sub-subcommand)
- `seqmeta` (with `diff`, `validate`, `serve`)
- `results` (with `register`, `search`, `get`, `delete`,
  `rescan`, `serve`)

Migration:
- Move `main.go` inspector logic to `cmd/saga.go`. The
  `inspect` subcommand accepts the same flags (`--token`,
  `--base-url`, `--timeout`) and positional argument.
- Move `cmd/seqmeta/main.go` to `cmd/seqmeta.go`. Preserve
  all existing flags and behaviour.
- Rewrite root `main.go`:

```go
package main

import (
    "os"
    "github.com/wtsi-hgi/wa/cmd"
)

func main() {
    if err := cmd.NewRootCommand().Execute(); err != nil {
        os.Exit(1)
    }
}
```

- Remove `cmd/seqmeta/` directory.
- Move `cmd/seqmeta/main_test.go` to `cmd/seqmeta_test.go`.
- Move `main_test.go` to `cmd/saga_test.go`.

Root `main.go` persistent flags shared across subcommands:
none (each tree defines its own).

**Acceptance tests:**

1. Given `NewRootCommand()`, when `Execute()` with args
   `["saga", "inspect", "--help"]`, then exit code 0 and
   output contains `"identifier"`.
2. Given args `["seqmeta", "--help"]`, then output contains
   `"diff"` and `"validate"` and `"serve"`.
3. Given args `["results", "--help"]`, then output contains
   `"register"`, `"search"`, `"get"`, `"delete"`, `"rescan"`,
   `"serve"`.
4. Given args `["results", "serve", "--help"]`, then output
   contains `"--port"`, `"--db"`, `"--seqmeta-url"`.
5. Given `wa` with no subcommand, then help text lists all
   three top-level subcommands.

---

## G. Results CLI

### G1: wa results register

As an operator, I want to register pipeline outputs from the
command line, so that a single command after a pipeline run
records everything.

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_test.go`

```text
wa results register \
    --server http://localhost:8080 \
    --user alice --operator bob \
    --nextflow-workflow /path/to/main.nf \
    --runid 48522 --additional-unique random_exon \
    --command "nextflow run ..." \
    --input-file /path/to/sample_sheet.tsv \
    --meta library=random_exon \
    --meta seqmeta_runid=48522 \
    /path/to/output/dir
```

Flags:
- `--server` (persistent, default `$WA_RESULTS_BACKEND_URL` or
  `http://localhost:8080`)
- `--user`, `--operator`, `--command` (string)
- `--nextflow-workflow` (path, triggers `DetectPipeline`)
- `--runid`, `--additional-unique` (key-building)
- `--input-file` (repeatable, CLI calls `os.Stat` to fill
  mtime+size)
- `--meta` (repeatable `key=value`)
- `--include-hidden` (bool, default false)
- `--json` (read full Registration JSON from stdin)
- Positional: output directory

CLI flow: scan output dir -> detect pipeline -> build run
key -> build Registration -> POST to server -> print result
JSON to stdout.

**Acceptance tests:**

1. Given an `httptest.Server` as the results server and a
   `t.TempDir()` with 2 files, when `register` is run with
   `--user alice --operator bob --runid 48522
   --additional-unique exon --nextflow-workflow <path>`,
   then stdout is valid JSON with `"id"` field and server
   received a POST with 2 output files.
2. Given `--json` flag, when Registration JSON is piped to
   stdin, then it is sent as-is to the server (no directory
   scan).
3. Given `--input-file /path/to/existing_file`, then the
   Registration files include an entry with
   `Kind == "input"` and correct size from `os.Stat`.
4. Given missing `--user`, then exit code is non-zero and
   stderr contains error.

### G2: wa results search

As a user, I want to search result sets from the command line.

```text
wa results search [--server ...] [--user X] [--operator Y] \
    [--pipeline-name Z] [--meta key=value] \
    [--output-dir-prefix /path]
```

Prints JSON array to stdout.

**Acceptance tests:**

1. Given a server with 2 result sets, when `search --user
   alice` is run, then stdout is valid JSON array with
   matching results.
2. Given no matches, then stdout is `[]`.

### G3: wa results get

As a user, I want to view a specific result set.

```text
wa results get [--server ...] <id>
```

Prints JSON to stdout. `--files` flag also fetches and
includes the file list.

**Acceptance tests:**

1. Given a valid ID, when `get <id>` is run, then stdout is
   valid JSON with the result set.
2. Given `get <id> --files`, then the JSON includes a
   `"files"` array.
3. Given non-existent ID, then exit code is non-zero.

### G4: wa results delete

As a user, I want to delete a result set.

```text
wa results delete [--server ...] <id>
```

**Acceptance tests:**

1. Given a valid ID, when `delete <id>` is run, then exit
   code 0 and subsequent `get` returns error.
2. Given non-existent ID, then exit code is non-zero.

### G5: wa results rescan

As an operator, I want to refresh the output file list for an
existing result set by re-scanning the output directory.

```text
wa results rescan [--server ...] [--include-hidden] <id> <dir>
```

CLI scans `<dir>` locally, builds file list, PUTs to server.

**Acceptance tests:**

1. Given a registered result set and a `t.TempDir()` with 3
   files (1 new since registration), when `rescan <id>
   <dir>` is run, then the server's file list for that ID
   has 3 output files.
2. Given non-existent ID, then exit code is non-zero.

---

## H. Server Command

### H1: wa results serve

As a deployer, I want to start the results REST server from
the command line.

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_test.go`

```text
wa results serve [--port 8080] [--db results.db] \
    [--seqmeta-url http://...]
```

Opens the database (SQLite for file paths, MySQL for DSNs
containing `@`), creates `results.NewStore`, optionally
creates `results.NewSeqmetaValidator`, creates
`results.NewServer`, listens on the given port.

Flags:
- `--port` (int, default 8080)
- `--db` (string, default `"results.db"`)
- `--seqmeta-url` (string, default empty = no validation)
- `--seqmeta-timeout` (duration, default 30s)

**Acceptance tests:**

1. Given `results serve --port 0 --db :memory:`, when
   started, then server accepts HTTP requests and
   `POST /results` with valid JSON returns 201.
2. Given `results serve --port 0 --db :memory: --seqmeta-url
   <httptest.URL>`, when a result with `seqmeta_runid` is
   POST'd, then the seqmeta server receives a validation
   request.
3. Given `results serve --port abc`, then exit code is
   non-zero.

---

## I. Integration Tests

### I1: MySQL Store Integration

As a developer, I want Store CRUD tests to run against real
MySQL when a DSN is provided, so that cross-dialect SQL is
verified against the production database engine.

**Package:** `cmd/`
**Test file:** `cmd/results_test.go`

Gated by `WA_RESULTS_TEST_MYSQL_DSN` environment variable.
When unset, all MySQL tests skip with `t.Skip`. When set,
connect to MySQL using `go-sql-driver/mysql`, DROP the three
results tables (`result_sets`, `result_files`,
`result_metadata`) if they exist to ensure a clean slate,
then call `results.NewStore(db)`. Run the full Store CRUD
lifecycle: Upsert, Get, Search, GetFiles,
ReplaceOutputFiles, Delete -- equivalent to C1-C7
acceptance tests but against real MySQL.

Verify: ON DELETE CASCADE works (MySQL requires InnoDB),
LIKE-based search works, RFC 3339 timestamps round-trip
correctly, PRAGMA foreign_keys is silently ignored.

**Acceptance tests:**

1. Given `WA_RESULTS_TEST_MYSQL_DSN` is unset, when any
   MySQL store test runs, then the test is skipped via
   `t.Skip`.
2. Given valid DSN, when `NewStore(db)` is called after
   DROPping tables, then no error and tables are created.
3. Given Upsert with a valid Registration, when `Get` is
   called with the returned ID, then all scalar fields and
   metadata match.
4. Given Upsert called twice with the same key but
   `Requester` changed, then `CreatedAt` is preserved,
   `UpdatedAt` advances, and `Requester` reflects the
   second call.
5. Given 3 result sets with requesters "alice", "alice",
   "bob", when `Search(ctx,
   SearchParams{Requester:"alice"})` is called, then
   `len(results) == 2`.
6. Given a result set with `metadata={"library":"exon"}`,
   when `Search(ctx,
   SearchParams{Meta:map{"library":"exon"}})` is called,
   then `len(results) == 1`.
7. Given result sets with output directories `/a/b/c` and
   `/a/d/e`, when `Search(ctx,
   SearchParams{OutputDirPrefix:"/a/b"})` is called, then
   `len(results) == 1`.
8. Given a result set with 2 output files and 1 input file,
   when `GetFiles(ctx, id)` is called, then
   `len(files) == 3` with correct kinds and sizes.
9. Given `ReplaceOutputFiles` with 2 new output files, when
   `GetFiles` is called, then old output files are removed
   and input files are preserved.
10. Given `Delete(ctx, id)`, when `Get(ctx, id)` is called,
    then error wraps `ErrNotFound`, and `GetFiles` returns
    empty (cascade removes result_files and
    result_metadata rows).
11. Given a result set created via `Upsert`, when `Get`
    returns it, then `CreatedAt` and `UpdatedAt` both parse
    as valid RFC 3339 timestamps via `time.Parse` and are
    within 1 second of the wall clock at insertion time.
12. Given `NewStore(db)` called twice on the same MySQL
    `*sql.DB`, then the second call succeeds without error
    (`CREATE TABLE IF NOT EXISTS` is idempotent).
13. Given a DSN with an unreachable host
    (e.g. `"testuser:pass@tcp(192.0.2.1:3306)/db?timeout=1s"`),
    when `sql.Open` + `db.Ping` is called, then `db.Ping`
    returns a non-nil error (verifies the test helper's
    connection-check logic).

### I2: End-to-End CLI Integration

As a developer, I want end-to-end tests that start a real
`wa results serve` via Cobra and exercise CLI commands
through their Cobra command paths, so that the full wiring
(flag parsing, DSN detection, serve logic) is verified.

**Package:** `cmd/`
**Test file:** `cmd/results_test.go`

All commands are invoked through Cobra:

- **Server:** `NewRootCommand().SetArgs(["results",
  "serve", "--port", "0", "--db", "<tempfile>"])`
  executed in a goroutine. The test captures stdout to
  extract the actual listen port.
- **CLI commands:** each exercised via
  `NewRootCommand().SetArgs(["results", "<sub>", ...])`
  with `--server http://localhost:<port>`. This verifies
  flag parsing, argument wiring, and HTTP integration.

SQLite variant: always runs. Server started with
`--db <tempfile>` (SQLite path, no `@`).

MySQL variant: only runs when `WA_RESULTS_TEST_MYSQL_DSN`
is set. Server started with `--db <dsn>` where the DSN
contains `@`, verifying the DSN-to-driver selection logic.

Tests exercise the full round-trip: register -> search ->
get -> rescan -> delete. Uses `t.TempDir()` for output
directories with real files.

**Acceptance tests:**

1. Given a server started via
   `NewRootCommand().SetArgs(["results", "serve",
   "--port", "0", "--db", "<tempfile>"])` in a
   goroutine, and a `t.TempDir()` with 2 output files and
   1 input file, when `NewRootCommand().SetArgs(["results",
   "register", "--server", "http://localhost:<port>",
   ...])` is called, then stdout is JSON with correct ID,
   requester, and file count.
2. Given a registered result set, when
   `NewRootCommand().SetArgs(["results", "search",
   "--server", ..., "--user", "<requester>"])` is
   called, then stdout JSON array contains the registered
   result set.
3. Given a registered result set, when
   `NewRootCommand().SetArgs(["results", "get",
   "--server", ..., "<id>"])` is called, then stdout
   JSON contains full result set with metadata.
4. Given `NewRootCommand().SetArgs(["results", "get",
   "--server", ..., "--files", "<id>"])`, then stdout
   JSON includes file entries with correct kinds and sizes.
5. Given output directory modified (1 file added), when
   `NewRootCommand().SetArgs(["results", "rescan",
   "--server", ..., "<id>", "<dir>"])` is called,
   then a subsequent `get --files` shows updated output
   file count.
6. Given `NewRootCommand().SetArgs(["results", "delete",
   "--server", ..., "<id>"])`, when a subsequent
   `get <id>` is called, then exit code is non-zero
   (not found).
7. Given `WA_RESULTS_TEST_MYSQL_DSN` is set, when the
   server is started via `NewRootCommand().SetArgs(
   ["results", "serve", "--port", "0", "--db",
   "<dsn>"])` (DSN contains `@`, selecting MySQL
   driver), then the same 6 tests above pass against the
   MySQL-backed server. When unset, the MySQL variant is
   skipped via `t.Skip`.

---

## Implementation Order

### Phase 1: Types, Keys, and Scanner

Stories: A1, A2, A3, B1

Foundation: types, key generation, pipeline detection,
directory scanning. No database or HTTP dependencies.
A1 and A2 sequential (A2 depends on A1 for key concepts),
then A3 and B1 parallel.

### Phase 2: Database Store

Stories: C1, C2, C3, C4, C5, C6, C7

SQL store with full CRUD. Depends on Phase 1 types.
C1 first (schema), then C2 (upsert), then C3-C7 parallel
(all depend on C2 for test data).

### Phase 3: Seqmeta Validation

Stories: D1

HTTP client for seqmeta validation. Independent of Phase 2.
Can run parallel with Phase 2.

### Phase 4: REST API Server

Stories: E1, E2, E3, E4, E5, E6

chi HTTP server exposing all endpoints. Depends on Phases 2
(store) and 3 (validator). E1 first (POST establishes test
data), then E2-E6 parallel.

### Phase 5: Unified wa Binary

Stories: F1

Cobra root command, saga + seqmeta migration. Independent of
Phases 1-4 (migrates existing code only). Can run parallel
with Phases 1-4. Must complete before Phase 6.

### Phase 6: Results CLI and Server Command

Stories: G1, G2, G3, G4, G5, H1

CLI subcommands including serve. Depends on Phases 4
(server) and 5 (root command). H1 first (server needed by
CLI commands), then G1-G5 parallel.

### Phase 7: Integration Tests

Stories: I1, I2

End-to-end and MySQL integration tests. Depends on Phase 6
(CLI commands must exist). I1 first (store-level MySQL
tests), then I2 (end-to-end CLI tests that build on working
store).

---

## Appendix: Key Decisions

- **Separate package:** `results/` parallel to `saga/` and
  `seqmeta/`. No import dependency on seqmeta package;
  validates via HTTP only.
- **`*sql.DB` injection:** Store accepts a `*sql.DB` rather
  than opening its own connection. Caller chooses driver
  (SQLite for tests, MySQL for production). Same SQL works
  in both.
- **Cross-dialect SQL:** VARCHAR for indexed columns, TEXT
  for unbounded. No auto-increment. LIKE with Go-built
  patterns. PRAGMA for SQLite FK enforcement.
- **Deterministic ID:** SHA256 of natural key. No
  server-side auto-increment. Stable across environments.
  URL-safe hex encoding.
- **Client-side scanning:** Server never touches the
  filesystem. CLI scans locally, sends file list via HTTP.
  Simplifies server deployment and security.
- **Upsert via SELECT + INSERT/UPDATE:** Cross-compatible
  SQL within a transaction. Preserves `created_at` on
  update. REPLACE INTO would reset created_at and trigger
  cascading deletes.
- **Rescan = output files only:** Input files and pipeline
  path are historical facts about the run. Only output
  files can change (e.g. post-processing adds files).
- **No pagination for MVP:** Search returns all matches.
  File lists returned in full. Acceptable for internal use
  with moderate data volumes.
- **Seqmeta validation is strict:** No lenient/warning mode.
  Invalid data -> 422. Unreachable seqmeta -> 502. Clear
  failure modes for operators.
- **Migration support:** `output_dir_prefix` search param
  enables finding all result sets under a parent directory.
  Combined with upsert, supports mass file path migration
  without knowing result set keys. Migration CLI is NOT
  part of MVP.
- **Unified binary:** Single `wa` binary reduces deployment
  complexity. Cobra subcommand trees isolate concerns.
  `cmd/` package contains only CLI wiring.
- **Testing:** GoConvey `So()` assertions. In-memory SQLite
  for store tests. `httptest.Server` for REST and seqmeta
  validation tests. Real git repos in `t.TempDir()` for
  pipeline detection tests. Reference go-implementor and
  go-reviewer skills for TDD workflow.
- **Last-write-wins:** Concurrent upserts with the same key
  are serialised by the database transaction. No conflict
  detection for MVP.
- **Hard delete:** No soft delete. `ON DELETE CASCADE`
  removes files and metadata when a result set is deleted.
- **Integration testing:** MySQL Store tests gated by
  `WA_RESULTS_TEST_MYSQL_DSN` environment variable. When
  set, tests DROP and recreate tables, then run full CRUD
  suite against real MySQL. End-to-end CLI tests exercise
  all commands through Cobra command paths
  (`NewRootCommand().SetArgs(...)`) -- not direct function
  calls -- verifying flag parsing, DSN-to-driver selection,
  and serve wiring. SQLite variant always runs; MySQL
  variant runs when DSN is available. All integration tests
  live in `cmd/results_test.go`.
