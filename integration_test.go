package ntsvg

import (
	"os"
	"path/filepath"
	"testing"

	"nimbleterminal.dev/ntsvg/stage"
)

// repoTestdata locates an SVG fixture regardless of where the test runs.
func repoTestdata(t *testing.T, name string) string {
	t.Helper()
	for _, rel := range []string{
		"testdata/" + name,
		"svg/testdata/" + name,
	} {
		if _, err := os.Stat(rel); err == nil {
			if abs, err := filepath.Abs(rel); err == nil {
				return abs
			}
			return rel
		}
	}
	t.Skipf("testdata fixture %q not found", name)
	return ""
}

func TestLoadSampleFile(t *testing.T) {
	path := repoTestdata(t, "sample.svg")
	m := New(100, 40)

	cmd := m.SetSVG(path)
	if cmd == nil {
		t.Fatal("SetSVG returned nil Cmd")
	}
	m, _ = m.Update(cmd())
	if m.Err() != nil {
		t.Fatalf("load error: %v", m.Err())
	}

	w, h := m.ViewBox()
	if w != 320 || h != 240 {
		t.Errorf("ViewBox() = %g×%g, want 320×240", w, h)
	}
	if m.Title() != "ntsvg sample" {
		t.Errorf("Title() = %q, want %q", m.Title(), "ntsvg sample")
	}

	m = rasterize(t, m)
	if m.Err() != nil {
		t.Fatalf("render error: %v", m.Err())
	}
	if m.sourceImage == nil {
		t.Fatal("sourceImage nil after rasterizing the sample file")
	}
}

func TestLoadMissingFile(t *testing.T) {
	m := New(40, 10)
	cmd := m.SetSVG("testdata/does-not-exist.svg")
	m, _ = m.Update(cmd())
	if m.Err() == nil {
		t.Fatal("expected error loading a missing file")
	}
}

func TestFileSizeLimit(t *testing.T) {
	path := repoTestdata(t, "sample.svg")
	cfg := Config{Cols: 40, Rows: 10, Limits: Limits{MaxFileBytes: 16}}
	m := NewWithConfig(cfg)
	cmd := m.SetSVG(path)
	m, _ = m.Update(cmd())
	if m.Err() == nil {
		t.Fatal("expected MaxFileBytes to reject the sample file")
	}
}

func TestRoundTripStageThroughModel(t *testing.T) {
	st := stage.NewChart(300, 180).
		SetTitle("round trip").
		SetLabels("x", "y").
		AddSeries("v", "", 5, 9).
		BarStage()

	m := New(80, 24)
	cmd := m.ShowStage(st)
	if cmd == nil {
		t.Fatal("ShowStage returned nil Cmd")
	}
	m, _ = m.Update(cmd())
	if m.Err() != nil {
		t.Fatalf("ShowStage load error: %v", m.Err())
	}
	if m.Title() != "round trip" {
		t.Errorf("Title() = %q, want %q", m.Title(), "round trip")
	}
	m = rasterize(t, m)
	if m.sourceImage == nil {
		t.Fatal("generated stage did not rasterize through the Model")
	}
}

func TestRasterizeStage(t *testing.T) {
	st := stage.New(120, 60).
		Background("white").
		Circle(60, 30, 25, stage.Fill("#0a0"))
	img, err := RasterizeStage(st, 200, 200)
	if err != nil {
		t.Fatalf("RasterizeStage: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy()*2 {
		t.Errorf("rasterized stage bounds %v lost the 2:1 aspect", b)
	}

	bar := stage.NewChart(320, 200).
		SetTitle("bars").
		SetLabels("a", "b", "c").
		AddSeries("s1", "", 3, 7, 5).
		AddSeries("s2", "#f80", 6, 2, 8).
		BarStage()
	if _, err := RasterizeStage(bar, 400, 400); err != nil {
		t.Errorf("generated bar chart did not rasterize: %v", err)
	}

	line := stage.NewChart(320, 200).
		SetLabels("jan", "feb", "mar", "apr").
		AddSeries("temp", "", 1, 4, 9, 16).
		LineStage()
	if _, err := RasterizeStage(line, 400, 400); err != nil {
		t.Errorf("generated line chart did not rasterize: %v", err)
	}

	emptyBar := stage.NewChart(200, 120).BarStage()
	if _, err := RasterizeStage(emptyBar, 200, 200); err != nil {
		t.Errorf("empty bar chart did not rasterize: %v", err)
	}
	emptyLine := stage.NewChart(200, 120).LineStage()
	if _, err := RasterizeStage(emptyLine, 200, 200); err != nil {
		t.Errorf("empty line chart did not rasterize: %v", err)
	}
}

func TestSanitizeForTerminal(t *testing.T) {
	cases := map[string]string{
		"plain":            "plain",
		"tab\there":        "tab here",
		"line\nbreak":      "line break",
		"bell\x07char":     "bellchar",
		"‮rvld":            "rvld", // bidi override stripped
		"emoji \U0001F600": "emoji \U0001F600",
	}
	for in, want := range cases {
		if got := SanitizeForTerminal(in); got != want {
			t.Errorf("SanitizeForTerminal(%q) = %q, want %q", in, got, want)
		}
	}
}
