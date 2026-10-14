package ntsvg

import (
	"image"
	"testing"
)

// quadSVG is a 100×100 document split into four solid-color quadrants:
// red top-left, green top-right, blue bottom-left, yellow bottom-right.
// Sampling a known quadrant verifies which region was rasterized.
var quadSVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100">
<rect x="0" y="0" width="50" height="50" fill="#ff0000"/>
<rect x="50" y="0" width="50" height="50" fill="#00ff00"/>
<rect x="0" y="50" width="50" height="50" fill="#0000ff"/>
<rect x="50" y="50" width="50" height="50" fill="#ffff00"/>
</svg>`)

// newRenderer builds a default oksvg renderer for data.
func newRenderer(t *testing.T, data []byte) Renderer {
	t.Helper()
	r, err := DefaultRendererFactory()("quad", data)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return r
}

// quadrantAt classifies the pixel at fractional position (fx, fy) of img
// into one of the quadSVG fill colors.
func quadrantAt(t *testing.T, img image.Image, fx, fy float64) string {
	t.Helper()
	b := img.Bounds()
	x := b.Min.X + int(fx*float64(b.Dx()))
	y := b.Min.Y + int(fy*float64(b.Dy()))
	r, g, bl, _ := img.At(x, y).RGBA()
	r8, g8, b8 := int(r>>8), int(g>>8), int(bl>>8)
	hi := func(v int) bool { return v > 200 }
	lo := func(v int) bool { return v < 60 }
	switch {
	case hi(r8) && lo(g8) && lo(b8):
		return "red"
	case lo(r8) && hi(g8) && lo(b8):
		return "green"
	case lo(r8) && lo(g8) && hi(b8):
		return "blue"
	case hi(r8) && hi(g8) && lo(b8):
		return "yellow"
	default:
		return "other"
	}
}

// TestRenderRegionTopLeftQuadrant verifies RenderRegion rasterizes only
// the requested sub-rectangle of the viewBox — here the top-left
// quadrant, which fills the whole bitmap with red.
func TestRenderRegionTopLeftQuadrant(t *testing.T) {
	r := newRenderer(t, quadSVG)
	defer r.Close()
	img, err := r.RenderRegion(200, 200, 0, 0, 0.5, 0.5)
	if err != nil {
		t.Fatalf("RenderRegion: %v", err)
	}
	if got := quadrantAt(t, img, 0.5, 0.5); got != "red" {
		t.Errorf("top-left region center = %s, want red", got)
	}
}

// TestRenderRegionBottomRightQuadrant verifies a non-origin region.
func TestRenderRegionBottomRightQuadrant(t *testing.T) {
	r := newRenderer(t, quadSVG)
	defer r.Close()
	img, err := r.RenderRegion(200, 200, 0.5, 0.5, 0.5, 0.5)
	if err != nil {
		t.Fatalf("RenderRegion: %v", err)
	}
	if got := quadrantAt(t, img, 0.5, 0.5); got != "yellow" {
		t.Errorf("bottom-right region center = %s, want yellow", got)
	}
}

// TestRenderFullShowsFourQuadrants verifies Render (the whole document)
// still produces all four quadrants — i.e. it delegates to RenderRegion
// with the full 0,0,1,1 rect.
func TestRenderFullShowsFourQuadrants(t *testing.T) {
	r := newRenderer(t, quadSVG)
	defer r.Close()
	img, err := r.Render(200, 200)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, c := range []struct {
		fx, fy float64
		want   string
	}{
		{0.25, 0.25, "red"}, {0.75, 0.25, "green"},
		{0.25, 0.75, "blue"}, {0.75, 0.75, "yellow"},
	} {
		if got := quadrantAt(t, img, c.fx, c.fy); got != c.want {
			t.Errorf("full render at (%g,%g) = %s, want %s", c.fx, c.fy, got, c.want)
		}
	}
}

// TestZoomInReRastersRegion verifies that, with a renderer attached,
// ZoomIn re-rasterizes the viewport region (region == true) rather than
// cropping the fixed bitmap.
func TestZoomInReRastersRegion(t *testing.T) {
	m := loadData(t, New(80, 24), "quad.svg", quadSVG)
	m = rasterize(t, m)
	cmd := m.ZoomIn()
	if cmd == nil {
		t.Fatal("ZoomIn returned nil Cmd")
	}
	msg := cmd()
	rm, ok := msg.(renderedMsg)
	if !ok {
		t.Fatalf("ZoomIn cmd produced %T, want renderedMsg", msg)
	}
	if !rm.region {
		t.Error("ZoomIn render should be a region re-render (region == true)")
	}
}

// TestZoomZeroReusesFullBitmap verifies that at zoom 0 the viewer reuses
// the full-document bitmap instead of issuing a fresh rasterize.
func TestZoomZeroReusesFullBitmap(t *testing.T) {
	m := loadData(t, New(80, 24), "quad.svg", quadSVG)
	m = rasterize(t, m)
	cmd := m.applyViewport()
	if cmd == nil {
		return // nothing to display is acceptable
	}
	if _, ok := cmd().(renderedMsg); ok {
		t.Error("zoom 0 should reuse the full bitmap, not re-rasterize")
	}
}

// TestSetImageDetachesRenderer verifies that installing a host bitmap
// detaches any document renderer: with render-on-zoom, a stale renderer
// left attached would re-rasterize the wrong document on the next zoom.
func TestSetImageDetachesRenderer(t *testing.T) {
	m := loadData(t, New(80, 24), "quad.svg", quadSVG)
	if !m.HasRenderer() {
		t.Fatal("precondition: expected a renderer after load")
	}
	m.SetImage(image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if m.HasRenderer() {
		t.Error("SetImage should detach the document renderer")
	}
}
