package main

import (
	"image"
	"image/color"
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
	face := clip.UniformRRect(body.Inset(5), radius-5).Push(ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, inset), Color1: rgb(0x161c25),
		Stop2: f32.Pt(0, size-inset), Color2: rgb(0x05070a),
	}.Add(ops)
	paint.PaintOp{}.Add(ops)

	// Three lines of text the nib has written, fading out to the left.
	for i, w := range []int{300, 210, 250} {
		y := 215 + i*62
		r := image.Rect(170, y, 170+w, y+22)
		paint.FillShape(ops, argb(0x12ffffff), clip.UniformRRect(r, 11).Op(ops))
	}

	// A soft glow where the nib touches the page.
	tip := image.Pt(317, 707)
	for i := range 60 {
		r := 230 - i*3
		paint.FillShape(ops, argb(0x013ee6a8), clip.Ellipse(image.Rect(tip.X-r, tip.Y-r, tip.X+r, tip.Y+r)).Op(ops))
	}

	// The ink: a stroke from the tip that swells and fades out along the
	// bottom, built as an outline around a sampled centerline.
	curve := func(t float32) f32.Point {
		q := func(a, b, c f32.Point, t float32) f32.Point {
			u := 1 - t
			return f32.Pt(u*u*a.X+2*u*t*b.X+t*t*c.X, u*u*a.Y+2*u*t*b.Y+t*t*c.Y)
		}
		if t < 0.5 {
			return q(f32.Pt(317, 707), f32.Pt(262, 850), f32.Pt(560, 862), t*2)
		}
		return q(f32.Pt(560, 862), f32.Pt(790, 850), f32.Pt(905, 760), t*2-1)
	}
	const n = 80
	var left, right [n + 1]f32.Point
	for i := 0; i <= n; i++ {
		t := float32(i) / n
		p, q := curve(t), curve(min(1, t+0.01))
		d := q.Sub(p)
		l := float32(math.Hypot(float64(d.X), float64(d.Y)))
		nx, ny := -d.Y/l, d.X/l
		w := 26 * float32(math.Pow(float64(1-t), 0.7)) * min(1, t*3)
		left[i], right[i] = f32.Pt(p.X+nx*w, p.Y+ny*w), f32.Pt(p.X-nx*w, p.Y-ny*w)
	}
	var ink clip.Path
	ink.Begin(ops)
	ink.MoveTo(left[0])
	for _, p := range left[1:] {
		ink.LineTo(p)
	}
	for i := n; i >= 0; i-- {
		ink.LineTo(right[i])
	}
	ink.Close()
	st := clip.Outline{Path: ink.End()}.Op().Push(ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(317, 707), Color1: rgb(0xa8ffdc),
		Stop2: f32.Pt(905, 760), Color2: argb(0x0022c98d),
	}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
	face.Pop()

	// A fountain-pen nib, drawn pointing down around (0, 0) and turned to
	// write toward the lower left.
	tr := op.Affine(f32.Affine2D{}.Rotate(f32.Pt(0, 0), math.Pi/4).Offset(f32.Pt(size/2+10, size/2-10))).Push(ops)
	outline := func() clip.PathSpec {
		var p clip.Path
		p.Begin(ops)
		p.MoveTo(f32.Pt(-150, -230))
		p.LineTo(f32.Pt(150, -230))
		p.QuadTo(f32.Pt(190, -60), f32.Pt(110, 70))
		p.LineTo(f32.Pt(0, 290))
		p.LineTo(f32.Pt(-110, 70))
		p.QuadTo(f32.Pt(-190, -60), f32.Pt(-150, -230))
		p.Close()
		return p.End()
	}
	// A drop shadow under the nib.
	sh := op.Offset(image.Pt(14, 22)).Push(ops)
	paint.FillShape(ops, argb(0x66000000), clip.Outline{Path: outline()}.Op())
	sh.Pop()
	// The right half is in shadow, the left catches the light: a fold down
	// the slit.
	shade := func(r image.Rectangle, top, bottom color.NRGBA) {
		half := clip.Rect(r).Push(ops)
		st := clip.Outline{Path: outline()}.Op().Push(ops)
		paint.LinearGradientOp{Stop1: f32.Pt(0, -230), Color1: top, Stop2: f32.Pt(0, 290), Color2: bottom}.Add(ops)
		paint.PaintOp{}.Add(ops)
		st.Pop()
		half.Pop()
	}
	shade(image.Rect(-200, -240, 0, 300), rgb(0x8dfad1), rgb(0x2fd69a))
	shade(image.Rect(0, -240, 200, 300), rgb(0x4fdfa8), rgb(0x179c70))
	// The collar: a darker band with a bright rim.
	band := clip.Rect(image.Rect(-200, -230, 200, -165)).Push(ops)
	st = clip.Outline{Path: outline()}.Op().Push(ops)
	paint.LinearGradientOp{Stop1: f32.Pt(-150, 0), Color1: rgb(0x3bcf98), Stop2: f32.Pt(150, 0), Color2: rgb(0x1f9c70)}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
	band.Pop()
	paint.FillShape(ops, argb(0xb0ffffff), clip.Rect(image.Rect(-150, -230, 150, -224)).Op())
	paint.FillShape(ops, rgb(0x0d1014), clip.Rect(image.Rect(-160, -168, 160, -156)).Op())
	// A thin bright edge along the lit side.
	st = clip.Stroke{Path: outline(), Width: 7}.Op().Push(ops)
	paint.LinearGradientOp{Stop1: f32.Pt(-150, -230), Color1: argb(0xc0ffffff), Stop2: f32.Pt(60, 290), Color2: argb(0x00ffffff)}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
	// The breather hole and the slit to the tip.
	cut := rgb(0x0d1014)
	paint.FillShape(ops, cut, clip.Ellipse(image.Rect(-40, -70, 40, 10)).Op(ops))
	paint.FillShape(ops, argb(0x50ffffff), clip.Ellipse(image.Rect(-30, -62, 10, -40)).Op(ops))
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
