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
	"gioui.org/io/system"
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

// version is set for releases with -ldflags "-X main.version=v1.2.3".
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version // go install github.com/hipjim/quod@v1.2.3
	}
	return "dev"
}

const usage = `usage: quod [file or folder]

With no argument, quod reopens the files of the last session.
See https://github.com/hipjim/quod for the keys.
`

func main() {
	path := ""
	if len(os.Args) > 1 {
		path = os.Args[1]
		switch path {
		case "-v", "-version", "--version":
			fmt.Println("quod", versionString())
			return
		case "-h", "-help", "--help":
			fmt.Print(usage)
			return
		}
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

	// Load the file and session while the window is being created.
	ready := make(chan *Editor, 1)
	go func() {
		debug.SetPanicOnFault(true)
		ed := NewEditor(path)
		ed.sessionPath = sessionFile()
		if path == "" {
			ed.restoreSession()
		} else {
			ed.keepSession()
		}
		ed.WarmUp()
		ready <- ed
	}()
	var ed *Editor
	editor := func() *Editor {
		if ed == nil {
			ed = <-ready
			ed.SetTitle = func(t string) { w.Option(app.Title(t)) }
			ed.Quit = func() { w.Perform(system.ActionClose) }
			invalidate := w.Invalidate
			wake.Store(&invalidate)
			go ed.watch.run(w.Invalidate)
		}
		return ed
	}
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case *app.ClosingEvent:
			if !editor().MayClose() {
				e.Abort()
				w.Invalidate()
			}
		case app.DestroyEvent:
			editor().saveSession(true)
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			editor().Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}
