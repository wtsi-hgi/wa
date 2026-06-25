# Deep-Zoom OME-TIFF Viewer Prompt

Target spec path for the spec-writer workflow: `.docs/tiff/spec.md`.

## Feature Description

Users who work with microscopy-style OME-TIFF outputs are likely to expect a full image viewer, not only a file-browser sanity preview. The current implementation can preview one selected TIFF/OME-TIFF plane at a time by rendering a server-side WebP for a chosen channel, Z slice, and timepoint. That is useful for quick checks, but it is not the full experience microscopy users will ask for.

Assume the target users want the full thing: a responsive deep-zoom OME-TIFF viewer in the results web UI that can handle large multichannel OME-TIFF stacks without downloading the whole file into the browser. The feature should let users inspect high-resolution image data interactively with pan/zoom, tiled loading, channel compositing, per-channel display controls, and practical navigation through Z/time dimensions.

## Background

The existing OME-TIFF preview support was added as a safe minimal implementation:

- TIFF previews use authenticated `HEAD` checks through `/api/file`.
- The Next.js server uses Sharp to render a selected TIFF plane to WebP.
- OME XML metadata drives channel, Z, and T controls.
- Grid previews use derived first-plane thumbnails.
- The browser does not fetch full TIFF stacks.
- Known limit: this is single-plane preview, not deep-zoom, pyramidal, or multichannel-composite tissue viewing.

This is probably enough for "is this the right file?" but not enough for users who expect a microscopy viewer. Those users will likely ask for:

- Smooth pan and zoom to native/high-resolution detail.
- Progressive tile loading rather than one resized image.
- Viewing multiple channels as a composite, not one channel at a time.
- Per-channel color/LUT, visibility, contrast/window, and opacity controls.
- Better Z-stack and timepoint navigation.
- A full-screen or enlarged inspection mode suitable for real image review.

Example user-provided files for local investigation:

- `.tmp/ome.tiff`: a relatively large 4D OME-TIFF stack, around 200 MB, with channel, Z, X, and Y dimensions.
- `.tmp/ome_collapsed.tiff`: a smaller related OME-TIFF where the Z dimension has been collapsed.

Do not commit these `.tmp` files. If tests need TIFF fixtures, generate small synthetic TIFF/OME-TIFF fixtures during tests or commit tiny purpose-built fixtures only.

## References

Use these as background and inspiration, not as mandatory implementation choices:

- User reference: https://github.com/davidvi/tissueviewer
- User reference / TissueViewer paper: https://academic.oup.com/bioinformatics/article/41/5/btaf246/8120081
- OpenSeadragon, a common web deep-zoom image viewer: https://openseadragon.github.io/
- Viv, a web-based visualization library for OME-TIFF and OME-Zarr style bioimaging data: https://github.com/hms-dbmi/viv
- OME-NGFF / OME-Zarr specifications for multiscale bioimaging data: https://ngff.openmicroscopy.org/latest/
- OME-TIFF format documentation: https://docs.openmicroscopy.org/ome-model/latest/ome-tiff/

## Desired User Experience

When a user selects an OME-TIFF/TIFF file in the results file browser, they should be able to open a rich image viewer from the preview pane. The lightweight single-plane preview can remain as an inline default if that helps performance, but there must be an obvious path to a full viewer without downloading the original TIFF.

The full viewer should provide:

- A tile-based image viewport with smooth pan and zoom.
- Initial fit-to-view rendering, zoom controls, mouse wheel/trackpad zoom, drag-to-pan, and reset-to-fit.
- Full-screen or large modal/detail mode for inspection.
- Channel list with names from OME metadata when available.
- Per-channel visibility toggles.
- Per-channel color selection or sensible default colors.
- Per-channel intensity controls, at least min/max/window and preferably auto-contrast based on sampled/tile statistics.
- Composite rendering for visible channels.
- Z-slice navigation for stacks.
- Timepoint navigation when `SizeT > 1`.
- Plane/series fallback for non-OME multi-page TIFFs.
- Clear loading, error, and unavailable states.
- No visible implementation instructions or explanatory prose in the app UI.

The viewer must avoid surprising browser memory/network use. A user opening a 200 MB OME-TIFF should not cause the browser to fetch that whole file. Browser requests should be for metadata, small derived thumbnails, and viewport tiles.

## Technical Direction

The spec-writer should research the current codebase before deciding the final architecture. The likely direction is:

- Keep authentication and access checks through the existing `/api/file` proxy path.
- Add tile endpoints under the existing file API or a closely related API route.
- Render tiles server-side from the source TIFF/OME-TIFF into WebP or PNG.
- Use a browser viewer library rather than hand-rolling pan/zoom math if a suitable library fits the app.
- Consider OpenSeadragon for tiled viewport/pan/zoom.
- Consider Viv/deck.gl-style rendering if it is a good fit for OME multichannel compositing, but weigh dependency and bundle impact carefully.
- Consider an internal tile manifest format or an OME-NGFF-like multiscale tile model.
- Add caching for derived tiles and metadata so repeated pan/zoom operations are not prohibitively expensive.
- Enforce resource limits: tile size, maximum concurrent tile renders, maximum output dimensions, cache bounds, timeouts, and clear failures for unsupported files.

Important: do not implement a solution that downloads the original TIFF to the browser and relies on client-side full-file decoding. That would fail the central requirement for large files.

## Tile and Cache Expectations

The spec should define a concrete strategy for serving tiles. It should answer:

- Whether tiles are generated on demand, precomputed on first open, or mixed.
- How tile coordinates map to source TIFF pages/planes and downsample levels.
- Whether non-pyramidal TIFFs are tiled by server-side region extraction, whole-plane resize, cached multiscales, or another approach.
- How OME metadata dimensions map to channel/Z/T plane selection.
- How channel composites are produced: server-side composite tiles, client-side blend of per-channel tile layers, or another design.
- How derived tiles are cached and invalidated.
- Where cache files live, how large they may get, and how cleanup works.
- What happens when a TIFF lacks OME metadata, is malformed, is too large for the configured limits, or cannot be tiled efficiently.

The design should prefer correctness and bounded resource use over trying to support every possible TIFF variant silently.

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

The spec should confirm these paths by codebase research rather than assuming the list is complete.

## Non-Goals

Do not require users to install desktop tools to view images.

Do not require users to download OME-TIFF files before inspection.

Do not commit large TIFF fixtures.

Do not weaken existing image, SVG, PDF, HTML, text, subfolder preview, file-type dropdown, saved filter, auth, or locked-result behavior.

Do not introduce `wa mlwh sync` or MLWH cache changes; this feature is file-preview/viewer work.

Do not fake OME channel or image data in production code. If a file is unsupported, show an honest unsupported/error state.

## Acceptance-Test Themes

The eventual spec should include behavior-focused acceptance tests for at least:

- OME metadata discovery exposes channel names, dimensions, Z count, T count, and physical pixel sizes when available.
- Opening a large OME-TIFF viewer does not request the original TIFF body in the browser.
- The tile endpoint enforces authenticated access and locked-result behavior consistently with existing file preview/download routes.
- The viewer initially renders a fit-to-view image from derived tiles.
- Panning/zooming requests tile URLs rather than the full file.
- Changing channel visibility or color changes the rendered/composited viewport.
- Changing Z or T requests the correct plane/tile set.
- Non-OME multi-page TIFFs fall back to plane selection with clear labeling.
- Unsupported or malformed TIFFs show a clear error and do not crash the page.
- Existing preview behavior for non-TIFF file types remains unchanged.
- Large local examples such as `.tmp/ome.tiff` and `.tmp/ome_collapsed.tiff` can be used manually for evidence, but automated tests use small generated fixtures.

## Open Questions for the Spec Writer to Resolve

The spec-writer clarification loop should ask the user questions where needed, especially:

- Should the inline file preview become the full deep-zoom viewer by default, or should the inline preview stay lightweight with an "open viewer" action?
- Is channel compositing mandatory for the first implementation, or can phase 1 deliver deep-zoom single-channel tiles with compositing in phase 2?
- Are server-side derived tile caches acceptable on disk, and if so where should they live and how aggressively should they be cleaned?
- Is OpenSeadragon acceptable as a dependency if the implementation handles compositing through multiple tile layers or server-side composite tiles?
- Should the viewer support only OME-TIFF initially, or generic pyramidal TIFF / multi-page TIFF too?
- What maximum file size and tile render cost should be supported before returning a bounded unsupported/error state?
