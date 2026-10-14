// config.go: configuration, key bindings, modes, and resource limits.

package ntsvg

import (
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg/stage"
)

// Mode selects how the current SVG is presented.
type Mode int8

const (
	// RasterMode rasterizes the SVG to a bitmap and renders it via
	// ntpicture.Model — half-block glyphs everywhere, Kitty graphics on
	// terminals that support them. This is the default: unlike PDF
	// rasterization, the SVG rasterizer (oksvg/rasterx) is pure Go, so
	// RasterMode works on every build target including WASM.
	RasterMode Mode = iota

	// InfoMode shows a textual summary of the document — view box,
	// element-type histogram, embedded titles — instead of pixels. It
	// is the zero-dependency fallback shown while a raster is pending
	// or when rasterization fails.
	InfoMode
)

// RenderMode is the underlying picture protocol used by RasterMode.
// Aliased so callers can reach picture's constants without importing
// ntpicture directly.
type RenderMode = picture.PictureMode

const (
	RenderGlyph = picture.PictureGlyph // universal half-block ANSI
	RenderKitty = picture.PictureKitty // high-resolution Kitty graphics
)

// FitMode controls how the rasterized image is mapped onto the cell
// rectangle. Re-exported from ntpicture so callers can drive
// picture.Model behavior (Contain / Fill / Cover) through ntsvg's API
// surface.
type FitMode = picture.FitMode

const (
	FitContain = picture.FitContain // preserve aspect ratio, letterbox (default)
	FitFill    = picture.FitFill    // stretch to fill the cell rectangle
	FitCover   = picture.FitCover   // preserve aspect ratio, crop to fill
)

// FitAnchor controls which edge or center is preserved when FitCover must
// crop overflow. Re-exported from ntpicture.
type FitAnchor = picture.FitAnchor

const (
	AnchorCenter = picture.AnchorCenter // center crop (default)
	AnchorTop    = picture.AnchorTop
	AnchorBottom = picture.AnchorBottom
	AnchorLeft   = picture.AnchorLeft
	AnchorRight  = picture.AnchorRight
)

// DefaultRenderEdge is the target length, in pixels, of the longer edge
// of the rasterized bitmap when Config.RenderEdge is unset. The SVG is
// drawn once at this resolution; ZoomIn then crops into the bitmap, so a
// larger value buys sharper zoomed inspection at the cost of memory.
//
// Rasterize cost scales with pixel area: 1024 keeps a full render well
// under ~200 ms and the bitmap near 4 MB, while a 64× zoom still crops
// to a ~16 px region — ample for chunky-pixel inspection. Hosts that
// need crisper deep-zoom can raise Config.RenderEdge.
const DefaultRenderEdge = 1024

// Limits caps resource consumption when loading and rasterizing SVGs.
// SVG is an untrusted-input format — a hostile document can nest
// elements millions deep, inline a gigabyte of base64 image data, or
// declare a viewBox that projects to a terabyte bitmap. Defaults
// (applied when a field is zero) are conservative; set a field to a
// negative value to disable that cap entirely (trusted input only).
type Limits struct {
	// MaxFileBytes caps the byte size of the SVG read into memory.
	// Zero = 64 MiB default.
	MaxFileBytes int64

	// MaxElements caps the number of XML elements the metadata scanner
	// will count before giving up. Zero = 250000 default. A hostile
	// document with millions of empty elements can't burn the load
	// goroutine — the scan stops and the count is reported as capped.
	MaxElements int

	// MaxRenderPixels caps the pixel area (width × height) of the
	// rasterized bitmap. Zero = 100 million pixel default (≈ 400 MB at
	// RGBA). The renderer scales the target down to fit this budget.
	MaxRenderPixels int

	// MaxRenderEdge caps the length of either edge of the rasterized
	// bitmap. Zero = 8192 default. Guards against a pathological
	// viewBox aspect ratio producing a 1 × 100000 sliver.
	MaxRenderEdge int
}

// applyDefaults fills zero-valued fields with the package defaults.
// Negative values are preserved as the "unbounded" sentinel.
func (l *Limits) applyDefaults() {
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = 64 << 20 // 64 MiB
	}
	if l.MaxElements == 0 {
		l.MaxElements = 250_000
	}
	if l.MaxRenderPixels == 0 {
		l.MaxRenderPixels = 100_000_000
	}
	if l.MaxRenderEdge == 0 {
		l.MaxRenderEdge = 8192
	}
}

// KeyMap binds the widget's actions to keys. Disabled bindings (the zero
// key.Binding) let host programs hand the corresponding action off to
// another widget.
type KeyMap struct {
	ToggleMode   key.Binding
	ToggleRender key.Binding
	Reload       key.Binding
	ZoomIn       key.Binding
	ZoomOut      key.Binding
	ResetView    key.Binding
	CycleFit     key.Binding
	PanLeft      key.Binding
	PanRight     key.Binding
	PanUp        key.Binding
	PanDown      key.Binding
}

// DefaultKeyMap matches the bindings documented in the README status bar:
// m or t toggle Raster/Info, g toggle Glyph/Kitty, r reload, +/- zoom,
// arrows pan, f fit mode, 0 reset view.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		ToggleMode:   key.NewBinding(key.WithKeys("m", "t"), key.WithHelp("m", "raster/info")),
		ToggleRender: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "glyph/kitty")),
		Reload:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		ZoomIn:       key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom in")),
		ZoomOut:      key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "zoom out")),
		ResetView:    key.NewBinding(key.WithKeys("0"), key.WithHelp("0", "reset view")),
		CycleFit:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fit mode")),
		PanLeft:      key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "pan left")),
		PanRight:     key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "pan right")),
		PanUp:        key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "pan up")),
		PanDown:      key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "pan down")),
	}
}

// Styles bundles all lipgloss styling the widget uses. The zero value
// yields uncolored defaults; DefaultStyles supplies sensible ones.
type Styles struct {
	Text   lipgloss.Style
	Info   lipgloss.Style
	Status lipgloss.Style
	Error  lipgloss.Style
}

// DefaultStyles produces the styling used by the example program.
// Designed to read well on both dark and light terminals — no
// hard-coded background colors.
func DefaultStyles() Styles {
	return Styles{
		Text: lipgloss.NewStyle(),
		Info: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("244")).
			BorderBackground(lipgloss.Color("235")).
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1),
		Status: lipgloss.NewStyle().Foreground(lipgloss.Color("242")),
		Error:  lipgloss.NewStyle().Foreground(lipgloss.Color("9")),
	}
}

// Config configures a Model at construction.
type Config struct {
	// Cols/Rows is the cell rectangle the widget renders into. Use
	// SetSize at runtime when the layout changes; these are the
	// initial values.
	Cols, Rows int

	// InitialPath, if set, queues a path-based load via the Cmd
	// returned from Init(). Equivalent to calling SetSVG(InitialPath)
	// after construction. Ignored when InitialData or InitialStage is set.
	InitialPath string

	// InitialData, when non-nil, queues an in-memory load via the Cmd
	// returned from Init(). Equivalent to SetSVGData(InitialName,
	// InitialData). nil = "not supplied"; an explicit empty slice
	// routes to the bytes loader and surfaces "empty svg data".
	//
	// The byte slice is copied inside NewWithConfig, so callers may
	// reuse or mutate the underlying buffer immediately afterward.
	InitialData []byte

	// InitialName is the display label used when InitialData is set
	// (falls back to "embedded" when blank). No effect unless
	// InitialData != nil.
	InitialName string

	// InitialStage, when non-nil and InitialData is nil, queues an
	// in-memory load of the generated stage via the Cmd returned from
	// Init(). Equivalent to calling ShowStage(InitialStage) after
	// construction.
	InitialStage *stage.Stage

	// DefaultMode selects the initial mode (RasterMode if zero).
	DefaultMode Mode

	// RendererFactory parses an SVG from bytes and returns a Renderer
	// bound to that document. When nil the constructor uses
	// DefaultRendererFactory, which rasterizes via oksvg/rasterx —
	// pure Go, available on every build target.
	RendererFactory RendererFactory

	// RenderEdge is the target length in pixels of the rasterized
	// bitmap's longer edge. Zero falls back to DefaultRenderEdge.
	RenderEdge int

	// Limits caps resource consumption for untrusted documents. Zero-
	// valued fields use sensible defaults (see Limits); set a field to
	// -1 to disable that specific cap for trusted input.
	Limits Limits

	// Fit controls how the rasterized image is mapped onto the cell
	// rectangle. The zero value is FitContain (preserve aspect ratio,
	// letterbox).
	Fit FitMode

	// Anchor controls which edge or center is preserved when FitCover must
	// crop overflow. Zero value is AnchorCenter.
	Anchor FitAnchor

	// PictureConfig is forwarded verbatim to the underlying picture.Model.
	PictureConfig picture.Config

	// Styles overrides the default lipgloss styling. Nil uses DefaultStyles.
	Styles *Styles
}
