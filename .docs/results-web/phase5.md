# Phase 5: Detail and files

Ref: [spec.md](spec.md) sections M1, N1, P1, O1

## Instructions

Use the `orchestrator` skill to complete this phase,
coordinating subagents with the `nextjs-fastapi-implementor`
and `nextjs-fastapi-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 5.1: M1 - Result detail, seqmeta enrichment [parallel with 5.2, 5.3]

spec.md section: M1

Create Server Component at
`frontend/app/(results)/results/[id]/page.tsx`. Fetch
result via `fetchResult(id)` and files via
`fetchFiles(id)`. Display all ResultSet fields in
structured layout and metadata key-value table. For
`seqmeta_*` metadata, call `validateIdentifier(value)`
in parallel via Server Action. Create
`frontend/components/result-metadata.tsx` for metadata
display. Create `frontend/components/seqmeta-badge.tsx`
showing resolved type + label, hover tooltip with details,
"?" indicator on enrichment failure. Create
`frontend/lib/seqmeta-cache.ts` with `SeqmetaCache`
class and `SeqmetaCacheContext` React context for
client-side enrichment caching across navigations. Tests
in `frontend/tests/seqmeta-badge.test.ts`. Covering all
6 acceptance tests from M1.

- [ ] implemented
- [ ] reviewed

#### Item 5.2: N1 - Tabbed folder tree [parallel with 5.1, 5.3]

spec.md section: N1

Create `frontend/components/file-browser.tsx` client
component with shadcn/ui `Tabs`: "Outputs", "Inputs",
"Pipeline". Build collapsible folder tree from file
paths per kind using `buildFileTree(files)` utility.
Folder nodes show: name, `typeCounts` summary (e.g.
"3 csv, 1 png"), expand/collapse chevron. File leaves
show: name, human-readable size, mtime. Clicking a file
calls `onSelectFile`. Root folders auto-expand if only
one. Internal `TreeNode` type with `fileCount` and
`typeCounts` aggregated from descendants. Tests in
`frontend/tests/file-browser.test.ts`. Covering all 9
acceptance tests from N1.

- [ ] implemented
- [ ] reviewed

#### Item 5.3: P1 - File content streaming API route [parallel with 5.1, 5.2]

spec.md section: P1

Create `frontend/app/api/file/route.ts` API route.
Query params: `id`, `path`, optional `download=true`.
Calls `resultsRaw` to fetch from Go's
`GET /results/{id}/file?path=...&download=...`. Streams
response body back with same `Content-Type` and
`Content-Disposition` headers. Forwards error status and
JSON from Go on failure. Preserves `X-File-Size` header
on 413. Returns 400 for missing `id` or `path`. Tests in
`frontend/tests/file-proxy.test.ts`. Covering all 6
acceptance tests from P1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer`
skill (review all items in the batch together in a single
review pass).

### Item 5.4: O1 - Content-type renderer selection

spec.md section: O1

Depends on item 5.2 (N1) for file selection from the
file browser, and item 5.3 (P1) for the binary proxy
URL used by image, PDF, and download renderers.

Create `frontend/components/file-preview.tsx` client
component. Implement `selectRenderer(contentType)`
returning one of: `image`, `csv`, `markdown`, `html`,
`svg`, `pdf`, `code`, `binary`. Renderer behaviours:
images via `<img>` proxy URL with thumbnail (320x240)
and lightbox on click; CSV/TSV parsed into shadcn/ui
`Table` with first 100 rows, "Show all rows" expansion,
column sorting, and text filtering; markdown via
`react-markdown`; HTML in
`<iframe sandbox="allow-same-origin">` (no scripts);
SVG as `<img>` (no inline); PDF in `<iframe>` via proxy;
code with syntax highlighting; binary shows file metadata
and path only. Handle 413 with "File too large" message
and `X-File-Size` header. Download button for all
previewable files. No download for non-previewable
(`.bam`, `.cram`, `.h5`). Tests in
`frontend/tests/file-preview.test.ts`. Covering all 22
acceptance tests from O1.

- [ ] implemented
- [ ] reviewed
