# Phase 2: Database Store

Ref: [spec.md](spec.md) sections C1, C2, C3, C4, C5, C6, C7

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 2.1: C1 - NewStore and schema creation

spec.md section: C1

Implement `NewStore(db *sql.DB) (*Store, error)` and
`Close() error` in `results/store.go`. Executes PRAGMA
foreign_keys, runs all three CREATE TABLE IF NOT EXISTS
statements. Also define `Store` struct, `ResultSet`,
`Registration`, `SearchParams` types, and sentinel errors
in `results/types.go`. Covers all 3 acceptance tests from C1.

- [x] implemented
- [x] reviewed

### Item 2.2: C2 - Upsert result set

spec.md section: C2

Implement `(s *Store) Upsert(ctx context.Context,
reg *Registration) (*ResultSet, error)` in `results/store.go`.
Computes ID via CompositeKeyID, transactional insert-or-update
preserving created_at on upsert, replaces files and metadata.
Covers all 5 acceptance tests from C2.

- [x] implemented
- [x] reviewed

### Batch 1 (parallel, after item 2.2 is reviewed)

#### Item 2.3: C3 - Get result set by ID [parallel with 2.4, 2.5, 2.6, 2.7]

spec.md section: C3

Implement `(s *Store) Get(ctx context.Context, id string)
(*ResultSet, error)` in `results/store.go`. Returns result set
with metadata populated. Covers all 2 acceptance tests from C3.

- [x] implemented
- [x] reviewed

#### Item 2.4: C4 - Search result sets [parallel with 2.3, 2.5, 2.6, 2.7]

spec.md section: C4

Implement `(s *Store) Search(ctx context.Context,
params SearchParams) ([]ResultSet, error)` in
`results/store.go`. Supports pre-defined field filters,
metadata filters, and output_directory prefix matching. All
filters ANDed. Covers all 6 acceptance tests from C4.

- [x] implemented
- [x] reviewed

#### Item 2.5: C5 - Get files for result set [parallel with 2.3, 2.4, 2.6, 2.7]

spec.md section: C5

Implement `(s *Store) GetFiles(ctx context.Context,
resultID string) ([]FileEntry, error)` in `results/store.go`.
Returns all tracked files for a result set. Covers all 2
acceptance tests from C5.

- [x] implemented
- [x] reviewed

#### Item 2.6: C6 - Replace output files [parallel with 2.3, 2.4, 2.5, 2.7]

spec.md section: C6

Implement `(s *Store) ReplaceOutputFiles(ctx context.Context,
resultID string, files []FileEntry) error` in
`results/store.go`. Transactional delete of kind='output' rows
then insert of new output files, preserving input and pipeline
files. Covers all 3 acceptance tests from C6.

- [x] implemented
- [x] reviewed

#### Item 2.7: C7 - Delete result set [parallel with 2.3, 2.4, 2.5, 2.6]

spec.md section: C7

Implement `(s *Store) Delete(ctx context.Context, id string)
error` in `results/store.go`. Hard delete relying on
ON DELETE CASCADE for files and metadata. Covers all 2
acceptance tests from C7.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
