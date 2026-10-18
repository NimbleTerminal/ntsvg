# CHANGELOG

## v0.5.1 (2026-10-18)

 * Upgrade `github.com/NimbleMarkets/oksvg` to v0.1.2, adding SVG marker rendering (including Mermaid arrowheads) and percentage-sized backgrounds.
 * Add a native and browser Mermaid diagram gallery (`task run-mermaid`) with five diagram types, five themes, zoom, pan, and Raster/Info modes.
 * Generate SVG with upstream `go-mermaid` v0.1.6 and pass it directly to ntsvg for rasterization, including zoomed viewports; keep the dependency in the examples module.
 * Include the Mermaid demo in the browser gallery, CI, and Pages builds, with rendering coverage across all diagrams/themes and browser interaction smoke tests.

## v0.5.0 (2026-10-17)

 * **Breaking:** rename module from `github.com/NimbleMarkets/ntcharts-svg` to `nimbleterminal.dev/ntsvg` (`package ntsvg` at the module root).
 * **Breaking:** extract the immediate-mode vector `Canvas` into `nimbleterminal.dev/ntsvg/stage` (`stage.Stage`, `stage.New`, `Chart.BarStage`, `Chart.LineStage`), keeping `stage` stdlib-only and pairing with `ntsvg.Model.ShowStage`, `ntsvg.RasterizeStage`, and `ntsvg.WriteStagePNG`.
 * Migrate terminal picture rendering from `github.com/NimbleMarkets/ntcharts/v2/picture` to `nimbleterminal.dev/ntpicture` `v0.1.0`.
 * Isolate `go-booba` and browser asset tools in `examples/go.mod` so library consumers only pull core dependencies.
 * Expose `ntpicture.Picture` configuration and lifecycle controls on `Config` and `Model` (`RenderAuto`, `AutoRenderMode`, `Crop`, `Viewport`, `Stop`, `Keep`, `KeepOnMainScreen`, `Dispose`, `Session`, `TerminalState`, `SetTerminal`, `SetCellSize`, `CellPixelSize`, `SetKittyCapability`, `Resolution`, `SetResolution`, `MaxRasterPixels`, `SetMaxRasterPixels`, `Background`, `SetBackground`, `Visible`, `SetVisible`, `Size`, `RasterGeometry`, and `PictureStatus`).
 * Add `gallery`, `stage`, and `charts` demos alongside `svgview`, `InfoMode` overlay over rasterized SVGs, and flicker-free document transitions.

## v0.4.0 (2026-10-14)

 * Move the browser Bubble Tea `replace` directive out of `go.mod` into `wasm.work` so native consumers use upstream `charm.land/bubbletea/v2` v2.1.0.
 * Upgrade `ntcharts/v2` to v2.8.0, `go-booba` to v0.9.2, and `charm.land/bubbletea/v2` to v2.1.0 (plus transitive dependencies).

## v0.3.0 (2026-09-28)

 * Default the demo to Kitty graphics once support is detected, preserving manual `g` toggles and glyph fallback.
 * Upgrade ntcharts to v2.4.0, gaining faster Kitty PNG encoding and opt-in RGBA/shared-memory transport.
 * Upgrade the WASM demo to booba v0.7.0 and the matching Bubble Tea fork; use shared-memory RGBA frames in the browser with direct PNG fallback.
 * Release picture transport resources in `Model.Close`, including caller-supplied images without an SVG renderer.
 * Require Go 1.26.8 or newer.

## v0.2.2 (2026-05-28)

 * feat: Add Fit and Anchor properties

## v0.2.1 (2026-05-25)

 * Improve error handling with more leniency to allow rasterizing of somewhat malformed SVG
 * Switch to `github.com/NimbleMarkets/oksvg` fork for Pattern, Text, Emoji, and Image support.

## v0.2.0 (2026-05-24)

 * Add Patterns and Text with `replace` to NimbleMarkets fork of `srwiley/oksvg`
 * Input hardening

## v0.1.2 (2026-05-19)

 * Add `Image()` getter to extract the rendered SVG image.  Handy for caching.
 * Implement `Renderer.RenderRegion` for vector-sharp zoom
 * Reduce `DefaultRenderEdge` to `1024` to optimize rasterization performance

## v0.1.1 (2026-05-17)

 * Add [GitHub pages demo](https://nimblemarkets.github.io/ntcharts-svg/) using [`booba`](https://github.com/NimbleMarkets/go-booba)

## v0.1.0 (2026-05-17)

  * Initial release
  * `svg.Model` Bubble Tea widget with RasterMode and InfoMode
  * Pure-Go SVG rasterization via `oksvg`/`rasterx` (works on native, WASM, WASI)
  * Zoom / pan viewport for inspecting rasterized pixels
  * Immediate-mode `Canvas` SVG generator with bar / line `Chart` helpers
  * `Config.Limits` resource caps for untrusted documents
