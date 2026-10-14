// info.go: the InfoMode renderer — a zero-dependency textual
// summary of the loaded document, shown when RasterMode is off, while a
// raster is pending, or when the rasterizer is unavailable.

package ntsvg

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// renderInfoView builds the InfoMode summary for the loaded document:
// view box, declared size, an element-type histogram, and any embedded
// <title> / <desc>. All document-derived strings are sanitized before
// display — a hostile SVG can carry terminal control sequences in its
// element names or metadata.
func (m Model) renderInfoView() string {
	if m.doc == nil {
		return m.style.Status.Render("(no document)")
	}
	d := m.doc

	var lines []string
	add := func(label, value string) {
		lines = append(lines, fmt.Sprintf("%-10s %s", label+":", SanitizeForTerminal(value)))
	}

	add("file", d.name)
	if t := d.Title(); t != "" {
		add("title", t)
	}
	w, h := d.ViewBox()
	add("viewBox", fmt.Sprintf("%g × %g", w, h))
	if d.rawWidth != "" || d.rawHeight != "" {
		add("size", fmt.Sprintf("%s × %s", orDash(d.rawWidth), orDash(d.rawHeight)))
	}

	total := fmt.Sprintf("%d", d.total)
	if d.capped {
		total += "+ (count capped)"
	}
	add("elements", total)

	// Element histogram, most frequent first. Capped to a handful of
	// rows so a document with hundreds of distinct element types can't
	// overflow the cell rectangle.
	hist := d.Histogram()
	const maxRows = 12
	for i, e := range hist {
		if i >= maxRows {
			lines = append(lines, fmt.Sprintf("%-10s …and %d more", "", len(hist)-maxRows))
			break
		}
		lines = append(lines, fmt.Sprintf("%-10s %-12s %d", "",
			SanitizeForTerminal(e.Name), e.Count))
	}

	if len(d.descs) > 0 {
		add("desc", d.descs[0])
	}

	body := m.style.Info.Render(strings.Join(lines, "\n"))
	// Final clamp: never let the summary overflow the host's cell rect.
	if m.rows > 0 {
		body = lipgloss.NewStyle().MaxHeight(m.rows).MaxWidth(maxInt(m.cols, 1)).Render(body)
	}
	return body
}

// orDash returns s, or "—" when s is empty.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
