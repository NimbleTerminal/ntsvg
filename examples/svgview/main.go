// examples/svgview/main.go — single-pane ntsvg demo and CLI viewer.
//
// Loads the SVG named on the command line (or an embedded sample when
// no path is given) and renders it. It also demonstrates the
// immediate-mode Stage: press `c` to generate a bar chart on the fly
// and display it, `e` to export the most recent generated chart to an
// .svg file.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg"
	"nimbleterminal.dev/ntsvg/examples/internal/demohelp"
	"nimbleterminal.dev/ntsvg/stage"
)

// embeddedSample is the demo SVG compiled into the binary, used when
// the user runs the example without a path argument.
//
//go:embed testdata/sample.svg
var embeddedSample []byte

const chartExportPath = "svgview-chart.svg"

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	offStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
)

func kittyBadge(cap picture.KittyCapability) string {
	switch cap {
	case picture.KittyCapabilitySupported:
		return okStyle.Render("kitty:✓")
	case picture.KittyCapabilityUnsupported:
		return offStyle.Render("kitty:✗")
	default:
		return offStyle.Render("kitty:?")
	}
}

type appKeys struct {
	mode   key.Binding
	render key.Binding
	zoom   key.Binding
	pan    key.Binding
	fit    key.Binding
	chart  key.Binding
	export key.Binding
	help   key.Binding
	quit   key.Binding
}

func newAppKeys() appKeys {
	return appKeys{
		mode:   key.NewBinding(key.WithKeys("m", "t"), key.WithHelp("m", "raster/info")),
		render: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "glyph/kitty")),
		zoom:   key.NewBinding(key.WithKeys("+", "-"), key.WithHelp("+/-", "zoom")),
		pan:    key.NewBinding(key.WithKeys("left", "right", "up", "down"), key.WithHelp("←↑↓→", "pan")),
		fit:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fit mode")),
		chart:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "demo chart")),
		export: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "export chart")),
		help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k appKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.zoom, k.pan, k.mode, k.chart, k.help, k.quit}
}

func (k appKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.mode, k.render, k.fit},
		{k.zoom, k.pan},
		{k.chart, k.export, k.help, k.quit},
	}
}

type model struct {
	sv            ntsvg.Model
	help          help.Model
	keys          appKeys
	width, height int
	preferKitty   bool

	lastChart *stage.Stage
	notice    string
}

func initialModel(cfg ntsvg.Config) model {
	if runtime.GOOS == "js" {
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		sv:          ntsvg.NewWithConfig(cfg),
		help:        help.New(),
		keys:        newAppKeys(),
		preferKitty: true,
	}
}

func (m model) Init() tea.Cmd { return m.sv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Raster / Info", "m", "t"),
		demohelp.Key("Demo chart", "c"),
		demohelp.Key("Fit mode", "f"),
		demohelp.Key("Zoom in", "+", "="),
		demohelp.Key("Zoom out", "-", "_"),
		demohelp.Key("Glyph / Kitty", "g"),
		demohelp.Key("Reset", "r"),
		demohelp.Key("Quit", "q", "ctrl+c"),
	}
}

func (m model) Summary() string {
	mode := "raster"
	if m.sv.Mode() == ntsvg.InfoMode {
		mode = "info"
	}
	return fmt.Sprintf("Viewer: name=%s mode=%s chart=%t",
		filepath.Base(m.sv.Name()), mode, m.lastChart != nil)
}

func (m *model) resize() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	m.help.SetWidth(m.width)
	helpH := lipgloss.Height(m.help.View(m.keys))
	innerW := max(1, m.width-2)
	innerH := max(1, m.height-helpH-1-2)
	return m.sv.SetSize(innerW, innerH)
}

func demoChart() *stage.Stage {
	return stage.NewChart(360, 240).
		SetTitle("Quarterly Revenue").
		SetLabels("Q1", "Q2", "Q3", "Q4").
		AddSeries("2025", "", 12, 19, 14, 23).
		AddSeries("2026", "", 17, 15, 21, 28).
		BarStage()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, m.resize()

	case tea.KeyMsg:
		if key.Matches(msg, m.sv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Sequence(m.sv.Dispose(), tea.Quit)
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			return m, m.resize()
		case "c":
			m.lastChart = demoChart()
			m.notice = "generated demo chart"
			return m, m.sv.ShowStage(m.lastChart)
		case "r":
			if m.lastChart != nil {
				m.lastChart = nil
				m.notice = ""
				return m, m.sv.SetSVGData("sample.svg", embeddedSample)
			}
		case "e":
			if m.lastChart == nil {
				m.notice = "press 'c' to generate a chart first"
				return m, nil
			}
			if err := m.lastChart.WriteSVG(chartExportPath); err != nil {
				m.notice = "export failed: " + err.Error()
			} else {
				m.notice = "exported " + chartExportPath
			}
			return m, nil
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
	body := boxStyle.Render(m.sv.View().Content)

	modeName := "Raster"
	if m.sv.Mode() == ntsvg.InfoMode {
		modeName = "Info"
	} else if !m.sv.HasRenderer() {
		label := "Raster (no renderer)"
		if err := m.sv.RendererErr(); err != nil {
			label = fmt.Sprintf("Raster (error: %s)", ntsvg.SanitizeForTerminal(err.Error()))
		}
		modeName = warnStyle.Render(label)
	}
	renderName := "Glyph"
	if m.sv.RenderMode() == ntsvg.RenderKitty {
		renderName = "Kitty"
	}
	zoomTag := ""
	if z := m.sv.Zoom(); z > 0 {
		zoomTag = fmt.Sprintf("  ×%d", 1<<z)
	}
	fitName := "Contain"
	switch m.sv.Fit() {
	case ntsvg.FitFill:
		fitName = "Fill"
	case ntsvg.FitCover:
		fitName = "Cover"
	}
	w, h := m.sv.ViewBox()
	name := ntsvg.SanitizeForTerminal(filepath.Base(m.sv.Name()))
	status := fmt.Sprintf("%s  %g×%g%s  %s  %s  fit:%s  %s",
		name, w, h, zoomTag, modeName, renderName, fitName,
		kittyBadge(m.sv.KittySupported()))
	if err := m.sv.Err(); err != nil {
		status = errStyle.Render(ntsvg.SanitizeForTerminal(err.Error())) + "  " + status
	} else if m.notice != "" {
		status = okStyle.Render(m.notice) + "  " + status
	}
	footer := footerStyle.Width(m.width).Render(status)

	out := lipgloss.JoinVertical(lipgloss.Left, body, footer, m.help.View(m.keys))
	v := tea.NewView(out)
	v.AltScreen = true
	return v
}

func main() {
	cfg, err := initialConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	demohelp.Main(initialModel(cfg))
}

func initialConfig() (ntsvg.Config, error) {
	if len(os.Args) > 1 {
		path := os.Args[1]
		if _, err := os.Stat(path); err != nil {
			return ntsvg.Config{}, fmt.Errorf("argv[1] %q: %w", path, err)
		}
		return ntsvg.Config{InitialPath: path}, nil
	}
	return ntsvg.Config{
		InitialData: embeddedSample,
		InitialName: "sample.svg",
	}, nil
}
