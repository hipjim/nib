package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestRender draws the editor offscreen. Set NIB_SHOT=out.png to save it,
// NIB_FILE to pick the file or folder and NIB_LINE=line[:col] to put the
// cursor there. NIB_FINDER=query opens the file finder, NIB_INFO=1 the properties,
// NIB_GREP=query the folder search and NIB_REPLACE=old:new the replace
// prompt, NIB_KEYS=1 the key reference (NIB_KEYS=end scrolled to its end), NIB_TABS=a,b opens more files (NIB_TABX=1
// hovers the first tab's ×) and NIB_FILE=- an empty scratch buffer. NIB_LIGHT=1
// uses the light theme and NIB_WS=1 shows whitespace.
func TestRender(t *testing.T) {
	out := os.Getenv("NIB_SHOT")
	if out == "" {
		t.Skip("set NIB_SHOT to render a screenshot")
	}
	img := renderScene(t, os.LookupEnv)
	if err := writePNG(out, img); err != nil {
		t.Fatal(err)
	}
}

// goldenScenes are the screens TestGolden renders, as the NIB_* settings
// TestRender takes. They show a generated file, so editing the editor's own
// sources does not change them.
var goldenScenes = []struct {
	name string
	env  map[string]string
}{
	{"empty", map[string]string{"NIB_FILE": "-"}},
	{"code", nil},
	{"light", map[string]string{"NIB_LIGHT": "1"}},
	{"whitespace", map[string]string{"NIB_WS": "1"}},
	{"tabs", map[string]string{"NIB_TABS": "tabs.go,panels.go,docs.go", "NIB_TABX": "1"}},
	{"keys", map[string]string{"NIB_KEYS": "1"}},
	{"keys-end", map[string]string{"NIB_KEYS": "end"}},
	{"find", map[string]string{"NIB_FIND": "func"}},
	{"replace", map[string]string{"NIB_REPLACE": "func:fn"}},
}

// TestGolden renders goldenScenes and compares each with <name>.png in the
// folder NIB_GOLDEN. A scene with no PNG there yet is written, so the first
// run records the baseline on this machine; later runs fail on any scene
// whose pixels changed and write <name>.diff.png beside it, the new frame
// with the changed pixels in red. NIB_GOLDEN_UPDATE=1 rewrites every PNG.
//
// Baselines belong to one machine: fonts and GPUs rasterize differently,
// so they are not checked in. Record them before a UI change, make the
// change and run again to see exactly what moved.
func TestGolden(t *testing.T) {
	dir := os.Getenv("NIB_GOLDEN")
	if dir == "" {
		t.Skip("set NIB_GOLDEN to a folder to compare screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	update := os.Getenv("NIB_GOLDEN_UPDATE") != ""
	sample := filepath.Join(t.TempDir(), "sample.go")
	if err := os.WriteFile(sample, []byte(sampleSource()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, sc := range goldenScenes {
		t.Run(sc.name, func(t *testing.T) {
			env := map[string]string{"NIB_FILE": sample}
			for k, v := range sc.env {
				env[k] = v
			}
			lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
			got := renderScene(t, lookup)
			path := filepath.Join(dir, sc.name+".png")
			want, err := readPNG(path)
			if update || os.IsNotExist(err) {
				if err := writePNG(path, got); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			n, r, diff := diffImages(want, got)
			if n == 0 {
				return
			}
			diffPath := filepath.Join(dir, sc.name+".diff.png")
			if err := writePNG(diffPath, diff); err != nil {
				t.Fatal(err)
			}
			t.Errorf("%d pixels changed in %v; see %s", n, r, diffPath)
		})
	}
}

// renderScene lays the editor out offscreen as the NIB_* settings from
// lookup ask, and returns the frame.
func renderScene(t *testing.T, lookup func(string) (string, bool)) *image.RGBA {
	t.Helper()
	get := func(k string) string { v, _ := lookup(k); return v }
	const w, h = 1400, 900
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip("no GPU:", err)
	}
	defer win.Release()

	e := NewEditor(orDefault(get("NIB_FILE"), "editor.go"))
	if get("NIB_FILE") == "-" {
		e = NewEditor("")
	}
	e.focused = true
	e.setLight(get("NIB_LIGHT") != "")
	e.showWS = get("NIB_WS") != ""
	defer applyTheme(false)
	e.cursor = Pos{98, 12}
	e.anchor = Pos{96, 2}
	if l, c, _ := strings.Cut(get("NIB_LINE"), ":"); l != "" {
		n, _ := strconv.Atoi(l)
		col, _ := strconv.Atoi(c)
		e.cursor, e.anchor = Pos{n - 1, col}, Pos{n - 1, col}
	}
	e.followCursor = true
	var ops op.Ops
	now := time.Now()
	e.blinkStart = now
	gtx := layout.Context{
		Ops:         &ops,
		Now:         now,
		Metric:      unit.Metric{PxPerDp: 2, PxPerSp: 2},
		Constraints: layout.Exact(image.Pt(w, h)),
	}
	e.Layout(gtx)
	for range 3 {
		ops.Reset()
		start := time.Now()
		e.Layout(gtx)
		t.Logf("layout: %v", time.Since(start))
	}
	if tabs := get("NIB_TABS"); tabs != "" {
		first := e.doc
		for _, p := range strings.Split(tabs, ",") {
			e.open(p)
		}
		e.insert("x")
		e.show(first)
		if get("NIB_TABX") != "" {
			e.tabHover, e.tabClose = first, first
		}
		ops.Reset()
		e.Layout(gtx)
	}
	if q, ok := lookup("NIB_FINDER"); ok {
		e.openFinder()
		for {
			if _, walking := e.finder.poll(); !walking {
				break
			}
			time.Sleep(time.Millisecond)
		}
		e.setInput(q)
		ops.Reset()
		e.Layout(gtx)
	}
	if get("NIB_INFO") != "" {
		e.info = true
		ops.Reset()
		e.Layout(gtx)
	}
	if v := get("NIB_KEYS"); v != "" {
		e.keys = true
		if v == "end" {
			e.keysY = 1 << 30
		}
		ops.Reset()
		e.Layout(gtx)
	}
	if get("NIB_FIND") != "" {
		e.openPrompt(promptFind, "")
		e.setInput(get("NIB_FIND"))
		ops.Reset()
		e.Layout(gtx)
	}
	if q, ok := lookup("NIB_GREP"); ok {
		e.openGrep()
		e.setInput(q)
		for {
			if _, running := e.grep.poll(); !running {
				break
			}
			time.Sleep(time.Millisecond)
		}
		ops.Reset()
		e.Layout(gtx)
	}
	if r := get("NIB_REPLACE"); r != "" {
		q, with, _ := strings.Cut(r, ":")
		e.openReplace()
		e.setInput(q)
		e.submitPrompt()
		e.setInput(with)
		ops.Reset()
		e.Layout(gtx)
	}
	if err := win.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// sampleSource is a Go file of a few hundred lines for the golden scenes,
// with comments, strings, numbers and keywords for the highlighter.
func sampleSource() string {
	var sb strings.Builder
	sb.WriteString("package sample\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\n")
	for i := 0; sb.Len() < 6000; i++ {
		fmt.Fprintf(&sb, "// Shape%d describes a shape with %d sides.\ntype Shape%d struct {\n\tName  string\n\tSides int // %d\n}\n\n", i, i+3, i, i+3)
		fmt.Fprintf(&sb, "func (s Shape%d) String() string {\n\tif s.Sides > %d {\n\t\treturn fmt.Sprintf(\"%%s: %%d sides\", s.Name, s.Sides)\n\t}\n\treturn strings.ToUpper(s.Name) + \" (%d)\"\n}\n\n", i, i*2, i)
	}
	return sb.String()
}

func readPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// diffImages counts the pixels that differ between want and got, with the
// rectangle enclosing them, and returns got with those pixels in red.
func diffImages(want, got image.Image) (n int, r image.Rectangle, diff *image.RGBA) {
	b := got.Bounds()
	diff = image.NewRGBA(b)
	draw.Draw(diff, b, got, b.Min, draw.Src)
	if want.Bounds() != b {
		return b.Dx() * b.Dy(), b, diff
	}
	red := color.RGBA{255, 0, 0, 255}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !sameColor(want.At(x, y), got.At(x, y)) {
				if n == 0 {
					r = image.Rect(x, y, x+1, y+1)
				} else {
					r = r.Union(image.Rect(x, y, x+1, y+1))
				}
				n++
				diff.SetRGBA(x, y, red)
			}
		}
	}
	return n, r, diff
}

func sameColor(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}
