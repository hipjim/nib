// Command quod is a small, fast, GPU-rendered text editor.
package main

import (
	_ "embed"
	"fmt"
	"os"
	"runtime/debug"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/op"
	"gioui.org/unit"
)

// JetBrains Mono NL (no ligatures), licensed under the OFL; see fonts/OFL.txt.
var (
	//go:embed fonts/JetBrainsMonoNL-Regular.ttf
	monoRegular []byte
	//go:embed fonts/JetBrainsMonoNL-Bold.ttf
	monoBold []byte
)

const typeface = "JetBrains Mono NL"

func fontCollection() []font.FontFace {
	var faces []font.FontFace
	for _, ttf := range [][]byte{monoRegular, monoBold} {
		f, err := opentype.ParseCollection(ttf)
		if err != nil {
			panic(err)
		}
		faces = append(faces, f[0])
	}
	return faces
}

func main() {
	path := ""
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	go func() {
		w := new(app.Window)
		w.Option(app.Title("quod"), app.Size(unit.Dp(1000), unit.Dp(700)))
		if err := run(w, path); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window, path string) error {
	// Let a truncated memory-mapped file panic instead of crashing; the
	// editor recovers from it.
	debug.SetPanicOnFault(true)
	ed := NewEditor(path)
	ed.SetTitle = func(t string) { w.Option(app.Title(t)) }
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			ed.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}
