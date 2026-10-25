package main

import (
	"math/rand/v2"
	"os"
	"strings"
	"testing"
)

// naiveFind is the plain line-by-line search Find must agree with.
func naiveFind(b *Buffer, q string, from Pos, backward bool) (Pos, bool) {
	index := matcher(q)
	n := b.LineCount()
	for i := 0; i <= n; i++ {
		li := (from.Line + i) % n
		if backward {
			li = ((from.Line-i)%n + n) % n
		}
		s := b.Line(li)
		if backward {
			lim := len(s)
			if i == 0 {
				lim = from.Col
			}
			last := -1
			for off := 0; ; {
				j := index(s[off:lim])
				if j < 0 {
					break
				}
				last = off + j
				off = last + 1
			}
			if last >= 0 {
				return Pos{li, last}, true
			}
		} else {
			off := 0
			if i == 0 {
				off = from.Col
			}
			if j := index(s[off:]); j >= 0 {
				return Pos{li, off + j}, true
			}
		}
	}
	return Pos{}, false
}

func TestFindMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	words := []string{"alpha", "Beta", "gamma", "delta", "x", "", "needle", "NeedLe", "zz"}
	for _, eol := range []string{"\n", "\r\n"} {
		lines := make([]string, 60000)
		for i := range lines {
			var sb strings.Builder
			for range rng.IntN(6) {
				sb.WriteString(words[rng.IntN(len(words))])
				sb.WriteByte(' ')
			}
			lines[i] = sb.String()
		}
		b := NewBuffer(strings.Join(lines, eol))
		// Edit a few places so some blocks are materialized.
		for range 20 {
			l := rng.IntN(b.LineCount())
			b.Replace(Pos{l, 0}, Pos{l, 0}, "edit needle\nmore ", Pos{})
		}
		for range 400 {
			q := []string{"needle", "NEEDLE", "eta", "zz", "a", "nomatch", "ma de"}[rng.IntN(7)]
			l := rng.IntN(b.LineCount())
			from := Pos{l, rng.IntN(len(b.Line(l)) + 1)}
			back := rng.IntN(2) == 0
			got, gok := b.Find(q, from, back)
			want, wok := naiveFind(b, q, from, back)
			if got != want || gok != wok {
				t.Fatalf("eol %q: Find(%q, %v, back=%v) = %v %v, want %v %v", eol, q, from, back, got, gok, want, wok)
			}
		}
	}
}

func TestIndexRare(t *testing.T) {
	for _, c := range []struct {
		s, q string
		fold bool
		want int
	}{
		{"the quick brown fox", "quick", false, 4},
		{"the QUICK brown fox", "quick", true, 4},
		{"the QUICK brown fox", "quick", false, -1},
		{"zzzq", "zq", false, 2},
		{"q", "zq", false, -1},
		{"abcabd", "abd", true, 3},
		{"some lines of text here", "lines", false, 5},
		{"SOME LINES of text", "lines", true, 5},
		{"tenet tenets", "tenets", false, 6},
		{"aaaa", "aaaaa", false, -1},
	} {
		if got := indexRare(c.s, c.q, c.fold); got != c.want {
			t.Errorf("indexRare(%q, %q, %v) = %d, want %d", c.s, c.q, c.fold, got, c.want)
		}
	}
}

// Saving writes unedited runs straight from the file; the result must equal
// joining the lines.
func TestSaveMatchesJoin(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	out := t.TempDir() + "/out"
	for _, eol := range []string{"\n", "\r\n"} {
		for _, trailing := range []bool{false, true} {
			lines := make([]string, 5000)
			for i := range lines {
				lines[i] = strings.Repeat("ab", rng.IntN(4))
			}
			text := strings.Join(lines, eol)
			if trailing {
				text += eol
			}
			b := NewBuffer(text)
			for step := range 300 {
				n := b.LineCount()
				switch rng.IntN(4) {
				case 0: // delete a tail of lines
					l := rng.IntN(n)
					if l > 0 {
						b.Replace(Pos{l - 1, len(b.Line(l - 1))}, b.End(), "", Pos{})
					}
				case 1: // insert lines
					l := rng.IntN(n)
					b.Replace(Pos{l, 0}, Pos{l, 0}, "x\ny\n", Pos{})
				case 2: // edit within a line
					l := rng.IntN(n)
					b.Replace(Pos{l, 0}, Pos{l, 0}, "e", Pos{})
				case 3: // delete a span
					a := rng.IntN(n)
					z := min(n-1, a+rng.IntN(3000))
					b.Replace(Pos{a, 0}, Pos{z, 0}, "", Pos{})
				}
				if step%10 != 0 {
					continue
				}
				if err := b.Save(out); err != nil {
					t.Fatal(err)
				}
				got, _ := os.ReadFile(out)
				var want []string
				for i := range b.LineCount() {
					want = append(want, b.Line(i))
				}
				if string(got) != strings.Join(want, eol) {
					t.Fatalf("eol %q trailing %v step %d: saved %d bytes, want %d", eol, trailing, step, len(got), len(strings.Join(want, eol)))
				}
			}
		}
	}
}
