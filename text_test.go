package main

import (
	"math/rand/v2"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestColIndex checks the columns of long lines against counting from the
// line start, through random edits, undo and redo.
func TestColIndex(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	pieces := []string{"a", "bc", "\t", " ", "é", "日本", "\xff", "\n"}
	gen := func(n int) string {
		var sb strings.Builder
		for sb.Len() < n {
			p := pieces[r.IntN(len(pieces))]
			if p == "\n" && r.IntN(1000) > 0 {
				continue
			}
			sb.WriteString(p)
		}
		return sb.String()
	}
	b := NewBuffer(gen(200_000))
	runeStart := func(s string, col int) int {
		for col > 0 && col < len(s) && !utf8.RuneStart(s[col]) {
			col--
		}
		return col
	}
	long := 0
	for i := range b.LineCount() {
		if len(b.Line(i)) >= longLine {
			long++
		}
	}
	if long < 3 {
		t.Fatalf("only %d long lines", long)
	}
	for step := range 1500 {
		l := r.IntN(b.LineCount())
		s := b.Line(l)
		col := runeStart(s, r.IntN(len(s)+1))
		if v, want := b.VisCol(l, col), visCol(s, col); v != want {
			t.Fatalf("step %d: VisCol(%d, %d) = %d, want %d", step, l, col, v, want)
		}
		x := float32(r.IntN(visCol(s, len(s))+8)) + r.Float32()
		if c, want := b.ColAt(l, x), colAt(s, x); c != want {
			t.Fatalf("step %d: ColAt(%d, %v) = %d, want %d", step, l, x, c, want)
		}
		v0 := r.IntN(visCol(s, len(s)) + 4)
		if got, want := b.Visible(l, v0, v0+100), displaySlice(s, v0, v0+100); got != want {
			t.Fatalf("step %d: Visible(%d, %d) = %q, want %q", step, l, v0, got, want)
		}
		switch r.IntN(10) {
		case 0:
			b.Undo()
		case 1:
			b.Redo()
		case 2:
			b.ReplaceAll("a", "\t", Pos{})
		default:
			end := Pos{l, runeStart(s, min(len(s), col+r.IntN(8)))}
			if r.IntN(4) == 0 && l+1 < b.LineCount() {
				end = Pos{l + 1, 0}
			}
			b.Replace(Pos{l, col}, end, gen(r.IntN(6)), Pos{})
		}
	}
}

func BenchmarkVisColLong(b *testing.B) {
	s := strings.Repeat(`{"id":1,"name":"item"},`, 1<<20/23)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		visCol(s, len(s))
	}
}
