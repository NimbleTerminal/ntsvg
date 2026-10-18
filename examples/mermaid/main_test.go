package main

import (
	"testing"

	"nimbleterminal.dev/ntsvg"
)

// Exercise the integration for every document/palette, including the region
// rendering path used by zoom. Mermaid's raster subpackage is never involved.
func TestSamplesRender(t *testing.T) {
	for i, sample := range samples {
		for j, theme := range themes {
			t.Run(sample.name+"/"+string(theme), func(t *testing.T) {
				data, err := renderSample(i, j)
				if err != nil {
					t.Fatal(err)
				}

				renderer, err := ntsvg.DefaultRendererFactory()(sample.name, data)
				if err != nil {
					t.Fatal(err)
				}
				defer renderer.Close()
				w, h := renderer.ViewBox()
				if w <= 0 || h <= 0 {
					t.Fatalf("invalid viewbox: %v x %v", w, h)
				}
				img, err := renderer.Render(640, 480)
				if err != nil {
					t.Fatal(err)
				}
				if img.Bounds().Empty() {
					t.Fatal("empty render")
				}
				r, g, b, a := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
				want := uint32(0xffff)
				if theme == "dark" {
					want = 0x1e1e
				}
				if r != want || g != want || b != want || a != 0xffff {
					t.Fatalf("incorrect theme background: %x %x %x %x", r, g, b, a)
				}
				// An all-background image would indicate unsupported SVG content.
				colors := make(map[[4]uint32]bool)
				for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
					for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
						r, g, b, a := img.At(x, y).RGBA()
						colors[[4]uint32{r, g, b, a}] = true
					}
				}
				if len(colors) < 3 {
					t.Fatalf("diagram has only %d colors", len(colors))
				}
				if _, err := renderer.RenderRegion(640, 480, .25, .25, .5, .5); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
