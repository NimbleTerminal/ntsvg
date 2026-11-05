package ntsvg

import (
	"bytes"
	"fmt"
	"image"
	"testing"
)

var viewBoxFallbackCases = []struct {
	name, attrs string
	want        [4]float64
}{
	{"missing viewport", ``, [4]float64{0, 0, 300, 150}},
	{"relative viewport", `width="100%" height="100%"`, [4]float64{0, 0, 300, 150}},
	{"invalid declared size", `width="bad" height="bad"`, [4]float64{0, 0, 300, 150}},
	{"nan x", `viewBox="NaN 0 100 10"`, [4]float64{0, 0, 100, 10}},
	{"infinite y", `viewBox="0 +Inf 100 10"`, [4]float64{0, 0, 100, 10}},
	{"invalid x preserves y", `viewBox="NaN 2 100 10"`, [4]float64{0, 2, 100, 10}},
	{"nan width uses attributes", `viewBox="0 0 NaN 10" width="100" height="10"`, [4]float64{0, 0, 100, 10}},
	{"infinite height uses attributes", `viewBox="0 0 100 +Inf" width="100" height="10"`, [4]float64{0, 0, 100, 10}},
	{"zero width uses attributes", `viewBox="5 2 0 10" width="100" height="10"`, [4]float64{0, 0, 100, 10}},
	{"invalid dimensions use default", `viewBox="0 0 NaN 10"`, [4]float64{0, 0, 300, 150}},
	{"exponent dimensions", `width="1e2" height="1e1"`, [4]float64{0, 0, 100, 10}},
	{"exponent pixel dimensions", `width="1e2px" height="1e1px"`, [4]float64{0, 0, 100, 10}},
	{"signed exponents", `width="1E+2" height="1e+1"`, [4]float64{0, 0, 100, 10}},
	{"negative exponent", `width="1e2" height="1e-1"`, [4]float64{0, 0, 100, .1}},
	{"invalid viewBox exponent dimensions", `viewBox="0 0 NaN 10" width="1e2" height="10"`, [4]float64{0, 0, 100, 10}},
	{"invalid viewBox exponent pixels", `viewBox="0 0 NaN 10" width="1e2px" height="1e1px"`, [4]float64{0, 0, 100, 10}},
	{"valid viewBox exponents", `viewBox="0 0 1e2 1e1"`, [4]float64{0, 0, 100, 10}},
	{"valid offset", `viewBox="5 2 100 10"`, [4]float64{5, 2, 100, 10}},
}

func checkViewBoxFallbacks(t *testing.T, factory RendererFactory) {
	t.Helper()
	for _, tc := range viewBoxFallbackCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, percentages := range []bool{false, true} {
				t.Run(fmt.Sprintf("percentages=%v", percentages), func(t *testing.T) {
					want := tc.want
					rect := fmt.Sprintf(`<rect x="%g" y="%g" width="%g" height="%g" fill="#f00"/>`, want[0], want[1], want[2], want[3])
					// The inner shape exposes incorrect scale/translation even when a
					// full-viewBox background would cover the output under either transform.
					rect += fmt.Sprintf(`<rect x="%g" y="%g" width="%g" height="%g" fill="#00f"/>`, want[0]+want[2]/4, want[1]+want[3]/4, want[2]/2, want[3]/2)
					geometry := rect
					if percentages {
						geometry = fmt.Sprintf(`<rect x="%g" y="%g" width="100%%" height="100%%" fill="#f00"/><rect x="%g" y="%g" width="50%%" height="50%%" fill="#00f"/>`, want[0], want[1], want[0]+want[2]/4, want[1]+want[3]/4)
					}
					source := []byte(`<svg ` + tc.attrs + `>` + geometry + `</svg>`)
					msg := runLoadFromBytes(tc.name, source, 1, factory, Limits{})
					loaded, ok := msg.(svgLoadedMsg)
					if !ok || loaded.renderer == nil {
						t.Fatalf("load = %#v", msg)
					}
					r := loaded.renderer
					defer r.Close()
					metadata := r.(MetadataProvider).Metadata()
					if metadata.ViewBox != want {
						t.Fatalf("metadata = %v, want %v", metadata.ViewBox, want)
					}
					w, h := r.ViewBox()
					if w != want[2] || h != want[3] {
						t.Fatalf("renderer = %gx%g, want %gx%g", w, h, want[2], want[3])
					}
					if dw, dh := loaded.doc.ViewBox(); dw != w || dh != h {
						t.Fatalf("document = %gx%g, renderer = %gx%g", dw, dh, w, h)
					}
					canonical := fmt.Sprintf(`<svg viewBox="%g %g %g %g">%s</svg>`, want[0], want[1], want[2], want[3], rect)
					reference, err := DefaultRendererFactory()("reference", []byte(canonical))
					if err != nil {
						t.Fatal(err)
					}
					defer reference.Close()
					for _, region := range []bool{false, true} {
						render := func(r Renderer) (image.Image, error) {
							if region {
								return r.RenderRegion(64, 64, .25, .25, .5, .5)
							}
							return r.Render(64, 64)
						}
						got, err := render(r)
						if err != nil {
							t.Fatal(err)
						}
						expected, err := render(reference)
						if err != nil {
							t.Fatal(err)
						}
						gotRGBA, wantRGBA := got.(*image.RGBA), expected.(*image.RGBA)
						if got.Bounds() != expected.Bounds() || !bytes.Equal(gotRGBA.Pix, wantRGBA.Pix) {
							t.Fatalf("region=%v: bitmap %v differs from canonical %v", region, got.Bounds(), expected.Bounds())
						}
						if _, _, _, alpha := got.At(got.Bounds().Dx()/2, got.Bounds().Dy()/2).RGBA(); alpha == 0 {
							t.Fatal("empty bitmap")
						}
					}
				})
			}
		})
	}
}

func TestDefaultRendererViewBoxFallbacks(t *testing.T) {
	checkViewBoxFallbacks(t, DefaultRendererFactory())
}

func TestParseLengthNumbersAndUnits(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  float64
	}{
		{"1e2", 100}, {"1e2px", 100}, {"1E+2pt", 100}, {"1e-1", .1},
		{".5e2", 50}, {"+1.e1", 10}, {"2em", 2}, {"2ex", 2},
		{" 100px ", 100}, {"1e2%", 0}, {"1e", 0}, {"1e+px", 0},
		{"1e999", 0}, {"NaN", 0}, {"Inf", 0}, {"-1e2", 0}, {"", 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := parseLength(tc.input); got != tc.want {
				t.Fatalf("parseLength(%q) = %g, want %g", tc.input, got, tc.want)
			}
		})
	}
}
