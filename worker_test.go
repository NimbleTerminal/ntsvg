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
