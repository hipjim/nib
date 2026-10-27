package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestLineStoreRandom applies random edits to a lineStore and a plain slice
// and checks that they agree.
func TestLineStoreRandom(t *testing.T) {
	var ref []string
	for i := range 5000 {
		ref = append(ref, fmt.Sprintf("line %d", i))
	}
	var s lineStore
	s.init(strings.Join(ref, "\n"))
	r := rand.New(rand.NewPCG(1, 2))
	for step := range 3000 {
		first := r.IntN(len(ref))
		last := min(len(ref)-1, first+r.IntN(4)*r.IntN(800))
		repl := make([]string, 1+r.IntN(3)*r.IntN(1500))
		for i := range repl {
			repl[i] = fmt.Sprintf("s%d.%d", step, i)
		}
		ref = slices.Replace(ref, first, last+1, repl...)
		s.replace(first, last, repl)
		if s.count != len(ref) {
			t.Fatalf("step %d: count %d want %d", step, s.count, len(ref))
		}
		for _, i := range []int{0, first, len(ref) - 1, r.IntN(len(ref))} {
			if got := s.line(i); got != ref[i] {
				t.Fatalf("step %d: line %d = %q want %q", step, i, got, ref[i])
			}
		}
	}
	for i := range ref {
		if s.line(i) != ref[i] {
			t.Fatalf("final line %d = %q want %q", i, s.line(i), ref[i])
		}
	}
}

func TestLoadLargeAsync(t *testing.T) {
	var sb strings.Builder
	for i := 0; sb.Len() < mmapMin+(4<<20); i++ {
		fmt.Fprintf(&sb, "row %d with some padding text\r\n", i)
	}
	sb.WriteString("last")
	want := strings.Split(strings.ReplaceAll(sb.String(), "\r\n", "\n"), "\n")
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Loading() || b.LineCount() == 0 || b.LineCount() >= len(want) {
		t.Fatalf("expected a partial first batch, got %d of %d lines (loading=%v)", b.LineCount(), len(want), b.Loading())
	}
	for b.Poll() {
	}
	if b.LineCount() != len(want) || !b.CRLF {
		t.Fatalf("lines %d want %d, crlf %v", b.LineCount(), len(want), b.CRLF)
	}
	for _, i := range []int{0, 1, len(want) / 2, len(want) - 1} {
		if b.Line(i) != want[i] {
			t.Fatalf("line %d = %q want %q", i, b.Line(i), want[i])
		}
	}
	b.Replace(Pos{len(want) - 1, 0}, Pos{len(want) - 1, 0}, "very ", Pos{})
	if err := b.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(data), "padding text\r\nvery last") || len(data) != sb.Len()+5 {
		t.Fatalf("saved file wrong: len %d, tail %q", len(data), data[len(data)-30:])
	}
}

func TestIndexSpanLimit(t *testing.T) {
	defer func(old int) { maxSpan = old }(maxSpan)
	maxSpan = 20 // force blocks to split on byte span, not line count
	text := "short\na much longer line here\n\nx\nanother fairly long line\nend"
	var s lineStore
	s.init(text)
	want := strings.Split(text, "\n")
	if s.count != len(want) {
		t.Fatalf("count %d want %d", s.count, len(want))
	}
	if len(s.blocks) < 3 {
		t.Fatalf("expected span splits, got %d blocks", len(s.blocks))
	}
	for i, w := range want {
		if got := s.line(i); got != w {
			t.Errorf("line %d = %q want %q", i, got, w)
		}
	}
}

// TestIndexParallel checks that indexing in pieces on all cores finds the
// same lines as splitting the text, whatever the text looks like at the
// cuts.
func TestIndexParallel(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	random := func(n int, nl string) string {
		var sb strings.Builder
		for sb.Len() < n {
			if r.IntN(40) == 0 {
				sb.WriteString(nl)
			} else {
				sb.WriteByte('a' + byte(r.IntN(26)))
			}
		}
		return sb.String()
	}
	texts := map[string]string{
		"lines":           random(asyncMin+12345, "\n"),
		"trailing":        random(asyncMin, "\n") + "\n",
		"empty lines":     strings.Repeat("\n", asyncMin+7),
		"one line":        strings.Repeat("x", asyncMin+1),
		"few lines":       strings.Repeat("x", asyncMin) + "\n" + strings.Repeat("y", 100) + "\nz",
		"crlf":            random(asyncMin+99, "\r\n"),
		"long and spread": random(asyncMin, "\n") + strings.Repeat("q", asyncMin) + "\n",
	}
	for name, text := range texts {
		for _, span := range []int{maxSpan, 1 << 16} {
			func() {
				defer func(old int) { maxSpan = old }(maxSpan)
				maxSpan = span
				var s lineStore
				s.init(text)
				for s.poll() {
				}
				want := strings.Split(text, "\n")
				if s.crlf {
					want = strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
				}
				if s.count != len(want) {
					t.Fatalf("%s, span %d: %d lines, want %d", name, span, s.count, len(want))
				}
				for i, w := range want {
					if got := s.line(i); got != w {
						t.Fatalf("%s, span %d: line %d = %.20q…, want %.20q…", name, span, i, got, w)
					}
				}
				if p := s.progressPct(); p != 100 {
					t.Errorf("%s: progress %d%%", name, p)
				}
			}()
		}
	}
}
