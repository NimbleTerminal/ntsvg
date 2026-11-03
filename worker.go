package ntsvg

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"runtime"
	"slices"
	"sync"
	"time"

	"nimbleterminal.dev/ntmediabox"
)

// DefaultWorkerTimeout bounds a single SVG parse or rasterize operation in a
// confined worker when the caller does not supply a shorter context deadline.
const DefaultWorkerTimeout = 15 * time.Second

// WorkerConfig configures an OS-confined ntmediabox worker process for SVG
// parsing and rasterization.
type WorkerConfig struct {
	// WorkerPath is the worker executable path. When empty, os.Executable() is used.
	WorkerPath string
	// WorkerArgs are arguments passed to WorkerPath to enter ServeWorker mode.
	WorkerArgs []string
	// StagePath is the Linux Landlock + seccomp stage binary. When empty on Linux,
	// WorkerRendererFactory checks for "ntconfine-stage" beside os.Executable() and
	// falls back to WorkerPath when StageArgs is non-empty.
	StagePath string
	// StageArgs are optional prefix arguments passed to StagePath before
	// ntmediabox.StageArg.
	StageArgs []string
	// AllowUnsandboxed permits running the worker in an isolated child process
	// without OS kernel confinement when Probe().Available is false.
	AllowUnsandboxed bool
	// Timeout bounds each open or render operation (0 uses DefaultWorkerTimeout).
	Timeout time.Duration
	// Limits bounds file size, element count, edge length, and pixel count.
	Limits Limits
	// Start overrides process startup (used by tests or custom transports).
	Start func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error)
}

type workerSVGRenderer struct {
	mu              sync.Mutex
	client          *ntmediabox.RenderClient
	closeFn         func() error
	vbW, vbH        float64
	maxRenderPixels int
	maxRenderEdge   int
	timeout         time.Duration
	closed          bool
}

// WorkerRendererFactory returns a RendererFactory that parses and rasterizes
// each SVG inside a separate OS-confined ntmediabox worker process.
func WorkerRendererFactory(cfg WorkerConfig) RendererFactory {
	cfg.Limits.applyDefaults()
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultWorkerTimeout
	}
	return func(name string, data []byte) (Renderer, error) {
		if len(data) == 0 {
			return nil, fmt.Errorf("parse %q: empty svg data", name)
		}
		if cfg.Limits.MaxFileBytes > 0 && int64(len(data)) > cfg.Limits.MaxFileBytes {
			return nil, fmt.Errorf("parse %q: %d bytes exceeds limit %d", name, len(data), cfg.Limits.MaxFileBytes)
		}
		if len(data) > ntmediabox.MaxRenderDataBytes {
			return nil, fmt.Errorf("parse %q: %d bytes exceeds wire limit %d", name, len(data), ntmediabox.MaxRenderDataBytes)
		}

		startFn := cfg.Start
		if startFn == nil {
			startFn = defaultStartSVGWorker
		}
		conn, closeFn, err := startFn(cfg)
		if err != nil {
			return nil, err
		}
		var closeOnce sync.Once
		killAndClose := func() error {
			var cerr error
			closeOnce.Do(func() {
				_ = conn.Close()
				if closeFn != nil {
					cerr = closeFn()
				}
			})
			return cerr
		}
		client := ntmediabox.NewRenderClientWithKill(conn, func() {
			_ = killAndClose()
		})

		maxPixels := cfg.Limits.MaxRenderPixels
		if maxPixels <= 0 || maxPixels > ntmediabox.MaxRenderPixels {
			maxPixels = ntmediabox.MaxRenderPixels
		}
		maxEdge := cfg.Limits.MaxRenderEdge
		if maxEdge <= 0 || maxEdge > ntmediabox.MaxRenderSide {
			maxEdge = ntmediabox.MaxRenderSide
		}

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		resp, err := client.Do(ctx, ntmediabox.RenderRequest{
			Kind:      "svg-open",
			Name:      name,
			Width:     maxEdge,
			Height:    maxEdge,
			MaxPixels: maxPixels,
			Data:      data,
		})
		if err != nil {
			_ = client.Close()
			_ = killAndClose()
			return nil, err
		}
		return &workerSVGRenderer{
			client:          client,
			closeFn:         killAndClose,
			vbW:             resp.Width,
			vbH:             resp.Height,
			maxRenderPixels: maxPixels,
			maxRenderEdge:   maxEdge,
			timeout:         timeout,
		}, nil
	}
}

func defaultStartSVGWorker(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error) {
	workerPath := cfg.WorkerPath
	if workerPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, nil, err
		}
		workerPath = exe
	}
	resolvedWorker, err := ntmediabox.VerifyBinary(workerPath)
	if err != nil {
		return nil, nil, err
	}
	stagePath := cfg.StagePath
	stageArgs := slices.Clone(cfg.StageArgs)
	if runtime.GOOS == "linux" && stagePath == "" {
		if sib, sibErr := ntmediabox.ResolveSibling(ntmediabox.DefaultStageBinary); sibErr == nil {
			stagePath = sib
		} else if len(stageArgs) > 0 {
			stagePath = resolvedWorker
		}
	}
	execPaths := append(slices.Clone(ntmediabox.DefaultExecPaths()), resolvedWorker)
	h, err := ntmediabox.StartHelper(ntmediabox.HelperConfig{
		WorkerPath: resolvedWorker,
		WorkerArgs: slices.Clone(cfg.WorkerArgs),
		StagePath:  stagePath,
		StageArgs:  stageArgs,
		Policy: ntmediabox.Policy{
			ReadPaths:        ntmediabox.DefaultLibPaths(),
			ExecPaths:        execPaths,
			AllowUnsandboxed: cfg.AllowUnsandboxed,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return h.Conn, h.Close, nil
}

func (r *workerSVGRenderer) ViewBox() (w, h float64) { return r.vbW, r.vbH }

func (r *workerSVGRenderer) Render(maxW, maxH int) (image.Image, error) {
	return r.RenderRegion(maxW, maxH, 0, 0, 1, 1)
}

func (r *workerSVGRenderer) RenderRegion(maxW, maxH int, nx, ny, nw, nh float64) (image.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.client == nil {
		return nil, errors.New("svg renderer closed")
	}
	if maxW <= 0 || maxH <= 0 {
		return nil, fmt.Errorf("invalid target dimensions %d×%d", maxW, maxH)
	}
	if r.maxRenderEdge > 0 {
		maxW = min(maxW, r.maxRenderEdge)
		maxH = min(maxH, r.maxRenderEdge)
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	resp, err := r.client.Do(ctx, ntmediabox.RenderRequest{
		Kind:      "svg-region",
		Width:     maxW,
		Height:    maxH,
		MaxPixels: r.maxRenderPixels,
		Region:    [4]float64{nx, ny, nw, nh},
	})
	if err != nil {
		return nil, err
	}
	if resp.Image == nil {
		return nil, errors.New("svg worker returned nil image")
	}
	return resp.Image, nil
}

func (r *workerSVGRenderer) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var err error
	if r.client != nil {
		_ = r.client.Close()
	}
	if r.closeFn != nil {
		err = r.closeFn()
	}
	return err
}

// ServeWorker runs the confined worker side of an SVG renderer over conn using
// DefaultRendererFactoryWithLimits(limits).
func ServeWorker(conn io.ReadWriter, limits Limits) error {
	return ServeWorkerWithFactory(conn, limits, nil)
}

// ServeWorkerWithFactory runs the confined worker side of an SVG renderer over conn.
func ServeWorkerWithFactory(conn io.ReadWriter, limits Limits, factory RendererFactory) error {
	limits.applyDefaults()
	return ntmediabox.ServeRender(conn, &svgWorkerHandler{
		limits:  limits,
		factory: factory,
	})
}

type svgWorkerHandler struct {
	limits  Limits
	factory RendererFactory
	r       Renderer
}

func (h *svgWorkerHandler) Handle(req ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
	switch req.Kind {
	case "svg-open":
		if h.r != nil {
			_ = h.r.Close()
			h.r = nil
		}
		lim := h.limits
		if req.MaxPixels > 0 && (lim.MaxRenderPixels <= 0 || req.MaxPixels < lim.MaxRenderPixels) {
			lim.MaxRenderPixels = req.MaxPixels
		}
		if req.Width > 0 && (lim.MaxRenderEdge <= 0 || req.Width < lim.MaxRenderEdge) {
			lim.MaxRenderEdge = req.Width
		}
		if lim.MaxFileBytes > 0 && int64(len(req.Data)) > lim.MaxFileBytes {
			return ntmediabox.RenderResponse{Ended: true}, fmt.Errorf("svg exceeds %d bytes", lim.MaxFileBytes)
		}
		fn := h.factory
		if fn == nil {
			fn = DefaultRendererFactoryWithLimits(lim)
		}
		r, err := fn(req.Name, req.Data)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		h.r = r
		vbW, vbH := r.ViewBox()
		return ntmediabox.RenderResponse{
			Width:  vbW,
			Height: vbH,
		}, nil

	case "svg-region":
		if h.r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("no SVG open in worker")
		}
		img, err := h.r.RenderRegion(req.Width, req.Height, req.Region[0], req.Region[1], req.Region[2], req.Region[3])
		if err != nil {
			return ntmediabox.RenderResponse{}, err
		}
		rgba := ntmediabox.ToRGBA(img)
		if rgba == nil {
			return ntmediabox.RenderResponse{}, errors.New("empty rendered svg")
		}
		return ntmediabox.RenderResponse{
			Image: rgba,
		}, nil

	case "svg":
		// One-shot parse + rasterize request.
		lim := h.limits
		if req.MaxPixels > 0 && (lim.MaxRenderPixels <= 0 || req.MaxPixels < lim.MaxRenderPixels) {
			lim.MaxRenderPixels = req.MaxPixels
		}
		w, hh := req.Width, req.Height
		if w <= 0 {
			w = DefaultRenderEdge
		}
		if hh <= 0 {
			hh = w
		}
		if lim.MaxRenderEdge <= 0 || max(w, hh) < lim.MaxRenderEdge {
			lim.MaxRenderEdge = max(w, hh)
		}
		fn := h.factory
		if fn == nil {
			fn = DefaultRendererFactoryWithLimits(lim)
		}
		r, err := fn(req.Name, req.Data)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		defer r.Close()
		img, err := r.Render(w, hh)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		vbW, vbH := r.ViewBox()
		return ntmediabox.RenderResponse{
			Width:  vbW,
			Height: vbH,
			Image:  ntmediabox.ToRGBA(img),
			Ended:  true,
		}, nil

	default:
		return ntmediabox.RenderResponse{Ended: true}, fmt.Errorf("unknown svg worker request %q", req.Kind)
	}
}

func (h *svgWorkerHandler) Close() error {
	if h.r != nil {
		err := h.r.Close()
		h.r = nil
		return err
	}
	return nil
}
