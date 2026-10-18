// Generate Mermaid diagrams as SVG and display them through ntsvg.
package main

import (
	"fmt"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	mermaid "github.com/zkrebbekx/go-mermaid"
	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg"
	"nimbleterminal.dev/ntsvg/examples/internal/demohelp"
)

// These are Mermaid source documents, rendered at runtime (not cached SVGs).
type sampleItem struct {
	name   string
	source string
}

var samples = []sampleItem{
	{"Pipeline", `flowchart LR
    A[Mermaid source] --> B[go-mermaid]
    B --> C[SVG bytes]
    C --> D[ntsvg]
    D --> E{Terminal}
    E --> F[Kitty graphics]
    E --> G[Half-block glyphs]
`},
	{"Sequence", `sequenceDiagram
    participant User
    participant App
    participant Viewer
    User->>App: Select diagram
    App->>Viewer: SetSVGData
    Viewer-->>User: Render SVG
    User->>Viewer: Zoom and pan
    Viewer-->>User: Rasterize viewport
`},
	{"State machine", `stateDiagram-v2
    [*] --> Ready
    Ready --> Rendering: Load SVG
    Rendering --> Viewing: Success
    Rendering --> Failed: Error
    Failed --> Ready: Retry
    Viewing --> Rendering: Zoom
    Viewing --> Ready: Next diagram
`},
	{"Pie chart", `pie title Example traffic
    "Flowcharts" : 45
    "Sequences" : 30
    "State machines" : 15
    "Other" : 10
`},
	{"Class diagram", `classDiagram
    class Renderer {
        Render()
        RenderRegion()
        ViewBox()
        Close()
    }
    class Model {
        SetSVGData()
        ZoomIn()
        PanLeft()
    }
    Model --> Renderer : uses
`},
}

var themes = []mermaid.Theme{mermaid.Default, mermaid.Dark, mermaid.Forest, mermaid.Neutral, mermaid.Base}

func renderSample(index, theme int) ([]byte, error) {
	// Only use the SVG API. ntsvg owns rasterization, including zoomed regions.
	return mermaid.Render(samples[index].source, mermaid.WithTheme(themes[theme]))
}

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8de3b6"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	sv            ntsvg.Model
	theme         int
	err           error
	index         int
	width, height int
}

func newModel() model {
	data, err := renderSample(0, 0)
	cfg := ntsvg.Config{
		InitialData: data,
		InitialName: samples[0].name,
	}
	cfg.PictureOptions.AutoMode = true
	if runtime.GOOS == "js" {
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		sv:  ntsvg.NewWithConfig(cfg),
		err: err,
	}
}

func (m model) Init() tea.Cmd { return m.sv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next diagram", "n", "tab"),
		demohelp.Key("Prev diagram", "p", "shift+tab"),
		demohelp.Key("Raster / Info", "m", "t"),
		demohelp.Key("Theme", "c"),
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
	fit := "contain"
	switch m.sv.Fit() {
	case ntsvg.FitFill:
		fit = "fill"
	case ntsvg.FitCover:
		fit = "cover"
	}
	return fmt.Sprintf("Mermaid: diagram=%s (%d/%d) theme=%s mode=%s fit=%s zoom=%dx",
		samples[m.index].name, m.index+1, len(samples), themes[m.theme], mode, fit, 1<<m.sv.Zoom())
}

func (m *model) resize() tea.Cmd {
	if m.width <= 0 || m.height <= 0 {
		return nil
	}
	innerW := max(1, m.width-2)
	innerH := max(1, m.height-4)
	return m.sv.SetSize(innerW, innerH)
}

func (m *model) selectSample(idx int) tea.Cmd {
	n := len(samples)
	m.index = ((idx % n) + n) % n
	data, err := renderSample(m.index, m.theme)
	m.err = err
	if err != nil {
		return nil
	}
	return m.sv.SetSVGData(samples[m.index].name, data)
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
		switch msg.String() {
		case "n", "tab":
			return m, m.selectSample(m.index + 1)
		case "p", "shift+tab":
			return m, m.selectSample(m.index - 1)
		case "c":
			m.theme = (m.theme + 1) % len(themes)
			return m, m.selectSample(m.index)
		case "r":
			m.theme = 0
			fitCmd := m.sv.SetFit(ntsvg.FitContain)
			var modeCmd tea.Cmd
			if m.sv.Mode() != ntsvg.RasterMode {
				modeCmd = m.sv.ToggleMode()
			}
			return m, tea.Batch(fitCmd, modeCmd, m.selectSample(0))
		}
	}

	var cmd tea.Cmd
	m.sv, cmd = m.sv.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("loading…")
	}
	header := headerStyle.Render("MERMAID → SVG") + "  " +
		footerStyle.Render("n/p: diagram · c: theme · m: raster/info · f: fit · +/-: zoom · arrows: pan · g: glyph/kitty · q: quit")
	content := m.sv.View().Content
	if m.err != nil {
		content = "Mermaid: " + ntsvg.SanitizeForTerminal(m.err.Error())
	}
	body := boxStyle.Render(content)
	status := footerStyle.Width(m.width).Render(m.Summary())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
