package ntsvg

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"math"
	"reflect"
	"testing"
)

// Keep XML mutation inputs bounded so each iteration tests parser behavior
// rather than exhausting memory. Normal tests cover the larger wire limits.
const maxFuzzSVGBytes = 64 << 10

func FuzzMetadataDecode(f *testing.F) {
	valid, err := encodeMetadataWithWarning(testMetadata(), "partial SVG")
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range [][]byte{
		valid, nil, []byte(`null`), []byte(`{"version":99}`),
		[]byte(`{"version":1,"metadata":{"view_box":[0,0,100,10],"element_counts":{"svg":-1},"total_elements":-1}}`),
		[]byte(`{"version":1,"metadata":{"view_box":[0,0,1e999,10]}}`),
		[]byte(`{"version":1,"metadata":{"view_box":[0,0,100,10],"element_counts":{"svg":1},"total_elements":2}}`),
		[]byte("\xff\x00{"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxMetadataBytes+1 {
			return
		}
		envelope, err := decodeMetadataEnvelope(data)
		if err != nil {
			return
		}
		// An accepted envelope must survive canonical JSON serialization with
		// no lost fields or numeric changes. Encoding can expand escapes past
		// the byte limit, so that separate size rejection is permitted.
		canonical, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("accepted metadata cannot be serialized: %v", err)
		}
		if len(canonical) > maxMetadataBytes {
			return
		}
		again, err := decodeMetadataEnvelope(canonical)
		if err != nil || !reflect.DeepEqual(envelope, again) {
			t.Fatalf("unstable metadata round trip: %v", err)
		}
	})
}

func FuzzSVGMetadata(f *testing.F) {
	for _, seed := range []string{
		`<svg viewBox="0 0 100 10"><title>Example</title><rect width="100%" height="100%"/></svg>`,
		`<svg viewBox="NaN 0 100 10"><desc>A & B</desc>`,
		`<svg viewBox="0 0 NaN 10" width="1e2px" height="1e1px"/>`,
		`<s:svg xmlns:s="http://www.w3.org/2000/svg"><s:title>Text</s:title></s:svg>`,
		"<svg><title>\x00</title></svg>", ``,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSVGBytes {
			return
		}
		doc, _ := scanMetadata("fuzz.svg", data, 128)
		if doc == nil {
			return
		}
		metadata := doc.metadata()
		encoded, err := encodeMetadata(metadata)
		if err != nil {
			t.Fatalf("extracted metadata cannot cross the wire: %v", err)
		}
		decoded, err := decodeMetadata(encoded)
		if err != nil || !reflect.DeepEqual(metadata, decoded) {
			t.Fatalf("metadata changed across the wire: %v", err)
		}
		if decoded.TotalElements != doc.TotalElements() || decoded.CountCapped != doc.CountCapped() {
			t.Fatal("metadata lost element totals")
		}
	})
}

func FuzzDimensions(f *testing.F) {
	for _, seed := range [][3]string{
		{"0 0 100 10", "100", "10"}, {"NaN 2 100 10", "100", "10"},
		{"0 0 NaN 10", "1e2px", "1e1px"}, {"", "1e-2", "1E+2"},
		{"", "100%", "NaN"}, {"0 0 1e309 10", "1e999", "1e-999"},
		{"", "1em", "2ex"}, {"", "1e+px", ".5e2"},
	} {
		f.Add(seed[0], seed[1], seed[2])
	}
	f.Fuzz(func(t *testing.T, box, width, height string) {
		if len(box)+len(width)+len(height) > maxFuzzSVGBytes {
			return
		}
		for _, text := range []string{width, height} {
			v := parseLength(text)
			if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatal("length parser returned an unusable size")
			}
		}
		doc := &Document{viewBox: box, rawWidth: width, rawHeight: height}
		doc.deriveSize()
		metadata := doc.metadata()
		if err := validateMetadata(metadata); err != nil {
			t.Fatalf("dimension fallback produced invalid metadata: %v", err)
		}
		if metadata.ViewBox[2] != doc.viewW || metadata.ViewBox[3] != doc.viewH {
			t.Fatal("metadata and document dimensions disagree")
		}
	})
}

func FuzzFitDims(f *testing.F) {
	f.Add(16.0, 9.0, uint16(8191), uint16(8191), uint16(8191), uint32((4<<20)-1))
	f.Add(10000.0, 1.0, uint16(8191), uint16(0), uint16(8191), uint32(2))
	f.Add(1.0, 10000.0, uint16(0), uint16(8191), uint16(8191), uint32(2))
	f.Add(math.NaN(), math.Inf(1), uint16(63), uint16(63), uint16(63), uint32(4095))
	f.Add(math.SmallestNonzeroFloat64, math.MaxFloat64, uint16(8191), uint16(8191), uint16(8191), uint32(0))
	f.Fuzz(func(t *testing.T, vw, vh float64, width, height, edge uint16, pixels uint32) {
		maxW, maxH, maxEdge := 1+int(width%8192), 1+int(height%8192), 1+int(edge%8192)
		maxPixels := 1 + int(pixels%(16<<20))
		w, h := fitDims(vw, vh, maxW, maxH, maxEdge, maxPixels)
		if w < 1 || h < 1 || w > maxW || h > maxH || w > maxEdge || h > maxEdge || w > maxPixels/h {
			t.Fatalf("fitDims(%g,%g,%d,%d,%d,%d) = %dx%d exceeds bounds", vw, vh, maxW, maxH, maxEdge, maxPixels, w, h)
		}
	})
}

func FuzzRootViewBox(f *testing.F) {
	for _, seed := range []string{
		`<svg viewBox="NaN 0 100 10"><rect width="100%"/></svg>`,
		`<?xml version="1.0"?><!-- <svg/> --><svg width='1e2px' height='10'/>`,
		`<s:svg xmlns:s="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xlink:href="a&amp;b"><s:rect/>`,
		`<svg data-note='a &amp; &quot;b&quot; &gt;'><title>A & B</title>`,
		`<svg viewBox="bad" viewBox="0 0 20 10"/>`, ``,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzSVGBytes {
			return
		}
		box := [4]float64{3, 4, 100, 10}
		normalized, err := withRootViewBox(data, box)
		if err != nil {
			return
		}
		before, start, end, err := fuzzRootToken(data)
		if err != nil {
			t.Fatal(err)
		}
		after, nextStart, nextEnd, err := fuzzRootToken(normalized)
		if err != nil {
			t.Fatalf("normalization broke the opening tag: %v", err)
		}
		if before.Name != after.Name || !bytes.Equal(data[:start], normalized[:nextStart]) || !bytes.Equal(data[end:], normalized[nextEnd:]) {
			t.Fatal("normalization changed content outside the opening tag")
		}
		var original, actual []xml.Attr
		for _, attr := range before.Attr {
			if attr.Name.Local != "viewBox" {
				original = append(original, attr)
			}
		}
		viewBoxes := 0
		for _, attr := range after.Attr {
			if attr.Name.Local == "viewBox" {
				viewBoxes++
				parsed, ok := parseViewBoxRect(attr.Value)
				if !ok || parsed != box {
					t.Fatal("resolved viewport was lost")
				}
			} else {
				actual = append(actual, attr)
			}
		}
		if viewBoxes != 1 || !reflect.DeepEqual(original, actual) {
			t.Fatal("normalization changed unrelated attributes")
		}
		again, err := withRootViewBox(normalized, box)
		if err != nil || !bytes.Equal(normalized, again) {
			t.Fatalf("normalization is not idempotent: %v", err)
		}
	})
}

func fuzzRootToken(data []byte) (xml.StartElement, int64, int64, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		start := dec.InputOffset()
		token, err := dec.RawToken()
		if err != nil {
			return xml.StartElement{}, 0, 0, err
		}
		if root, ok := token.(xml.StartElement); ok && root.Name.Local == "svg" {
			return root, start, dec.InputOffset(), nil
		}
	}
}
