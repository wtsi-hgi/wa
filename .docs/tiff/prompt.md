# OME-TIFF Viewer Improvements Prompt

Target spec path for the spec-writer workflow: `.docs/tiff/spec.md`.

## Feature Description

The results web UI now has a useful first implementation of TIFF/OME-TIFF
previewing. It is good enough for quick inspections such as "is our cell
density good?" and "did this pipeline run correctly?", and that is a meaningful
workflow win. The next spec should not treat TIFF support as absent. It should
start from the implemented quick-look behavior, preserve it, and decide which
improvements would give users the most value before attempting a full
microscopy workstation in the browser.

User feedback after the initial implementation:

- The current preview is useful for quick pipeline and cell-density sanity
  checks.
- A full suite of image controls would be valuable, but is understood to be a
  larger feature.
- Downloading the TIFF files is easy, and users can open them locally in
  Napari for deeper inspection.

That feedback changes the priority. The web UI should remain excellent for
fast browser-based triage, while a Napari-like full viewer should be treated as
an incremental improvement path rather than the only acceptable outcome.

## What Is Implemented Now

Current TIFF support is a server-rendered quick-look preview:

- TIFF and OME-TIFF paths are recognized as previewable image-like files.
- Inline file-browser previews render a maximal-speed first-plane view that
  behaves like any other graphic preview.
- Preview grid/subfolder modes use derived first-plane URLs instead of loading
  the source TIFF stack directly.
- Enlarged previews share the same OME-aware component regardless of whether
  they were opened from normal file previews or subfolder preview rows.
- Enlarged OME previews can fetch metadata and expose controls for channel, Z
  slice, and timepoint when those dimensions exist.
- OME XML metadata is parsed when available, including channel names, SizeC,
  SizeZ, SizeT, SizeX, SizeY, and physical pixel sizes.
- Non-OME or generic multi-page TIFFs fall back to page/plane-style metadata.
- The Next.js API supports:
  - `GET /api/file?...&ome=metadata`
  - `GET /api/file?...&ome=plane&channel=...&z=...&t=...&w=...&h=...`
- The Next.js server authorizes OME reads with a backend `HEAD` request using
  `download=true`; it does not download the source TIFF through the browser.
- The results server exposes `X-WA-Resolved-File-Path` only for the internal
  `HEAD` download probe needed by the Next server, avoiding path leaks on
  ordinary previews/downloads.
- The Next server requires an absolute local path before reading TIFF data,
  either from the backend-resolved header or an absolute requested path.
- Sharp is used server-side to read TIFF metadata and render selected planes as
  WebP responses.
- A pixel safety limit is enforced via `WA_OME_TIFF_MAX_INPUT_PIXELS`.
- Derived plane images are produced on demand by the server and returned from
  memory with HTTP cache headers; they are not created at register time and are
  not stored in a persistent tile/thumbnail cache.
- Tests cover small generated TIFFs, registered output subdirectory paths,
  path-resolution safety, preview route behavior, and the file-browser preview
  UX.

Known limits of the implemented version:

- It is not a deep-zoom viewer.
- It does not provide tiled loading, native-resolution pan/zoom, or multiscale
  pyramids.
- It does not composite multiple channels together.
- It does not provide per-channel color/LUT, opacity, contrast/window, or
  auto-contrast controls in the UI.
- It does not offer annotation, measurement, segmentation overlays, 3D volume
  rendering, playback, or plugin-style analysis.
- On-demand whole-plane WebP rendering is acceptable for one or a few previews,
  but may be too expensive for dozens of TIFFs at once without caching or
  precomputed derivatives.

## Napari Context

Napari is a fast, interactive, multi-dimensional image viewer for Python:
https://napari.org/ and https://github.com/napari/napari

Relevant Napari capabilities to use as inspiration:

- Browsing, annotating, and analyzing large multi-dimensional images.
- 2D, 3D, and higher-dimensional array viewing.
- GPU-backed interactive rendering through Qt/VisPy.
- Multiple layer types such as images, labels, points, vectors, shapes, and
  surfaces.
- Overlaying derived data such as segmentations, points, polygons, and other
  analysis outputs.
- Standard scientific Python integration with NumPy, SciPy, Zarr, notebooks,
  scripts, and plugins.
- Programmatic control and extensibility through Python.

Benefits Napari has over the current web implementation:

- It can be a true local microscopy analysis environment rather than only a
  result-browser preview.
- It has mature interaction patterns for multidimensional image stacks:
  channel/layer controls, contrast, LUTs, Z/T navigation, 2D/3D views, and
  overlays.
- It supports annotation and analysis workflows that are outside the scope of
  a lightweight web file browser.
- Once the file is downloaded locally, it can use the local scientific Python
  ecosystem and plugins rather than being limited to server-rendered WebP
  planes.

Tradeoffs compared with the web UI:

- Napari requires a local app/Python environment and local file access.
- It is not integrated with WA result-set permissions, locked-result behavior,
  browser sharing, or the existing file-browser context.
- It is better for deep analysis, while WA is better for quick remote triage
  and deciding whether a result/file is worth downloading.

The spec should therefore frame Napari as the recommended full-analysis escape
hatch and a source of UX ideas, not as something WA must fully replace.

## Example Files

Example user-provided files for local investigation:

- `.tmp/ome.tiff`: a relatively large 4D OME-TIFF stack, around 200 MB, with
  channel, Z, X, and Y dimensions.
- `.tmp/ome_collapsed.tiff`: a smaller related OME-TIFF where the Z dimension
  has been collapsed.

Do not commit these `.tmp` files. If tests need TIFF fixtures, generate small
synthetic TIFF/OME-TIFF fixtures during tests or commit tiny purpose-built
fixtures only.

## References

Use these as background and inspiration, not as mandatory implementation
choices:

- User reference: https://github.com/davidvi/tissueviewer
- User reference / TissueViewer paper:
  https://academic.oup.com/bioinformatics/article/41/5/btaf246/8120081
- Napari documentation: https://napari.org/
- Napari source/readme: https://github.com/napari/napari
- OpenSeadragon, a common web deep-zoom image viewer:
  https://openseadragon.github.io/
- Viv, a web-based visualization library for OME-TIFF and OME-Zarr style
  bioimaging data: https://github.com/hms-dbmi/viv
- OME-NGFF / OME-Zarr specifications for multiscale bioimaging data:
  https://ngff.openmicroscopy.org/latest/
- OME-TIFF format documentation:
  https://docs.openmicroscopy.org/ome-model/latest/ome-tiff/

## Desired User Experience

Preserve the existing fast inline preview as the default. It should remain a
low-friction, graphic-like signal for quick visual inspection. Users should not
need to understand OME, channels, tiles, or Napari just to see whether the file
looks plausible.

Improve the enlarged preview so it becomes a more capable inspection surface,
inspired by the parts of Napari that matter for quick QA:

- Large modal or full-screen image area.
- Fit-to-view rendering, reset-to-fit, and predictable zoom/pan controls.
- Keyboard and slider controls for channel, Z, and timepoint.
- Channel names from OME metadata when present.
- Per-channel visibility, color/LUT, and opacity controls.
- Per-channel or global contrast/window controls, with a useful auto-contrast
  default.
- Optional multi-channel composite rendering.
- Clear metadata display for dimensions, physical pixel size, current plane,
  and source format.
- A prominent download/original-file action for opening the file in Napari.
- Clear unsupported/error states that preserve the ordinary download path.

Do not add visible explanatory prose about implementation details in the app UI.
Controls should be named naturally and behave like image-viewer controls.

## Recommended Product Direction

The next spec should prefer an incremental plan:

1. Polish the existing quick-look preview.
   - Keep first-plane inline preview fast.
   - Make enlarged controls easier to scan and use.
   - Add obvious original-file download/open-in-local-viewer affordance.
   - Improve metadata display and error messages.

2. Add Napari-inspired controls without deep zoom.
   - Channel visibility/color/LUT.
   - Contrast/window and auto-contrast.
   - Better Z/T controls, keyboard shortcuts, and optional simple playback.
   - Optional server-side composite WebP rendering for selected channels.

3. Add deep-zoom only if user demand justifies it.
   - Tile-based viewport with pan/zoom.
   - Persistent tile/cache strategy.
   - Multiscale/pyramidal support or bounded fallback for non-pyramidal TIFFs.
   - Potential use of OpenSeadragon, Viv, or an OME-NGFF-style manifest.

This direction acknowledges that users can already download files and use
Napari for full analysis, while still improving WA for quick remote triage.

## Technical Direction

The spec-writer should research the current codebase before deciding the final
architecture. Likely directions:

- Keep authentication and access checks through the existing `/api/file` proxy
  path.
- Keep the backend `HEAD download=true` authorization probe for server-side TIFF
  access.
- Preserve the absolute-path guard and the limited
  `X-WA-Resolved-File-Path` exposure.
- Continue returning derived WebP/PNG images rather than full TIFF bodies to
  the browser.
- For additional single-plane controls, extend the current `ome=plane` route
  rather than introducing a separate viewer service.
- For channel composites, decide whether to produce server-side composite
  images or client-side overlays from multiple derived plane images.
- For deep zoom, add tile endpoints under the existing file API or a closely
  related route.
- If tiles are added, define tile size, coordinate scheme, plane mapping,
  downsample levels, cache behavior, concurrency limits, and cleanup.
- Add persistent caching only with explicit bounds and invalidation rules.
- Enforce resource limits: maximum input pixels, output dimensions, tile render
  cost, concurrent renders, timeouts, and clear unsupported states.

Important: do not implement a solution that downloads the original TIFF to the
browser and relies on client-side full-file decoding. That would fail the
central requirement for large files.

## Tile and Cache Expectations

If the spec includes deep zoom, it must define a concrete tile strategy. It
should answer:

- Whether tiles are generated on demand, precomputed on first open, or mixed.
- How tile coordinates map to source TIFF pages/planes and downsample levels.
- Whether non-pyramidal TIFFs are tiled by server-side region extraction,
  whole-plane resize, cached multiscales, or another approach.
- How OME metadata dimensions map to channel/Z/T plane selection.
- How channel composites are produced: server-side composite tiles, client-side
  blend of per-channel tile layers, or another design.
- How derived tiles are cached and invalidated.
- Where cache files live, how large they may get, and how cleanup works.
- What happens when a TIFF lacks OME metadata, is malformed, is too large for
  configured limits, or cannot be tiled efficiently.

The design should prefer correctness and bounded resource use over trying to
support every possible TIFF variant silently.

## Integration Points

Current relevant files likely include:

- `frontend/app/api/file/route.ts`
- `frontend/components/file-preview.tsx`
- `frontend/components/result-detail-files.tsx`
- `frontend/lib/ome-tiff.ts`
- `frontend/lib/ome-tiff-server.ts`
- `frontend/lib/preview-file-types.ts`
- `frontend/tests/ome-tiff.test.ts`
- `frontend/tests/file-proxy.test.ts`
- `frontend/tests/file-preview.test.ts`
- `frontend/tests/result-detail-files.test.ts`
- `frontend/tests/file-browser.test.ts`
- `results/server_file.go`
- `results/server_file_test.go`

The spec should confirm these paths by codebase research rather than assuming
the list is complete.

## Non-Goals

Do not replace Napari or attempt a complete desktop microscopy analysis suite
inside WA as the next step.

Do not require users to download OME-TIFF files before quick browser
inspection. Downloading for full Napari analysis is acceptable and should remain
easy.

Do not commit large TIFF fixtures.

Do not weaken existing image, SVG, PDF, HTML, text, subfolder preview,
file-type dropdown, saved filter, auth, or locked-result behavior.

Do not introduce `wa mlwh sync` or MLWH cache changes; this feature is
file-preview/viewer work.

Do not fake OME channel or image data in production code. If a file is
unsupported, show an honest unsupported/error state.

## Acceptance-Test Themes

The eventual spec should include behavior-focused acceptance tests for at
least:

- Existing inline TIFF preview still renders a first-plane graphic-like preview
  without requesting the source TIFF body in the browser.
- Enlarged preview controls appear from both normal file previews and subfolder
  preview rows.
- OME metadata discovery exposes channel names, dimensions, Z count, T count,
  and physical pixel sizes when available.
- The OME file proxy authorizes via backend `HEAD download=true`, rejects
  relative local paths, and does not fall back to a backend GET body fetch.
- Registered output subdirectory TIFFs preview correctly.
- Changing channel, Z, or T requests the correct derived plane.
- Non-OME multi-page TIFFs fall back to plane selection with clear labeling.
- Unsupported or malformed TIFFs show a clear error and do not crash the page.
- The original file remains easy to download for Napari/local analysis.
- Existing preview behavior for non-TIFF file types remains unchanged.
- If channel compositing is added, changing visibility/color/contrast changes
  the rendered preview.
- If deep zoom is added, opening a large OME-TIFF viewer does not request the
  original TIFF body in the browser, and panning/zooming requests tile URLs.
- Large local examples such as `.tmp/ome.tiff` and `.tmp/ome_collapsed.tiff`
  can be used manually for evidence, but automated tests use small generated
  fixtures.

## Open Questions for the Spec Writer to Resolve

The spec-writer clarification loop should ask the user questions where needed,
especially:

- Is the next goal better quick inspection, or a true browser replacement for
  some Napari workflows?
- Which Napari-like controls matter first: channel composite, LUT/color,
  contrast/window, Z/T navigation, playback, annotation, measurement, or
  overlays?
- Should the enlarged preview become full-screen, or is the current modal size
  enough if it gains better controls?
- Should phase 1 deliver better single-plane controls before deep zoom?
- Is channel compositing mandatory for the next implementation, or can it come
  after contrast/LUT controls?
- Are server-side derived tile or composite caches acceptable on disk, and if
  so where should they live and how aggressively should they be cleaned?
- Is OpenSeadragon acceptable as a dependency if the implementation handles
  compositing through multiple tile layers or server-side composite tiles?
- Should the viewer support only OME-TIFF initially, or generic pyramidal TIFF
  / multi-page TIFF too?
- What maximum file size, plane size, and tile render cost should be supported
  before returning a bounded unsupported/error state?
