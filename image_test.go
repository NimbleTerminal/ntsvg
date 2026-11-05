package ntsvg

import (
	"image"
	"image/color"
	"testing"
)

func TestRendererImageAllowlist(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	for _, enabled := range []bool{false, true} {
		var images map[string]image.Image
		if enabled {
			images = map[string]image.Image{"asset": src}
		}
		for _, source := range []string{
			`<svg width="10" height="10"><image href="asset" width="10" height="10"/></svg>`,
			`<s:svg xmlns:s="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="10" height="10" viewBox="0 0 NaN 10"><s:image xlink:href="asset" width="100%" height="100%"/></s:svg>`,
		} {
			r, err := DefaultRendererFactoryWithImages(Limits{}, images)("test", []byte(source))
			if err != nil {
				t.Fatal(err)
			}
			img, err := r.Render(10, 10)
			r.Close()
			if err != nil {
				t.Fatal(err)
			}
			red, _, _, alpha := img.At(5, 5).RGBA()
			if enabled && (red != 65535 || alpha != 65535) || !enabled && alpha != 0 {
				t.Fatalf("enabled=%v: %d %d", enabled, red, alpha)
			}
		}
	}
}
