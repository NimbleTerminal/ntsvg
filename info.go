// info.go: the InfoMode renderer — a zero-dependency textual
// summary of the loaded document, shown as a floating overlay over the
// rasterized SVG in InfoMode or standalone when the rasterizer is unavailable.

package ntsvg

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// activeDoc returns the current Document, or prevDoc while a replacement load
// is in flight so the info overlay does not blink during transitions.
func (m Model) activeDoc() *Document {
	if m.doc != nil {
		return m.doc
	}
	return m.prevDoc
}

// renderInfoView builds the InfoMode summary for the loaded document:
// view box, declared size, an element-type histogram, and any embedded
// <title> / <desc>. All document-derived strings are sanitized before
// display — a hostile SVG can carry terminal control sequences in its
// element names or metadata.
func (m Model) renderInfoView() string {
	d := m.activeDoc()
	if d == nil {
		return m.style.Status.Render("(no document)")
	}

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

// overlayInfoView composites the rendered info card onto the base raster view.
// Splicing each overlaid row with ansi.Truncate and ansi.TruncateLeft preserves
// both half-block Glyph SGR state and Kitty Unicode virtual placement
// (\U0010EEEE + row/col diacritics + foreground image ID) on both sides of the card.
func (m Model) overlayInfoView(base string) string {
	if m.activeDoc() == nil {
		return base
	}
	panel := m.renderInfoView()
	if panel == "" || base == "" {
		return base
	}

	baseLines := strings.Split(base, "\n")
	panelLines := strings.Split(panel, "\n")
	panelW := lipgloss.Width(panel)
	panelH := len(panelLines)

	x0, y0 := 0, 0
	if m.cols >= panelW+4 {
		x0 = 2
	} else if m.cols > panelW {
		x0 = (m.cols - panelW) / 2
	}
	if m.rows >= panelH+2 {
		y0 = 1
	}

	for i, pLine := range panelLines {
		row := y0 + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		bLine := baseLines[row]
		pw := ansi.StringWidth(pLine)
		left := ansi.Truncate(bLine, x0, "")
		if lw := ansi.StringWidth(left); lw < x0 {
			left += strings.Repeat(" ", x0-lw)
		}
		right := ansi.TruncateLeft(bLine, x0+pw, "")
		baseLines[row] = left + "\x1b[0m" + pLine + "\x1b[0m" + right
	}
	return strings.Join(baseLines, "\n")
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
