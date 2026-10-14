// examples/svgview/main.go — single-pane ntsvg demo.
//
// Loads the SVG named on the command line (or an embedded sample when
// no path is given) and renders it. It also demonstrates the
// immediate-mode Stage: press `c` to generate a bar chart on the fly
// and display it, `e` to export the most recent generated chart to an
// .svg file. Keys:
//
//	m or t      toggle Raster ↔ Info mode
//	g           toggle Glyph ↔ Kitty (Raster mode only)
//	r           reload / re-rasterize
//	+/-         zoom in / out
//	←↑↓→        pan (when zoomed)
//	f           cycle fit mode
//	c           generate a demo chart and show it
//	e           export the last generated chart to ./svgview-chart.svg
//	?           toggle help
//	q / ctrl+c  quit
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
	booba "github.com/NimbleMarkets/go-booba"

	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg"
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

// kittyBadge renders a compact indicator for the terminal's Kitty
// graphics capability.
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

// appKeys is the parent program's help surface. The ntsvg widget owns
// the mode / zoom / pan bindings; this struct only exists so the
// bubbles/help bubble has a KeyMap to render.
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
	preferKitty   bool // pending automatic selection; cleared by a manual toggle

	// lastChart holds the most recently generated demo Stage so the
	// export key has something to write.
	lastChart *stage.Stage
	notice    string // transient status message (export result, etc.)
}

func initialModel(cfg ntsvg.Config) model {
	// Booba receives raw RGBA through its browser shared-memory bridge,
	// avoiding PNG encoding and base64 transport. Keep direct PNG on native
	// terminals, which may be remote or lack shared-memory support.
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

func (m *model) resize() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	m.help.SetWidth(m.width)
	helpH := lipgloss.Height(m.help.View(m.keys))
	innerW := m.width - 2
	innerH := m.height - helpH - 1 /* footer */ - 2 /* box borders */
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	return m.sv.SetSize(innerW, innerH)
}

// demoChart builds a small grouped bar chart with the immediate-mode
// Stage API — the "generate chart → SVG" half of the widget.
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
			_ = m.sv.Close()
			return m, tea.Quit
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			return m, m.resize()
		case "c":
			m.lastChart = demoChart()
			m.notice = "generated demo chart"
			return m, m.sv.ShowStage(m.lastChart)
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
	// Update consumes the asynchronous capability reply. Keep waiting after
	// a timeout, since a late positive reply can still establish support.
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
	return tea.NewView(out)
}

func main() {
	cfg, err := initialConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// booba.Run runs the Bubble Tea program normally on native builds
	// and bridges it to the booba terminal emulator under GOOS=js, so
	// the same example binary powers both the CLI and the WASM demo.
	if err := booba.Run(initialModel(cfg)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// initialConfig assembles the ntsvg.Config the example launches with.
// argv[1] wins as a filesystem path; otherwise the embedded sample SVG
// bytes are passed directly via Config.InitialData.
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
