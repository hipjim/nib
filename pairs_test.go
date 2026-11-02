package main

import "testing"

func TestTypeTextPairs(t *testing.T) {
	cases := []struct {
		text  string
		cur   Pos
		typed string
		want  string
		after Pos
	}{
		{"", Pos{}, "(", "()", Pos{0, 1}},
		{"f", Pos{0, 1}, "(", "f()", Pos{0, 2}},
		{"()", Pos{0, 1}, ")", "()", Pos{0, 2}},       // skip over the closer
		{"x = ", Pos{0, 4}, `"`, `x = ""`, Pos{0, 5}}, // a quote pairs
		{`""`, Pos{0, 1}, `"`, `""`, Pos{0, 2}},       // and is skipped over
		{"don", Pos{0, 3}, "'", "don'", Pos{0, 4}},    // not after a word
		{"ab", Pos{0, 1}, "(", "a(b", Pos{0, 2}},      // not before text
		{"f()", Pos{0, 2}, "[", "f([])", Pos{0, 3}},   // before a closer
		{"x", Pos{0, 1}, "y", "xy", Pos{0, 2}},
	}
	for _, c := range cases {
		e := newTestEditor(c.text, c.cur)
		e.typeText(c.typed)
		if got := content(e); got != c.want || e.cursor != c.after {
			t.Errorf("%q + %q: got %q at %v, want %q at %v", c.text, c.typed, got, e.cursor, c.want, c.after)
		}
	}
}

func TestTypeTextWrapsSelection(t *testing.T) {
	e := newTestEditor("say hello now", Pos{0, 9})
	e.anchor = Pos{0, 4}
	e.typeText("(")
	if got := content(e); got != "say (hello) now" || e.anchor != (Pos{0, 5}) || e.cursor != (Pos{0, 10}) {
		t.Fatalf("got %q, selection %v-%v", got, e.anchor, e.cursor)
	}
	e.typeText("x") // typing replaces the still-selected word
	if got := content(e); got != "say (x) now" {
		t.Fatalf("got %q", got)
	}
}

func TestBackspaceDeletesPair(t *testing.T) {
	e := newTestEditor("f()", Pos{0, 2})
	e.backspace(0)
	if got := content(e); got != "f" || e.cursor != (Pos{0, 1}) {
		t.Fatalf("got %q at %v", got, e.cursor)
	}
	e = newTestEditor("f(x)", Pos{0, 2})
	e.backspace(0)
	if got := content(e); got != "fx)" {
		t.Fatalf("got %q", got)
	}
}
