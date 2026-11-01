package main

import "testing"

func TestBracketPair(t *testing.T) {
	cases := []struct {
		text string
		cur  Pos
		a, b Pos
		ok   bool
	}{
		{"f(x)", Pos{0, 1}, Pos{0, 1}, Pos{0, 3}, true},   // before (
		{"f(x)", Pos{0, 2}, Pos{0, 1}, Pos{0, 3}, true},   // after (
		{"f(x)", Pos{0, 4}, Pos{0, 3}, Pos{0, 1}, true},   // after )
		{"f(x)", Pos{0, 0}, Pos{}, Pos{}, false},          // on f
		{"f(x)(", Pos{0, 4}, Pos{0, 3}, Pos{0, 1}, true},  // before wins
		{"a[(b)]", Pos{0, 1}, Pos{0, 1}, Pos{0, 5}, true}, // nested, other kinds ignored
		{"{\n\t{\n\t}\n}", Pos{3, 1}, Pos{3, 0}, Pos{0, 0}, true},
		{"{\n\t{\n\t}\n}", Pos{0, 0}, Pos{0, 0}, Pos{3, 0}, true},
		{"(((", Pos{0, 3}, Pos{}, Pos{}, false}, // unmatched
		{"x)", Pos{0, 2}, Pos{}, Pos{}, false},
	}
	for _, c := range cases {
		e := newTestEditor(c.text, c.cur)
		a, b, ok := e.bracketPair(0, e.buf.LineCount()-1)
		if ok != c.ok || a != c.a || b != c.b {
			t.Errorf("%q at %v: got %v %v %v, want %v %v %v", c.text, c.cur, a, b, ok, c.a, c.b, c.ok)
		}
	}
	// A partner beyond reach is not found, and the scan stays bounded.
	e := newTestEditor("("+repeatLines("x", bracketReach+10)+")", Pos{0, 1})
	if _, _, ok := e.bracketPair(0, 0); ok {
		t.Error("found a partner beyond bracketReach")
	}
	e = newTestEditor("("+repeatLines("x", 100)+")", Pos{0, 1})
	if _, b, ok := e.bracketPair(0, 0); !ok || b != (Pos{101, 0}) {
		t.Errorf("partner 100 lines down: got %v %v", b, ok)
	}
}

func repeatLines(s string, n int) string {
	out := ""
	for range n {
		out += "\n" + s
	}
	return out + "\n"
}
