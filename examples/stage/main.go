// Immediate-mode vector generation with nimbleterminal.dev/ntsvg/stage bridged into ntsvg.Model.
package main

import (
	"fmt"
	"math"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntsvg"
	"nimbleterminal.dev/ntsvg/examples/internal/demohelp"
	"nimbleterminal.dev/ntsvg/stage"
)

type palette struct {
	name                   string
	bg, fg, c1, c2, c3, c4 string
}

var palettes = []palette{
	{"midnight", "#0d1b2a", "#e4e8ee", "#e15759", "#4ea8de", "#8de3b6", "#f28e2b"},
	{"solar", "#1a1423", "#fff3b0", "#ff6b6b", "#ffd166", "#06d6a0", "#a8f9ff"},
	{"emerald", "#081c15", "#d8f3dc", "#52b788", "#95d5b2", "#ffd166", "#f4a261"},
}

var sceneNames = []string{"primitives", "bezier-waves", "rosette", "telemetry-hud"}

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#8de3b6"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

type model struct {
	sv               ntsvg.Model
	scene, pal, step int
	width, height    int
	preferKitty      bool
}

func newModel() model {
	cfg := ntsvg.Config{}
	if runtime.GOOS == "js" {
		cfg.PictureConfig.KittyMedium = picture.KittyMediumSharedMemory
	}
	m := model{
		sv:          ntsvg.NewWithConfig(cfg),
		preferKitty: true,
	}
	return m
}

func (m model) buildStage() *stage.Stage {
	p := palettes[m.pal]
	s := stage.New(360, 240).Background(p.bg)
	switch m.scene {
	case 0:
		s.SetTitle("Stage Primitives")
		r := 42.0 + float64(m.step)*3.0
		s.Circle(86, 86, r, stage.Fill(p.c1).WithOpacity(0.88))
		s.Circle(138, 122, r, stage.Fill(p.c2).WithOpacity(0.78))
		s.Ellipse(106, 156, r*1.1, r*0.8, stage.Fill(p.c3).WithOpacity(0.72))
		s.RoundRect(212, 42, 118, 64, 12, 12, stage.Fill(p.c4).WithStroke(p.fg, 2))
		s.Polygon([]stage.Point{{X: 270, Y: 124}, {X: 330, Y: 212}, {X: 210, Y: 212}},
			stage.Fill(p.c3).WithStroke(p.fg, 1.5))
		s.Polyline([]stage.Point{
			{X: 22, Y: 222},
			{X: 88, Y: 190 - float64(m.step)*4},
			{X: 156, Y: 214},
			{X: 224, Y: 172 + float64(m.step)*4},
			{X: 338, Y: 204},
		}, stage.Stroke(p.c2, 4).WithLineCap("round"))
		s.Text(180, 26, "stage.Stage · Immediate Primitives",
			stage.Fill(p.fg).WithFont("sans-serif", 15).WithAnchor("middle"))

	case 1:
		s.SetTitle("Bezier Waves")
		count := 5 + max(0, m.step)
		for i := 0; i < count; i++ {
			t := float64(i) / float64(max(1, count-1))
			y := 56.0 + t*140.0
			amp := 36.0 + float64(m.step)*6.0 + float64(i%2)*18.0
			d := fmt.Sprintf("M 24 %.1f C 110 %.1f, 250 %.1f, 336 %.1f", y, y-amp, y+amp, y)
			color := p.c2
			if i%3 == 1 {
				color = p.c3
			} else if i%3 == 2 {
				color = p.c4
			}
			paint := stage.Stroke(color, 3.5).WithLineCap("round")
			if i%2 == 1 {
				paint = paint.WithDash("10 6")
			}
			s.Path(d, paint)
		}
		s.Text(180, 26, "stage.Stage · Cubic Bezier Splines",
			stage.Fill(p.fg).WithFont("sans-serif", 15).WithAnchor("middle"))

	case 2:
		s.SetTitle("Geometric Rosette")
		cx, cy := 180.0, 130.0
		spokes := 8 + m.step*2
		if spokes < 4 {
			spokes = 4
		}
		for i := 0; i < spokes; i++ {
			a := (2 * math.Pi * float64(i)) / float64(spokes)
			x2 := cx + math.Cos(a)*86
			y2 := cy + math.Sin(a)*86
			s.Line(cx, cy, x2, y2, stage.Stroke(p.c2, 2).WithOpacity(0.75))
			s.Circle(x2, y2, 12, stage.Fill(p.c4).WithOpacity(0.85))
		}
		s.Circle(cx, cy, 52, stage.Stroke(p.c3, 3).WithDash("8 5"))
		s.Circle(cx, cy, 24, stage.Fill(p.c1).WithStroke(p.fg, 2))
		s.Text(180, 26, fmt.Sprintf("Procedural Rosette (%d spokes)", spokes),
			stage.Fill(p.fg).WithFont("sans-serif", 15).WithAnchor("middle"))

	default:
		s.SetTitle("Vector HUD Card")
		s.RoundRect(20, 40, 320, 180, 10, 10, stage.Stroke(p.c2, 2))
		s.Line(20, 74, 340, 74, stage.Stroke(p.c2, 1).WithOpacity(0.5))
		s.Text(36, 62, "VECTOR TELEMETRY HUD", stage.Fill(p.c3).WithFont("monospace", 13))
		for i := 0; i < 4; i++ {
			w := 44.0 + float64(((i+1)*37+m.step*19)%120)
			y := 92.0 + float64(i)*28.0
			s.Rect(36, y, 140, 16, stage.Fill(p.fg).WithOpacity(0.12))
			s.Rect(36, y, w, 16, stage.Fill(p.c3))
		}
		s.Polyline([]stage.Point{
			{X: 200, Y: 188},
			{X: 224, Y: 152 - float64(m.step)*5},
			{X: 250, Y: 168},
			{X: 276, Y: 118 + float64(m.step)*4},
			{X: 302, Y: 134},
			{X: 324, Y: 98},
		}, stage.Stroke(p.c4, 3.5).WithLineCap("round"))
		s.Text(180, 24, "stage.Stage · Telemetry Card",
			stage.Fill(p.fg).WithFont("sans-serif", 15).WithAnchor("middle"))
	}
	return s
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.sv.Init(), m.sv.ShowStage(m.buildStage()))
}

func (m model) Help() []key.Binding {
	return []key.Binding{
		demohelp.Key("Next scene", "s", "tab"),
		demohelp.Key("Palette", "c"),
		demohelp.Key("More detail", "up", "k", "]"),
		demohelp.Key("Less detail", "down", "j", "["),
		demohelp.Key("Raster / Info", "m", "t"),
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
	return fmt.Sprintf("Stage: scene=%s palette=%s step=%d mode=%s",
		sceneNames[m.scene], palettes[m.pal].name, m.step, mode)
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
			_ = m.sv.Close()
			return m, tea.Quit
		}
		if key.Matches(msg, m.sv.KeyMap().ToggleRender) {
			m.preferKitty = false
		}
		switch msg.String() {
		case "s", "tab":
			m.scene = (m.scene + 1) % len(sceneNames)
			return m, m.sv.ShowStage(m.buildStage())
		case "c":
			m.pal = (m.pal + 1) % len(palettes)
			return m, m.sv.ShowStage(m.buildStage())
		case "up", "k", "]":
			m.step = min(4, m.step+1)
			return m, m.sv.ShowStage(m.buildStage())
		case "down", "j", "[":
			m.step = max(-2, m.step-1)
			return m, m.sv.ShowStage(m.buildStage())
		case "r":
			m.scene, m.pal, m.step = 0, 0, 0
			var modeCmd tea.Cmd
			if m.sv.Mode() != ntsvg.RasterMode {
				modeCmd = m.sv.ToggleMode()
			}
			return m, tea.Batch(modeCmd, m.sv.ShowStage(m.buildStage()))
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
	header := headerStyle.Render("IMMEDIATE-MODE VECTOR STAGE") + "  " +
		footerStyle.Render("s: scene · c: palette · ↑/↓: detail · m: raster/info · g: glyph/kitty · r: reset · q: quit")
	body := boxStyle.Render(m.sv.View().Content)
	status := footerStyle.Width(m.width).Render(m.Summary())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body, status))
	v.AltScreen = true
	return v
}

func main() { demohelp.Main(newModel()) }
