// render.go: the Renderer abstraction and the default oksvg/rasterx
// rasterizer.
//
// The widget owns a per-document Renderer whose lifetime is tied to the
// loaded SVG — SetSVG closes the previous one and opens a new one via
// the configured RendererFactory. The default backend (oksvg + rasterx)
// is pure Go with no system dependencies, so RasterMode works on every
// build target, including GOOS=js and GOOS=wasip1.

package ntsvg

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"sync"

	"github.com/NimbleMarkets/oksvg" // fork of github.com/srwiley/oksvg
	"github.com/srwiley/rasterx"
)

// Renderer rasterizes a single parsed SVG document. Implementations are
// constructed via a RendererFactory (which parses the document) and
// released via Close.
type Renderer interface {
	// Render rasterizes the SVG into an RGBA bitmap that fits within
	// maxW × maxH pixels while preserving the document's aspect ratio.
	// The renderer may return a smaller image when its own resource
	// caps (pixel budget, edge length) bind first. Equivalent to
	// RenderRegion over the full 0, 0, 1, 1 viewBox rectangle.
	Render(maxW, maxH int) (image.Image, error)

	// RenderRegion rasterizes only the sub-rectangle of the document
	// given in normalized [0, 1] viewBox coordinates — top-left
	// (nx, ny), size (nw, nh) — into a bitmap fitted to maxW × maxH.
	// Re-rasterizing the region (rather than upscaling a crop of a
	// fixed bitmap) keeps zoomed-in views vector-sharp.
	RenderRegion(maxW, maxH int, nx, ny, nw, nh float64) (image.Image, error)

	// ViewBox reports the document's intrinsic dimensions in user
	// units (its viewBox, or width/height attributes, or the SVG
	// spec's 300 × 150 default when the document declares neither).
	ViewBox() (w, h float64)

	// Close releases any resources held by the renderer. Safe to call
	// more than once.
	Close() error
}

// RendererFactory parses an SVG from raw bytes and returns a Renderer
// bound to it. name is a human-readable label (typically the filename
// or "embedded"), used for diagnostics; it is not parsed.
//
// The widget invokes the factory on every SetSVG / SetSVGData call. For
// path-based loads the widget reads the file once (subject to
// Limits.MaxFileBytes) and passes the bytes here, so factories never
// have to do their own I/O.
type RendererFactory func(name string, data []byte) (Renderer, error)

// DefaultRendererFactory returns a RendererFactory backed by
// oksvg/rasterx with default Limits applied. Pure Go, no CGO, no system
// dependencies — works on native, WASM, and WASI builds alike.
func DefaultRendererFactory() RendererFactory {
	return DefaultRendererFactoryWithLimits(Limits{})
}

// DefaultRendererFactoryWithLimits is the same factory parameterised by
// resource caps. MaxRenderPixels and MaxRenderEdge are enforced inside
// the renderer (the only layer that knows the document's aspect ratio);
// MaxFileBytes is enforced upstream of the factory by the loader.
func DefaultRendererFactoryWithLimits(limits Limits) RendererFactory {
	return DefaultRendererFactoryWithImages(limits, nil)
}

// DefaultRendererFactoryWithImages adds a host-supplied allowlist of decoded
// raster images, keyed by SVG image href. No resources are fetched or decoded.
// Images must remain immutable for the lifetime of the returned renderers.
func DefaultRendererFactoryWithImages(limits Limits, images map[string]image.Image) RendererFactory {
	limits.applyDefaults()
	return func(name string, data []byte) (Renderer, error) {
		if len(data) == 0 {
			return nil, fmt.Errorf("parse %q: empty svg data", name)
		}
		icon, err := parseIconWithImages(data, images)
		if icon == nil {
			return nil, fmt.Errorf("parse %q: %w", name, err)
		}
		vbW, vbH := icon.ViewBox.W, icon.ViewBox.H
		if math.IsNaN(vbW) || math.IsNaN(vbH) || math.IsInf(vbW, 0) || math.IsInf(vbH, 0) || vbW <= 0 || vbH <= 0 {
			// SVG spec default when a document declares neither a
			// viewBox nor width/height.
			vbW, vbH = 300, 150
		}
		r := &oksvgRenderer{
			icon:      icon,
			vbW:       vbW,
			vbH:       vbH,
			maxPixels: limits.MaxRenderPixels,
			maxEdge:   limits.MaxRenderEdge,
		}
		if err != nil {
			return r, fmt.Errorf("parse %q: %w", name, err)
		}
		return r, nil
	}
}

// parseIcon wraps oksvg.ReadIconStream with a panic recover. The oksvg
// parser is robust for well-formed input but a hostile or truncated
// document can trip an unguarded slice index deep in the path parser;
// turning that into an error keeps the load goroutine alive.
func parseIcon(data []byte) (*oksvg.SvgIcon, error) {
	return parseIconWithImages(data, nil)
}

func parseIconWithImages(data []byte, images map[string]image.Image) (icon *oksvg.SvgIcon, err error) {
	defer func() {
		if r := recover(); r != nil {
			icon, err = nil, fmt.Errorf("svg parser panic: %v", r)
		}
	}()
	// IgnoreErrorMode: skip elements oksvg doesn't understand rather
	// than failing the whole parse — a viewer should render what it
	// can of a partially-supported document.
	return oksvg.ReadIconStreamWithImages(bytes.NewReader(data), images, oksvg.IgnoreErrorMode)
}

// oksvgRenderer rasterizes one parsed SVG icon. The icon is immutable
// after parse except for SetTarget, which Render mutates under mu so
// concurrent Render calls (e.g. an in-flight render racing a resize-
// triggered one) can't corrupt the shared transform.
type oksvgRenderer struct {
	mu        sync.Mutex
	icon      *oksvg.SvgIcon
	vbW, vbH  float64
	maxPixels int
	maxEdge   int
}

// ViewBox reports the parsed document's intrinsic dimensions.
func (r *oksvgRenderer) ViewBox() (w, h float64) { return r.vbW, r.vbH }

// Render rasterizes the whole icon into a bitmap fitted to maxW × maxH
// and clamped by the renderer's pixel / edge budget. It is exactly
// RenderRegion over the full 0, 0, 1, 1 viewBox rectangle.
func (r *oksvgRenderer) Render(maxW, maxH int) (image.Image, error) {
	return r.RenderRegion(maxW, maxH, 0, 0, 1, 1)
}

// RenderRegion rasterizes the sub-rectangle of the icon given in
// normalized [0, 1] viewBox coords into a bitmap fitted to maxW × maxH
// and clamped by the renderer's pixel / edge budget.
func (r *oksvgRenderer) RenderRegion(maxW, maxH int, nx, ny, nw, nh float64) (img image.Image, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.icon == nil {
		return nil, errors.New("svg renderer closed")
	}
	if math.IsNaN(nx) || math.IsNaN(ny) || math.IsNaN(nw) || math.IsNaN(nh) ||
		math.IsInf(nx, 0) || math.IsInf(ny, 0) || math.IsInf(nw, 0) || math.IsInf(nh, 0) {
		return nil, errors.New("invalid region coordinates: NaN or Inf")
	}
	if nw < 1e-6 {
		nw = 1e-6
	} else if nw > 1.0 {
		nw = 1.0
	}
	if nh < 1e-6 {
		nh = 1e-6
	} else if nh > 1.0 {
		nh = 1.0
	}
	if nx < -2.0 {
		nx = -2.0
	} else if nx > 2.0 {
		nx = 2.0
	}
	if ny < -2.0 {
		ny = -2.0
	} else if ny > 2.0 {
		ny = 2.0
	}
	// The region carries the document's aspect ratio (the viewport is a
	// square fraction of an aspect-correct bitmap), so it fits into the
	// budget exactly as a full render would.
	w, h := fitDims(r.vbW, r.vbH, maxW, maxH, r.maxEdge, r.maxPixels)

	defer func() {
		if rec := recover(); rec != nil {
			img, err = nil, fmt.Errorf("svg rasterizer panic: %v", rec)
		}
	}()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, dst, dst.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	// Map the whole viewBox onto a (w/nw) × (h/nh) rect, shifted so the
	// requested sub-region's top-left lands on the bitmap origin;
	// rasterx clips everything outside 0..w / 0..h for free.
	fullW := float64(w) / nw
	fullH := float64(h) / nh
	r.icon.SetTarget(-nx*fullW, -ny*fullH, fullW, fullH)
	r.icon.DrawTarget = dst
	defer func() { r.icon.DrawTarget = nil }()
	r.icon.Draw(raster, 1.0)
	return dst, nil
}

// Close drops the parsed icon so subsequent Render calls fail cleanly.
func (r *oksvgRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.icon = nil
	return nil
}

// fitDims projects an SVG of intrinsic size (vbW × vbH) onto the
// largest integer pixel rectangle that (a) preserves the aspect ratio,
// (b) fits inside maxW × maxH, (c) keeps either edge ≤ maxEdge, and
// (d) keeps the total pixel count ≤ maxPixels. maxEdge / maxPixels ≤ 0
// disable that clamp. The result is always at least 1 × 1.
func fitDims(vbW, vbH float64, maxW, maxH, maxEdge, maxPixels int) (int, int) {
	if math.IsNaN(vbW) || math.IsNaN(vbH) || math.IsInf(vbW, 0) || math.IsInf(vbH, 0) || vbW <= 0 || vbH <= 0 {
		vbW, vbH = 300, 150
	}
	if maxW < 1 {
		maxW = 1
	}
	if maxH < 1 {
		maxH = 1
	}
	aspect := vbW / vbH
	w := float64(maxW)
	h := w / aspect
	if h > float64(maxH) {
		h = float64(maxH)
		w = h * aspect
	}
	if maxEdge > 0 {
		if w > float64(maxEdge) {
			w, h = float64(maxEdge), float64(maxEdge)/aspect
		}
		if h > float64(maxEdge) {
			w, h = float64(maxEdge)*aspect, float64(maxEdge)
		}
	}
	if maxPixels > 0 && w*h > float64(maxPixels) {
		scale := math.Sqrt(float64(maxPixels) / (w * h))
		w *= scale
		h *= scale
	}
	iw, ih := int(w+0.5), int(h+0.5)
	if iw < 1 {
		iw = 1
	}
	if ih < 1 {
		ih = 1
	}
	return iw, ih
}
