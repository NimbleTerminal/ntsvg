package ntsvg

import (
	"image"
	"strings"
	"testing"
)

// TestImageAccessor verifies Image() exposes the rasterized bitmap so
// hosts can cache it (and re-install it later via SetImage).
func TestImageAccessor(t *testing.T) {
	m := New(80, 24)
	if m.Image() != nil {
		t.Error("Image() should be nil before any load")
	}
	m = loadData(t, m, "fixture.svg", sampleSVG)
	m = rasterize(t, m)
	if m.Image() == nil {
		t.Fatal("Image() nil after rasterize")
	}
	if m.Image() != m.sourceImage {
		t.Error("Image() should return the sourceImage bitmap")
	}
}

// TestSetImageSupersedesPendingRender verifies that installing a
// caller-supplied bitmap cancels an in-flight rasterize: a stale
// renderedMsg arriving afterward must not clobber the installed image.
func TestSetImageSupersedesPendingRender(t *testing.T) {
	m := loadData(t, New(80, 24), "fixture.svg", sampleSVG)
	renderCmd := m.Reload()
	if renderCmd == nil {
		t.Fatal("Reload returned nil Cmd")
	}
	myImg := image.NewRGBA(image.Rect(0, 0, 7, 7))
	m.SetImage(myImg)
	m, _ = m.Update(renderCmd()) // deliver the now-stale render result
	if m.Image() != image.Image(myImg) {
		t.Error("stale render clobbered the SetImage bitmap")
	}
}

// TestSetImageSupersedesPendingLoad verifies that installing a
// caller-supplied bitmap cancels an in-flight load: a stale svgLoadedMsg
// arriving afterward must be dropped rather than mutating the document.
func TestSetImageSupersedesPendingLoad(t *testing.T) {
	m := New(80, 24)
	loadCmd := m.SetSVGData("fixture.svg", sampleSVG)
	if loadCmd == nil {
		t.Fatal("SetSVGData returned nil Cmd")
	}
	m.SetImage(image.NewRGBA(image.Rect(0, 0, 7, 7)))
	m, _ = m.Update(loadCmd()) // deliver the now-stale load result
	if m.Document() != nil {
		t.Error("stale load was accepted after SetImage superseded it")
	}
}

// TestViewShowsSetImageWithoutDocument verifies that a bitmap installed
// via SetImage is displayed even when no document is loaded (doc == nil
// while another entry's load is mid-flight). The bitmap must win over
// the "Loading" placeholder.
func TestViewShowsSetImageWithoutDocument(t *testing.T) {
	m := loadData(t, New(80, 24), "first.svg", sampleSVG)
	m.SetSVGData("second.svg", sampleSVG) // resetForLoad clears doc
	if m.Document() != nil {
		t.Fatal("precondition: Document() should be nil after SetSVGData")
	}
	m.SetImage(image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if got := m.View().Content; strings.Contains(got, "Loading") {
		t.Errorf("View showed the Loading placeholder despite a SetImage bitmap")
	}
}
