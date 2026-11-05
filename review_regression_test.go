//go:build !js && !wasm

package ntsvg

import (
	"image"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func pipeWorkerFactory(limits Limits, rendererFactory RendererFactory) RendererFactory {
	return WorkerRendererFactory(WorkerConfig{
		Limits: limits,
		Start: func(WorkerConfig) (io.ReadWriteCloser, func() error, error) {
			a, b := net.Pipe()
			go func() { defer b.Close(); _ = ServeWorkerWithFactory(b, Limits{}, rendererFactory) }()
			return a, func() error { return a.Close() }, nil
		},
	})
}

func TestWorkerKeepsPartialRenderer(t *testing.T) {
	for name, source := range map[string]string{
		"ampersand":  `<svg width="20" height="10"><title>A & B</title><rect width="20" height="10"/></svg>`,
		"mismatched": `<svg width="20" height="10"><g><rect width="20" height="10"/></svg>`,
		"truncated":  `<svg width="20" height="10"><rect width="20" height="10"/>`,
		"null":       "<svg width=\"20\" height=\"10\"><rect width=\"20\" height=\"10\"/>\x00</svg>",
	} {
		t.Run(name, func(t *testing.T) {
			for _, legacy := range []bool{false, true} {
				var factory RendererFactory
				if legacy {
					factory = func(name string, data []byte) (Renderer, error) {
						r, err := DefaultRendererFactory()(name, data)
						if r == nil {
							return nil, err
						}
						return struct{ Renderer }{r}, err
					}
				}
				msg := runLoadFromBytes(name, []byte(source), 1, pipeWorkerFactory(Limits{}, factory), Limits{})
				loaded, ok := msg.(svgLoadedMsg)
				if !ok || loaded.renderer == nil {
					t.Fatalf("legacy=%v: load = %#v", legacy, msg)
				}
				if loaded.rendererErr == nil {
					t.Fatal("lost partial parse diagnostic")
				}
				if _, err := loaded.renderer.Render(32, 32); err != nil {
					t.Fatal(err)
				}
				_ = loaded.renderer.Close()
			}
		})
	}
}

func TestWorkerPixelBudgetKeepsSessionAlive(t *testing.T) {
	const cap = 4 << 20
	r, err := pipeWorkerFactory(Limits{MaxRenderPixels: cap}, nil)("wide.svg", []byte(`<svg viewBox="0 0 16 9"><rect width="16" height="9"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, size := range []int{8192, 32} {
		img, err := r.Render(size, size)
		if err != nil {
			t.Fatalf("Render(%d): %v", size, err)
		}
		b := img.Bounds()
		if b.Dx() > cap/b.Dy() {
			t.Fatalf("over budget image: %v", b)
		}
	}
}

type regionOverride struct {
	Renderer
	render func() (image.Image, error)
}

func (r regionOverride) RenderRegion(int, int, float64, float64, float64, float64) (image.Image, error) {
	return r.render()
}

func TestOversizedWorkerImageIsRecoverable(t *testing.T) {
	calls := 0
	factory := func(name string, data []byte) (Renderer, error) {
		r, err := DefaultRendererFactory()(name, data)
		return regionOverride{r, func() (image.Image, error) {
			calls++
			if calls == 1 {
				return image.NewRGBA(image.Rect(0, 0, 8, 8)), nil
			}
			return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
		}}, err
	}
	r, err := pipeWorkerFactory(Limits{MaxRenderPixels: 4}, factory)("custom.svg", []byte(`<svg width="20" height="10"/>`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Render(32, 32); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("expected application budget error, got %v", err)
	}
	if _, err := r.Render(32, 32); err != nil {
		t.Fatalf("session lost after application error: %v", err)
	}
}

func TestWorkerCloseCancelsRender(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	factory := func(name string, data []byte) (Renderer, error) {
		r, err := DefaultRendererFactory()(name, data)
		return regionOverride{r, func() (image.Image, error) {
			close(started)
			<-release
			return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
		}}, err
	}
	r, err := pipeWorkerFactory(Limits{}, factory)("hung.svg", []byte(`<svg width="20" height="10"/>`))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := r.Render(32, 32); done <- err }()
	<-started
	closed := make(chan error, 1)
	go func() { closed <- r.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close blocked behind render RPC")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed render succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("render was not cancelled")
	}
}

func TestWorkerBoundsExtractedMetadata(t *testing.T) {
	for name, source := range map[string]string{
		"nonfinite": `<svg viewBox="0 0 NaN 10" width="20" height="10"><rect width="20" height="10"/></svg>`,
		"long size": `<svg viewBox="0 0 20 10" width="` + strings.Repeat("x", maxMetadataSizeBytes+1) + `"><rect width="20" height="10"/></svg>`,
		"long name": `<svg viewBox="0 0 20 10"><` + strings.Repeat("a", maxMetadataNameBytes+1) + `/><rect width="20" height="10"/></svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			msg := runLoadFromBytes(name, []byte(source), 1, pipeWorkerFactory(Limits{}, nil), Limits{})
			loaded, ok := msg.(svgLoadedMsg)
			if !ok || loaded.renderer == nil {
				t.Fatalf("load = %#v", msg)
			}
			defer loaded.renderer.Close()
			if _, err := loaded.renderer.Render(32, 32); err != nil {
				t.Fatal(err)
			}
		})
	}
}
