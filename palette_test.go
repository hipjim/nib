package main

import (
	"strings"
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"
)

// Typing narrows the palette to the commands that match, best first, and
// Enter runs the one picked.
func TestPalette(t *testing.T) {
	e := newTestEditor("a\nb\n", Pos{})
	e.lineH, e.viewH = 20, 400
	e.key(layout.Context{}, keyEvent("P", true), false)
	if e.prompt != promptPalette || len(e.palette.results) != len(e.palette.all) {
		t.Fatalf("prompt %v, %d of %d listed", e.prompt, len(e.palette.results), len(e.palette.all))
	}
	e.setInput("thm")
	if p := &e.palette; len(p.results) == 0 || !strings.HasSuffix(p.all[p.results[0]].name, " theme") {
		t.Fatalf("thm → %v", p.results)
	}
	light := e.light
	e.key(layout.Context{}, key.Event{Name: key.NameReturn}, false)
	if e.prompt != promptNone || e.light == light {
		t.Fatalf("after Enter: prompt %v light %v", e.prompt, e.light)
	}

	// ↑ walks up the list, ⌘⇧P again or Esc closes it, and no match says so.
	e.openPalette()
	e.setInput("line")
	e.key(layout.Context{}, key.Event{Name: key.NameUpArrow}, false)
	if e.palette.sel != 1 {
		t.Fatalf("sel %d", e.palette.sel)
	}
	e.key(layout.Context{}, keyEvent("P", true), false)
	if e.prompt != promptNone {
		t.Fatal("⌘⇧P did not close the palette")
	}
	e.openPalette()
	e.setInput("zzzz")
	if len(e.palette.results) != 0 {
		t.Fatalf("zzzz matched %v", e.palette.results)
	}
	e.key(layout.Context{}, key.Event{Name: key.NameEscape}, false)
	if e.prompt != promptNone {
		t.Fatal("Esc did not close the palette")
	}
}

// Every command has a name, a key and something to run.
func TestPaletteKeys(t *testing.T) {
	e := newTestEditor("", Pos{})
	for _, c := range e.commands() {
		if c.name == "" || c.keys == "" || c.run == nil {
			t.Errorf("command %+v incomplete", c)
		}
	}
}
