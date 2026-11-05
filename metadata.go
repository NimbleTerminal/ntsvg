package ntsvg

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Metadata describes an SVG without retaining or parsing its source markup.
// Strings are untrusted document content; sanitize them before terminal display.
// ViewBox contains min-x, min-y, width, height in user units. If no usable
// viewBox was declared, it contains the derived intrinsic size at origin zero.
type Metadata struct {
	ViewBox       [4]float64     `json:"view_box"`
	Width         string         `json:"width"`
	Height        string         `json:"height"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	ElementCounts map[string]int `json:"element_counts"`
	TotalElements int            `json:"total_elements"`
	CountCapped   bool           `json:"count_capped"`
}

// MetadataProvider is an optional Renderer capability. Metadata returns an
// independent snapshot, including a copy of ElementCounts. Implementations
// without this capability retain host-side metadata scanning.
type MetadataProvider interface {
	Metadata() Metadata
}

const (
	metadataVersion      = 1
	maxMetadataBytes     = 1 << 20
	maxMetadataEntries   = 4096
	maxMetadataNameBytes = 256
	maxMetadataSizeBytes = 4096
)

type metadataEnvelope struct {
	Version  int       `json:"version"`
	Metadata *Metadata `json:"metadata"`
}

func cloneMetadata(m Metadata) Metadata {
	m.ElementCounts = maps.Clone(m.ElementCounts)
	return m
}

func (d *Document) metadata() Metadata {
	m := Metadata{
		ViewBox: [4]float64{0, 0, d.viewW, d.viewH},
		Width:   d.rawWidth, Height: d.rawHeight,
		Title: d.Title(), ElementCounts: maps.Clone(d.counts),
		TotalElements: d.total, CountCapped: d.capped,
	}
	if len(d.descs) > 0 {
		m.Description = d.descs[0]
	}
	if _, _, ok := parseViewBox(d.viewBox); ok {
		fields := strings.FieldsFunc(d.viewBox, func(r rune) bool {
			return r == ' ' || r == ',' || r == '\t' || r == '\n'
		})
		x, ex := strconv.ParseFloat(fields[0], 64)
		y, ey := strconv.ParseFloat(fields[1], 64)
		if ex == nil && ey == nil {
			m.ViewBox[0], m.ViewBox[1] = x, y
		}
	}
	return m
}

func documentFromMetadata(name string, m Metadata) *Document {
	d := &Document{
		name: name, viewW: m.ViewBox[2], viewH: m.ViewBox[3],
		rawWidth: m.Width, rawHeight: m.Height,
		counts: maps.Clone(m.ElementCounts), total: m.TotalElements, capped: m.CountCapped,
	}
	if m.Title != "" {
		d.titles = []string{m.Title}
	}
	if m.Description != "" {
		d.descs = []string{m.Description}
	}
	return d
}

func validateMetadata(m Metadata) error {
	for _, v := range m.ViewBox {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("non-finite metadata viewBox")
		}
	}
	if m.ViewBox[2] <= 0 || m.ViewBox[3] <= 0 {
		return errors.New("invalid metadata dimensions")
	}
	for _, s := range []string{m.Width, m.Height} {
		if len(s) > maxMetadataSizeBytes || !utf8.ValidString(s) {
			return errors.New("invalid metadata declared size")
		}
	}
	for _, s := range []string{m.Title, m.Description} {
		if len(s) > 4*maxCapturedText || !utf8.ValidString(s) || utf8.RuneCountInString(s) > maxCapturedText {
			return errors.New("invalid metadata text")
		}
	}
	if len(m.ElementCounts) > maxMetadataEntries || m.TotalElements < 0 {
		return errors.New("invalid metadata element count")
	}
	total := 0
	for name, count := range m.ElementCounts {
		if name == "" || len(name) > maxMetadataNameBytes || !utf8.ValidString(name) || count < 0 || count > m.TotalElements-total {
			return errors.New("invalid metadata histogram")
		}
		total += count
	}
	if total != m.TotalElements {
		return errors.New("metadata histogram total mismatch")
	}
	return nil
}

func encodeMetadata(m Metadata) ([]byte, error) {
	if err := validateMetadata(m); err != nil {
		return nil, err
	}
	data, err := json.Marshal(metadataEnvelope{Version: metadataVersion, Metadata: &m})
	if err == nil && len(data) > maxMetadataBytes {
		err = errors.New("metadata payload too large")
	}
	return data, err
}

func decodeMetadata(data []byte) (Metadata, error) {
	if len(data) == 0 || len(data) > maxMetadataBytes || !utf8.Valid(data) {
		return Metadata{}, errors.New("missing or oversized SVG metadata")
	}
	var envelope metadataEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return Metadata{}, fmt.Errorf("decode SVG metadata: %w", err)
	}
	if envelope.Version != metadataVersion || envelope.Metadata == nil {
		return Metadata{}, errors.New("unsupported or missing SVG metadata envelope")
	}
	if err := validateMetadata(*envelope.Metadata); err != nil {
		return Metadata{}, err
	}
	return *envelope.Metadata, nil
}

// confinedLoadError prevents worker failures from falling back to host XML
// scanning. It is deliberately preserved through errors.Join and wrapping.
type confinedLoadError struct{ error }

func (e *confinedLoadError) Unwrap() error { return e.error }
