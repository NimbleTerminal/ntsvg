# Examples

Run these from the repository root. Each browser demo publishes its keys through
booba so the same actions appear as interactive buttons above the terminal.

| Command | What it shows | Controls |
| --- | --- | --- |
| `task run-gallery` | Embedded SVG sample gallery (`01-shapes.svg` .. `05-pipeline.svg`) across Raster & Info modes | `n`/`p`: next/prev sample, `m`: raster/info, `f`: fit mode, `+/-`: zoom, arrows: pan, `g`: glyph/kitty, `r`: reset |
| `task run-stage` | Immediate-mode vector generation with `nimbleterminal.dev/ntsvg/stage` (`Primitives`, `Bezier Waves`, `Rosette`, `Telemetry HUD`) | `s`: next scene, `c`: palette, `↑`/`↓`: detail, `m`: raster/info, `g`: glyph/kitty, `r`: reset |
| `task run-charts` | Interactive grouped bar and multi-series line charts via `stage.Chart` (`BarStage` / `LineStage`) | `t`: bar/line, `d`: dataset, `v`: vary values, `m`: raster/info, `f`: fit mode, `g`: glyph/kitty, `r`: reset |
| `task run-svgview` | Single-pane CLI viewer (`bin/ntsvg-svgview [path.svg]`) with on-demand chart generation and SVG export | `m`: raster/info, `c`: demo chart, `e`: export chart, `f`: fit mode, `+/-`: zoom, arrows: pan, `g`: glyph/kitty |

Use `q` or `Ctrl+C` to quit. `task serve-wasm-site` builds and serves all four
interactive demos at `http://localhost:8000/web/`.

## Sample SVG Files

Curated SVG samples live in [`examples/gallery/samples/`](file:///Users/evan/projects/ntcharts-svg/examples/gallery/samples):

- `01-shapes.svg` — overlapping translucent circles, rounded rectangles, polygons, polylines, and text
- `02-gradients.svg` — multi-stop linear and radial gradients with transforms and gradient-stroked Bezier paths
- `03-paths.svg` — cubic and quadratic Bezier splines with dashed strokes, line caps, and compound rings
- `04-orbit.svg` — space orbital transfer diagram with planet, transfer ellipse, spacecraft vector icon, and callouts
- `05-pipeline.svg` — `ntsvg` architecture diagram with `<title>`, `<desc>`, and `<text>` labels (press `m` to inspect in `InfoMode`)

You can also pass any sample file directly to `ntsvg-svgview`:

```sh
task build-examples
./bin/ntsvg-svgview examples/gallery/samples/02-gradients.svg
```
