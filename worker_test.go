//go:build !js && !wasm

package ntsvg

import (
	"image"
	"io"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"nimbleterminal.dev/ntmediabox"
)

func init() {
	if len(os.Args) >= 2 && os.Args[1] == "--ntsvg-test-worker" {
		conn := ntmediabox.WorkerConn()
		defer conn.Close()
		if err := ServeWorker(conn, Limits{}); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

const sampleWorkerSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 10"><rect width="20" height="10" fill="#ff0000"/></svg>`

func TestWorkerRendererInMemoryPipe(t *testing.T) {
	factory := WorkerRendererFactory(WorkerConfig{
		Start: func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error) {
			a, b := net.Pipe()
			go ServeWorker(b, cfg.Limits)
			return a, func() error { return a.Close() }, nil
		},
	})

	r, err := factory("rect.svg", []byte(sampleWorkerSVG))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	vbW, vbH := r.ViewBox()
	if vbW != 20 || vbH != 10 {
		t.Fatalf("ViewBox() = %v×%v, want 20×10", vbW, vbH)
	}

	img, err := r.Render(40, 40)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 40, 20) {
		t.Fatalf("Render(40,40) bounds = %v, want 40×20", img.Bounds())
	}

	sub, err := r.RenderRegion(20, 20, 0.25, 0.25, 0.5, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if sub.Bounds().Dx() <= 0 || sub.Bounds().Dy() <= 0 {
		t.Fatalf("unexpected region bounds: %v", sub.Bounds())
	}
}

func TestWorkerRendererSandboxedSubprocess(t *testing.T) {
	sup := ntmediabox.Probe()
	if !sup.Available && runtime.GOOS != "windows" {
		t.Skipf("sandbox unavailable: %s", sup.Detail)
	}
	if runtime.GOOS == "linux" {
		if _, err := ntmediabox.ResolveSibling(ntmediabox.DefaultStageBinary); err != nil {
			t.Skip("ntconfine-stage not beside test binary on Linux")
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	factory := WorkerRendererFactory(WorkerConfig{
		WorkerPath:       exe,
		WorkerArgs:       []string{"--ntsvg-test-worker"},
		AllowUnsandboxed: !sup.Available,
		Timeout:          5 * time.Second,
	})
	r, err := factory("sandboxed.svg", []byte(sampleWorkerSVG))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	img, err := r.Render(20, 20)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 20, 10) {
		t.Fatalf("unexpected sandboxed render bounds: %v", img.Bounds())
	}
}

func TestWorkerMetadataRoundTrip(t *testing.T) {
	factory := WorkerRendererFactory(WorkerConfig{
		Limits: Limits{MaxElements: 3},
		Start: func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error) {
			a, b := net.Pipe()
			go func() { defer b.Close(); _ = ServeWorker(b, Limits{}) }()
			return a, func() error { return a.Close() }, nil
		},
	})
	msg := runLoadFromBytes("meta.svg", []byte(`<svg viewBox="2 3 20 10" width="100%" height="24px"><title>Worker</title><desc>Description</desc><rect/></svg>`), 1, factory, Limits{})
	loaded, ok := msg.(svgLoadedMsg)
	if !ok {
		t.Fatalf("load = %#v", msg)
	}
	defer loaded.renderer.Close()
	d := loaded.doc
	if d.Title() != "Worker" || d.rawWidth != "100%" || d.rawHeight != "24px" || d.total != 3 || !d.capped || len(d.descs) != 1 || d.descs[0] != "Description" {
		t.Fatalf("document = %+v", d)
	}
	p := loaded.renderer.(MetadataProvider)
	m := p.Metadata()
	if m.ViewBox != [4]float64{2, 3, 20, 10} {
		t.Fatalf("viewBox = %v", m.ViewBox)
	}
	m.ElementCounts["svg"] = 100
	if p.Metadata().ElementCounts["svg"] != 1 {
		t.Fatal("metadata snapshot aliases worker renderer")
	}
}

func TestWorkerMetadataRequiredWithoutHostFallback(t *testing.T) {
	valid, err := encodeMetadata(testMetadata())
	if err != nil {
		t.Fatal(err)
	}
	for name, meta := range map[string][]byte{"valid": valid, "missing": nil, "malformed": []byte(`{"version":1}`), "unknown version": []byte(`{"version":99}`)} {
		t.Run(name, func(t *testing.T) {
			closed := false
			factory := WorkerRendererFactory(WorkerConfig{Start: func(WorkerConfig) (io.ReadWriteCloser, func() error, error) {
				a, b := net.Pipe()
				go func() {
					defer b.Close()
					_ = ntmediabox.ServeRender(b, ntmediabox.RenderFunc(func(ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
						return ntmediabox.RenderResponse{Width: 20, Height: 10, Meta: meta}, nil
					}))
				}()
				return a, func() error { closed = true; return a.Close() }, nil
			}})
			// A valid source makes fallback observable: invalid worker metadata
			// must fail the load rather than produce a host-side Info summary.
			source := []byte(`<svg><title>Host fallback</title></svg>`)
			if name == "valid" {
				source = []byte("not XML")
			}
			msg := runLoadFromBytes("worker.svg", source, 1, factory, Limits{})
			if name == "valid" {
				loaded, ok := msg.(svgLoadedMsg)
				if !ok || loaded.doc.Title() != "Example" {
					t.Fatalf("load = %#v", msg)
				}
				_ = loaded.renderer.Close()
			} else if _, ok := msg.(svgErrMsg); !ok {
				t.Fatalf("load fell back to host: %#v", msg)
			}
			if !closed {
				t.Fatal("worker not closed")
			}
		})
	}
}

func TestWorkerStartupFailureWithoutHostFallback(t *testing.T) {
	factory := WorkerRendererFactory(WorkerConfig{Start: func(WorkerConfig) (io.ReadWriteCloser, func() error, error) {
		return nil, nil, os.ErrPermission
	}})
	msg := runLoadFromBytes("valid.svg", []byte(`<svg/>`), 1, factory, Limits{})
	if _, ok := msg.(svgErrMsg); !ok {
		t.Fatalf("load fell back to host: %#v", msg)
	}
}
