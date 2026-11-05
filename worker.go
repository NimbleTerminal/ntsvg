package ntsvg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
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
	// Also set Config.Limits to bound host file reads. Negative values remove
	// the application cap, but cannot disable ntmediabox wire maxima or the
	// limits configured by ServeWorker.
	Limits Limits
	// Start overrides process startup (used by tests or custom transports).
	Start func(cfg WorkerConfig) (io.ReadWriteCloser, func() error, error)
}

type workerSVGRenderer struct {
	mu              sync.Mutex
	client          *ntmediabox.RenderClient
	closeFn         func() error
	vbW, vbH        float64
	metadata        Metadata
	maxRenderPixels int
	maxRenderEdge   int
	timeout         time.Duration
	closed          bool
}

// WorkerRendererFactory returns a RendererFactory that parses and rasterizes
// each SVG inside a separate OS-confined ntmediabox worker process, including
// metadata extraction. The returned renderer implements MetadataProvider.
// Startup or metadata errors fail widget loading without host XML fallback.
func WorkerRendererFactory(cfg WorkerConfig) RendererFactory {
	cfg.Limits.applyDefaults()
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultWorkerTimeout
	}
	return func(name string, data []byte) (renderer Renderer, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				err = fmt.Errorf("SVG worker panic: %v", rec)
			}
			if err != nil && renderer == nil {
				err = &confinedLoadError{err}
			}
		}()
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
		opened := false
		defer func() {
			if !opened {
				_ = killAndClose()
			}
		}()
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
		options, _ := json.Marshal(svgOpenOptions{MaxElements: cfg.Limits.MaxElements, MaxFileBytes: cfg.Limits.MaxFileBytes})
		resp, err := client.Do(ctx, ntmediabox.RenderRequest{
			Kind:      "svg-open",
			Meta:      options,
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
		envelope, err := decodeMetadataEnvelope(resp.Meta)
		if err != nil {
			return nil, err
		}
		if resp.Ended || resp.Width <= 0 || resp.Height <= 0 || math.IsNaN(resp.Width) || math.IsNaN(resp.Height) || math.IsInf(resp.Width, 0) || math.IsInf(resp.Height, 0) {
			return nil, errors.New("invalid SVG worker open response")
		}
		var warning error
		if envelope.Warning != "" {
			warning = errors.New(envelope.Warning)
		}
		opened = true
		return &workerSVGRenderer{
			client:          client,
			metadata:        *envelope.Metadata,
			closeFn:         killAndClose,
			vbW:             resp.Width,
			vbH:             resp.Height,
			maxRenderPixels: maxPixels,
			maxRenderEdge:   maxEdge,
			timeout:         timeout,
		}, warning
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

func (r *workerSVGRenderer) Metadata() Metadata { return cloneMetadata(r.metadata) }

func (r *workerSVGRenderer) ViewBox() (w, h float64) { return r.vbW, r.vbH }

func (r *workerSVGRenderer) Render(maxW, maxH int) (image.Image, error) {
	return r.RenderRegion(maxW, maxH, 0, 0, 1, 1)
}

func (r *workerSVGRenderer) RenderRegion(maxW, maxH int, nx, ny, nw, nh float64) (image.Image, error) {
	r.mu.Lock()
	if r.closed || r.client == nil {
		r.mu.Unlock()
		return nil, errors.New("svg renderer closed")
	}
	client := r.client
	r.mu.Unlock()
	if maxW <= 0 || maxH <= 0 {
		return nil, fmt.Errorf("invalid target dimensions %d×%d", maxW, maxH)
	}
	if r.maxRenderEdge > 0 {
		maxW = min(maxW, r.maxRenderEdge)
		maxH = min(maxH, r.maxRenderEdge)
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	resp, err := client.Do(ctx, ntmediabox.RenderRequest{
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
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	client, closeFn := r.client, r.closeFn
	r.mu.Unlock()
	// RenderClient.Close interrupts the transport without waiting for Do's
	// serialization mutex, so disposal can cancel a blocked RPC immediately.
	if client != nil {
		_ = client.Close()
	}
	if closeFn != nil {
		return closeFn()
	}
	return nil
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

type svgOpenOptions struct {
	MaxElements  int   `json:"max_elements"`
	MaxFileBytes int64 `json:"max_file_bytes"`
}

type svgWorkerHandler struct {
	renderLimits Limits
	limits       Limits
	factory      RendererFactory
	r            Renderer
}

func (h *svgWorkerHandler) Handle(req ntmediabox.RenderRequest) (ntmediabox.RenderResponse, error) {
	switch req.Kind {
	case "svg-open":
		if h.r != nil {
			_ = h.r.Close()
			h.r = nil
		}
		lim := h.limits
		if len(req.Meta) > 0 {
			var options svgOpenOptions
			if len(req.Meta) > 1024 || json.Unmarshal(req.Meta, &options) != nil {
				return ntmediabox.RenderResponse{Ended: true}, errors.New("invalid SVG open options")
			}
			if options.MaxFileBytes > 0 && (lim.MaxFileBytes <= 0 || options.MaxFileBytes < lim.MaxFileBytes) {
				lim.MaxFileBytes = options.MaxFileBytes
			}
			if options.MaxElements > 0 && (lim.MaxElements <= 0 || options.MaxElements < lim.MaxElements) {
				lim.MaxElements = options.MaxElements
			}
		}
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
		r, diagnostic := fn(req.Name, req.Data)
		if r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.Join(errors.New("no SVG renderer"), diagnostic)
		}
		h.r = r
		h.renderLimits = lim
		var metadata Metadata
		if provider, ok := r.(MetadataProvider); ok {
			metadata = provider.Metadata()
		} else {
			doc, err := scanMetadata(req.Name, req.Data, lim.MaxElements)
			if doc == nil {
				return ntmediabox.RenderResponse{Ended: true}, err
			}
			diagnostic = errors.Join(diagnostic, err)
			metadata = doc.metadata()
		}
		warning := ""
		if diagnostic != nil {
			warning = diagnostic.Error()
		}
		meta, err := encodeMetadataWithWarning(metadata, warning)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		vbW, vbH := r.ViewBox()
		return ntmediabox.RenderResponse{
			Width:  vbW,
			Height: vbH,
			Meta:   meta,
		}, nil

	case "svg-region":
		if h.r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.New("no SVG open in worker")
		}
		img, err := h.r.RenderRegion(req.Width, req.Height, req.Region[0], req.Region[1], req.Region[2], req.Region[3])
		if err != nil {
			return ntmediabox.RenderResponse{}, err
		}
		rgba, err := checkedWorkerImage(img, req, h.renderLimits)
		if err != nil {
			return ntmediabox.RenderResponse{}, err
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
		if r == nil {
			return ntmediabox.RenderResponse{Ended: true}, errors.Join(errors.New("no SVG renderer"), err)
		}
		defer r.Close()
		img, err := r.Render(w, hh)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		rgba, err := checkedWorkerImage(img, req, lim)
		if err != nil {
			return ntmediabox.RenderResponse{Ended: true}, err
		}
		vbW, vbH := r.ViewBox()
		return ntmediabox.RenderResponse{
			Width:  vbW,
			Height: vbH,
			Image:  rgba,
			Ended:  true,
		}, nil

	default:
		return ntmediabox.RenderResponse{Ended: true}, fmt.Errorf("unknown svg worker request %q", req.Kind)
	}
}

// checkedWorkerImage converts only images within session, request, and wire
// budgets. Return application errors before the transport can kill the session.
func checkedWorkerImage(img image.Image, req ntmediabox.RenderRequest, limits Limits) (*image.RGBA, error) {
	if img == nil {
		return nil, errors.New("empty rendered svg")
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	maxW, maxH, maxPixels := ntmediabox.MaxRenderSide, ntmediabox.MaxRenderSide, ntmediabox.MaxRenderPixels
	if req.Width > 0 {
		maxW = min(maxW, req.Width)
	}
	if req.Height > 0 {
		maxH = min(maxH, req.Height)
	}
	if limits.MaxRenderEdge > 0 {
		maxW, maxH = min(maxW, limits.MaxRenderEdge), min(maxH, limits.MaxRenderEdge)
	}
	if req.MaxPixels > 0 {
		maxPixels = min(maxPixels, req.MaxPixels)
	}
	if limits.MaxRenderPixels > 0 {
		maxPixels = min(maxPixels, limits.MaxRenderPixels)
	}
	if w <= 0 || h <= 0 || w > maxW || h > maxH || w > maxPixels/h {
		return nil, fmt.Errorf("rendered SVG %dx%d exceeds image budget", w, h)
	}
	return ntmediabox.ToRGBA(img), nil
}

func (h *svgWorkerHandler) Close() error {
	if h.r != nil {
		err := h.r.Close()
		h.r = nil
		return err
	}
	return nil
}
