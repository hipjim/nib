package main

import (
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// TestRender draws the editor offscreen. Set QUOD_SHOT=out.png to save it.
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
	if os.Getenv("QUOD_FIND") != "" {
		e.openPrompt(promptFind, "")
		e.setInput(os.Getenv("QUOD_FIND"))
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
