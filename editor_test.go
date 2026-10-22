package main

import (
	"os"
	"testing"

	"gioui.org/io/key"
)

func newTestEditor(text string, cur Pos) *Editor {
	e := &Editor{buf: NewBuffer(text), goal: -1}
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
	e := &Editor{buf: buf, goal: -1}
	e.cursor = Pos{buf.LineCount() / 2, 0}
	e.anchor = e.cursor
	for b.Loop() {
		e.insert("x")
		e.newline()
	}
}
