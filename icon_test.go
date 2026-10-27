package main

import (
	"image"
	"image/png"
	"math"
	"os"
	"testing"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// TestIcon draws the app icon. Set NIB_ICON=appicon.png to write it.
func TestIcon(t *testing.T) {
	out := os.Getenv("NIB_ICON")
	if out == "" {
		t.Skip("set NIB_ICON to draw the icon")
	}
	const size = 1024
	win, err := headless.NewWindow(size, size)
	if err != nil {
		t.Skip("no GPU:", err)
	}
	defer win.Release()
	ops := new(op.Ops)

	// A rounded square on Apple's icon grid, with a thin edge and a soft
	// top-to-bottom shade.
	const inset, radius = 100, 185
	body := image.Rect(inset, inset, size-inset, size-inset)
	paint.FillShape(ops, rgb(0x2a323d), clip.UniformRRect(body, radius).Op(ops))
	st := clip.UniformRRect(body.Inset(5), radius-5).Push(ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, inset), Color1: rgb(0x1a2029),
		Stop2: f32.Pt(0, size-inset), Color2: rgb(0x07090c),
	}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()

	// A fountain-pen nib, drawn pointing down around (0, 0) and turned to
	// write toward the lower left.
	tr := op.Affine(f32.Affine2D{}.Rotate(f32.Pt(0, 0), math.Pi/4).Offset(f32.Pt(size/2+10, size/2-10))).Push(ops)
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(-150, -230))
	p.LineTo(f32.Pt(150, -230))
	p.QuadTo(f32.Pt(190, -60), f32.Pt(110, 70))
	p.LineTo(f32.Pt(0, 290))
	p.LineTo(f32.Pt(-110, 70))
	p.QuadTo(f32.Pt(-190, -60), f32.Pt(-150, -230))
	p.Close()
	st = clip.Outline{Path: p.End()}.Op().Push(ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, -230), Color1: rgb(0x6ff5c4),
		Stop2: f32.Pt(0, 290), Color2: rgb(0x22c98d),
	}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
	// The collar, the breather hole and the slit to the tip.
	cut := rgb(0x0d1014)
	paint.FillShape(ops, cut, clip.Rect(image.Rect(-160, -175, 160, -160)).Op())
	paint.FillShape(ops, cut, clip.Ellipse(image.Rect(-38, -68, 38, 8)).Op(ops))
	paint.FillShape(ops, cut, clip.Rect(image.Rect(-7, -30, 7, 292)).Op())
	tr.Pop()

	if err := win.Frame(ops); err != nil {
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
