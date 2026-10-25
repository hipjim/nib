package main

import (
	"image"
	"image/png"
	"os"
	"testing"

	"gioui.org/font"
	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
)

// TestIcon draws the app icon. Set QUOD_ICON=appicon.png to write it.
func TestIcon(t *testing.T) {
	out := os.Getenv("QUOD_ICON")
	if out == "" {
		t.Skip("set QUOD_ICON to draw the icon")
	}
	const size = 1024
	win, err := headless.NewWindow(size, size)
	if err != nil {
		t.Skip("no GPU:", err)
	}
	defer win.Release()
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(size, size)),
	}

	// A rounded square on Apple's icon grid, with a thin edge.
	const inset, radius = 100, 185
	body := image.Rect(inset, inset, size-inset, size-inset)
	paint.FillShape(gtx.Ops, rgb(0x232a33), clip.UniformRRect(body, radius).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, colBg, clip.UniformRRect(body.Inset(6), radius-6).Op(gtx.Ops))

	// A bold "q" and a block cursor after it.
	shaper := text.NewShaper(text.NoSystemFonts(), text.WithCollection(fontCollection()))
	f := font.Font{Typeface: typeface, Weight: font.Bold}
	m := op.Record(gtx.Ops)
	paint.ColorOp{Color: colAccent}.Add(gtx.Ops)
	col := m.Stop()
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(size, size)}
	m = op.Record(gtx.Ops)
	dims := widget.Label{MaxLines: 1}.Layout(g, shaper, f, 620, "q", col)
	call := m.Stop()
	// Center the mark from the top of the "q" to the end of its tail; the
	// cursor runs from the x-height to the baseline.
	const xHeight, descent, curW, gap = 0.55 * 620, 0.2 * 620, 170, 36
	base := size/2 + int(xHeight-descent)/2
	x := (size - dims.Size.X - gap - curW) / 2
	st := op.Offset(image.Pt(x, base-(dims.Size.Y-dims.Baseline))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	st.Pop()
	cx := x + dims.Size.X + gap
	paint.FillShape(gtx.Ops, colAccent, clip.Rect(image.Rect(cx, base-int(xHeight), cx+curW, base)).Op())

	if err := win.Frame(&ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	if err := win.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	fo, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer fo.Close()
	png.Encode(fo, img)
}
