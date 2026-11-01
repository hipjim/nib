package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// TestCountMatches compares the parallel count with a naive one, with the
// cursor anywhere, on a buffer spanning several search chunks.
func TestCountMatches(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	var sb strings.Builder
	n := 3*searchChunk + 17
	for i := 0; i < n; i++ {
		for range rng.Intn(12) {
			sb.WriteString([]string{"the", "The", "then", " ", "x", "thethe"}[rng.Intn(6)])
		}
		if i < n-1 {
			sb.WriteByte('\n')
		}
	}
	b := NewBuffer(sb.String())
	b.Replace(Pos{5, 0}, Pos{5, len(b.Line(5))}, "the edited line with the and THE", Pos{}) // one materialized block
	for _, q := range []string{"the", "The", "thethe", "zq"} {
		index := matcher(q)
		for range 20 {
			at := Pos{rng.Intn(n), rng.Intn(8)}
			at = b.Clamp(at)
			total, before := 0, 0
			for i := 0; i < n; i++ {
				s := b.Line(i)
				for o := 0; ; {
					m := index(s[o:])
					if m < 0 {
						break
					}
					total++
					if (Pos{i, o + m}).Less(at) {
						before++
					}
					o += m + len(q)
				}
			}
			gt, gb, ok := b.CountMatches(q, at, time.Minute)
			if !ok || gt != total || gb != before {
				t.Fatalf("%q at %v: got %d %d %v, want %d %d", q, at, gt, gb, ok, total, before)
			}
		}
	}
	if _, _, ok := b.CountMatches("the", Pos{}, 0); ok {
		t.Error("a zero budget reported a complete count")
	}
	if tot, _, ok := b.CountMatches("", Pos{}, time.Minute); tot != 0 || !ok {
		t.Error("empty query")
	}
}

func TestMatchCountText(t *testing.T) {
	e := newTestEditor("the cat\nThe hat\nthe end", Pos{})
	if e.matchCountText() != "" {
		t.Fatal("text without a find prompt")
	}
	e.openPrompt(promptFind, "")
	e.setInput("the")
	if got := e.matchCountText(); got != "1 of 3" {
		t.Fatalf("got %q", got)
	}
	e.findNext(false)
	if got := e.matchCountText(); got != "2 of 3" {
		t.Fatalf("after next: got %q", got)
	}
	e.setInput("The")
	if got := e.matchCountText(); got != "1 of 1" {
		t.Fatalf("exact case: got %q", got)
	}
	e.setInput("dog")
	if got := e.matchCountText(); got != "no matches" {
		t.Fatalf("missing: got %q", got)
	}
	e.setInput("the")
	e.cursor, e.anchor = Pos{0, 1}, Pos{0, 1} // no selection
	if got := e.matchCountText(); got != "3 matches" {
		t.Fatalf("no selection: got %q", got)
	}
	if rows := keyRows(); len(rows) < 20 || rows[0][0] == "" {
		t.Fatal("key rows")
	}
}
