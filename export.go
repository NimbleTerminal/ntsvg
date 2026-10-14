// export.go: turning SVG markup — raw bytes or generated Stages — into
// raster images and PNG files, plus the bridge that feeds a Stage into
// the viewer Model.

package ntsvg

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/ntsvg/stage"
)

// RasterizeSVG rasterizes raw SVG bytes into an RGBA image fitted to
// maxW × maxH pixels while preserving aspect ratio. It uses the same
// pure-Go oksvg/rasterx backend as the widget's default renderer, so it
// works on every build target.
func RasterizeSVG(data []byte, maxW, maxH int) (image.Image, error) {
	r, err := DefaultRendererFactory()("export", data)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Render(maxW, maxH)
}

// RasterizeStage rasterizes a generated Stage into an RGBA image fitted
// to maxW × maxH pixels while preserving aspect ratio.
func RasterizeStage(s *stage.Stage, maxW, maxH int) (image.Image, error) {
	if s == nil {
		return nil, errors.New("nil stage")
	}
	return RasterizeSVG(s.Bytes(), maxW, maxH)
}

// WritePNG rasterizes raw SVG bytes and writes them to path as a PNG (0644).
// maxW / maxH cap the output resolution.
func WritePNG(path string, data []byte, maxW, maxH int) error {
	img, err := RasterizeSVG(data, maxW, maxH)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("encode png %q: %w", path, err)
	}
	return nil
}

// WriteStagePNG rasterizes a generated Stage and writes it to path as a
// PNG (0644). maxW / maxH cap the output resolution.
func WriteStagePNG(path string, s *stage.Stage, maxW, maxH int) error {
	if s == nil {
		return errors.New("nil stage")
	}
	return WritePNG(path, s.Bytes(), maxW, maxH)
}

// ShowStage loads a generated Stage into the viewer, exactly as if its
// SVG markup had been opened from disk. The returned Cmd performs the
// metadata scan and rasterize off the UI goroutine. Pair with stage.New
// / stage.NewChart to display generated vector graphics or charts inside
// a Bubble Tea program.
func (m *Model) ShowStage(s *stage.Stage) tea.Cmd {
	if s == nil {
		return m.SetSVGData("stage.svg", nil)
	}
	name := "stage.svg"
	if title := s.Title(); title != "" {
		name = title
	}
	return m.SetSVGData(name, s.Bytes())
}
