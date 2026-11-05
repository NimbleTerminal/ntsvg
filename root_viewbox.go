package ntsvg

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
)

// withRootViewBox gives oksvg the resolved viewport before it parses geometry
// lengths such as "100%". Only the opening SVG tag is re-encoded; the preamble
// and remaining source bytes (including partially malformed XML) stay intact.
func withRootViewBox(data []byte, box [4]float64) ([]byte, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		start := dec.InputOffset()
		token, err := dec.RawToken()
		if err != nil {
			return nil, err
		}
		root, ok := token.(xml.StartElement)
		if !ok || root.Name.Local != "svg" {
			continue
		}
		end := dec.InputOffset()
		var out bytes.Buffer
		out.Grow(len(data) + 128)
		out.Write(data[:start])
		out.WriteByte('<')
		writeRawXMLName(&out, root.Name)
		fmt.Fprintf(&out, ` viewBox="%g %g %g %g"`, box[0], box[1], box[2], box[3])
		for _, attr := range root.Attr {
			if attr.Name.Local == "viewBox" {
				continue
			}
			out.WriteByte(' ')
			writeRawXMLName(&out, attr.Name)
			out.WriteString(`="`)
			if err := xml.EscapeText(&out, []byte(attr.Value)); err != nil {
				return nil, err
			}
			out.WriteByte('"')
		}
		if end <= start || data[end-1] != '>' {
			return nil, errors.New("invalid SVG opening tag")
		}
		if end-start >= 2 && data[end-2] == '/' {
			out.WriteByte('/')
		}
		out.WriteByte('>')
		out.Write(data[end:])
		return out.Bytes(), nil
	}
}

// RawToken keeps namespace prefixes in Name.Space, rather than resolving them
// to URIs. Preserve those prefixes and their xmlns declarations verbatim.
func writeRawXMLName(out *bytes.Buffer, name xml.Name) {
	if name.Space != "" {
		out.WriteString(name.Space)
		out.WriteByte(':')
	}
	out.WriteString(name.Local)
}
