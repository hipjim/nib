package main

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// The logo is a capital N written with a broad-edged pen: the nib is a
// flat edge held at one angle, so a stroke is thick where it runs across
// the edge and a hairline where it runs along it, as in italic capitals.
// It is the letter on the app icon, and greets an empty window.

// logoBox is where the letter lands on its 1024-unit grid, including the
// width of the strokes.
var logoBox = image.Rect(185, 200, 850, 775)

// drawLogo draws the letter in color c, with the grid scaled by scale and
// its origin at origin.
func drawLogo(ops *op.Ops, origin f32.Point, scale float32, c color.NRGBA) {
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(scale, scale)).Offset(origin)).Push(ops).Pop()
	const penAngle = 52 * math.Pi / 180
	edge := f32.Pt(float32(math.Cos(penAngle)), -float32(math.Sin(penAngle)))
	// pen draws one stroke along cubic segments; nib is the edge length
	// at the start, middle and end of the stroke, for tapered swashes.
	pen := func(segs [][4]f32.Point, nib [3]float32) {
		const n = 160
		var left, right [n + 1]f32.Point
		for i := 0; i <= n; i++ {
			t := float32(i) / n
			k := min(len(segs)-1, int(t*float32(len(segs))))
			sg := segs[k]
			p := bezier(sg[0], sg[1], sg[2], sg[3], t*float32(len(segs))-float32(k))
			var l float32
			if t < 0.5 {
				l = nib[0] + (nib[1]-nib[0])*t*2
			} else {
				l = nib[1] + (nib[2]-nib[1])*(t-0.5)*2
			}
			left[i] = f32.Pt(p.X+edge.X*l/2, p.Y+edge.Y*l/2)
			right[i] = f32.Pt(p.X-edge.X*l/2, p.Y-edge.Y*l/2)
		}
		var path clip.Path
		path.Begin(ops)
		path.MoveTo(left[0])
		for _, p := range left[1:] {
			path.LineTo(p)
		}
		for i := n; i >= 0; i-- {
			path.LineTo(right[i])
		}
		path.Close()
		paint.FillShape(ops, c, clip.Outline{Path: path.End()}.Op())
	}
	// The entry swash sweeping up into the left stem.
	pen([][4]f32.Point{
		{f32.Pt(200, 500), f32.Pt(215, 380), f32.Pt(290, 300), f32.Pt(362, 282)},
		{f32.Pt(362, 282), f32.Pt(352, 420), f32.Pt(336, 600), f32.Pt(320, 740)},
	}, [3]float32{24, 74, 74})
	// The diagonal, the heavy stroke of the letter.
	pen([][4]f32.Point{
		{f32.Pt(358, 290), f32.Pt(450, 420), f32.Pt(580, 600), f32.Pt(690, 742)},
	}, [3]float32{74, 78, 74})
	// The right stem up into a flourish that curls over and trails off.
	pen([][4]f32.Point{
		{f32.Pt(690, 742), f32.Pt(698, 600), f32.Pt(706, 420), f32.Pt(714, 286)},
		{f32.Pt(714, 286), f32.Pt(722, 232), f32.Pt(772, 198), f32.Pt(840, 212)},
	}, [3]float32{74, 70, 6})
}

func bezier(a, b, c, d f32.Point, t float32) f32.Point {
	u := 1 - t
	return f32.Pt(u*u*u*a.X+3*u*u*t*b.X+3*u*t*t*c.X+t*t*t*d.X, u*u*u*a.Y+3*u*u*t*b.Y+3*u*t*t*c.Y+t*t*t*d.Y)
}
