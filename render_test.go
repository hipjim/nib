package main

import (
	"image"
	"image/png"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestRender draws the editor offscreen. Set QUOD_SHOT=out.png to save it,
// QUOD_FILE to pick the file or folder and QUOD_LINE to put the cursor on a
// line. QUOD_FINDER=query opens the file finder, QUOD_INFO=1 the properties,
// QUOD_GREP=query the folder search and QUOD_REPLACE=old:new the replace
// prompt.
func TestRender(t *testing.T) {
	out := os.Getenv("QUOD_SHOT")
	if out == "" {
		t.Skip("set QUOD_SHOT to render a screenshot")
	}
	const w, h = 1400, 900
	win, err := headless.NewWindow(w, h)
	if err != nil {
		t.Skip("no GPU:", err)
	}
	defer win.Release()

	e := NewEditor(orDefault(os.Getenv("QUOD_FILE"), "editor.go"))
	e.focused = true
	e.cursor = Pos{98, 12}
	e.anchor = Pos{96, 2}
	if n, err := strconv.Atoi(os.Getenv("QUOD_LINE")); err == nil {
		e.cursor, e.anchor = Pos{n - 1, 0}, Pos{n - 1, 0}
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
	if q, ok := os.LookupEnv("QUOD_FINDER"); ok {
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
	if os.Getenv("QUOD_INFO") != "" {
		e.info = true
		ops.Reset()
		e.Layout(gtx)
	}
	if os.Getenv("QUOD_FIND") != "" {
		e.openPrompt(promptFind, "")
		e.setInput(os.Getenv("QUOD_FIND"))
		ops.Reset()
		e.Layout(gtx)
	}
	if q, ok := os.LookupEnv("QUOD_GREP"); ok {
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
	if r := os.Getenv("QUOD_REPLACE"); r != "" {
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
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, img)
}
