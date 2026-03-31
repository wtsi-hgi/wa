# Sequence Metadata Cache Specification

## Overview

`seqmeta` package providing hash-based change detection over SAGA
metadata, watermark persistence in SQLite, a REST API for polling,
and a CLI for ad-hoc use. Delegates all MLWH/iRODS queries to a
mockable interface backed by the existing `saga` package.

Key behaviours:

- Per-entity SHA256 watermarking with tombstones for removals.
- First poll returns all current results as "added".
- Atomic failure: saga errors prevent any store update.
- Identifier validation via live SAGA lookup returning type + full
  matched object.
- Generic `Diff[T]` function works with any saga type; REST API
  exposes study-sample and sample-file diffs.

## Architecture

**Package:** `seqmeta/`
**CLI:** `cmd/seqmeta/`

**Dependencies:** `saga`, `modernc.org/sqlite`,
`github.com/go-chi/chi/v5`, `github.com/spf13/cobra`

### Types

```go
// SAGAProvider is the mockable interface over saga.Client.
// Contains only methods seqmeta actually calls.
type SAGAProvider interface {
    GetStudy(ctx context.Context, studyID string) (
        *saga.Study, error)
    AllStudies(ctx context.Context) ([]saga.Study, error)
    AllSamples(ctx context.Context) (
        []saga.MLWHSample, error)
    AllSamplesForStudy(ctx context.Context,
        studyID string) ([]saga.MLWHSample, error)
    GetSampleFiles(ctx context.Context,
        sangerID string) ([]saga.IRODSFile, error)
    ListProjects(ctx context.Context) (
        []saga.Project, error)
}

// ClientAdapter wraps saga.Client to satisfy SAGAProvider.
type ClientAdapter struct{ client *saga.Client }

// Store persists watermark entries in SQLite.
type Store struct{ db *sql.DB }

// StoredEntry is one row in the watermarks table.
type StoredEntry struct {
    EntryHash string
    Tombstone bool
    UpdatedAt time.Time
}

// DiffResult holds the outcome of a diff poll.
// Added/Modified contain full objects. Removed contains
// entry IDs only (objects no longer exist upstream).
type DiffResult[T any] struct {
    Added    []T      `json:"added"`
    Modified []T      `json:"modified"`
    Removed  []string `json:"removed"`
}

// IdentifierType classifies a sequencing identifier.
type IdentifierType string

const (
    IdentifierStudyID         IdentifierType = "study_id"
    IdentifierStudyAccession  IdentifierType = "study_accession"
    IdentifierSangerSampleID  IdentifierType = "sanger_sample_id"
    IdentifierSampleLimsID    IdentifierType = "sample_lims_id"
    IdentifierSampleAccession IdentifierType = "sample_accession"
    IdentifierRunID           IdentifierType = "run_id"
    IdentifierLibraryType     IdentifierType = "library_type"
    IdentifierProjectName     IdentifierType = "project_name"
)

// IdentifierResult is returned by Validate.
type IdentifierResult struct {
    Identifier string         `json:"identifier"`
    Type       IdentifierType `json:"type"`
    Object     any            `json:"object"`
}

// Server is the REST API server.
type Server struct {
    provider SAGAProvider
    store    *Store
    handler  http.Handler
}

// Sentinel errors.
var (
    ErrUnknownIdentifier = errors.New(
        "seqmeta: unknown identifier")
    ErrStoreOpen = errors.New(
        "seqmeta: failed to open store")
)
```

### SQLite schema (auto-created by OpenStore)

```sql
CREATE TABLE IF NOT EXISTS watermarks (
    query_key  TEXT    NOT NULL,
    entry_id   TEXT    NOT NULL,
    entry_hash TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    tombstone  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (query_key, entry_id)
);
```

### Query keys

- Study samples: `"study_samples:<studyID>"`
- Sample files: `"sample_files:<sangerID>"`

### Entry identity (idFunc per type)

- `saga.MLWHSample` -> `SangerID`
- `saga.IRODSFile` -> `Collection`

Items sharing an ID are grouped; the group hash covers all items
sorted by their JSON representation.

### Hash computation

SHA256 of JSON-marshalled entry (or sorted JSON strings of grouped
entries joined by newline). Hex-encoded.

### Validation order

Tried sequentially; first match wins:

1. `GetStudy(identifier)` -> `IdentifierStudyID`
2. `AllStudies()` search by `AccessionNumber` ->
   `IdentifierStudyAccession`
3. `AllSamples()` search by `SangerID` ->
   `IdentifierSangerSampleID`
4. `AllSamples()` search by `IDSampleLims` ->
   `IdentifierSampleLimsID`
5. `AllSamples()` search by `AccessionNumber` ->
   `IdentifierSampleAccession`
6. `AllSamples()` search by `IDRun` (string to int) ->
   `IdentifierRunID`
7. `AllSamples()` search by `LibraryType` ->
   `IdentifierLibraryType`
8. `ListProjects()` search by `Name` -> `IdentifierProjectName`

Steps 3-7 reuse a single `AllSamples()` call.

### REST API routes (chi)

- `GET /diff/study/{id}` -> `DiffResult[saga.MLWHSample]`
- `GET /diff/sample/{id}` -> `DiffResult[saga.IRODSFile]`
- `GET /validate/{identifier}` -> `IdentifierResult`

Responses: `application/json`. Error body: `{"error":"<msg>"}`.
Status codes: 200 success, 404 unknown identifier, 502 saga
failure, 500 internal error.

### CLI (Cobra)

```text
seqmeta diff --study <id> [--db seqmeta.db] [--token ...] [--base-url ...]
seqmeta diff --sample <id> [--db seqmeta.db] [--token ...] [--base-url ...]
seqmeta validate <identifier> [--token ...] [--base-url ...]
seqmeta serve [--port 8080] [--db seqmeta.db] [--token ...] [--base-url ...]
```

Persistent flags: `--token` (default `$SAGA_API_TOKEN`),
`--base-url`, `--db` (default `"seqmeta.db"`).

### Testing

- Mock tests: `MockProvider` struct implementing `SAGAProvider`.
  In-memory SQLite (`":memory:"`). `httptest.Server` for REST.
- GoConvey with `So()` assertions per go-conventions.

---

## A. SAGAProvider Interface

### A1: Interface definition

As a developer, I want a minimal interface over saga.Client, so
that seqmeta can be tested without a real SAGA instance.

**Package:** `seqmeta/`
**File:** `seqmeta/provider.go`
**Test file:** `seqmeta/provider_test.go`

```go
type SAGAProvider interface {
    GetStudy(ctx context.Context, studyID string) (
        *saga.Study, error)
    AllStudies(ctx context.Context) ([]saga.Study, error)
    AllSamples(ctx context.Context) (
        []saga.MLWHSample, error)
    AllSamplesForStudy(ctx context.Context,
        studyID string) ([]saga.MLWHSample, error)
    GetSampleFiles(ctx context.Context,
        sangerID string) ([]saga.IRODSFile, error)
    ListProjects(ctx context.Context) (
        []saga.Project, error)
}
```

**Acceptance tests:**

1. Given a `MockProvider` implementing `SAGAProvider` that returns
   a study for `"6568"`, when `GetStudy(ctx, "6568")` is called,
   then the study is returned with no error.
2. Given a `MockProvider` configured to return `saga.ErrNotFound`
   for `GetStudy`, when called, then `errors.Is(err,
   saga.ErrNotFound)` is true.

### A2: ClientAdapter

As a developer, I want an adapter wrapping `saga.Client` to satisfy
`SAGAProvider`, so that production code uses the real client.

```go
func NewClientAdapter(client *saga.Client) *ClientAdapter
```

Each method delegates to the corresponding `saga.Client` sub-client:

- `GetStudy` -> `client.MLWH().GetStudy`
- `AllStudies` -> `client.MLWH().AllStudies`
- `AllSamples` -> `client.MLWH().AllSamples`
- `AllSamplesForStudy` -> `client.MLWH().AllSamplesForStudy`
- `GetSampleFiles` -> `client.IRODS().GetSampleFiles`
- `ListProjects` -> `client.Projects().List`

**Acceptance tests:**

1. Given a `saga.Client` backed by a mock HTTP server returning
   study JSON for `"100"`, when
   `NewClientAdapter(client).GetStudy(ctx, "100")` is called,
   then the returned study has `IDStudyLims == "100"`.
2. Given a mock server returning 2 MLWH samples for study `"100"`,
   when `adapter.AllSamplesForStudy(ctx, "100")` is called, then
   2 samples are returned.
3. Given a mock server returning 1 iRODS file for `"SANG1"`, when
   `adapter.GetSampleFiles(ctx, "SANG1")` is called, then 1 file
   is returned with correct `Collection`.
4. Given a mock server returning 2 projects, when
   `adapter.ListProjects(ctx)` is called, then 2 projects are
   returned.
5. Given `NewClientAdapter(client)`, when assigned to a variable
   of type `SAGAProvider`, then it compiles (interface
   satisfaction check).

---

## B. Watermark Store

### B1: Open and create store

As a developer, I want to open a SQLite database that auto-creates
the watermarks table, so that setup is zero-ops.

**Package:** `seqmeta/`
**File:** `seqmeta/store.go`
**Test file:** `seqmeta/store_test.go`

```go
func OpenStore(path string) (*Store, error)
func (s *Store) Close() error
```

**Acceptance tests:**

1. Given path `":memory:"`, when `OpenStore(":memory:")` is called,
   then error is nil and store is non-nil.
2. Given a `t.TempDir()` path, when `OpenStore(filepath.Join(dir,
   "test.db"))` is called, then the file is created on disk.
3. Given an open store, when `Close()` is called, then no error.
4. Given a closed store, when `Close()` is called again, then no
   panic.
5. Given an invalid path `/proc/nonexistent/db`, when `OpenStore`
   is called, then error wraps `ErrStoreOpen`.

### B2: Save and load entries

As a developer, I want to persist and retrieve watermark entries per
query key, so that diffs can resume across restarts.

```go
func (s *Store) LoadEntries(queryKey string) (
    map[string]StoredEntry, error)
func (s *Store) SaveEntries(queryKey string,
    entries map[string]StoredEntry) error
```

`SaveEntries` upserts all entries in the map. Existing entries for
the same query key not present in the new map are left unchanged
(tombstones persist across saves).

**Acceptance tests:**

1. Given an empty store, when `LoadEntries("q1")` is called, then
   result is an empty map (not nil) with no error.
2. Given `SaveEntries("q1", map with "e1" hash "abc"
   Tombstone=false)`, when `LoadEntries("q1")` is called, then
   the map has key `"e1"` with `EntryHash == "abc"` and
   `Tombstone == false`.
3. Given existing entry `"e1"` with hash `"abc"`, when
   `SaveEntries("q1", map with "e1" hash "def")` is called, then
   loaded `"e1"` has `EntryHash == "def"`.
4. Given entries saved for `"q1"` and `"q2"`, when
   `LoadEntries("q1")` is called, then only `"q1"` entries are
   returned.
5. Given `SaveEntries` with `"e1"` having `Tombstone == true`,
   when loaded, then `"e1"` has `Tombstone == true`.
6. Given `SaveEntries("q1", map with "e1")` followed by
   `SaveEntries("q1", map with "e2" only)`, when
   `LoadEntries("q1")` is called, then both `"e1"` and `"e2"`
   are present (`"e1"` unchanged from first save).

---

## C. Diff Engine

### C1: First poll returns all added

As a developer, I want the first diff for a query to return all
current results as "added", so that consumers get the initial state.

**Package:** `seqmeta/`
**File:** `seqmeta/diff.go`
**Test file:** `seqmeta/diff_test.go`

```go
func Diff[T any](
    store *Store,
    queryKey string,
    current []T,
    idFunc func(T) string,
) (*DiffResult[T], error)
```

**Acceptance tests:**

1. Given an empty store and 3 items with IDs `"a"`, `"b"`, `"c"`,
   when `Diff(store, "q1", items, idFunc)` is called, then
   `len(Added) == 3`, `len(Modified) == 0`, `len(Removed) == 0`.
2. Given an empty store and zero items, when `Diff` is called,
   then `Added`, `Modified`, `Removed` are all empty slices
   (not nil).

### C2: Unchanged data returns empty diff

As a developer, I want a poll with no changes to return an empty
diff, so that consumers know nothing changed.

**Acceptance tests:**

1. Given a prior `Diff` call that stored entries for `"a"` and
   `"b"`, when `Diff` is called again with identical items, then
   `Added`,
   `Modified`, `Removed` are all empty slices.

### C3: Detect new, modified, and removed entries

As a developer, I want Diff to classify entries as added, modified,
or removed based on hash comparison.

**Acceptance tests:**

1. Given prior state has `"a"(hash1)` and `"b"(hash2)`, when
   current items are `"b"(hash3)` and `"c"(hash4)`, then:
   `Added` contains the item with ID `"c"`, `Modified` contains
   the item with ID `"b"`, `Removed == ["a"]`.
2. Given prior state has `"a"` as a tombstone, when current items
   include `"a"` again, then `Added` contains the item with ID
   `"a"` (re-appeared after removal).

### C4: Group hashing for shared IDs

As a developer, I want items sharing the same ID to be hashed as
a group, so that multi-row entities are tracked correctly.

Items within a group are sorted by their JSON representation
before hashing, ensuring determinism.

**Acceptance tests:**

1. Given 2 items both returning ID `"s1"` from `idFunc` (e.g.
   MLWHSamples with same SangerID, different IDRun), when `Diff`
   is called on first poll, then `Added` contains both items and
   `LoadEntries` shows 1 entry for `"s1"`.
2. Given prior state has group `"s1"` (2 items), when a third
   item with ID `"s1"` is added to current, then `Modified`
   contains all 3 items for `"s1"`.
3. Given prior state has group `"s1"` (2 items), when called
   with the same 2 items in different slice order, then diff is
   empty (sort makes hash order-independent).

### C5: Tombstone persistence

As a developer, I want removed entries to become tombstones that
are not re-reported on subsequent polls.

**Acceptance tests:**

1. Given prior state has `"a"` (not tombstoned), when current is
   empty, then `Removed == ["a"]`. Calling `Diff` again with
   empty current, then `Removed` is empty (already tombstoned).
2. After the first removal diff, `LoadEntries` shows `"a"` with
   `Tombstone == true`.

### C6: DiffStudySamples convenience wrapper

As a developer, I want a typed wrapper that fetches study samples
from saga and diffs them.

```go
func DiffStudySamples(
    ctx context.Context,
    provider SAGAProvider,
    store *Store,
    studyID string,
) (*DiffResult[saga.MLWHSample], error)
```

Uses query key `"study_samples:<studyID>"` and `idFunc` returning
`MLWHSample.SangerID`.

**Acceptance tests:**

1. Given mock provider returning 2 samples (different SangerIDs)
   for study `"100"` and an empty store, when
   `DiffStudySamples(ctx, provider, store, "100")` is called,
   then `len(Added) == 2`, `Modified` and `Removed` empty.
2. Given mock provider returns error, when called, then error is
   returned and store is unchanged (atomic failure).
3. Given a prior successful diff, when mock provider returns the
   same 2 samples plus 1 new sample, then `len(Added) == 1`,
   `Modified` and `Removed` empty.

### C7: DiffSampleFiles convenience wrapper

As a developer, I want a typed wrapper that fetches sample iRODS
files from saga and diffs them.

```go
func DiffSampleFiles(
    ctx context.Context,
    provider SAGAProvider,
    store *Store,
    sangerID string,
) (*DiffResult[saga.IRODSFile], error)
```

Uses query key `"sample_files:<sangerID>"` and `idFunc` returning
`IRODSFile.Collection`.

**Acceptance tests:**

1. Given mock provider returning 1 file for `"SANG1"` and an
   empty store, when `DiffSampleFiles(ctx, provider, store,
   "SANG1")` is called, then `len(Added) == 1`.
2. Given mock provider returns error, then error is returned and
   store is unchanged.
3. Given a prior diff with 2 files, when mock now returns 1 file
   (the other removed), then `len(Removed) == 1` with the
   missing file's Collection as the removed ID.

---

## D. Identifier Validation

### D1: Validate study identifiers

As a developer, I want to determine if a string is a valid study
identifier (by ID or accession), so that downstream tools can
classify user input.

**Package:** `seqmeta/`
**File:** `seqmeta/validate.go`
**Test file:** `seqmeta/validate_test.go`

```go
func Validate(
    ctx context.Context,
    provider SAGAProvider,
    identifier string,
) (*IdentifierResult, error)
```

**Acceptance tests:**

1. Given mock where `GetStudy(ctx, "6568")` returns a Study with
   `Name == "HCA"`, when `Validate(ctx, provider, "6568")` is
   called, then `Type == IdentifierStudyID` and `Object` is the
   Study with `Name == "HCA"`.
2. Given mock where `GetStudy` returns `ErrNotFound` but
   `AllStudies` returns a study with
   `AccessionNumber == "ERP001"`, when
   `Validate(ctx, provider, "ERP001")` is called, then
   `Type == IdentifierStudyAccession` and `Object` is that Study.

### D2: Validate sample identifiers

As a developer, I want to classify sample-related identifiers
(SangerID, IDSampleLims, accession, run ID, library type).

**Acceptance tests:**

1. Given mock where no study matches but `AllSamples` returns a
   sample with `SangerID == "SANG123"`, when
   `Validate(ctx, provider, "SANG123")` is called, then
   `Type == IdentifierSangerSampleID` and `Object` is the
   MLWHSample.
2. Given mock where `AllSamples` has a sample with
   `IDSampleLims == "LIMS456"` (no SangerID match), when
   validated, then `Type == IdentifierSampleLimsID`.
3. Given mock where a sample has
   `AccessionNumber == "SAM789"`, when validated, then
   `Type == IdentifierSampleAccession`.
4. Given mock where a sample has `IDRun == 12345`, when
   `Validate(ctx, provider, "12345")` is called (after study
   checks fail), then `Type == IdentifierRunID` and `Object`
   is the MLWHSample.
5. Given mock where a sample has
   `LibraryType == "Chromium single cell"`, when validated,
   then `Type == IdentifierLibraryType`.
6. Given mock where `"6568"` matches both as a study ID and a
   sample field, then `IdentifierStudyID` is returned (study
   check runs first).

### D3: Validate project name

As a developer, I want to classify a string as a project name
when no study or sample matches.

**Acceptance tests:**

1. Given mock where no study or sample matches but
   `ListProjects` returns a project with `Name == "MyProject"`,
   when `Validate(ctx, provider, "MyProject")` is called, then
   `Type == IdentifierProjectName` and `Object` is the Project.

### D4: Unknown identifier

As a developer, I want a clear error when no SAGA entity matches.

**Acceptance tests:**

1. Given mock where all lookups return empty or not-found, when
   `Validate(ctx, provider, "xyz")` is called, then
   `errors.Is(err, ErrUnknownIdentifier)` is true.
2. Given empty string `""`, when validated, then
   `errors.Is(err, ErrUnknownIdentifier)` is true.

### D5: Upstream errors propagate during validation

As a developer, I want non-`ErrNotFound` saga errors (network
timeout, 502, authorization failure) to propagate immediately
instead of being swallowed or falling through to the next
validation step.

**Acceptance tests:**

1. Given mock where `GetStudy` returns
   `errors.New("connection refused")` (not `ErrNotFound`), when
   `Validate(ctx, provider, "6568")` is called, then the
   returned error contains `"connection refused"` and
   `errors.Is(err, ErrUnknownIdentifier)` is false.
2. Given mock where `GetStudy` returns `ErrNotFound` but
   `AllStudies` returns `errors.New("502 bad gateway")`, when
   `Validate(ctx, provider, "ERP001")` is called, then the
   returned error contains `"502 bad gateway"`.
3. Given mock where `GetStudy` returns `ErrNotFound`,
   `AllStudies` returns empty, and `AllSamples` returns
   `errors.New("unauthorized")`, when
   `Validate(ctx, provider, "SANG123")` is called, then the
   returned error contains `"unauthorized"` and
   `errors.Is(err, ErrUnknownIdentifier)` is false.
4. Given mock where study and sample lookups all return
   empty/not-found but `ListProjects` returns
   `errors.New("timeout")`, when
   `Validate(ctx, provider, "MyProject")` is called, then the
   returned error contains `"timeout"`.

---

## E. REST API

### E1: Study diff endpoint

As a consumer, I want to poll `GET /diff/study/{id}` to get
changes in samples for a study since last poll.

**Package:** `seqmeta/`
**File:** `seqmeta/server.go`
**Test file:** `seqmeta/server_test.go`

```go
func NewServer(
    provider SAGAProvider, store *Store,
) *Server
func (s *Server) Handler() http.Handler
```

**Acceptance tests:**

1. Given mock returning 2 samples for study `"100"` and an empty
   store, when `GET /diff/study/100` is called, then status is
   200 and JSON body has `"added"` array with 2 entries,
   `"modified"` is `[]`, `"removed"` is `[]`.
2. Given a second `GET /diff/study/100` with same data, then all
   three arrays are empty.
3. Given mock returns error for the study, then status is 502 and
   body has `"error"` key with a message string.
4. Response `Content-Type` is `application/json`.

### E2: Sample diff endpoint

As a consumer, I want to poll `GET /diff/sample/{id}` to get
changes in iRODS files for a sample.

**Acceptance tests:**

1. Given mock returning 1 file for `"ABC"` and empty store, when
   `GET /diff/sample/ABC` is called, then status 200 and
   `"added"` has 1 file with correct `Collection`.
2. Given mock now returns 2 files (1 existing + 1 new), when
   polled again, then `"added"` has the new file only.
3. Given mock returns `saga.ErrNotFound`, then status is 502.

### E3: Validate endpoint

As a consumer, I want `GET /validate/{identifier}` to classify
an identifier.

**Acceptance tests:**

1. Given mock where `"6568"` is a valid study ID, when
   `GET /validate/6568` is called, then status 200, `"type"` is
   `"study_id"`, `"object"` contains the Study JSON.
2. Given unknown identifier `"xyz"`, then status is 404 and body
   has `"error"` key.
3. Given identifier with URL-special characters (e.g. `/`), when
   properly encoded as `GET /validate/foo%2Fbar`, then the raw
   identifier `"foo/bar"` is validated.

### E4: Error responses

As a consumer, I want consistent JSON error bodies.

**Acceptance tests:**

1. Given saga provider fails on diff, then status 502, body is
   `{"error":"<message>"}`, `Content-Type` is
   `application/json`.
2. Given store failure (e.g. closed store), then status 500.

---

## F. CLI

### F1: diff subcommand

As a user, I want `seqmeta diff --study <id>` to print changes
since last poll as JSON, so that I can script polling.

**Package:** `cmd/seqmeta/`
**File:** `cmd/seqmeta/main.go`

```go
// Root command: seqmeta
// Persistent flags: --token, --base-url, --db
// Subcommands: diff, validate, serve
```

**Acceptance tests:**

1. Given `diff --study 100 --db <tmpfile>` with a mock HTTP
   server providing saga data (2 samples), when run, then stdout
   is valid JSON with `"added"` array of length 2.
2. Given `diff --sample ABC --db <tmpfile>`, when run, then
   stdout contains valid JSON diff result.
3. Given `diff` with neither `--study` nor `--sample`, then exit
   code is non-zero and stderr contains usage hint.
4. Given `diff --study 100 --sample ABC`, then exit code is
   non-zero (mutually exclusive flags).

### F2: validate subcommand

As a user, I want `seqmeta validate <identifier>` to print the
identifier type and matched object as JSON.

**Acceptance tests:**

1. Given `validate 6568` with mock server returning study, then
   stdout is JSON with `"type": "study_id"` and `"object"` key.
2. Given `validate unknown_id` with mock returning nothing, then
   exit code is non-zero and stderr contains error.
3. Given `validate` with no argument, then exit code is non-zero
   and stderr contains usage.

### F3: serve subcommand

As a user, I want `seqmeta serve --port 8080` to start the REST
API server.

**Acceptance tests:**

1. Given `serve --port 0 --db <tmpfile>` with mock saga server,
   when started, then the server accepts requests and
   `GET /validate/6568` returns 200 with valid JSON.
2. Given `serve` without `--port`, then default port is 8080.
3. Given `serve --port abc`, then exit code is non-zero.

---

## Implementation Order

### Phase 1: SAGAProvider Interface

Stories: A1, A2

Foundation: mockable interface and saga.Client adapter. All
subsequent phases depend on this. Sequential.

### Phase 2: Watermark Store

Stories: B1, B2

SQLite open/close, save/load with in-memory mode for tests. No
dependency on Phase 1. Can be parallel with Phase 1.

### Phase 3: Diff Engine

Stories: C1, C2, C3, C4, C5, C6, C7

Generic diff, group hashing, tombstones, convenience wrappers.
Depends on Phase 1 (SAGAProvider for wrappers) and Phase 2
(Store). C1-C5 sequential (each builds on prior), then C6-C7
parallel.

### Phase 4: Identifier Validation

Stories: D1, D2, D3, D4, D5

Live SAGA lookup classification. Depends on Phase 1
(SAGAProvider). Can be parallel with Phases 2/3. D1 first
(establishes pattern), then D2-D5 parallel.

### Phase 5: REST API

Stories: E1, E2, E3, E4

chi HTTP server exposing diff and validate endpoints. Depends on
Phases 3 (diff wrappers) and 4 (validation). E1-E3 parallel,
then E4.

### Phase 6: CLI

Stories: F1, F2, F3

Cobra commands wiring provider, store, and server. Depends on
all prior phases. Sequential.

---

## Appendix: Key Decisions

- **Separate package:** `seqmeta/` not inside `saga/` to maintain
  separation of concerns. Depends on saga only via the
  `SAGAProvider` interface.
- **Minimal interface:** 6 methods. Easy to mock, decoupled from
  saga internals. Future saga methods added to the interface only
  when seqmeta needs them.
- **Pure-Go SQLite:** `modernc.org/sqlite` for zero-CGO deployment.
  In-memory (`:memory:`) for tests; file-backed for production.
- **Generic Diff[T]:** Works with any JSON-marshallable type.
  Convenience wrappers (`DiffStudySamples`, `DiffSampleFiles`)
  handle saga fetching and type-specific identity functions.
- **Group hashing:** Items sharing the same canonical ID are sorted
  by JSON string and hashed together. Handles MLWHSample rows
  with duplicate SangerIDs (multiple runs/lanes per sample).
- **Tombstones permanent:** Removed entries stay in the DB with
  `tombstone=1`. Not re-reported on subsequent diffs. Reappearing
  entries are reported as "added" again.
- **Atomic failure:** Saga errors in convenience wrappers prevent
  `Diff` from being called, so the store is never updated on
  upstream failure.
- **Validation priority:** Study ID first (cheapest single-entity
  lookup), then study accession (reuses `AllStudies` once), then
  all sample fields (reuses `AllSamples` once), then projects.
  First match wins.
- **No concurrency guards:** Single-process access to SQLite as
  specified. No mutex or WAL mode needed.
- **REST error codes:** 200 success, 404 unknown identifier (validate
  only), 502 upstream saga failure, 500 internal/store error.
- **Context-first:** All public functions take `context.Context`.
- **Testing:** GoConvey `So()` assertions. Mock provider for unit
  tests. `httptest.Server` for REST and CLI adapter tests.
  Reference go-implementor and go-reviewer skills for TDD workflow.
