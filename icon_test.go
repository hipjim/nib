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

	ink := rgb(0x1d2333) // blue-black
	drawLogo(ops, f32.Point{}, 1, ink)
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
