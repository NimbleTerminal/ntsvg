package ntsvg

import (
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func testMetadata() Metadata {
	return Metadata{ViewBox: [4]float64{2, 3, 20, 10}, Width: "100%", Height: "24px",
		Title: "Example", Description: "A rectangle", ElementCounts: map[string]int{"svg": 1, "rect": 1}, TotalElements: 2}
}

func TestMetadataWireValidation(t *testing.T) {
	want := testMetadata()
	data, err := encodeMetadata(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeMetadata(data)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	for _, data := range [][]byte{nil, []byte(`{`), []byte(`null`), []byte(`{"version":2}`), []byte(`{"version":1}`), []byte(`{"version":1,"metadata":{}}`), []byte(strings.Repeat(" ", maxMetadataBytes+1))} {
		if _, err := decodeMetadata(data); err == nil {
			t.Fatalf("accepted invalid metadata (%d bytes)", len(data))
		}
	}
	for name, change := range map[string]func(*Metadata){
		"nan":            func(m *Metadata) { m.ViewBox[0] = math.NaN() },
		"infinity":       func(m *Metadata) { m.ViewBox[2] = math.Inf(1) },
		"zero dimension": func(m *Metadata) { m.ViewBox[3] = 0 },
		"negative count": func(m *Metadata) { m.ElementCounts["svg"] = -1 },
		"overflow count": func(m *Metadata) { m.ElementCounts["svg"] = int(^uint(0) >> 1) },
		"total mismatch": func(m *Metadata) { m.TotalElements++ },
		"negative total": func(m *Metadata) { m.TotalElements = -1 },
		"long name":      func(m *Metadata) { m.ElementCounts[strings.Repeat("a", maxMetadataNameBytes+1)] = 0 },
		"long title":     func(m *Metadata) { m.Title = strings.Repeat("a", maxCapturedText+1) },
		"long size":      func(m *Metadata) { m.Width = strings.Repeat("a", maxMetadataSizeBytes+1) },
		"invalid UTF8":   func(m *Metadata) { m.Description = "\xff" },
		"many entries": func(m *Metadata) {
			for i := range maxMetadataEntries + 1 {
				m.ElementCounts["element"+strconv.Itoa(i)] = 0
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := testMetadata()
			change(&m)
			if err := validateMetadata(m); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
}

func TestDefaultRendererMetadata(t *testing.T) {
	r, err := DefaultRendererFactory()("meta.svg", []byte(`<svg viewBox="2 3 20 10" width="100%" height="24px"><title>Example</title><desc>A rectangle</desc><rect width="20" height="10"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	provider := r.(MetadataProvider)
	m := provider.Metadata()
	if m.ViewBox != [4]float64{2, 3, 20, 10} || m.Width != "100%" || m.Height != "24px" || m.Title != "Example" || m.Description != "A rectangle" || m.TotalElements != 4 {
		t.Fatalf("metadata = %+v", m)
	}
	m.ElementCounts["rect"] = 99
	if provider.Metadata().ElementCounts["rect"] != 1 {
		t.Fatal("metadata snapshot aliases renderer counts")
	}
}

type metadataTestRenderer struct {
	Renderer
	metadata Metadata
	closed   bool
}

func (r *metadataTestRenderer) Metadata() Metadata { return cloneMetadata(r.metadata) }
func (r *metadataTestRenderer) Close() error       { r.closed = true; return nil }

func TestLoadUsesProviderWithoutParsingSource(t *testing.T) {
	r := &metadataTestRenderer{metadata: testMetadata()}
	msg := runLoadFromBytes("opaque", []byte("not XML"), 7, func(string, []byte) (Renderer, error) { return r, nil }, Limits{})
	loaded, ok := msg.(svgLoadedMsg)
	if !ok {
		t.Fatalf("load = %#v", msg)
	}
	if loaded.doc.Title() != "Example" || loaded.doc.ElementCount("rect") != 1 || loaded.gen != 7 || r.closed {
		t.Fatalf("load = %+v", loaded)
	}
	r.metadata.ElementCounts["rect"] = 100
	if loaded.doc.ElementCount("rect") != 1 {
		t.Fatal("document aliases renderer metadata")
	}
}

func TestLoadRejectsInvalidProviderAndClosesRenderer(t *testing.T) {
	r := &metadataTestRenderer{}
	msg := runLoadFromBytes("valid.svg", []byte(`<svg/>`), 1, func(string, []byte) (Renderer, error) { return r, nil }, Limits{})
	if _, ok := msg.(svgErrMsg); !ok || !r.closed {
		t.Fatalf("load = %#v, closed = %v", msg, r.closed)
	}
}

func TestLegacyRendererMetadataFallback(t *testing.T) {
	unavailable := errors.New("rasterizer unavailable")
	for _, factory := range []RendererFactory{nil,
		func(string, []byte) (Renderer, error) { return nil, unavailable },
		func(name string, data []byte) (Renderer, error) {
			r, err := DefaultRendererFactory()(name, data)
			return struct{ Renderer }{r}, err // expose only the legacy interface
		},
	} {
		msg := runLoadFromBytes("legacy.svg", []byte(`<svg viewBox="0 0 20 10"><title>Legacy</title></svg>`), 1, factory, Limits{})
		loaded, ok := msg.(svgLoadedMsg)
		if !ok || loaded.doc.Title() != "Legacy" {
			t.Fatalf("load = %#v", msg)
		}
		if loaded.renderer != nil {
			defer loaded.renderer.Close()
		}
		if loaded.renderer == nil && factory != nil && !errors.Is(loaded.rendererErr, unavailable) {
			t.Fatal("lost renderer error")
		}
	}
}

func TestBuiltinMetadataDoesNotPreventRendering(t *testing.T) {
	for name, source := range map[string]string{
		"nonfinite": `<svg viewBox="0 0 NaN 10" width="20" height="10"><rect width="20" height="10"/></svg>`,
		"long size": `<svg viewBox="0 0 20 10" width="` + strings.Repeat("x", maxMetadataSizeBytes+1) + `"><rect width="20" height="10"/></svg>`,
		"long name": `<svg viewBox="0 0 20 10"><` + strings.Repeat("a", maxMetadataNameBytes+1) + `/><rect width="20" height="10"/></svg>`,
	} {
		t.Run(name, func(t *testing.T) {
			msg := runLoadFromBytes(name, []byte(source), 1, DefaultRendererFactory(), Limits{})
			loaded, ok := msg.(svgLoadedMsg)
			if !ok || loaded.renderer == nil {
				t.Fatalf("load = %#v", msg)
			}
			defer loaded.renderer.Close()
			if err := validateMetadata(loaded.renderer.(MetadataProvider).Metadata()); err != nil {
				t.Fatal(err)
			}
			if _, err := loaded.renderer.Render(32, 32); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFitDimsNeverRoundsOverBudget(t *testing.T) {
	for _, size := range [][5]int{{16, 9, 8192, 8192, 4 << 20}, {1, 1, 8192, 8192, 10_000_000}, {10000, 1, 10000, 1, 3}, {1, 10000, 1, 10000, 3}} {
		w, h := fitDims(float64(size[0]), float64(size[1]), size[2], size[3], 0, size[4])
		if w < 1 || h < 1 || w > size[4]/h {
			t.Errorf("%v: %dx%d exceeds budget", size, w, h)
		}
	}
}

func TestExtractedMetadataCompactionPreservesTotals(t *testing.T) {
	doc := &Document{viewW: 20, viewH: 10, counts: make(map[string]int)}
	for i := range maxMetadataEntries + 10 {
		// Distinct names, long enough to hit the combined name-byte budget.
		doc.counts[strconv.Itoa(i)+strings.Repeat("a", maxMetadataNameBytes)] = 1
		doc.total++
	}
	m := doc.metadata()
	if m.TotalElements != doc.total || m.CountCapped || m.ElementCounts["[other]"] == 0 {
		t.Fatalf("incorrect compacted metadata: total=%d, capped=%v", m.TotalElements, m.CountCapped)
	}
	data, err := encodeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeMetadata(data); err != nil {
		t.Fatal(err)
	}
}
