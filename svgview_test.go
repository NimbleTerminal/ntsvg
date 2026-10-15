package ntsvg

import (
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// sampleSVG is a small well-formed document exercising most element
// kinds the metadata scanner reports.
var sampleSVG = []byte(`<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100" viewBox="0 0 200 100">
  <title>unit fixture</title>
  <desc>fixture description</desc>
  <rect x="0" y="0" width="200" height="100" fill="#222"/>
  <circle cx="50" cy="50" r="20" fill="red"/>
  <circle cx="120" cy="50" r="20" fill="blue"/>
  <line x1="0" y1="0" x2="200" y2="100" stroke="white"/>
</svg>`)

// driveCmd recursively executes a Bubble Tea command tree against m.Update.
func driveCmd(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	msg := cmd()
	if msg == nil {
		return m
	}
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice {
		for i := 0; i < v.Len(); i++ {
			if sub, ok := v.Index(i).Interface().(tea.Cmd); ok {
				m = driveCmd(m, sub)
			}
		}
		return m
	}
	var next tea.Cmd
	m, next = m.Update(msg)
	return driveCmd(m, next)
}

// loadData runs SetSVGData synchronously and feeds the resulting message
// back through Update, returning the post-load Model.
func loadData(t *testing.T, m Model, name string, data []byte) Model {
	t.Helper()
	cmd := m.SetSVGData(name, data)
	if cmd == nil {
		t.Fatal("SetSVGData returned nil Cmd")
	}
	m, _ = m.Update(cmd())
	return m
}

// rasterize runs Reload synchronously, feeding the renderedMsg and
// picture preparation commands back so m.sourceImage and the picture
// front are populated.
func rasterize(t *testing.T, m Model) Model {
	t.Helper()
	cmd := m.Reload()
	if cmd == nil {
		t.Fatal("Reload returned nil Cmd")
	}
	return driveCmd(m, cmd)
}

func TestLoadSampleData(t *testing.T) {
	m := loadData(t, New(80, 24), "fixture.svg", sampleSVG)

	if m.Err() != nil {
		t.Fatalf("unexpected load error: %v", m.Err())
	}
	if got := m.Name(); got != "fixture.svg" {
		t.Errorf("Name() = %q, want fixture.svg", got)
	}
	if !m.HasRenderer() {
		t.Errorf("HasRenderer() = false, want true (default oksvg factory)")
	}
	w, h := m.ViewBox()
	if w != 200 || h != 100 {
		t.Errorf("ViewBox() = %g×%g, want 200×100", w, h)
	}
	if got := m.Title(); got != "unit fixture" {
		t.Errorf("Title() = %q, want %q", got, "unit fixture")
	}
	doc := m.Document()
	if doc == nil {
		t.Fatal("Document() = nil after load")
	}
	if n := doc.ElementCount("circle"); n != 2 {
		t.Errorf("circle count = %d, want 2", n)
	}
	if n := doc.ElementCount("rect"); n != 1 {
		t.Errorf("rect count = %d, want 1", n)
	}
}

func TestEmptyDataIsError(t *testing.T) {
	m := loadData(t, New(40, 10), "empty.svg", []byte{})
	if m.Err() == nil {
		t.Fatal("expected error loading empty data, got nil")
	}
	if m.Document() != nil {
		t.Error("Document() should be nil after a failed load")
	}
}

func TestBrokenSVGIsError(t *testing.T) {
	m := loadData(t, New(40, 10), "broken.svg", []byte("not xml { ["))
	if m.Err() == nil {
		t.Fatal("expected error loading non-SVG data, got nil")
	}
}

func TestMalformedSVGGenerous(t *testing.T) {
	// A document with a valid root <svg> but malformed elements inside/after.
	malformedSVG := []byte(`<?xml version="1.0"?>
<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100" viewBox="0 0 200 100">
  <rect x="0" y="0" width="200" height="100" fill="#222"/>
  <circle cx="50" cy="50" r="20" fill="red"/>
  <invalid-tag-syntax
</svg>`)

	m := loadData(t, New(80, 24), "malformed.svg", malformedSVG)

	// Since it's generous, the load succeeds (m.Document() is non-nil), but
	// there is a rendererErr/error returned.
	if m.Document() == nil {
		t.Fatal("expected non-nil Document for malformed SVG with valid root")
	}
	if m.RendererErr() == nil {
		t.Fatal("expected non-nil RendererErr reporting the syntax/parsing error")
	}
	if !m.HasRenderer() {
		t.Fatal("expected to have a renderer constructed anyway")
	}

	m = rasterize(t, m)
	if m.sourceImage == nil {
		t.Fatal("expected to be able to render/rasterize the partial SVG")
	}
}

func TestRasterizeProducesImage(t *testing.T) {
	m := loadData(t, New(80, 24), "fixture.svg", sampleSVG)
	m = rasterize(t, m)
	if m.Err() != nil {
		t.Fatalf("unexpected render error: %v", m.Err())
	}
	if m.sourceImage == nil {
		t.Fatal("sourceImage nil after rasterize")
	}
	b := m.sourceImage.Bounds()
	// 200×100 viewBox → aspect 2:1; the longer edge should hit the
	// render edge default.
	if b.Dx() != b.Dy()*2 {
		t.Errorf("rasterized bounds %v do not preserve 2:1 aspect", b)
	}
}

func TestToggleMode(t *testing.T) {
	m := New(80, 24)
	if m.Mode() != RasterMode {
		t.Fatalf("default mode = %v, want RasterMode", m.Mode())
	}
	m.ToggleMode()
	if m.Mode() != InfoMode {
		t.Errorf("after toggle, mode = %v, want InfoMode", m.Mode())
	}
	m.ToggleMode()
	if m.Mode() != RasterMode {
		t.Errorf("after second toggle, mode = %v, want RasterMode", m.Mode())
	}
}

func TestZoomPan(t *testing.T) {
	m := loadData(t, New(80, 24), "fixture.svg", sampleSVG)
	m = rasterize(t, m)

	if m.Zoom() != 0 {
		t.Fatalf("initial zoom = %d, want 0", m.Zoom())
	}
	// Pan at zoom 0 is a no-op.
	if cmd := m.PanRight(); cmd != nil {
		t.Error("PanRight at zoom 0 should return nil Cmd")
	}
	for i := 0; i < 3; i++ {
		m.ZoomIn()
	}
	if m.Zoom() != 3 {
		t.Errorf("zoom after 3 ZoomIn = %d, want 3", m.Zoom())
	}
	// Panning while zoomed must keep the viewport inside [0,1].
	for i := 0; i < 20; i++ {
		m.PanRight()
		m.PanDown()
	}
	vp := m.viewportSize()
	if m.panX < 0 || m.panX+vp > 1.0001 || m.panY < 0 || m.panY+vp > 1.0001 {
		t.Errorf("viewport escaped bounds: panX=%g panY=%g vp=%g", m.panX, m.panY, vp)
	}
	m.ResetView()
	if m.Zoom() != 0 || m.panX != 0 || m.panY != 0 {
		t.Errorf("ResetView left zoom=%d pan=(%g,%g)", m.Zoom(), m.panX, m.panY)
	}
}

func TestZoomCapped(t *testing.T) {
	m := loadData(t, New(80, 24), "fixture.svg", sampleSVG)
	m = rasterize(t, m)
	for i := 0; i < 50; i++ {
		m.ZoomIn()
	}
	if m.Zoom() != zoomMax {
		t.Errorf("zoom = %d after many ZoomIn, want cap %d", m.Zoom(), zoomMax)
	}
}

func TestStaleLoadDropped(t *testing.T) {
	m := New(80, 24)
	// Build an old load Cmd, then supersede it before delivering.
	old := m.SetSVGData("old.svg", sampleSVG)
	m.SetSVGData("new.svg", sampleSVG) // bumps loadGen
	m, _ = m.Update(old())
	if m.Name() != "new.svg" {
		t.Errorf("stale load was accepted: Name() = %q", m.Name())
	}
}

func TestViewPlaceholders(t *testing.T) {
	m := New(40, 8)
	if got := m.View().Content; got == "" {
		t.Error("View() with no document produced empty content")
	}
	m = loadData(t, New(40, 8), "fixture.svg", sampleSVG)
	// In RasterMode after load but before rasterization completes, View()
	// must stay on the Loading placeholder rather than flashing InfoMode text.
	if got := m.View().Content; !strings.Contains(got, "Loading") || strings.Contains(got, "elements:") {
		t.Errorf("RasterMode pre-render View() flashed info text: %q", got)
	}
	m.ToggleMode() // InfoMode renders without a raster
	if got := m.View().Content; got == "" {
		t.Error("InfoMode View() produced empty content")
	}
}

func TestInfoOverlayOverRaster(t *testing.T) {
	m := loadData(t, New(80, 24), "fixture.svg", sampleSVG)
	m = rasterize(t, m)
	m.ToggleMode()
	if m.Mode() != InfoMode {
		t.Fatalf("Mode() = %v, want InfoMode", m.Mode())
	}
	if m.Image() == nil {
		t.Fatal("ToggleMode into InfoMode should preserve the rasterized Image()")
	}
	view := m.View().Content
	if !strings.Contains(view, "unit fixture") {
		t.Error("InfoMode overlay should include the SVG title")
	}
	if !strings.Contains(view, "▄") {
		t.Error("InfoMode overlay should preserve underlying half-block raster cells")
	}
}

func TestNoInfoFlickerOnTransition(t *testing.T) {
	m := loadData(t, New(80, 24), "first.svg", sampleSVG)
	m = rasterize(t, m)

	// Start loading a second SVG; neither the immediate pre-load frame nor
	// the post-svgLoadedMsg pre-renderedMsg frame may flash InfoMode text
	// or a "Loading…" placeholder while the previous raster is available.
	cmd := m.SetSVGData("second.svg", sampleSVG)
	if got := m.View().Content; strings.Contains(got, "Loading") || strings.Contains(got, "elements:") {
		t.Errorf("in-flight transition flashed placeholder/info text: %q", got)
	}
	m, _ = m.Update(cmd()) // delivers svgLoadedMsg; renderCmd not yet run
	if got := m.View().Content; strings.Contains(got, "Loading") || strings.Contains(got, "elements:") {
		t.Errorf("post-load pre-render transition flashed placeholder/info text: %q", got)
	}
}

func TestFitDims(t *testing.T) {
	// 2:1 aspect, generous max — should fill width.
	w, h := fitDims(200, 100, 1000, 1000, 0, 0)
	if w != 1000 || h != 500 {
		t.Errorf("fitDims wide = %d×%d, want 1000×500", w, h)
	}
	// Pixel budget clamps area.
	w, h = fitDims(100, 100, 10000, 10000, 0, 10000)
	if w*h > 10000 {
		t.Errorf("fitDims ignored pixel budget: %d×%d = %d px", w, h, w*h)
	}
	// Edge clamp.
	w, h = fitDims(100, 100, 10000, 10000, 256, 0)
	if w > 256 || h > 256 {
		t.Errorf("fitDims ignored edge cap: %d×%d", w, h)
	}
	// Degenerate input still yields a valid bitmap.
	w, h = fitDims(0, 0, 0, 0, 0, 0)
	if w < 1 || h < 1 {
		t.Errorf("fitDims degenerate = %d×%d, want >= 1×1", w, h)
	}
}

func TestNewWithConfig_FitAndAnchor(t *testing.T) {
	cfg := Config{
		Fit:    FitCover,
		Anchor: AnchorTop,
	}
	m := NewWithConfig(cfg)
	if m.Fit() != FitCover {
		t.Fatalf("expected Fit to be FitCover from Config, got %v", m.Fit())
	}
	if m.Anchor() != AnchorTop {
		t.Fatalf("expected Anchor to be AnchorTop from Config, got %v", m.Anchor())
	}
}

func TestNewWithConfig_FitAndAnchor_PictureConfigPrecedence(t *testing.T) {
	cfg := Config{
		Fit:    FitCover,
		Anchor: AnchorTop,
	}
	cfg.PictureConfig.Fit = FitFill
	cfg.PictureConfig.Anchor = AnchorBottom
	m := NewWithConfig(cfg)
	if m.Fit() != FitFill {
		t.Fatalf("expected Fit to be FitFill from PictureConfig, got %v", m.Fit())
	}
	if m.Anchor() != AnchorBottom {
		t.Fatalf("expected Anchor to be AnchorBottom from PictureConfig, got %v", m.Anchor())
	}
}

func TestNewWithConfig_PictureOptionsRender(t *testing.T) {
	var cfg Config
	cfg.PictureOptions.Render.Fit = FitFill
	cfg.PictureOptions.Render.Anchor = AnchorBottom
	m := NewWithConfig(cfg)
	if m.Fit() != FitFill {
		t.Fatalf("expected FitFill from PictureOptions.Render, got %v", m.Fit())
	}
	if m.Anchor() != AnchorBottom {
		t.Fatalf("expected AnchorBottom from PictureOptions.Render, got %v", m.Anchor())
	}
}

func TestModel_SetFit_Forwards(t *testing.T) {
	m := New(80, 24)
	if m.Fit() != FitContain {
		t.Fatalf("expected initial Fit to be FitContain, got %v", m.Fit())
	}
	_ = m.SetFit(FitCover)
	if m.Fit() != FitCover {
		t.Fatalf("expected Fit to be FitCover after SetFit, got %v", m.Fit())
	}
}

func TestModel_SetAnchor_Forwards(t *testing.T) {
	m := New(80, 24)
	if m.Anchor() != AnchorCenter {
		t.Fatalf("expected initial Anchor to be AnchorCenter, got %v", m.Anchor())
	}
	_ = m.SetAnchor(AnchorTop)
	if m.Anchor() != AnchorTop {
		t.Fatalf("expected Anchor to be AnchorTop after SetAnchor, got %v", m.Anchor())
	}
}

// TestMain keeps `go test` output quiet about the testdata dir absence
// when run from unusual working directories.
func TestMain(m *testing.M) { os.Exit(m.Run()) }
