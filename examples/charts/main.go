// Interactive bar and line charts generated with stage.Chart and displayed in ntsvg.Model.
package main

import (
	"fmt"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg"
	"nimbleterminal.dev/ntsvg/examples/internal/demohelp"
	"nimbleterminal.dev/ntsvg/stage"
)

type dataset struct {
	key    string
	title  string
	labels []string
	series []stage.Series
}

var datasets = []dataset{
	{
		key:    "revenue",
		title:  "Quarterly Revenue ($M)",
		labels: []string{"Q1", "Q2", "Q3", "Q4"},
		series: []stage.Series{
			{Name: "2025", Color: "#4ea8de", Values: []float64{12, 19, 14, 23}},
			{Name: "2026", Color: "#8de3b6", Values: []float64{17, 15, 21, 28}},
		},
	},
	{
		key:    "latency",
		title:  "Frame Render Latency (ms)",
		labels: []string{"60x20", "80x24", "120x40", "160x50"},
		series: []stage.Series{
			{Name: "p50", Color: "#8de3b6", Values: []float64{1.4, 2.1, 3.8, 5.2}},
			{Name: "p95", Color: "#f28e2b", Values: []float64{2.6, 3.9, 6.4, 8.9}},
			{Name: "p99", Color: "#e15759", Values: []float64{3.5, 5.1, 8.2, 11.4}},
		},
	},
	{
		key:    "throughput",
		title:  "Terminal Frame Throughput (fps)",
		labels: []string{"Glyph", "Kitty PNG", "SharedMem", "Cached Pan"},
		series: []stage.Series{
			{Name: "Native", Color: "#edc948", Values: []float64{58, 60, 60, 120}},
			{Name: "Browser", Color: "#4ea8de", Values: []float64{54, 42, 60, 110}},
		},
	},
}

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8de3b6"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	sv               ntsvg.Model
	lineMode         bool
	dataIdx, variant int
	width, height    int
	preferKitty      bool
}

func newModel() model {
	cfg := ntsvg.Config{}
	if runtime.GOOS == "js" {
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		sv:          ntsvg.NewWithConfig(cfg),
		preferKitty: true,
	}
}

func (m model) buildChart() *stage.Stage {
	d := datasets[m.dataIdx]
	c := stage.NewChart(380, 240).
		SetBackground("#f8fafc").
		SetTitle(d.title).
		SetLabels(d.labels...)
	for si, s := range d.series {
		vals := make([]float64, len(s.Values))
		for i, v := range s.Values {
			delta := float64(((m.variant*(i+2)+si*3)%5)-2) * 0.12 * v
			if m.variant == 0 {
				delta = 0
			}
			vals[i] = max(0.5, v+delta)
		}
		c.AddSeries(s.Name, s.Color, vals...)
	}
	if m.lineMode {
		return c.LineStage()
	}
	return c.BarStage()
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.sv.Init(), m.sv.ShowStage(m.buildChart()))
}

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Bar / Line", "t", "c"),
		demohelp.Key("Next dataset", "d", "tab"),
		demohelp.Key("Vary values", "v"),
		demohelp.Key("Raster / Info", "m"),
		demohelp.Key("Fit mode", "f"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset", "r"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	kind := "bar"
	if m.lineMode {
		kind = "line"
	}
	mode := "raster"
	if m.sv.Mode() == ntsvg.InfoMode {
		mode = "info"
	}
	return fmt.Sprintf("Charts: type=%s dataset=%s variant=%d mode=%s",
		kind, datasets[m.dataIdx].key, m.variant, mode)
}

func (m *model) resize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	return m.sv.SetSize(max(1, m.width-2), max(1, m.height-4))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.resize()

	case tea.KeyMsg:
		if demohelp.Quit(msg) {
			return m, tea.Sequence(m.sv.Dispose(), tea.Quit)
		}
		if key.Matches(msg, m.sv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "t", "c":
			m.lineMode = !m.lineMode
			return m, m.sv.ShowStage(m.buildChart())
		case "d", "tab":
			m.dataIdx = (m.dataIdx + 1) % len(datasets)
			return m, m.sv.ShowStage(m.buildChart())
		case "v":
			m.variant = (m.variant + 1) % 5
			return m, m.sv.ShowStage(m.buildChart())
		case "r":
			m.lineMode, m.dataIdx, m.variant = false, 0, 0
			fitCmd := m.sv.SetFit(ntsvg.FitContain)
			var modeCmd tea.Cmd
			if m.sv.Mode() != ntsvg.RasterMode {
				modeCmd = m.sv.ToggleMode()
			}
			return m, tea.Batch(fitCmd, modeCmd, m.sv.ShowStage(m.buildChart()))
		}
	}

	var cmd tea.Cmd
	m.sv, cmd = m.sv.Update(msg)
	if m.preferKitty && m.sv.KittySupported() == picture.KittyCapabilitySupported {
		m.preferKitty = false
		if m.sv.RenderMode() != ntsvg.RenderKitty {
			cmd = tea.Batch(cmd, m.sv.ToggleRenderMode())
		}
	}
	return m, cmd
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("loading…")
	}
	header := headerStyle.Render("STAGE CHART BUILDER") + "  " +
		footerStyle.Render("t: bar/line · d: dataset · v: vary · m: raster/info · f: fit · g: glyph/kitty · r: reset · q: quit")
	body := boxStyle.Render(m.sv.View().Content)
	status := footerStyle.Width(m.width).Render(m.Summary())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
