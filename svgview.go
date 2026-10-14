// Package ntsvg is a Bubble Tea v2 widget that makes SVG documents
// first-class citizens in terminal UIs (and WASM/browser builds). It
// offers two modes:
//
//   - RasterMode rasterizes the SVG via a pluggable Renderer (the
//     default is oksvg/rasterx — pure Go, no CGO) and feeds the bitmap
//     to nimbleterminal.dev/ntpicture, which emits Kitty graphics when
//     the terminal supports them and falls back to half-block glyphs
//     otherwise. Zoom (up to ×64) and pan inspect the rasterized pixels.
//   - InfoMode shows a zero-dependency textual summary of the document:
//     its view box, an element-type histogram, and any embedded title.
//
// Because the default rasterizer is pure Go, RasterMode works on every
// build target, including GOOS=js and GOOS=wasip1.
//
// Companion subpackage nimbleterminal.dev/ntsvg/stage provides an
// immediate-mode vector Stage for generating charts and diagrams as
// clean SVG; ShowStage wires a *stage.Stage straight into the viewer.
package ntsvg

import (
	"fmt"
	"image"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"
)

// Model is the ntsvg viewer widget. Use New or NewWithConfig to construct.
type Model struct {
	cfg   Config
	keys  KeyMap
	style Styles

	pic        picture.Model
	cols, rows int

	// name is the display label for the loaded document (file path, or
	// the InitialName / SetSVGData name for in-memory loads). Seeded at
	// construction so View() can show "Loading <name>…" while the async
	// load is in flight.
	name string
	mode Mode

	// doc is the InfoMode summary of the currently-loaded SVG. nil
	// until the first successful load.
	doc *Document

	// sourceImage is the full-resolution rasterized bitmap from the
	// most recent Render call. nil until the first successful render.
	// Cropped on demand to the (zoom, panX, panY) viewport before being
	// handed to picture.Model.
	sourceImage image.Image

	// Viewport state. zoom 0 means "fit the whole image to the cell
	// rect"; zoom N (N >= 1) crops the source to a 1/2^N square that
	// picture.Model scales back up. panX, panY are the normalized
	// [0, 1] coords of the visible rectangle's top-left corner.
	zoom       int
	panX, panY float64

	// cur is the Renderer for the currently-loaded SVG. nil while no
	// document is loaded or when the factory failed. Closed and
	// replaced by every successful load.
	cur Renderer

	// rendererErr is the most recent factory error, persisted (unlike
	// err) so hosts can keep displaying it for the document's lifetime.
	rendererErr error

	// loadGen / renderGen serialize async work. Every load bumps
	// loadGen; every rasterize bumps renderGen. Stale msgs are dropped
	// by comparing counters in Update. Pointers so the counters survive
	// Bubble Tea's value-receiver idiom.
	loadGen   *uint64
	renderGen *uint64

	err error
}

// New constructs a Model with the given cell rectangle and default config.
func New(cols, rows int) Model {
	return NewWithConfig(Config{Cols: cols, Rows: rows})
}

// NewWithConfig constructs a Model from a Config. Zero-valued fields are
// filled in from package defaults.
func NewWithConfig(cfg Config) Model {
	if cfg.RenderEdge <= 0 {
		cfg.RenderEdge = DefaultRenderEdge
	}
	cfg.Limits.applyDefaults()
	if cfg.RendererFactory == nil {
		cfg.RendererFactory = DefaultRendererFactoryWithLimits(cfg.Limits)
	}
	if cfg.InitialData == nil && cfg.InitialStage != nil {
		cfg.InitialData = cfg.InitialStage.Bytes()
		if cfg.InitialName == "" {
			if title := cfg.InitialStage.Title(); title != "" {
				cfg.InitialName = title
			} else {
				cfg.InitialName = "stage.svg"
			}
		}
	}
	// Detach InitialData from the caller's slice immediately — Init runs
	// asynchronously and the ownership contract promises callers can
	// mutate the buffer the moment NewWithConfig returns.
	cfg.InitialData = copyBytes(cfg.InitialData)
	styles := DefaultStyles()
	if cfg.Styles != nil {
		styles = *cfg.Styles
	}

	if cfg.Fit != 0 && cfg.PictureConfig.Fit == 0 {
		cfg.PictureConfig.Fit = cfg.Fit
	}
	if cfg.Anchor != 0 && cfg.PictureConfig.Anchor == 0 {
		cfg.PictureConfig.Anchor = cfg.Anchor
	}

	var lg, rg uint64
	m := Model{
		cfg:       cfg,
		keys:      DefaultKeyMap(),
		style:     styles,
		pic:       picture.NewModelWithConfig(cfg.PictureConfig),
		cols:      cfg.Cols,
		rows:      cfg.Rows,
		mode:      cfg.DefaultMode,
		name:      initialNameLabel(cfg),
		loadGen:   &lg,
		renderGen: &rg,
	}
	_ = m.pic.SetSize(cfg.Cols, cfg.Rows)
	return m
}

// Mode returns the current rendering mode.
func (m Model) Mode() Mode { return m.mode }

// RenderMode returns the underlying picture protocol used by RasterMode.
func (m Model) RenderMode() RenderMode { return m.pic.Mode() }

// Name returns the display label for the currently-loaded document.
// Empty when nothing has been loaded. Hosts wanting a short filename for
// a status bar can apply filepath.Base() at the call site.
func (m Model) Name() string { return m.name }

// Title returns the loaded SVG's first <title> element text, or "".
// Not sanitized — apply SanitizeForTerminal before display.
func (m Model) Title() string {
	if m.doc == nil {
		return ""
	}
	return m.doc.Title()
}

// Document returns the InfoMode summary of the loaded SVG, or nil when
// nothing is loaded.
func (m Model) Document() *Document { return m.doc }

// ViewBox returns the loaded document's intrinsic dimensions in user
// units, or (0, 0) when nothing is loaded.
func (m Model) ViewBox() (w, h float64) {
	if m.doc == nil {
		return 0, 0
	}
	return m.doc.ViewBox()
}

// Err returns the last load or render error, cleared on the next
// successful operation.
func (m Model) Err() error { return m.err }

// Image returns the full-resolution rasterized bitmap from the most
// recent successful render, or nil when nothing has been rasterized.
// Hosts can cache the result and re-install it later with SetImage to
// skip the load-and-rasterize round trip.
func (m Model) Image() image.Image { return m.sourceImage }

// KittySupported reports the terminal's Kitty graphics capability. The
// probe runs once per process; expect KittyCapabilityUnknown for the
// first few frames until the terminal responds.
func (m Model) KittySupported() picture.KittyCapability { return m.pic.KittySupported() }

// HasRenderer reports whether an active Renderer is attached to the
// loaded document. False means RasterMode falls back to InfoMode —
// check RendererErr for the reason.
func (m Model) HasRenderer() bool { return m.cur != nil }

// RendererErr returns the most recent RendererFactory error, or nil.
// The load otherwise succeeded — InfoMode works fine; only RasterMode is
// unavailable. Cleared on the next load.
func (m Model) RendererErr() error { return m.rendererErr }

// Close releases picture transport and renderer-side resources held by the loaded
// document. Safe to call when nothing is loaded.
func (m *Model) Close() error {
	// Shared-memory frames must be released even for caller-supplied images
	// that have no SVG renderer. Terminal cleanup is unnecessary on disposal.
	_ = m.pic.SetImage(nil)
	if m.cur == nil {
		return nil
	}
	err := m.cur.Close()
	m.cur = nil
	return err
}

// KeyMap exposes the active bindings; mutate fields to rebind.
func (m *Model) KeyMap() *KeyMap { return &m.keys }

// SetSize updates the cell rectangle and forwards to the picture.Model.
// The returned Cmd may carry a Kitty re-placement frame; batch it
// through the program's tea.Cmd pipeline. The rasterized bitmap is
// resolution-independent of the cell rect, so no re-rasterize is needed.
func (m *Model) SetSize(cols, rows int) tea.Cmd {
	m.cols, m.rows = cols, rows
	return m.pic.SetSize(cols, rows)
}

// SetSVG loads an SVG from disk. Returns a Cmd that performs the
// metadata scan and renderer open off the UI goroutine; Update later
// consumes an svgLoadedMsg (or svgErrMsg on failure).
func (m *Model) SetSVG(path string) tea.Cmd {
	m.resetForLoad(path)
	gen := bump(m.loadGen)
	bump(m.renderGen)
	return loadFromPathCmd(path, gen, m.cfg.RendererFactory, m.cfg.Limits)
}

// SetSVGData loads an in-memory SVG. name is the display label shown in
// status bars; it is not parsed. The data slice is copied before being
// handed to the async load Cmd, so callers may reuse the buffer
// immediately.
func (m *Model) SetSVGData(name string, data []byte) tea.Cmd {
	m.resetForLoad(name)
	gen := bump(m.loadGen)
	bump(m.renderGen)
	return loadFromBytesCmd(name, copyBytes(data), gen, m.cfg.RendererFactory, m.cfg.Limits)
}

// resetForLoad clears per-document state ahead of a new load and closes
// any renderer the previous document held.
func (m *Model) resetForLoad(name string) {
	if m.cur != nil {
		_ = m.cur.Close()
		m.cur = nil
	}
	// Clear the document and image only if we are loading a different file/resource.
	// Preserving them for the same name avoids screen flashing during streaming updates.
	if m.name != name {
		m.doc = nil
		m.sourceImage = nil
	}
	m.name = name
	m.rendererErr = nil
	m.zoom, m.panX, m.panY = 0, 0, 0
	m.err = nil
}

// SetImage installs a caller-supplied rasterized bitmap, bypassing the
// Renderer entirely. Useful when the host has its own SVG pipeline (a
// server pre-rasterizes and ships PNG bytes). Applies the current
// viewport and returns picture's render Cmd.
func (m *Model) SetImage(img image.Image) tea.Cmd {
	// Bump both generation counters so any load or rasterize still in
	// flight is superseded — its result would otherwise overwrite the
	// caller-supplied bitmap when it lands in Update.
	bump(m.loadGen)
	bump(m.renderGen)
	// Detach any document renderer: a host bitmap has no document, and
	// a stale renderer left attached would re-rasterize the wrong SVG
	// on the next zoom (render-on-zoom).
	if m.cur != nil {
		_ = m.cur.Close()
		m.cur = nil
	}
	m.sourceImage = img
	m.err = nil
	return m.applyViewport()
}

// Renderer returns the active Renderer of the loaded document, or nil.
func (m Model) Renderer() Renderer {
	return m.cur
}

// SetImageAndRenderer installs a caller-supplied rasterized bitmap along with its
// document renderer and document info, bypassing the async loader while preserving
// vector-sharp zoom capabilities.
func (m *Model) SetImageAndRenderer(img image.Image, r Renderer, doc *Document) tea.Cmd {
	bump(m.loadGen)
	bump(m.renderGen)
	if m.cur != nil && m.cur != r {
		_ = m.cur.Close()
	}
	m.cur = r
	m.doc = doc
	m.sourceImage = img
	m.err = nil
	return m.applyViewport()
}

// ToggleMode swaps Raster↔Info. Switching into RasterMode renders the
// SVG when no bitmap is cached yet; switching to InfoMode clears the
// picture image so the terminal doesn't leave a stale Kitty placement.
func (m *Model) ToggleMode() tea.Cmd {
	if m.mode == InfoMode {
		m.mode = RasterMode
		if m.sourceImage != nil {
			return m.applyViewport()
		}
		return m.renderCmd()
	}
	m.mode = InfoMode
	return m.pic.SetImage(nil)
}

// ToggleRenderMode swaps Glyph↔Kitty for RasterMode rendering.
func (m *Model) ToggleRenderMode() tea.Cmd { return m.pic.Toggle() }

// Reload re-rasterizes the current SVG (no-op in InfoMode or when no
// renderer is attached).
func (m *Model) Reload() tea.Cmd {
	if m.mode != RasterMode {
		return nil
	}
	return m.renderCmd()
}

// Zoom returns the current zoom level (0 = fit whole image; N >= 1 =
// each axis cropped to 1/2^N of the source).
func (m Model) Zoom() int { return m.zoom }

// ZoomIn doubles the magnification, recentering on the current viewport
// center. Caps at zoomMax. No-op outside RasterMode or before a bitmap
// has been rendered.
func (m *Model) ZoomIn() tea.Cmd {
	if m.mode != RasterMode || m.sourceImage == nil || m.zoom >= zoomMax {
		return nil
	}
	cx, cy := m.viewportCenter()
	m.zoom++
	m.recenterTo(cx, cy)
	return m.applyViewport()
}

// ZoomOut halves the magnification toward fit-to-rect, recentering on
// the current viewport center. No-op when already at zoom 0.
func (m *Model) ZoomOut() tea.Cmd {
	if m.mode != RasterMode || m.sourceImage == nil || m.zoom <= 0 {
		return nil
	}
	cx, cy := m.viewportCenter()
	m.zoom--
	m.recenterTo(cx, cy)
	return m.applyViewport()
}

// PanLeft, PanRight, PanUp, PanDown shift the viewport by panStep of its
// own width / height. They no-op outside RasterMode, when no bitmap is
// loaded, or at zoom 0 (the whole image fits — nothing to pan into).
func (m *Model) PanLeft() tea.Cmd  { return m.pan(-1, 0) }
func (m *Model) PanRight() tea.Cmd { return m.pan(+1, 0) }
func (m *Model) PanUp() tea.Cmd    { return m.pan(0, -1) }
func (m *Model) PanDown() tea.Cmd  { return m.pan(0, +1) }

// Fit returns the current FitMode (how the bitmap is mapped onto the
// cell rectangle by picture.Model).
func (m Model) Fit() FitMode { return m.pic.Fit() }

// CycleFit advances the FitMode through Contain → Fill → Cover → Contain.
func (m *Model) CycleFit() tea.Cmd {
	next := FitContain
	switch m.pic.Fit() {
	case FitContain:
		next = FitFill
	case FitFill:
		next = FitCover
	case FitCover:
		next = FitContain
	}
	return m.pic.SetFit(next)
}

// SetFit updates the FitMode. Forwards to the embedded picture.Model.
func (m *Model) SetFit(fit FitMode) tea.Cmd {
	return m.pic.SetFit(fit)
}

// Anchor returns the current FitAnchor.
func (m Model) Anchor() FitAnchor { return m.pic.Anchor() }

// SetAnchor updates the FitAnchor. Forwards to the embedded picture.Model.
func (m *Model) SetAnchor(anchor FitAnchor) tea.Cmd {
	return m.pic.SetAnchor(anchor)
}

// ResetView snaps zoom to 0 and pan to the origin (fit-to-rect, no crop).
func (m *Model) ResetView() tea.Cmd {
	if m.zoom == 0 && m.panX == 0 && m.panY == 0 {
		return nil
	}
	m.zoom, m.panX, m.panY = 0, 0, 0
	if m.mode != RasterMode || m.sourceImage == nil {
		return nil
	}
	return m.applyViewport()
}

// Init returns a Cmd that kicks off the initial load (when either
// Config.InitialData or Config.InitialPath is set) plus the picture
// model's own init (Kitty capability probe). InitialData wins over
// InitialPath when present, including the explicit empty-slice case.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.pic.Init()}
	switch {
	case m.cfg.InitialData != nil:
		gen := bump(m.loadGen)
		cmds = append(cmds, loadFromBytesCmd(initialName(m.cfg), copyBytes(m.cfg.InitialData),
			gen, m.cfg.RendererFactory, m.cfg.Limits))
	case m.cfg.InitialPath != "":
		gen := bump(m.loadGen)
		cmds = append(cmds, loadFromPathCmd(m.cfg.InitialPath, gen, m.cfg.RendererFactory, m.cfg.Limits))
	}
	return tea.Batch(cmds...)
}

// initialName resolves the display label for an in-memory initial load.
func initialName(cfg Config) string {
	if cfg.InitialName != "" {
		return cfg.InitialName
	}
	return "embedded"
}

// initialNameLabel picks the label seeded into m.name at construction,
// mirroring Init's "InitialData wins" precedence.
func initialNameLabel(cfg Config) string {
	if cfg.InitialData != nil {
		return initialName(cfg)
	}
	return cfg.InitialPath
}

// bump increments *p and returns the new value. Sole writer is the UI
// goroutine, so no atomic ordering is needed — the pointer indirection
// exists only so value-receiver method copies share one counter.
func bump(p *uint64) uint64 {
	*p++
	return *p
}

// copyBytes defensively copies a byte slice. Returns nil for nil input.
func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// Update routes messages through the picture.Model, consumes the
// widget's async load/render notifications, and dispatches the KeyMap
// bindings.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if cmd := m.dispatchKey(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}

	case svgLoadedMsg:
		if msg.gen != *m.loadGen {
			// Stale load superseded by a newer one — close the
			// Renderer it built so we don't leak it.
			if msg.renderer != nil {
				_ = msg.renderer.Close()
			}
			break
		}
		m.name = msg.name
		m.doc = msg.doc
		m.cur = msg.renderer
		m.rendererErr = msg.rendererErr
		m.err = nil
		if m.mode == RasterMode {
			if c := m.renderCmd(); c != nil {
				cmds = append(cmds, c)
			}
		}

	case svgErrMsg:
		if msg.gen == *m.loadGen {
			m.err = msg.err
			m.doc = nil
			m.sourceImage = nil
		}

	case renderedMsg:
		if msg.gen != *m.renderGen || msg.loadGen != *m.loadGen {
			break // stale render or stale document
		}
		m.err = nil
		if msg.region {
			// A viewport re-render: display-only. The canonical
			// full-document bitmap (Image(), zoom 0) is left untouched.
			if c := m.pic.SetImage(msg.img); c != nil {
				cmds = append(cmds, c)
			}
		} else {
			// The full-document raster — the bitmap Image() exposes.
			// Route through applyViewport so a zoom that happened while
			// the load was in flight is still honored.
			m.sourceImage = msg.img
			if c := m.applyViewport(); c != nil {
				cmds = append(cmds, c)
			}
		}

	case renderErrMsg:
		if msg.gen == *m.renderGen && msg.loadGen == *m.loadGen {
			m.err = msg.err
			m.sourceImage = nil
		}
	}

	// Forward every message to the picture model so it reacts to Kitty
	// frame applications and resize notifications. picture.Update is a
	// no-op for messages it doesn't recognize.
	if c := m.pic.Update(msg); c != nil {
		cmds = append(cmds, c)
	}

	return m, tea.Batch(cmds...)
}

// dispatchKey resolves a tea.KeyMsg against m.keys and calls the
// matching action method. No-op when no binding matches.
func (m *Model) dispatchKey(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.ToggleMode):
		return m.ToggleMode()
	case key.Matches(msg, m.keys.ToggleRender):
		return m.ToggleRenderMode()
	case key.Matches(msg, m.keys.Reload):
		return m.Reload()
	case key.Matches(msg, m.keys.ZoomIn):
		return m.ZoomIn()
	case key.Matches(msg, m.keys.ZoomOut):
		return m.ZoomOut()
	case key.Matches(msg, m.keys.ResetView):
		return m.ResetView()
	case key.Matches(msg, m.keys.CycleFit):
		return m.CycleFit()
	case key.Matches(msg, m.keys.PanLeft):
		return m.PanLeft()
	case key.Matches(msg, m.keys.PanRight):
		return m.PanRight()
	case key.Matches(msg, m.keys.PanUp):
		return m.PanUp()
	case key.Matches(msg, m.keys.PanDown):
		return m.PanDown()
	}
	return nil
}

// renderCmd returns a Cmd that rasterizes the loaded SVG via the
// attached Renderer. Returns nil when there is no Renderer — that's the
// legitimate "RasterMode degrades to InfoMode" fallback handled by View.
func (m *Model) renderCmd() tea.Cmd {
	if m.cur == nil {
		return nil
	}
	gen := bump(m.renderGen)
	loadGen := *m.loadGen
	r := m.cur
	edge := m.cfg.RenderEdge
	return func() tea.Msg {
		img, err := r.Render(edge, edge)
		if err != nil {
			return renderErrMsg{err: err, gen: gen, loadGen: loadGen}
		}
		return renderedMsg{img: img, gen: gen, loadGen: loadGen}
	}
}

// renderRegionCmd returns a Cmd that re-rasterizes just the current
// viewport rectangle via the attached Renderer, giving vector-sharp
// zoom instead of upscaling a crop of the fixed bitmap. Returns nil when
// there is no Renderer. The result is a renderedMsg with region == true.
func (m *Model) renderRegionCmd() tea.Cmd {
	if m.cur == nil {
		return nil
	}
	gen := bump(m.renderGen)
	loadGen := *m.loadGen
	r := m.cur
	edge := m.cfg.RenderEdge
	vp := m.viewportSize()
	nx, ny := m.panX, m.panY
	return func() tea.Msg {
		img, err := r.RenderRegion(edge, edge, nx, ny, vp, vp)
		if err != nil {
			return renderErrMsg{err: err, gen: gen, loadGen: loadGen}
		}
		return renderedMsg{img: img, gen: gen, loadGen: loadGen, region: true}
	}
}

// View renders the loaded SVG according to the active mode. RasterMode
// falls back to InfoMode when no renderer is attached or no bitmap has
// been rasterized yet.
func (m Model) View() tea.View {
	// Error first: a load failure should not hide behind "No SVG
	// loaded". Sanitize externally-derived strings before they reach
	// the terminal.
	if m.err != nil && m.doc == nil {
		return tea.NewView(m.placeholderView(m.style.Error,
			fmt.Sprintf("error: %s", SanitizeForTerminal(m.err.Error()))))
	}
	if m.mode == RasterMode && m.sourceImage != nil {
		// A rasterized bitmap is shown as soon as one exists — even with
		// no document, since SetImage installs caller-supplied bitmaps
		// that have no parsed Document. This must win over the "Loading"
		// placeholder so a SetImage during an unrelated in-flight load
		// is not hidden behind it.
		//
		// Pin picture's content to the full (cols × rows) envelope so
		// the host's surrounding border stays a stable size across
		// mode and fit transitions.
		pv := m.pic.View()
		if m.cols > 0 && m.rows > 0 {
			pv.Content = lipgloss.Place(m.cols, m.rows, lipgloss.Left, lipgloss.Top, pv.Content)
		}
		return pv
	}
	if m.name == "" {
		return tea.NewView(m.placeholderView(m.style.Status, "No SVG loaded"))
	}
	if m.doc == nil {
		return tea.NewView(m.placeholderView(m.style.Status,
			fmt.Sprintf("Loading %s…", SanitizeForTerminal(m.name))))
	}
	// InfoMode, or RasterMode with no bitmap yet.
	content := m.renderInfoView()
	if m.cols > 0 && m.rows > 0 {
		content = lipgloss.Place(m.cols, m.rows, lipgloss.Left, lipgloss.Top, content)
	}
	return tea.NewView(content)
}

// placeholderView renders a centered single-line message inside the
// widget's cell rectangle so the host's border keeps its size.
func (m Model) placeholderView(base lipgloss.Style, text string) string {
	if m.cols <= 0 || m.rows <= 0 {
		return base.Render(text)
	}
	return base.
		Width(m.cols).
		Height(m.rows).
		Align(lipgloss.Center, lipgloss.Center).
		Render(text)
}
