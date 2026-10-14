// stage/chart.go: high-level chart helpers built on the Stage
// primitives. A Chart collects one or more data series and renders them
// as a bar or line chart Stage — clean SVG you can hand straight to
// ntsvg.Model.ShowStage or persist with WriteSVG.

package stage

import "fmt"

// DefaultPalette is the series color cycle used when a Series leaves
// Color blank.
var DefaultPalette = []string{
	"#4e79a7", "#f28e2b", "#e15759", "#76b7b2",
	"#59a14f", "#edc948", "#b07aa1", "#ff9da7",
}

// Series is one named data set plotted on a Chart.
type Series struct {
	Name   string
	Color  string // CSS color; blank cycles through DefaultPalette
	Values []float64
}

// Chart accumulates series and renders them to a Stage. Construct with
// NewChart, configure fluently, then call BarStage or LineStage.
type Chart struct {
	width, height float64
	title         string
	background    string
	labels        []string // x-axis category labels
	series        []Series
}

// NewChart returns a Chart sized width × height user units. Non-positive
// dimensions are clamped so the rendered SVG is well-formed.
func NewChart(width, height float64) *Chart {
	if width <= 0 {
		width = 320
	}
	if height <= 0 {
		height = 200
	}
	return &Chart{width: width, height: height, background: "white"}
}

// SetTitle sets the chart title drawn along the top.
func (c *Chart) SetTitle(title string) *Chart { c.title = title; return c }

// SetBackground sets the stage background color ("" for transparent).
func (c *Chart) SetBackground(color string) *Chart { c.background = color; return c }

// SetLabels sets the x-axis category labels.
func (c *Chart) SetLabels(labels ...string) *Chart { c.labels = labels; return c }

// AddSeries appends a data series. A blank color cycles through
// DefaultPalette by series index.
func (c *Chart) AddSeries(name, color string, values ...float64) *Chart {
	c.series = append(c.series, Series{Name: name, Color: color, Values: values})
	return c
}

// seriesColor resolves the i-th series' color, falling back to the
// palette.
func (c *Chart) seriesColor(i int) string {
	if i < len(c.series) && c.series[i].Color != "" {
		return c.series[i].Color
	}
	return DefaultPalette[i%len(DefaultPalette)]
}

// bounds returns the value range across every series. includeZero
// extends the range to the origin (wanted for bar charts so bars rest
// on a meaningful baseline).
func (c *Chart) bounds(includeZero bool) (vmin, vmax float64) {
	vmin, vmax = 0, 0
	first := true
	for _, s := range c.series {
		for _, v := range s.Values {
			if first {
				vmin, vmax, first = v, v, false
				continue
			}
			if v < vmin {
				vmin = v
			}
			if v > vmax {
				vmax = v
			}
		}
	}
	if first {
		return 0, 1
	}
	if includeZero {
		if vmin > 0 {
			vmin = 0
		}
		if vmax < 0 {
			vmax = 0
		}
	}
	if vmin == vmax {
		vmax = vmin + 1
	}
	return vmin, vmax
}

// maxLen returns the longest series length — the category count.
func (c *Chart) maxLen() int {
	n := 0
	for _, s := range c.series {
		if len(s.Values) > n {
			n = len(s.Values)
		}
	}
	return n
}

// plot describes the inner drawing rectangle once margins are removed.
type plot struct {
	left, right, top, bottom float64
}

func (p plot) w() float64 { return p.right - p.left }
func (p plot) h() float64 { return p.bottom - p.top }

// frame draws the title, axes, y-value gridlines, and x labels common
// to both chart kinds, returning the inner plot rectangle.
func (c *Chart) frame(st *Stage, vmin, vmax float64) plot {
	p := plot{left: 46, right: c.width - 12, top: 12, bottom: c.height - 28}
	if c.title != "" {
		p.top = 30
		st.Text(c.width/2, 20, c.title,
			Paint{Fill: "#222"}.WithFont("sans-serif", 14).WithAnchor("middle"))
	}
	if len(c.series) > 0 {
		p.right -= 96 // legend gutter
	}
	axis := Stroke("#888", 1)
	grid := Stroke("#e0e0e0", 1)
	label := Paint{Fill: "#555"}.WithFont("sans-serif", 9)

	// Horizontal gridlines + y-axis value labels.
	const ticks = 4
	for i := 0; i <= ticks; i++ {
		f := float64(i) / ticks
		y := p.bottom - f*p.h()
		v := vmin + f*(vmax-vmin)
		st.Line(p.left, y, p.right, y, grid)
		st.Text(p.left-6, y+3, trimNum(v), label.WithAnchor("end"))
	}
	// Axis lines.
	st.Line(p.left, p.top, p.left, p.bottom, axis)
	st.Line(p.left, p.bottom, p.right, p.bottom, axis)

	// X-axis category labels.
	n := c.maxLen()
	for i := 0; i < n && i < len(c.labels); i++ {
		x := p.left + (float64(i)+0.5)/float64(n)*p.w()
		st.Text(x, p.bottom+14, c.labels[i], label.WithAnchor("middle"))
	}
	c.legend(st, p)
	return p
}

// legend draws series swatches down the right gutter.
func (c *Chart) legend(st *Stage, p plot) {
	x := p.right + 12
	for i, s := range c.series {
		y := p.top + float64(i)*16
		st.Rect(x, y, 10, 10, Fill(c.seriesColor(i)))
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("series %d", i+1)
		}
		st.Text(x+14, y+9, name, Paint{Fill: "#333"}.WithFont("sans-serif", 9))
	}
}

// yOf maps a data value to a stage y coordinate within the plot.
func yOf(v, vmin, vmax float64, p plot) float64 {
	return p.bottom - (v-vmin)/(vmax-vmin)*p.h()
}

// BarStage renders the chart as a grouped bar chart Stage.
func (c *Chart) BarStage() *Stage {
	st := New(c.width, c.height).Background(c.background)
	if c.title != "" {
		st.SetTitle(c.title)
	}
	vmin, vmax := c.bounds(true)
	p := c.frame(st, vmin, vmax)
	n := c.maxLen()
	if n == 0 || len(c.series) == 0 {
		return st
	}
	zero := yOf(0, vmin, vmax, p)
	groupW := p.w() / float64(n)
	barW := groupW * 0.8 / float64(len(c.series))
	for gi := 0; gi < n; gi++ {
		gx := p.left + float64(gi)*groupW + groupW*0.1
		for si, s := range c.series {
			if gi >= len(s.Values) {
				continue
			}
			v := s.Values[gi]
			y := yOf(v, vmin, vmax, p)
			top, h := y, zero-y
			if h < 0 { // negative value: bar grows downward
				top, h = zero, -h
			}
			st.Rect(gx+float64(si)*barW, top, barW*0.92, h, Fill(c.seriesColor(si)))
		}
	}
	return st
}

// LineStage renders the chart as a multi-series line chart Stage with
// point markers.
func (c *Chart) LineStage() *Stage {
	st := New(c.width, c.height).Background(c.background)
	if c.title != "" {
		st.SetTitle(c.title)
	}
	vmin, vmax := c.bounds(false)
	p := c.frame(st, vmin, vmax)
	n := c.maxLen()
	if n == 0 {
		return st
	}
	step := p.w() / float64(maxInt(n-1, 1))
	for si, s := range c.series {
		color := c.seriesColor(si)
		pts := make([]Point, 0, len(s.Values))
		for i, v := range s.Values {
			pts = append(pts, Point{
				X: p.left + float64(i)*step,
				Y: yOf(v, vmin, vmax, p),
			})
		}
		st.Polyline(pts, Stroke(color, 2))
		for _, pt := range pts {
			st.Circle(pt.X, pt.Y, 3, Fill(color))
		}
	}
	return st
}

// trimNum formats an axis value compactly.
func trimNum(v float64) string {
	return num(roundTo(v, 100)) // 2 decimal places
}

func roundTo(v, scale float64) float64 {
	if v >= 0 {
		return float64(int64(v*scale+0.5)) / scale
	}
	return float64(int64(v*scale-0.5)) / scale
}
