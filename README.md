# ntsvg — Terminal SVG viewer & immediate-mode vector stage for Bubble Tea

<p>
    <a href="https://github.com/nimbleterminal/ntsvg/tags"><img src="https://img.shields.io/github/tag/nimbleterminal/ntsvg.svg" alt="Latest Release"></a>
    <a href="https://pkg.go.dev/nimbleterminal.dev/ntsvg?tab=doc"><img src="https://godoc.org/github.com/golang/gddo?status.svg" alt="GoDoc"></a>
    <a href="https://github.com/nimbleterminal/ntsvg/blob/main/CODE_OF_CONDUCT.md"><img src="https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg" alt="Code Of Conduct"></a>
</p>

`ntsvg` (`nimbleterminal.dev/ntsvg`) is a [Bubble Tea](https://github.com/charmbracelet/bubbletea) widget that makes SVG documents first-class citizens in terminal UIs (and WASM/browser builds). It pairs [`oksvg`](https://github.com/NimbleMarkets/oksvg) + [`rasterx`](https://github.com/srwiley/rasterx) for **pure-Go** SVG rasterization with [`ntpicture`](https://github.com/nimbleterminal/ntpicture) (`nimbleterminal.dev/ntpicture`) for image rendering — half-block glyphs anywhere, full-resolution Kitty graphics on terminals that support them (Kitty, Ghostty, WezTerm).

[**Try out the live WASM demo.**](https://nimbleterminal.github.io/ntsvg)

Because the SVG rasterizer is pure Go, image rendering needs **no CGO, no system dependencies, and no JS bridge** — it works identically on native, browser-WASM, and WASI builds.

The module provides two packages:

- **`nimbleterminal.dev/ntsvg`** — the terminal SVG viewer widget: load any `.svg` file, in-memory bytes, or generated `*stage.Stage`, rasterize, zoom, and pan.
- **`nimbleterminal.dev/ntsvg/stage`** — a pure-Go, dependency-free immediate-mode vector stage and chart builder that emits standalone SVG markup (`ToSVG`, `Bytes`, `WriteTo`, `WriteSVG`).

## Quickstart

```go
package main

import (
    "fmt"
    "os"

    tea "charm.land/bubbletea/v2"
    "nimbleterminal.dev/ntsvg"
)

type model struct{ sv ntsvg.Model }

func (m model) Init() tea.Cmd { return m.sv.Init() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if k, ok := msg.(tea.KeyMsg); ok && (k.String() == "q" || k.String() == "ctrl+c") {
        return m, tea.Quit
    }
    if sz, ok := msg.(tea.WindowSizeMsg); ok {
        return m, m.sv.SetSize(sz.Width, sz.Height)
    }
    var cmd tea.Cmd
    m.sv, cmd = m.sv.Update(msg)
    return m, cmd
}

func (m model) View() tea.View { return m.sv.View() }

func main() {
    sv := ntsvg.NewWithConfig(ntsvg.Config{InitialPath: os.Args[1]})
    if _, err := tea.NewProgram(model{sv: sv}).Run(); err != nil {
        fmt.Println(err); os.Exit(1)
    }
}
```

Toggle Raster ↔ Info with `m`, swap Glyph ↔ Kitty with `g`, zoom with `+` / `-`, pan with arrows, cycle fit with `f`, reload with `r`, reset view with `0`. The widget's `Update` dispatches all of these through `DefaultKeyMap()`; rebind a field to change the keystroke (or set a binding to `key.Binding{}` to disable it).

## Demos & Sample Gallery

Building requires Go 1.26.8 or newer. See [`examples/README.md`](./examples/README.md) for full details:

- **`task run-gallery`** ([`examples/gallery`](./examples/gallery/main.go)) — browse five embedded SVG samples ([`examples/gallery/samples/`](./examples/gallery/samples)) across Raster and Info modes, fit policies, and zoom levels.
- **`task run-stage`** ([`examples/stage`](./examples/stage/main.go)) — interactive immediate-mode vector stage (`stage.Stage`) with geometric primitives, cubic Bezier waves, procedural rosettes, and telemetry HUD cards.
- **`task run-charts`** ([`examples/charts`](./examples/charts/main.go)) — live grouped bar and multi-series line chart builder using `stage.Chart`.
- **`task run-svgview`** ([`examples/svgview`](./examples/svgview/main.go)) — single-pane CLI viewer (`./bin/ntsvg-svgview [path.svg]`) with a status bar, help bubble, and `c` / `e` keys to generate and export a chart at runtime.

All demos automatically select Kitty graphics when terminal support is detected, and otherwise use half-block glyph rendering (`g` toggles manually).

The same examples compile to WebAssembly and run in the browser sample gallery via [`booba`](https://github.com/NimbleMarkets/go-booba), publishing their key bindings as interactive action buttons above the terminal:

```sh
task serve-wasm-site   # builds web/demos/*.wasm, stages booba assets, serves http://localhost:8000/web/
```

`task build-wasm-site` alone produces the deployable `web/` directory; the `.github/workflows/pages.yml` workflow publishes it to GitHub Pages on every push to `main`.

The browser demo uses booba v0.9.2 and requests `ntpicture`'s Kitty shared-memory
transport, passing raw RGBA frames without PNG encoding or base64 image payloads.
It falls back to direct PNG when the browser bridge is unavailable. Native builds
keep direct PNG transport. Library callers can opt in through
`Config.PictureConfig.KittyMedium` and select a direct format through
`Config.PictureConfig.KittyFormat`; call `Model.Close()` when disposing the widget
to release pending shared-memory frames.

## Modes

| Mode | What it does | Where it works |
|---|---|---|
| `ntsvg.RasterMode` (default) | Rasterizes the SVG via a `Renderer` (default: `oksvg`/`rasterx`), fed to `nimbleterminal.dev/ntpicture`. Supports zoom (`+`/`-` up to ×64) and pan (arrow keys when zoomed) | Everywhere — native, browser-WASM, WASI |
| `ntsvg.InfoMode` | Zero-dependency textual summary: view box, declared size, element-type histogram, embedded `<title>` / `<desc>` | Everywhere |

`sv.ToggleMode()` returns a `tea.Cmd` that swaps modes and re-renders. RasterMode internally uses `ntpicture.PictureGlyph` (universal half-blocks) or `ntpicture.PictureKitty` (high-resolution); switch between them with `sv.ToggleRenderMode()`.

## Renderer

```go
type Renderer interface {
    Render(maxW, maxH int) (image.Image, error)
    RenderRegion(maxW, maxH int, nx, ny, nw, nh float64) (image.Image, error)
    ViewBox() (w, h float64)
    Close() error
}

type RendererFactory func(name string, data []byte) (Renderer, error)
```

The factory is **bytes-first**: the widget reads file contents once (subject to `Limits.MaxFileBytes`) and hands the bytes to the factory, so custom factories never have to implement their own file I/O. Hosts that already have the bytes — an `//go:embed` blob, an HTTP response, a generated `stage.Stage` — skip the path round-trip via `sv.SetSVGData(name, data)`, `sv.ShowStage(st)`, or `Config.InitialData` / `InitialName` / `InitialStage`.

When `Config.RendererFactory` is nil, `NewWithConfig` installs `DefaultRendererFactory()`, backed by `oksvg`/`rasterx`. Bring-your-own renderers (resvg via CGO, a server-side rasterizer, a caching layer) plug in through the same interface. `Config.RenderEdge` (default 1024) sets the longer-edge resolution of the rasterized bitmap; zoom then re-rasterizes the sub-region via `RenderRegion`.

### Loading APIs

| Call | When to use |
|---|---|
| `sv.SetSVG(path)` / `Config.InitialPath` | SVG is on disk; widget reads it once (`Limits.MaxFileBytes` enforced via `os.Stat`) |
| `sv.SetSVGData(name, data)` / `Config.InitialData` + `InitialName` | SVG bytes already in memory (embedded, fetched, generated) |
| `sv.ShowStage(st)` / `Config.InitialStage` | Display a `*stage.Stage` you drew with `nimbleterminal.dev/ntsvg/stage` |
| `sv.SetImage(img)` | Install a pre-rasterized `image.Image`, bypassing the `Renderer` entirely |

### Renderer state introspection

- `sv.HasRenderer() bool` — true when the factory produced a Renderer for the current document.
- `sv.RendererErr() error` — the most recent factory error, persisted for the document's lifetime (cleared on the next load). InfoMode still works; surface this to explain why RasterMode is unavailable.

## Immediate-mode Vector Stage (`nimbleterminal.dev/ntsvg/stage`)

`stage.Stage` is a self-contained, pure-Go SVG writer with zero external dependencies — draw with chainable primitives, then export clean SVG or rasterize via `ntsvg`:

```go
import (
    "nimbleterminal.dev/ntsvg"
    "nimbleterminal.dev/ntsvg/stage"
)

st := stage.New(200, 120).Background("white")
st.Circle(60, 60, 40, stage.Fill("#4e79a7"))
st.Line(0, 0, 200, 120, stage.Stroke("#e15759", 3))
st.Text(100, 110, "hello", stage.Paint{}.WithFont("sans-serif", 12).WithAnchor("middle"))

svgText := st.ToSVG()                              // standalone SVG markup
_ = st.WriteSVG("out.svg")                         // write a .svg file
_ = ntsvg.WriteStagePNG("out.png", st, 800, 600)   // rasterize to PNG
img, _ := ntsvg.RasterizeStage(st, 800, 600)       // rasterize to image.Image
```

Primitives: `Line`, `Rect`, `RoundRect`, `Circle`, `Ellipse`, `Polyline`, `Polygon`, `Path`, `Text`, and `Raw` for an escape hatch. `Paint` carries fill / stroke / opacity / dash / linecap / font, with fluent `With*` builders and the `Fill` / `Stroke` constructors.

### Chart helpers

`stage.Chart` builds bar and line charts on top of `stage.Stage`:

```go
st := stage.NewChart(360, 240).
    SetTitle("Quarterly Revenue").
    SetLabels("Q1", "Q2", "Q3", "Q4").
    AddSeries("2025", "", 12, 19, 14, 23).
    AddSeries("2026", "", 17, 15, 21, 28).
    BarStage() // or LineStage()

cmd := mySvgModel.ShowStage(st) // render it straight into the viewer
```

This is the "generate chart → Stage → SVG → display" loop the `c` key in the example demonstrates.

## Resource limits (`Config.Limits`)

SVG is an untrusted-input format; a hostile document can nest elements millions deep, inline a gigabyte of base64 image data, or declare a viewBox that projects to a terabyte bitmap. `Config.Limits` exposes conservative caps applied automatically:

| Field | Default | What it caps |
|---|---|---|
| `MaxFileBytes` | 64 MiB | SVG size before the loader reads it |
| `MaxElements` | 250 000 | Element-histogram counting; the scan stops and reports "capped" |
| `MaxRenderPixels` | 100 000 000 | Rasterized bitmap area; the renderer scales down to fit |
| `MaxRenderEdge` | 8192 | Either edge of the rasterized bitmap |

Set any field to `-1` to disable that cap for trusted input; zero uses the default. SVG parsing and rasterization are both panic-guarded — a malformed document surfaces an error rather than crashing the load goroutine.

### Sanitizing SVG-derived strings

SVG titles, descriptions, element names, and error messages can carry C0/C1 control sequences and Unicode bidi format chars ("Trojan Source"-style attacks). The widget's own status text is already sanitized. For host-displayed strings derived from the SVG — `Model.Title()`, `Model.Name()` for in-memory docs, `RendererErr().Error()` — use `ntsvg.SanitizeForTerminal(s string) string`, which drops anything outside Unicode's printable categories and folds newlines / tabs to single spaces.

## Known caveats

- **WASM dependencies.** `wasm.work` selects the Bubble Tea fork with browser support; native builds use the upstream dependency in `go.mod`. The WASM tasks and CI select this workspace automatically; manual builds need `GOWORK="$PWD/wasm.work" GOOS=js GOARCH=wasm go build -o web/demos/gallery.wasm ./examples/gallery`.
- **Zoom is vector-sharp.** With a document `Renderer` attached, `ZoomIn` / `PanLeft` (etc.) re-rasterize the viewport sub-rectangle via `Renderer.RenderRegion`, so zoomed-in views stay crisp at any depth rather than upscaling a fixed bitmap. Zoom 0 reuses the full-document bitmap (no re-render); each zoom/pan keypress at depth costs one rasterize. A host bitmap installed via `SetImage` has no renderer, so it still crops (chunky-pixel inspection) — there are no vectors to re-rasterize.
- **`oksvg` supports a broad but not exhaustive slice of SVG.** Filters, advanced gradients, embedded fonts, and CSS-heavy documents may render approximately or skip elements (the parser runs in ignore-errors mode). For pixel-perfect fidelity, wire a custom `RendererFactory` around a stricter rasterizer.
- **The immediate-mode `stage.Stage` is a writer, not a layout engine.** It emits exactly the elements you draw. Text is not measured — `text-anchor` positions runs, but you supply the coordinates. This keeps `stage` dependency-free and WASM-safe.
- **`WriteSVG` / `WritePNG` / `WriteStagePNG` do filesystem I/O** and will fail at runtime under `GOOS=js`. Use `ToSVG` / `Bytes` / `WriteTo` / `RasterizeStage` in the browser.
- **Kitty placement composition through lipgloss** is best-effort. `ntpicture.Picture.View()` embeds the Kitty placement escapes directly in the rendered string, which composes cleanly inside `lipgloss.JoinVertical` and basic borders. Glyph mode has no such caveat.
- **Vibe coded.** This comment was inserted by a human.

## License

Thanks to the [`oksvg`](https://github.com/srwiley/oksvg) and [`rasterx`](https://github.com/srwiley/rasterx) libraries and their authors. We are currently using [a fork](https://github.com/NimbleMarkets/oksvg/tree/nm) of their `oksvg` library.

[MIT License](./LICENSE.txt) — Copyright (c) 2026 [Neomantra Corp](https://www.neomantra.com).

----
Made with :heart: and :fire: by the team behind [Nimble.Markets](https://nimble.markets).
