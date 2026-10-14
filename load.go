// load.go: SVG loading, lightweight metadata extraction, and the
// async messages the Model consumes.
//
// Loading is split in two: a streaming XML scan collects the document
// summary shown in InfoMode (view box, element histogram, titles), and
// the configured RendererFactory parses the bytes for RasterMode. The
// two are independent — a document whose rasterizer init fails still
// shows its InfoMode summary.

package ntsvg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// maxCapturedText bounds how many runes of a single <title> / <desc>
// element are retained — a hostile document could otherwise inline
// megabytes of text into one title.
const maxCapturedText = 4096

// maxCapturedMeta bounds how many <title> / <desc> elements are
// retained, so a document with millions of them can't grow the slice
// without bound.
const maxCapturedMeta = 16

// Document is the InfoMode summary of a parsed SVG: enough structure to
// describe the file without holding the rasterized pixels. Produced by
// scanMetadata and exposed through Model accessor methods.
type Document struct {
	name string

	viewW, viewH float64 // intrinsic size in user units (see deriveSize)
	rawWidth     string  // root <svg width="…"> verbatim
	rawHeight    string  // root <svg height="…"> verbatim
	viewBox      string  // root <svg viewBox="…"> verbatim

	counts map[string]int // element local-name → occurrence count
	total  int            // total elements counted
	capped bool           // true when the count hit Limits.MaxElements

	titles []string // <title> text content (sanitized at display time)
	descs  []string // <desc> text content
}

// ViewBox reports the document's intrinsic dimensions in user units.
func (d *Document) ViewBox() (w, h float64) { return d.viewW, d.viewH }

// TotalElements reports the number of XML elements counted. When
// CountCapped is true the real total is higher.
func (d *Document) TotalElements() int { return d.total }

// CountCapped reports whether element counting stopped at the
// Limits.MaxElements ceiling.
func (d *Document) CountCapped() bool { return d.capped }

// ElementCount reports how many elements with the given local name
// (e.g. "path", "rect", "g") were counted.
func (d *Document) ElementCount(name string) int { return d.counts[name] }

// Title returns the first <title> element's text, or "" when the
// document declares none. Not sanitized — apply SanitizeForTerminal
// before display.
func (d *Document) Title() string {
	if len(d.titles) == 0 {
		return ""
	}
	return d.titles[0]
}

// HistogramItem represents a single SVG element type and its occurrence count.
type HistogramItem struct {
	Name  string
	Count int
}

// Histogram returns the element counts sorted by descending frequency
// then name, for stable InfoMode rendering.
func (d *Document) Histogram() []HistogramItem {
	out := make([]HistogramItem, 0, len(d.counts))
	for k, v := range d.counts {
		out = append(out, HistogramItem{
			Name:  k,
			Count: v,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// svgLoadedMsg is delivered when a load Cmd completes successfully. doc
// is always non-nil; renderer is non-nil when the RendererFactory
// produced one, and rendererErr carries the factory's error otherwise —
// InfoMode stays usable in that case while RasterMode degrades.
type svgLoadedMsg struct {
	name        string
	doc         *Document
	renderer    Renderer
	rendererErr error
	gen         uint64
}

// svgErrMsg is delivered when a load Cmd fails outright (unreadable
// file, malformed XML — nothing can be shown).
type svgErrMsg struct {
	err error
	gen uint64
}

// renderedMsg is delivered when a Renderer.Render or RenderRegion call
// succeeds. gen and loadGen are checked against the Model's counters in
// Update to drop stale results. region is true for a viewport
// re-render (RenderRegion) — display-only output that must not replace
// the canonical full-document bitmap exposed by Image().
type renderedMsg struct {
	img     image.Image
	gen     uint64
	loadGen uint64
	region  bool
}

// renderErrMsg is delivered when a Renderer.Render call fails.
type renderErrMsg struct {
	err     error
	gen     uint64
	loadGen uint64
}

// loadFromPathCmd reads the file at path (subject to MaxFileBytes) and
// runs the bytes-based loader against the resulting in-memory copy.
func loadFromPathCmd(path string, gen uint64, factory RendererFactory, limits Limits) tea.Cmd {
	return func() tea.Msg {
		if limits.MaxFileBytes > 0 {
			info, err := os.Stat(path)
			if err != nil {
				return svgErrMsg{err: fmt.Errorf("stat %q: %w", path, err), gen: gen}
			}
			if info.Size() > limits.MaxFileBytes {
				return svgErrMsg{
					err: fmt.Errorf("SVG %q is %d bytes, exceeds MaxFileBytes=%d",
						path, info.Size(), limits.MaxFileBytes),
					gen: gen,
				}
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return svgErrMsg{err: fmt.Errorf("read %q: %w", path, err), gen: gen}
		}
		return runLoadFromBytes(path, data, gen, factory, limits)
	}
}

// loadFromBytesCmd loads an in-memory SVG without touching the disk.
func loadFromBytesCmd(name string, data []byte, gen uint64, factory RendererFactory, limits Limits) tea.Cmd {
	return func() tea.Msg { return runLoadFromBytes(name, data, gen, factory, limits) }
}

// runLoadFromBytes is the shared body of both loaders. It scans the SVG
// metadata and asks the factory for a Renderer. A panic anywhere in the
// path is recovered into an svgErrMsg so the load goroutine never
// crashes the program.
func runLoadFromBytes(name string, data []byte, gen uint64, factory RendererFactory, limits Limits) (msg tea.Msg) {
	defer func() {
		if r := recover(); r != nil {
			msg = svgErrMsg{err: fmt.Errorf("panic loading %q: %v", name, r), gen: gen}
		}
	}()

	if limits.MaxFileBytes > 0 && int64(len(data)) > limits.MaxFileBytes {
		return svgErrMsg{
			err: fmt.Errorf("SVG %q is %d bytes, exceeds MaxFileBytes=%d",
				name, len(data), limits.MaxFileBytes),
			gen: gen,
		}
	}
	if len(data) == 0 {
		return svgErrMsg{err: errors.New("empty svg data"), gen: gen}
	}

	doc, scanErr := scanMetadata(name, data, limits.MaxElements)
	if doc == nil {
		return svgErrMsg{err: fmt.Errorf("parse %q: %w", name, scanErr), gen: gen}
	}

	// Renderer-open failure is non-fatal: InfoMode renders fine from
	// doc alone; only RasterMode degrades. The error rides through
	// svgLoadedMsg.rendererErr for hosts to surface.
	var renderer Renderer
	var rendererErr error
	if factory != nil {
		var fErr error
		renderer, fErr = factory(name, data)
		rendererErr = errors.Join(scanErr, fErr)
	} else {
		rendererErr = scanErr
	}
	return svgLoadedMsg{
		name:        name,
		doc:         doc,
		renderer:    renderer,
		rendererErr: rendererErr,
		gen:         gen,
	}
}

// scanMetadata streams the SVG through encoding/xml and collects the
// InfoMode summary. The decoder runs in non-strict mode so minor
// well-formedness slips (a stray unescaped &, say) don't sink the whole
// scan — the rasterizer is the source of truth for renderability.
func scanMetadata(name string, data []byte, maxElements int) (*Document, error) {
	doc := &Document{name: name, counts: make(map[string]int)}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	var capture string // "title" / "desc" while inside one, else ""
	gotRoot := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if gotRoot {
				doc.deriveSize()
				return doc, err
			}
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			local := t.Name.Local
			if maxElements <= 0 || doc.total < maxElements {
				doc.counts[local]++
				doc.total++
			} else {
				doc.capped = true
			}
			switch local {
			case "svg":
				if !gotRoot {
					gotRoot = true
					for _, a := range t.Attr {
						switch a.Name.Local {
						case "width":
							doc.rawWidth = a.Value
						case "height":
							doc.rawHeight = a.Value
						case "viewBox":
							doc.viewBox = a.Value
						}
					}
				}
			case "title", "desc":
				capture = local
			}
		case xml.CharData:
			if capture != "" {
				s := strings.TrimSpace(string(t))
				if s == "" {
					break
				}
				s = truncateRunes(s, maxCapturedText)
				if capture == "title" && len(doc.titles) < maxCapturedMeta {
					doc.titles = append(doc.titles, s)
				} else if capture == "desc" && len(doc.descs) < maxCapturedMeta {
					doc.descs = append(doc.descs, s)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "title" || t.Name.Local == "desc" {
				capture = ""
			}
		}
	}
	if !gotRoot {
		return nil, errors.New("no <svg> root element")
	}
	doc.deriveSize()
	return doc, nil
}

// deriveSize resolves the document's intrinsic pixel-ish dimensions. The
// viewBox wins when present (its width/height are the last two of four
// space- or comma-separated numbers); otherwise the width/height
// attributes are parsed with their unit suffix stripped. Falls back to
// the SVG spec's 300 × 150 default when neither yields a positive size.
func (d *Document) deriveSize() {
	if vw, vh, ok := parseViewBox(d.viewBox); ok {
		d.viewW, d.viewH = vw, vh
		return
	}
	w := parseLength(d.rawWidth)
	h := parseLength(d.rawHeight)
	if w > 0 && h > 0 {
		d.viewW, d.viewH = w, h
		return
	}
	d.viewW, d.viewH = 300, 150
}

// parseViewBox parses an SVG viewBox ("min-x min-y width height").
func parseViewBox(s string) (w, h float64, ok bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == '\n'
	})
	if len(fields) != 4 {
		return 0, 0, false
	}
	w, errW := strconv.ParseFloat(fields[2], 64)
	h, errH := strconv.ParseFloat(fields[3], 64)
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// parseLength parses an SVG length, dropping any trailing unit suffix
// (px, pt, mm, …). Percentages are treated as no usable absolute size.
func parseLength(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, "%") {
		return 0
	}
	i := 0
	for i < len(s) && (s[i] == '+' || s[i] == '-' || s[i] == '.' ||
		(s[i] >= '0' && s[i] <= '9')) {
		i++
	}
	v, err := strconv.ParseFloat(s[:i], 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// truncateRunes clips s to at most n runes.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
