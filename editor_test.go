package main

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
)

func newTestEditor(text string, cur Pos) *Editor {
	e := &Editor{doc: newDoc("", NewBuffer(text))}
	e.cursor, e.anchor = cur, cur
	return e
}

func content(e *Editor) string { return e.buf.Text(Pos{}, e.buf.End()) }

func TestNewlineIndent(t *testing.T) {
	e := newTestEditor("\tif x {}", Pos{0, 7})
	e.newline()
	if got := content(e); got != "\tif x {\n\t\t\n\t}" {
		t.Fatalf("got %q", got)
	}
	if e.cursor != (Pos{1, 2}) {
		t.Fatalf("cursor %v", e.cursor)
	}
	e.insert("y")
	e.undo(false)
	e.undo(false)
	if got := content(e); got != "\tif x {}" {
		t.Fatalf("undo got %q", got)
	}
}

func TestSoftTabBackspace(t *testing.T) {
	e := newTestEditor("x\n      y", Pos{1, 6})
	e.backspace(0)
	if got := e.buf.Line(1); got != "    y" {
		t.Fatalf("got %q", got)
	}
}

func TestWordBackspace(t *testing.T) {
	e := newTestEditor("foo bar.baz", Pos{0, 11})
	e.backspace(key.ModShortcutAlt)
	e.backspace(key.ModShortcutAlt)
	if got := content(e); got != "foo bar" {
		t.Fatalf("got %q", got)
	}
}

func TestIndentBlock(t *testing.T) {
	e := newTestEditor("a\n  b\nc", Pos{0, 1})
	e.buf.UseTabs = false
	e.cursor = Pos{2, 1}
	e.indent(1)
	if got := content(e); got != "    a\n      b\n    c" {
		t.Fatalf("indent %q", got)
	}
	e.indent(-1)
	e.indent(-1)
	if got := content(e); got != "a\nb\nc" {
		t.Fatalf("dedent %q", got)
	}
}

func TestMoveDuplicateDeleteLines(t *testing.T) {
	e := newTestEditor("1\n2\n3", Pos{2, 0})
	e.moveLines(-1)
	if got := content(e); got != "1\n3\n2" || e.cursor.Line != 1 {
		t.Fatalf("move %q %v", got, e.cursor)
	}
	e.duplicateLines()
	if got := content(e); got != "1\n3\n3\n2" || e.cursor.Line != 2 {
		t.Fatalf("dup %q %v", got, e.cursor)
	}
	e.cursor, e.anchor = Pos{3, 0}, Pos{3, 0}
	e.deleteLines()
	if got := content(e); got != "1\n3\n3" {
		t.Fatalf("delete last %q", got)
	}
}

func TestLinePaste(t *testing.T) {
	e := newTestEditor("a\nb", Pos{1, 1})
	e.lineClip = "a\n"
	e.paste("a\n")
	if got := content(e); got != "a\na\nb" || e.cursor != (Pos{2, 1}) {
		t.Fatalf("got %q %v", got, e.cursor)
	}
}

func TestVerticalGoal(t *testing.T) {
	e := newTestEditor("abcdef\nab\nabcdef", Pos{0, 5})
	e.vertical(1, false)
	e.vertical(1, false)
	if e.cursor != (Pos{2, 5}) {
		t.Fatalf("cursor %v", e.cursor)
	}
}

func TestTextHelpers(t *testing.T) {
	if v := visCol("\tab\tc", 4); v != 8 {
		t.Errorf("visCol %d", v)
	}
	if c := colAt("\tab", 4.2); c != 1 {
		t.Errorf("colAt %d", c)
	}
	if s := displaySlice("\tab\tc", 2, 9); s != "  ab  c" {
		t.Errorf("displaySlice %q", s)
	}
	if w := wordEnd("  foo.bar", 0); w != 5 {
		t.Errorf("wordEnd %d", w)
	}
	if w := wordStart("foo.bar  ", 9); w != 4 {
		t.Errorf("wordStart %d", w)
	}
	if s, e := wordAt("x := hello_w;", 7); s != 5 || e != 12 {
		t.Errorf("wordAt %d %d", s, e)
	}
	if got := completePath("go.m"); got != "go.mod" {
		t.Errorf("completePath %q", got)
	}
}

func BenchmarkLoadBig(b *testing.B) {
	path := os.Getenv("QUOD_FILE")
	if path == "" {
		b.Skip("set QUOD_FILE")
	}
	for b.Loop() {
		if _, err := Load(path); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTypeInBig(b *testing.B) {
	path := os.Getenv("QUOD_FILE")
	if path == "" {
		b.Skip("set QUOD_FILE")
	}
	buf, _ := Load(path)
	e := &Editor{doc: newDoc("", buf)}
	e.cursor = Pos{buf.LineCount() / 2, 0}
	e.anchor = e.cursor
	for b.Loop() {
		e.insert("x")
		e.newline()
	}
}

func TestScrollbar(t *testing.T) {
	// 3M lines: the bottom is over 100M px down, past float32 precision.
	e := newTestEditor(strings.Repeat("x\n", 3_000_000), Pos{})
	e.lineH, e.charW, e.viewH = 40, 10, 800
	e.bar.trackH = 800
	_, size, ok := e.thumb(800)
	if !ok || size != e.lineH {
		t.Fatalf("thumb size %d, want the minimum %d", size, e.lineH)
	}
	e.bar.thumbH = size

	// Clicking the middle of the track jumps to the middle of the file.
	e.barEvent(pointer.Event{Kind: pointer.Press, Position: f32.Pt(0, 400)})
	e.barEvent(pointer.Event{Kind: pointer.Release})
	if mid := e.maxScroll() / 2; math.Abs(e.scrollY-mid) > mid/100 {
		t.Fatalf("scrollY %.0f, want about %.0f", e.scrollY, mid)
	}
	// Dragging past the end stops at the last line.
	e.barEvent(pointer.Event{Kind: pointer.Press, Position: f32.Pt(0, 400)})
	e.barEvent(pointer.Event{Kind: pointer.Drag, Position: f32.Pt(0, 5000)})
	if e.scrollY != e.maxScroll() {
		t.Fatalf("scrollY %.0f, want %.0f", e.scrollY, e.maxScroll())
	}
	// One wheel step near the bottom still moves, and maps to the right line.
	e.scrollY -= 41
	before := e.scrollY
	e.pointer(layout.Context{}, pointer.Event{Kind: pointer.Scroll, Scroll: f32.Pt(0, 1)})
	if e.scrollY != before+1 {
		t.Fatalf("wheel step lost: %v -> %v", before, e.scrollY)
	}
	if got, want := e.posAt(f32.Pt(0, 0)).Line, int(e.scrollY)/e.lineH; got != want {
		t.Fatalf("top line %d, want %d", got, want)
	}
}

func TestDoubleShift(t *testing.T) {
	e := newTestEditor("x", Pos{})
	e.root, e.docs = t.TempDir(), []*doc{e.doc}
	now := time.Now()
	tap := func(ms int) {
		gtx := layout.Context{Now: now.Add(time.Duration(ms) * time.Millisecond)}
		e.shiftKey(gtx, key.Event{Name: key.NameShift, State: key.Press})
		e.shiftKey(gtx, key.Event{Name: key.NameShift, State: key.Release})
	}
	tap(0)
	tap(500) // too slow: starts over
	if e.prompt != promptNone {
		t.Fatal("slow taps opened the finder")
	}
	tap(700)
	if e.prompt != promptFiles {
		t.Fatal("double Shift did not open the finder")
	}
	tap(2000)
	tap(2200)
	if e.prompt != promptNone {
		t.Fatal("double Shift did not close the finder")
	}
	// Shift held for a capital letter is not a tap.
	gtx := layout.Context{Now: now.Add(3 * time.Second)}
	e.shiftKey(gtx, key.Event{Name: key.NameShift, State: key.Press})
	e.shiftClean = false // what an EditEvent does
	e.shiftKey(gtx, key.Event{Name: key.NameShift, State: key.Release})
	tap(3100)
	if e.prompt != promptNone {
		t.Fatal("Shift+letter counted as a tap")
	}
}

func TestReplacePrompt(t *testing.T) {
	e := newTestEditor("foo bar\nFoo foo\n", Pos{})
	e.openReplace()
	e.setInput("foo")
	e.submitPrompt()
	if e.prompt != promptWith {
		t.Fatal("Enter did not ask for the replacement")
	}
	e.setInput("baz")
	e.submitPrompt() // replaces the first match, selects the next
	if got := content(e); got != "baz bar\nFoo foo\n" {
		t.Fatalf("got %q", got)
	}
	e.key(layout.Context{}, key.Event{Name: key.NameDownArrow}, false) // skip one
	e.key(layout.Context{}, key.Event{Name: key.NameReturn, Modifiers: key.ModShortcut}, false)
	if got := content(e); got != "baz bar\nbaz baz\n" || e.prompt != promptNone {
		t.Fatalf("replace all gave %q, prompt %v", got, e.prompt)
	}
	e.undo(false)
	if got := content(e); got != "baz bar\nFoo foo\n" {
		t.Fatalf("undo gave %q", got)
	}
}
