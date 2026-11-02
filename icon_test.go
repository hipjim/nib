package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand"
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
	rng := rand.New(rand.NewSource(7))

	// A rounded square on Apple's icon grid: a sheet of cream paper with
	// a grain, a little darker toward the bottom, and a thin edge.
	const inset, radius = 100, 185
	body := image.Rect(inset, inset, size-inset, size-inset)
	paint.FillShape(ops, rgb(0xcfc4ac), clip.UniformRRect(body, radius).Op(ops))
	face := clip.UniformRRect(body.Inset(4), radius-4).Push(ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, inset), Color1: rgb(0xf6efe0),
		Stop2: f32.Pt(0, size-inset), Color2: rgb(0xe9dfc9),
	}.Add(ops)
	paint.PaintOp{}.Add(ops)
	for range 3000 {
		x, y := inset+rng.Intn(size-2*inset), inset+rng.Intn(size-2*inset)
		paint.FillShape(ops, argb(0x0e6b5634), clip.Rect(image.Rect(x, y, x+2, y+2)).Op())
	}
	for range 60 { // a few fibres
		x, y := inset+rng.Intn(size-2*inset), inset+rng.Intn(size-2*inset)
		paint.FillShape(ops, argb(0x0c6b5634), clip.Rect(image.Rect(x, y, x+8+rng.Intn(18), y+1)).Op())
	}

	// A capital N written with a broad-edged pen: the nib is a flat edge
	// held at one angle, so a stroke is thick where it runs across the
	// edge and a hairline where it runs along it, as in italic capitals.
	bez := func(a, b, c, d f32.Point, t float32) f32.Point {
		u := 1 - t
		return f32.Pt(u*u*u*a.X+3*u*u*t*b.X+3*u*t*t*c.X+t*t*t*d.X, u*u*u*a.Y+3*u*u*t*b.Y+3*u*t*t*c.Y+t*t*t*d.Y)
	}
	const penAngle = 52 * math.Pi / 180
	edge := f32.Pt(float32(math.Cos(penAngle)), -float32(math.Sin(penAngle)))
	ink := rgb(0x1d2333) // blue-black
	// pen draws one stroke along cubic segments; nib is the edge length
	// at the start, middle and end of the stroke, for tapered swashes.
	pen := func(segs [][4]f32.Point, nib [3]float32) {
		const n = 160
		var left, right [n + 1]f32.Point
		for i := 0; i <= n; i++ {
			t := float32(i) / n
			k := min(len(segs)-1, int(t*float32(len(segs))))
			sg := segs[k]
			p := bez(sg[0], sg[1], sg[2], sg[3], t*float32(len(segs))-float32(k))
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
		paint.FillShape(ops, ink, clip.Outline{Path: path.End()}.Op())
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
	// A few drops of ink flung off the swashes.
	splat := func(cx, cy float32, count int, spread, maxR float32) {
		for range count {
			a := rng.Float64() * 2 * math.Pi
			d := float32(math.Pow(rng.Float64(), 0.6)) * spread
			x, y := cx+float32(math.Cos(a))*d, cy+float32(math.Sin(a))*d
			r := maxR * float32(math.Pow(rng.Float64(), 2.4))
			if r < 1.5 {
				continue
			}
			paint.FillShape(ops, ink, clip.Ellipse(image.Rect(int(x-r), int(y-r), int(x+r), int(y+r))).Op(ops))
		}
	}
	splat(210, 505, 14, 90, 6)
	splat(842, 215, 16, 90, 5)

	// A red seal in the corner, as a calligrapher signs a sheet.
	seal := image.Rect(792, 762, 866, 836)
	paper := rgb(0xebe1cb)
	paint.FillShape(ops, rgb(0xc8392f), clip.UniformRRect(seal, 10).Op(ops))
	paint.FillShape(ops, paper, clip.UniformRRect(seal.Inset(8), 4).Op(ops))
	paint.FillShape(ops, rgb(0xc8392f), clip.UniformRRect(seal.Inset(16), 3).Op(ops))
	paint.FillShape(ops, paper, clip.Rect(image.Rect(seal.Min.X+16, seal.Min.Y+35, seal.Max.X-16, seal.Min.Y+40)).Op())

	face.Pop()
	_ = color.NRGBA{}

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
