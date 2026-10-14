// Browse embedded SVG samples across Raster and Info modes, fit policies, and zoom levels.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg"
	"nimbleterminal.dev/ntsvg/examples/internal/demohelp"
)

//go:embed samples/*.svg
var samplesFS embed.FS

type sampleItem struct {
	name string
	data []byte
}

func loadSamples() []sampleItem {
	entries, err := fs.ReadDir(samplesFS, "samples")
	if err != nil {
		panic(err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	items := make([]sampleItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := samplesFS.ReadFile("samples/" + e.Name())
		if err != nil {
			panic(err)
		}
		items = append(items, sampleItem{name: e.Name(), data: b})
	}
	return items
}

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8de3b6"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	sv            ntsvg.Model
	samples       []sampleItem
	index         int
	width, height int
	preferKitty   bool
}

func newModel() model {
	samples := loadSamples()
	cfg := ntsvg.Config{
		InitialData: samples[0].data,
		InitialName: samples[0].name,
	}
	if runtime.GOOS == "js" {
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	return model{
		sv:          ntsvg.NewWithConfig(cfg),
		samples:     samples,
		preferKitty: true,
	}
}

func (m model) Init() tea.Cmd { return m.sv.Init() }

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next sample", "n", "tab"),
		demohelp.Key("Prev sample", "p", "shift+tab"),
		demohelp.Key("Raster / Info", "m", "t"),
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
	return fmt.Sprintf("Gallery: sample=%s (%d/%d) mode=%s fit=%s zoom=%dx",
		filepath.Base(m.sv.Name()), m.index+1, len(m.samples), mode, fit, 1<<m.sv.Zoom())
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
	n := len(m.samples)
	m.index = ((idx % n) + n) % n
	s := m.samples[m.index]
	return m.sv.SetSVGData(s.name, s.data)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, m.resize()

	case tea.KeyMsg:
		if demohelp.Quit(msg) {
			_ = m.sv.Close()
			return m, tea.Quit
		}
		if key.Matches(msg, m.sv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "n", "tab":
			return m, m.selectSample(m.index + 1)
		case "p", "shift+tab":
			return m, m.selectSample(m.index - 1)
		case "r":
			m.sv.SetFit(ntsvg.FitContain)
			var modeCmd tea.Cmd
			if m.sv.Mode() != ntsvg.RasterMode {
				modeCmd = m.sv.ToggleMode()
			}
			return m, tea.Batch(modeCmd, m.selectSample(0))
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
	header := headerStyle.Render("SVG SAMPLE GALLERY") + "  " +
		footerStyle.Render("n/p: sample · m: raster/info · f: fit · +/-: zoom · arrows: pan · g: glyph/kitty · q: quit")
	body := boxStyle.Render(m.sv.View().Content)
	status := footerStyle.Width(m.width).Render(m.Summary())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
