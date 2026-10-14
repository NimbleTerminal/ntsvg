package stage

import (
	"bytes"
	"strings"
	"testing"
)

func TestStageToSVG(t *testing.T) {
	s := New(100, 80).
		Background("white").
		SetTitle("test stage").
		Rect(10, 10, 30, 20, Fill("#f00")).
		Circle(50, 40, 15, Fill("blue").WithOpacity(0.5)).
		Line(0, 0, 100, 80, Stroke("black", 2).WithDash("4 2").WithLineCap("round")).
		Text(50, 70, "hi", Paint{}.WithFont("sans-serif", 10))

	if got := s.Title(); got != "test stage" {
		t.Errorf("Title() = %q, want %q", got, "test stage")
	}

	out := s.ToSVG()
	if s.String() != out {
		t.Error("String() did not match ToSVG()")
	}
	var buf bytes.Buffer
	if _, err := s.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if buf.String() != out {
		t.Error("WriteTo() did not match ToSVG()")
	}

	for _, want := range []string{
		`<svg xmlns="http://www.w3.org/2000/svg"`,
		`viewBox="0 0 100 80"`,
		"<title>test stage</title>",
		`<rect`, `<circle`, `<line`, `<text`,
		`opacity="0.5"`,
		`stroke-dasharray="4 2"`,
		`stroke-linecap="round"`,
		`font-size="10"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ToSVG() missing %q\n---\n%s", want, out)
		}
	}
}

func TestStageEscaping(t *testing.T) {
	s := New(50, 50).Text(0, 10, `a<b>&"c`, Paint{})
	out := s.ToSVG()
	if strings.Contains(out, "<b>") {
		t.Errorf("text content not escaped:\n%s", out)
	}
	if !strings.Contains(out, "&lt;b&gt;") {
		t.Errorf("expected escaped text in:\n%s", out)
	}
}

func TestStageClampsDimensions(t *testing.T) {
	s := New(-5, 0)
	w, h := s.Size()
	if w < 1 || h < 1 {
		t.Errorf("New(-5,0) Size = %g×%g, want >= 1×1", w, h)
	}
}

func TestChartBarStage(t *testing.T) {
	s := NewChart(320, 200).
		SetTitle("bars").
		SetLabels("a", "b", "c").
		AddSeries("s1", "", 3, 7, 5).
		AddSeries("s2", "#f80", 6, 2, 8).
		BarStage()

	out := s.ToSVG()
	if !strings.Contains(out, "<title>bars</title>") {
		t.Errorf("bar chart missing title:\n%s", out)
	}
	if strings.Count(out, "<rect") < 6 {
		t.Errorf("bar chart should have >= 6 bar rects, got:\n%s", out)
	}
}

func TestChartLineStage(t *testing.T) {
	s := NewChart(320, 200).
		SetLabels("jan", "feb", "mar", "apr").
		AddSeries("temp", "", 1, 4, 9, 16).
		LineStage()

	out := s.ToSVG()
	if !strings.Contains(out, "<polyline") {
		t.Errorf("line chart missing polyline:\n%s", out)
	}
}

func TestEmptyChartIsValid(t *testing.T) {
	s := NewChart(200, 120).BarStage()
	if !strings.Contains(s.ToSVG(), "<svg") {
		t.Error("empty bar chart did not emit <svg>")
	}
	s = NewChart(200, 120).LineStage()
	if !strings.Contains(s.ToSVG(), "<svg") {
		t.Error("empty line chart did not emit <svg>")
	}
}
