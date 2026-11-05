package ntsvg

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func TestRootViewBoxPreservesNamespacesAndSource(t *testing.T) {
	prefix := `<?xml version="1.0"?><!-- <svg viewBox="not the root"/> -->`
	// Deliberately malformed trailing XML must remain available to the
	// generous renderer, rather than being rejected by a whole-file rewrite.
	suffix := `<p:title>A & B</p:title><p:rect width="100%" height="100%"/>`
	source := []byte(prefix + `<p:svg xmlns:p="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 NaN 10" width="100" height="10" data-note='a &amp; &quot;b&quot; &gt;' xlink:href="asset">` + suffix)
	normalized, err := withRootViewBox(source, [4]float64{0, 0, 100, 10})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(normalized, []byte(prefix)) || !bytes.HasSuffix(normalized, []byte(suffix)) {
		t.Fatal("rewrote content outside the opening tag")
	}
	dec := xml.NewDecoder(bytes.NewReader(normalized))
	for {
		token, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		root, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if root.Name != (xml.Name{Space: "http://www.w3.org/2000/svg", Local: "svg"}) {
			t.Fatalf("root name = %+v", root.Name)
		}
		found := map[xml.Name]string{}
		for _, attr := range root.Attr {
			found[attr.Name] = attr.Value
		}
		if found[xml.Name{Local: "viewBox"}] != "0 0 100 10" || found[xml.Name{Local: "data-note"}] != `a & "b" >` || found[xml.Name{Space: "http://www.w3.org/1999/xlink", Local: "href"}] != "asset" {
			t.Fatalf("attributes changed: %+v", found)
		}
		break
	}
}

func TestRootViewBoxPreservesSelfClosingTag(t *testing.T) {
	normalized, err := withRootViewBox([]byte(`<svg width='100' height='10'/>`), [4]float64{0, 0, 100, 10})
	if err != nil {
		t.Fatal(err)
	}
	dec := xml.NewDecoder(bytes.NewReader(normalized))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	if token, err := dec.Token(); err != nil {
		t.Fatal(err)
	} else if _, ok := token.(xml.EndElement); !ok {
		t.Fatalf("missing end element: %T", token)
	}
}
