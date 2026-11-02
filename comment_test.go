package main

import (
	"strings"
	"testing"
)

func TestToggleComments(t *testing.T) {
	goLang, html := langs[".go"], langs[".html"]
	cases := []struct {
		l        *lang
		in, want string
	}{
		{goLang, "a\n\tb\n\n\tc", "// a\n// \tb\n\n// \tc"}, // at the common indent
		{goLang, "\tx := 1\n\ty := 2", "\t// x := 1\n\t// y := 2"},
		{goLang, "// a\n\t// b\n\n//c", "a\n\tb\n\nc"},
		{goLang, "// a\nb", "// // a\n// b"}, // not all commented: comment again
		{goLang, "", "// "},
		{html, "<p>a</p>\n  <br>", "<!-- <p>a</p> -->\n<!--   <br> -->"},
		{html, "<!-- <p>a</p> -->\n  <!--<br>-->", "<p>a</p>\n  <br>"},
	}
	for _, c := range cases {
		got, _ := toggleComments(strings.Split(c.in, "\n"), c.l)
		if s := strings.Join(got, "\n"); s != c.want {
			t.Errorf("%q: got %q, want %q", c.in, s, c.want)
		}
	}
}

func TestToggleCommentKeepsTheCursor(t *testing.T) {
	e := newTestEditor("\tfoo()\n\tbar()\n", Pos{1, 4})
	e.hl = newHighlighter("x.go")
	e.anchor = Pos{0, 2}
	e.toggleComment()
	if got := content(e); got != "\t// foo()\n\t// bar()\n" {
		t.Fatalf("got %q", got)
	}
	if e.anchor != (Pos{0, 5}) || e.cursor != (Pos{1, 7}) {
		t.Fatalf("anchor %v cursor %v", e.anchor, e.cursor)
	}
	e.toggleComment()
	if got := content(e); got != "\tfoo()\n\tbar()\n" || e.anchor != (Pos{0, 2}) || e.cursor != (Pos{1, 4}) {
		t.Fatalf("second toggle: %q, anchor %v cursor %v", got, e.anchor, e.cursor)
	}
	e.moveTo(Pos{0, 0}, false) // the cursor before the marker stays
	e.toggleComment()
	if e.cursor != (Pos{0, 0}) {
		t.Fatalf("cursor moved to %v", e.cursor)
	}
	e.undo(false)
	if got := content(e); got != "\tfoo()\n\tbar()\n" {
		t.Fatalf("undo got %q", got)
	}
}

func TestToggleCommentWithoutSyntax(t *testing.T) {
	e := newTestEditor("hello", Pos{})
	e.hl = newHighlighter("notes.txt")
	e.toggleComment()
	if content(e) != "hello" || e.msg == "" {
		t.Fatalf("content %q, msg %q", content(e), e.msg)
	}
}
